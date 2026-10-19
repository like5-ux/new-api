package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// callLogHandler runs a log handler as the given user and returns the JSON
// lines of the response.
func callLogHandler(t *testing.T, handler gin.HandlerFunc, userId int, role int, query string) []map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log?"+query, nil)
	ctx.Set("id", userId)
	ctx.Set("role", role)
	handler(ctx)
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(recorder.Body.String()), "\n") {
		var value map[string]any
		require.NoError(t, common.UnmarshalJsonStr(line, &value))
		lines = append(lines, value)
	}
	return lines
}

func exportedRequestIds(lines []map[string]any) []any {
	ids := make([]any, 0, len(lines))
	for _, line := range lines[:len(lines)-1] {
		ids = append(ids, line["request_id"])
	}
	return ids
}

// seedExportLogs stores logs of two users: three in the last hour and one
// from 400 days ago.
func seedExportLogs(t *testing.T, mainDB *gorm.DB, logDB *gorm.DB, now int64) {
	t.Helper()
	require.NoError(t, mainDB.Create(&model.Channel{Id: 9, Name: "primary"}).Error)
	logs := []model.Log{
		{UserId: 1, Username: "alice", CreatedAt: now - 3600, Type: model.LogTypeConsume, ModelName: "gpt-a", ChannelId: 9, Quota: 10, RequestId: "a-1", Other: `{"admin_info":{"use_channel":["9"]}}`},
		{UserId: 2, Username: "bob", CreatedAt: now - 1800, Type: model.LogTypeConsume, ModelName: "gpt-b", ChannelId: 9, Quota: 20, RequestId: "b-1"},
		{UserId: 1, Username: "alice", CreatedAt: now - 60, Type: model.LogTypeError, ModelName: "gpt-a", RequestId: "a-2"},
		{UserId: 1, Username: "alice", CreatedAt: now - 400*24*3600, Type: model.LogTypeConsume, ModelName: "gpt-a", RequestId: "a-old"},
	}
	for i := range logs {
		require.NoError(t, logDB.Create(&logs[i]).Error)
	}
}

func useLogExportDatabases(t *testing.T, mainDB *gorm.DB, logDB *gorm.DB, mainType common.DatabaseType, logType common.DatabaseType) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(mainType, logType)
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
}

func TestLogExportDatabaseMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	for _, tc := range []struct {
		kind, env string
		typ       common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
		{"clickhouse", "AUDIT_CLICKHOUSE_DSN", common.DatabaseTypeClickHouse},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if tc.env != "" && dsn == "" {
				t.Skip(tc.env + " is not configured")
			}
			db, isolatedDSN := newAuditTestDatabase(t, tc.kind, dsn)
			if tc.kind == "sqlite" {
				sqlDB, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			}
			mainDB, mainType := db, tc.typ
			if tc.kind == "clickhouse" {
				mainDB, _ = newAuditTestDatabase(t, "sqlite", "")
				mainType = common.DatabaseTypeSQLite
			}
			useLogExportDatabases(t, mainDB, db, mainType, tc.typ)
			require.NoError(t, mainDB.AutoMigrate(&model.Channel{}))
			if tc.kind == "clickhouse" {
				t.Setenv("LOG_SQL_DSN", isolatedDSN)
				require.NoError(t, model.InitLogDB())
			} else {
				require.NoError(t, db.AutoMigrate(&model.Log{}))
			}
			versionSQL := "SELECT version()"
			if tc.kind == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, model.LOG_DB.Raw(versionSQL).Scan(&version).Error)
			t.Logf("log database: %s %s", tc.kind, version)
			now := time.Now().Unix()
			seedExportLogs(t, mainDB, model.LOG_DB, now)
			query := fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", now-2*24*3600, now+3600)

			listed := callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, query)[0]
			require.Equal(t, true, listed["success"])
			assert.EqualValues(t, 2, listed["data"].(map[string]any)["total"])
			listed = callLogHandler(t, GetAllLogs, 3, common.RoleAdminUser, query+"&username=alice")[0]
			require.Equal(t, true, listed["success"])
			assert.EqualValues(t, 2, listed["data"].(map[string]any)["total"])

			own := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, query)
			require.Len(t, own, 3)
			assert.Equal(t, []any{"a-2", "a-1"}, exportedRequestIds(own))
			assert.Equal(t, map[string]any{"success": true, "message": "", "data": map[string]any{"count": float64(2)}}, own[2])
			assert.Equal(t, float64(1), own[0]["id"], "user exports carry list positions, not row ids")
			assert.Empty(t, own[1]["channel_name"])
			assert.NotContains(t, own[1]["other"], "admin_info")

			all := callLogHandler(t, ExportAllLogs, 3, common.RoleAdminUser, query+"&model_name=gpt-%25")
			require.Len(t, all, 4)
			assert.Equal(t, []any{"a-2", "b-1", "a-1"}, exportedRequestIds(all))
			assert.Equal(t, "primary", all[2]["channel_name"])
			assert.Contains(t, all[2]["other"], "admin_info")
			assert.Equal(t, true, all[3]["success"])
		})
	}
}

func TestLogExportRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Log{}, &model.QuotaData{}))
	now := time.Now().Unix()
	seedExportLogs(t, db, db, now)
	logSetting := operation_setting.GetLogSetting()
	previousSetting := *logSetting
	t.Cleanup(func() { *logSetting = previousSetting })
	recent := fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", now-2*24*3600, now)
	tooLong := fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", now-366*24*3600, now)

	t.Run("time range", func(t *testing.T) {
		rejected := callLogHandler(t, ExportAllLogs, 3, common.RoleAdminUser, tooLong)
		require.Len(t, rejected, 1)
		assert.Equal(t, false, rejected[0]["success"])
		assert.Equal(t, "The time range cannot exceed {{days}} days", rejected[0]["message_key"])
		assert.Equal(t, map[string]any{"days": float64(365)}, rejected[0]["message_params"])

		assert.Equal(t, false, callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, tooLong)[0]["success"])
		assert.Equal(t, false, callLogHandler(t, GetLogsSelfStat, 1, common.RoleCommonUser, tooLong)[0]["success"])
		assert.Equal(t, true, callLogHandler(t, GetUserLogs, 3, common.RoleAdminUser, tooLong)[0]["success"], "administrators search their own logs without a limit")

		// The dashboard follows the same range instead of its old one-month limit.
		fortyDays := fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", now-40*24*3600, now)
		for _, handler := range []gin.HandlerFunc{GetUserQuotaDates, GetUserFlowQuotaDates} {
			assert.Equal(t, "The time range cannot exceed {{days}} days", callLogHandler(t, handler, 1, common.RoleCommonUser, tooLong)[0]["message_key"])
			assert.Equal(t, true, callLogHandler(t, handler, 1, common.RoleCommonUser, fortyDays)[0]["success"])
		}

		// Without a start, a user's search reaches back the whole range.
		listed := callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, "")[0]
		assert.Equal(t, true, listed["success"])
		assert.EqualValues(t, 2, listed["data"].(map[string]any)["total"])

		logSetting.MaxRangeDays = 400
		assert.Equal(t, true, callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, tooLong)[0]["success"])
		logSetting.MaxRangeDays = previousSetting.MaxRangeDays
	})

	t.Run("user export switch", func(t *testing.T) {
		logSetting.UserExportEnabled = false
		t.Cleanup(func() { logSetting.UserExportEnabled = previousSetting.UserExportEnabled })
		disabled := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, recent)
		require.Len(t, disabled, 1)
		assert.Equal(t, "Log export is disabled", disabled[0]["message_key"])
		assert.Equal(t, true, callLogHandler(t, ExportUserLogs, 3, common.RoleAdminUser, recent)[0]["success"], "administrators export their own logs even when users cannot")
	})

	t.Run("one export per user", func(t *testing.T) {
		release, ok, err := service.AcquireLogExportLock(1)
		require.NoError(t, err)
		require.True(t, ok)
		busy := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, recent)
		require.Len(t, busy, 1)
		assert.Equal(t, "Another log export is running. Try again after it finishes.", busy[0]["message_key"])
		other := callLogHandler(t, ExportUserLogs, 2, common.RoleCommonUser, recent)
		assert.Equal(t, true, other[len(other)-1]["success"], "other users are not blocked")
		release()
		assert.Len(t, callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, recent), 3)
	})
}

func TestAcquireLogExportLock(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprintf("redis=%v", useRedis), func(t *testing.T) {
			previousRedis, previousClient := common.RedisEnabled, common.RDB
			common.RedisEnabled = useRedis
			if useRedis {
				server := miniredis.RunT(t)
				common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
				require.NoError(t, common.RDB.Ping(context.Background()).Err())
			}
			t.Cleanup(func() { common.RedisEnabled, common.RDB = previousRedis, previousClient })

			release, ok, err := service.AcquireLogExportLock(1)
			require.NoError(t, err)
			require.True(t, ok)
			_, ok, err = service.AcquireLogExportLock(1)
			require.NoError(t, err)
			assert.False(t, ok, "a second export of the same user must wait")
			releaseOther, ok, err := service.AcquireLogExportLock(2)
			require.NoError(t, err)
			assert.True(t, ok)
			releaseOther()
			release()
			releaseAgain, ok, err := service.AcquireLogExportLock(1)
			require.NoError(t, err)
			assert.True(t, ok, "a finished export frees the user")
			releaseAgain()
		})
	}
}

func TestUserLogModelDisplayKeepsOriginalNamesWhenUnconfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	logSetting := operation_setting.GetLogSetting()
	previousExportEnabled := logSetting.UserExportEnabled
	logSetting.UserExportEnabled = true
	t.Cleanup(func() { logSetting.UserExportEnabled = previousExportEnabled })

	for _, tc := range []struct {
		name  string
		model string
		other string
	}{
		{
			name:  "mapped request and response",
			model: "public-alias",
			other: `{"is_model_mapped":true,"upstream_model_name":"provider-model","response_model":{"requested_model":"public-alias","upstream_model":"provider-model","returned_model":"provider-response"},"public_id":9007199254740993}`,
		},
		{
			name:  "response request name takes precedence over billing model",
			model: "billing-model",
			other: `{"upstream_model_name":"provider-model","response_model":{"requested_model":"public-alias","upstream_model":"provider-model","returned_model":"provider-response"}}`,
		},
		{
			name:  "historical mapping without response observation",
			model: "public-alias",
			other: `{"is_model_mapped":true,"upstream_model_name":"provider-model"}`,
		},
		{
			name:  "historical response without requested name",
			model: "public-alias",
			other: `{"response_model":{"upstream_model":"provider-model","returned_model":"provider-response"}}`,
		},
		{
			name:  "historical log without requested name stays unchanged",
			other: `{"upstream_model_name":"provider-model","response_model":{"upstream_model":"provider-model","returned_model":"provider-response"}}`,
		},
		{
			name:  "malformed response metadata stays unchanged",
			model: "public-alias",
			other: `{"response_model":"provider-response","public_id":9007199254740993}`,
		},
		{
			name:  "unmapped log stays unchanged",
			model: "public-alias",
			other: `{"public_id":9007199254740993}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := model.Log{UserId: 1, CreatedAt: time.Now().Unix(), Type: model.LogTypeConsume, ModelName: tc.model, TokenId: 7, RequestId: tc.name, Other: tc.other}
			require.NoError(t, db.Create(&log).Error)
			query := "request_id=" + url.QueryEscape(tc.name)

			listed := callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, query)[0]
			require.Equal(t, true, listed["success"])
			items := listed["data"].(map[string]any)["items"].([]any)
			require.Len(t, items, 1)
			assert.JSONEq(t, tc.other, items[0].(map[string]any)["other"].(string))

			exported := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, query)
			require.Len(t, exported, 2)
			assert.JSONEq(t, tc.other, exported[0]["other"].(string))

			byKey, err := model.GetLogByTokenId(7)
			require.NoError(t, err)
			require.NotEmpty(t, byKey)
			assert.JSONEq(t, tc.other, byKey[0].Other)
			if strings.Contains(tc.other, "9007199254740993") {
				assert.Contains(t, byKey[0].Other, "9007199254740993")
			}

			for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
				admin := callLogHandler(t, GetAllLogs, 2, role, query)[0]
				require.Equal(t, true, admin["success"])
				adminItems := admin["data"].(map[string]any)["items"].([]any)
				require.Len(t, adminItems, 1)
				assert.Equal(t, tc.other, adminItems[0].(map[string]any)["other"])
				adminExport := callLogHandler(t, ExportAllLogs, 2, role, query)
				require.Len(t, adminExport, 2)
				assert.Equal(t, tc.other, adminExport[0]["other"])
			}

			var stored model.Log
			require.NoError(t, db.First(&stored, log.Id).Error)
			assert.Equal(t, tc.other, stored.Other)
		})
	}
}

func TestTaskLogModelDisplayKeepsOriginalNamesWhenUnconfigured(t *testing.T) {
	for _, tc := range []struct {
		name       string
		properties model.Properties
		expected   string
	}{
		{name: "mapped model", properties: model.Properties{OriginModelName: "public-alias", UpstreamModelName: "provider-model"}, expected: "provider-model"},
		{name: "no upstream model", properties: model.Properties{OriginModelName: "public-alias"}},
		{name: "historical task without requested name", properties: model.Properties{UpstreamModelName: "provider-model"}, expected: "provider-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{TaskID: "model-display", Properties: tc.properties}
			for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
				view := tasksToDto([]*model.Task{task}, false, role)[0]
				properties, ok := view.Properties.(model.Properties)
				require.True(t, ok)
				assert.Equal(t, tc.properties.OriginModelName, properties.OriginModelName)
				if role < common.RoleAdminUser {
					assert.Equal(t, tc.expected, properties.UpstreamModelName)
				} else {
					assert.Equal(t, tc.properties.UpstreamModelName, properties.UpstreamModelName)
				}
				assert.Equal(t, tc.properties, task.Properties)
			}
		})
	}
}

func TestUserModelDisplayOverrides(t *testing.T) {
	for _, database := range []struct {
		kind, env string
		typ       common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(database.kind, func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured")
			}
			gin.SetMode(gin.TestMode)
			versionSQL := "SELECT version()"
			if database.kind == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			db, _ := newAuditTestDatabase(t, database.kind, dsn)
			useLogExportDatabases(t, db, db, database.typ, database.typ)
			var databaseVersion string
			require.NoError(t, db.Raw(versionSQL).Scan(&databaseVersion).Error)
			t.Logf("model display database: %s %s", database.kind, databaseVersion)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Channel{}))
			invalid := model.Channel{OtherSettings: `{"user_model_mapping":{"alias":42}}`}
			require.Error(t, invalid.ValidateSettings())
			settings := `{"user_model_mapping":{"public-alias":"user-visible-model","another-alias":"another-display","blank-alias":"  "}}`
			channel := model.Channel{Id: 81, Name: "display-test", OtherSettings: settings}
			require.NoError(t, channel.ValidateSettings())
			require.NoError(t, db.Create(&channel).Error)
			channel.SetOtherSettings(channel.GetOtherSettings())
			require.Contains(t, channel.OtherSettings, "user-visible-model", "typed settings round trips must preserve display names")
			logSetting := operation_setting.GetLogSetting()
			previousExportEnabled := logSetting.UserExportEnabled
			logSetting.UserExportEnabled = true
			t.Cleanup(func() { logSetting.UserExportEnabled = previousExportEnabled })

			for _, tc := range []struct {
				name, requested, expected string
				channelID                 int
			}{
				{"configured", "public-alias", "user-visible-model", 81},
				{"different model", "another-alias", "another-display", 81},
				{"unconfigured", "unconfigured-alias", "", 81},
				{"blank name", "blank-alias", "", 81},
				{"deleted channel", "public-alias", "", 9999},
			} {
				t.Run(tc.name, func(t *testing.T) {
					other := model.NewLogOther()
					other.SetPublic("is_model_mapped", true)
					other.SetPublic("upstream_model_name", "provider-model")
					other.SetPublic("response_model", map[string]string{"requested_model": tc.requested, "upstream_model": "provider-model", "returned_model": "provider-response"})
					original := other.JSONString()
					entry := model.Log{UserId: 1, CreatedAt: time.Now().Unix(), Type: model.LogTypeConsume, ModelName: tc.requested, ChannelId: tc.channelID, TokenId: 91, RequestId: "display-" + tc.name, Content: "provider-model failed: provider-response", Other: original}
					require.NoError(t, db.Create(&entry).Error)
					query := "request_id=" + url.QueryEscape(entry.RequestId)
					expectedUpstream, expectedResponse := "provider-model", "provider-response"
					expectedContent := tc.requested + " failed: " + tc.requested
					if tc.expected != "" {
						expectedUpstream, expectedResponse = tc.expected, tc.expected
						expectedContent = tc.expected + " failed: " + tc.expected
					}
					expected := fmt.Sprintf(`{"is_model_mapped":true,"upstream_model_name":%q,"response_model":{"requested_model":%q,"upstream_model":%q,"returned_model":%q}}`, expectedUpstream, tc.requested, expectedUpstream, expectedResponse)
					listed := callLogHandler(t, GetUserLogs, 1, common.RoleCommonUser, query)[0]
					require.Equal(t, true, listed["success"])
					items := listed["data"].(map[string]any)["items"].([]any)
					require.Len(t, items, 1)
					assert.Equal(t, tc.requested, items[0].(map[string]any)["model_name"])
					assert.JSONEq(t, expected, items[0].(map[string]any)["other"].(string))
					assert.Equal(t, expectedContent, items[0].(map[string]any)["content"])
					exported := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, query)
					require.Len(t, exported, 2)
					assert.JSONEq(t, expected, exported[0]["other"].(string))
					assert.Equal(t, expectedContent, exported[0]["content"])
					tokenLogs, err := model.GetLogByTokenId(91)
					require.NoError(t, err)
					require.NotEmpty(t, tokenLogs)
					assert.JSONEq(t, expected, tokenLogs[0].Other)
					assert.Equal(t, expectedContent, tokenLogs[0].Content)
					adminTokenLogs, err := model.GetLogByTokenIdForViewer(91, common.RoleAdminUser)
					require.NoError(t, err)
					require.NotEmpty(t, adminTokenLogs)
					assert.Equal(t, original, adminTokenLogs[0].Other)
					assert.Equal(t, "provider-model failed: provider-response", adminTokenLogs[0].Content)
					adminOwn := callLogHandler(t, GetUserLogs, 1, common.RoleAdminUser, query)[0]
					adminOwnItems := adminOwn["data"].(map[string]any)["items"].([]any)
					require.Len(t, adminOwnItems, 1)
					assert.Equal(t, original, adminOwnItems[0].(map[string]any)["other"])
					assert.Equal(t, "provider-model failed: provider-response", adminOwnItems[0].(map[string]any)["content"])
					adminOwnExport := callLogHandler(t, ExportUserLogs, 1, common.RoleAdminUser, query)
					require.Len(t, adminOwnExport, 2)
					assert.Equal(t, original, adminOwnExport[0]["other"])
					assert.Equal(t, "provider-model failed: provider-response", adminOwnExport[0]["content"])
					for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
						admin := callLogHandler(t, GetAllLogs, 2, role, query)[0]
						adminItems := admin["data"].(map[string]any)["items"].([]any)
						require.Len(t, adminItems, 1)
						assert.Equal(t, original, adminItems[0].(map[string]any)["other"])
						assert.Equal(t, "provider-model failed: provider-response", adminItems[0].(map[string]any)["content"])
						adminExport := callLogHandler(t, ExportAllLogs, 2, role, query)
						assert.Equal(t, original, adminExport[0]["other"])
						assert.Equal(t, "provider-model failed: provider-response", adminExport[0]["content"])
					}
					task := &model.Task{TaskID: "display-task", ChannelId: tc.channelID, Properties: model.Properties{OriginModelName: tc.requested, UpstreamModelName: "provider-model"}}
					for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
						view := tasksToDto([]*model.Task{task}, false, role)[0]
						properties, ok := view.Properties.(model.Properties)
						require.True(t, ok)
						assert.Equal(t, tc.requested, properties.OriginModelName)
						if role < common.RoleAdminUser {
							assert.Equal(t, expectedUpstream, properties.UpstreamModelName)
						} else {
							assert.Equal(t, "provider-model", properties.UpstreamModelName)
						}
					}
					var stored model.Log
					require.NoError(t, db.First(&stored, entry.Id).Error)
					assert.Equal(t, original, stored.Other)
					assert.Equal(t, "provider-model", task.Properties.UpstreamModelName)
				})
			}
			// Editing display settings must also update views of existing logs.
			require.NoError(t, db.Model(&channel).Update("settings", `{"user_model_mapping":{"public-alias":"updated-display"}}`).Error)
			logs, _, err := model.GetUserLogs(1, model.LogFilter{RequestId: "display-configured"}, 0, 20)
			require.NoError(t, err)
			require.Len(t, logs, 1)
			assert.Contains(t, logs[0].Other, "updated-display")
			require.NoError(t, db.Model(&channel).Update("settings", `{}`).Error)
			restored, _, err := model.GetUserLogs(1, model.LogFilter{RequestId: "display-configured"}, 0, 20)
			require.NoError(t, err)
			require.Len(t, restored, 1)
			var originalLog model.Log
			require.NoError(t, db.Where("request_id = ?", "display-configured").First(&originalLog).Error)
			assert.JSONEq(t, originalLog.Other, restored[0].Other, "clearing the display name restores original model diagnostics")

		})
	}
}
