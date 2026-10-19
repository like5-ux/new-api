package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProcessChannelErrorUsesSnapshotWithoutLeakingChannelMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousErrorLogEnabled := constant.ErrorLogEnabled

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = database, database
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, database.Create(&model.User{Id: 7, Username: "log-owner", Group: "default"}).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 7)
	ctx.Set("username", "log-owner")
	ctx.Set("token_name", "test-token")
	ctx.Set("token_id", 11)
	ctx.Set("original_model", "gpt-test")
	ctx.Set("group", "default")
	ctx.Set("channel_id", 202)
	ctx.Set("channel_name", "mutable-context-channel")
	ctx.Set("channel_type", 9)
	ctx.Set("use_channel", []string{"101"})
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now().Add(-time.Second))

	channelSnapshot := types.ChannelError{
		ChannelId:   101,
		ChannelType: 1,
		ChannelName: "snapshot-channel",
		AutoBan:     false,
	}
	apiErr := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	processChannelError(ctx, channelSnapshot, apiErr, nil)

	var stored model.Log
	require.NoError(t, database.First(&stored).Error)
	assert.Equal(t, channelSnapshot.ChannelId, stored.ChannelId)
	storedOther, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	assert.Equal(t, float64(http.StatusBadGateway), storedOther["status_code"])
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, storedOther, key)
	}
	adminInfo, ok := storedOther["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"101"}, adminInfo["use_channel"])

	logs, total, err := model.GetUserLogs(7, model.LogFilter{Type: model.LogTypeError}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, channelSnapshot.ChannelId, logs[0].ChannelId)
	assert.Empty(t, logs[0].ChannelName)
	userOther, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, userOther, "admin_info")
	for _, key := range []string{"channel_id", "channel_name", "channel_type"} {
		assert.NotContains(t, userOther, key)
	}
}

func TestUserErrorLogsMaskModelNames(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Log{}, &model.Task{}))
	channel := model.Channel{Id: 31, Key: "test-key", ModelMapping: common.GetPointer(`{"public-alias":"provider-secret-model"}`), OtherSettings: `{"user_model_mapping":{"public-alias":"friendly-model"}}`}
	require.NoError(t, db.Create(&channel).Error)
	raw := `No available channel for model provider-secret-model or provider-returned-model under group private-group (request id: upstream-secret-id), body: {"diagnostic":"provider-secret-model"}`
	log := model.Log{UserId: 7, Type: model.LogTypeError, CreatedAt: time.Now().Unix(), ChannelId: 31, ModelName: "public-alias", RequestId: "local-request-id", Content: raw,
		Other: `{"error_code":"provider-secret-model","status_code":503,"body":"provider-secret-model","response_model":{"requested_model":"public-alias","upstream_model":"provider-secret-model","returned_model":"provider-returned-model"},"stream_status":{"status":"error","errors":["provider-secret-model"],"end_error":"provider-secret-model"},"admin_info":{"raw_error":"provider-secret-model"}}`}
	require.NoError(t, db.Create(&log).Error)
	user := callLogHandler(t, GetUserLogs, 7, common.RoleCommonUser, "")[0]
	encoded, err := common.Marshal(user)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "provider-secret-model")
	assert.NotContains(t, string(encoded), "provider-returned-model")
	assert.Contains(t, string(encoded), "private-group")
	assert.Contains(t, string(encoded), "upstream-secret-id")
	assert.Contains(t, string(encoded), "friendly-model")
	assert.Contains(t, string(encoded), "local-request-id")
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		admin := callLogHandler(t, GetAllLogs, 8, role, "")[0]
		encoded, err := common.Marshal(admin)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), "provider-secret-model")
		assert.Contains(t, string(encoded), "private-group")
	}
	var stored model.Log
	require.NoError(t, db.First(&stored, log.Id).Error)
	assert.Equal(t, raw, stored.Content)
	task := &model.Task{TaskID: "failed-task", UserId: 7, ChannelId: 31, Status: model.TaskStatusFailure, FailReason: "No available channel for model provider-secret-model under group private-group (request id: upstream-secret-id)", Properties: model.Properties{OriginModelName: "public-alias", UpstreamModelName: "provider-secret-model"}}
	taskView := tasksToDto([]*model.Task{task}, false, common.RoleCommonUser)[0]
	assert.NotContains(t, taskView.FailReason, "provider-secret-model")
	assert.Contains(t, taskView.FailReason, "friendly-model")
	assert.Equal(t, task.FailReason, tasksToDto([]*model.Task{task}, false, common.RoleAdminUser)[0].FailReason)

	require.NoError(t, db.Create(task).Error)
	readTask := func(role int) string {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID, nil)
		ctx.Params = gin.Params{{Key: "key", Value: task.TaskID}}
		ctx.Set("id", task.UserId)
		ctx.Set("role", role)
		GetTask(ctx)
		var response struct {
			FailReason string `json:"fail_reason"`
		}
		require.NoError(t, common.UnmarshalJsonStr(recorder.Body.String(), &response))
		return response.FailReason
	}
	userTaskReason := readTask(common.RoleCommonUser)
	assert.NotContains(t, userTaskReason, "provider-secret-model")

	assert.Contains(t, userTaskReason, "friendly-model")
	assert.Equal(t, "No available channel for model provider-secret-model under group private-group (request id: upstream-secret-id)", readTask(common.RoleAdminUser))
}

func TestUserErrorResponsesMaskOnlyModelNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.User{}))
	channel := model.Channel{Id: 41, Key: "test-key", ModelMapping: common.GetPointer(`{"public-alias":"provider-secret-model"}`), OtherSettings: `{"user_model_mapping":{"public-alias":"friendly-model"}}`}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.User{Id: 71, Username: "ordinary", AffCode: "ordinary-error-aff", Role: common.RoleCommonUser}).Error)
	require.NoError(t, db.Create(&model.User{Id: 72, Username: "admin", AffCode: "admin-error-aff", Role: common.RoleAdminUser}).Error)
	raw := `{"error":{"message":"No channel for provider-secret-model or provider-returned-model under group private-group (request id: upstream-id)","type":"new_api_error","code":"model_not_found","param":"model","model":"provider-returned-model","metadata":{"model":"\u0070rovider-secret-model","counter":9007199254740993}}}`
	stream := `{"type":"response.failed","sequence_number":8,"response":{"id":"resp_1","model":"provider-returned-model","status":"failed","error":{"message":"provider-returned-model is unavailable","code":"model_not_found"},"usage":{"input_tokens":2}}}`
	for _, tc := range []struct {
		name, mode         string
		role, userID       int
		statedRole, masked bool
	}{
		{"user JSON", "json", common.RoleCommonUser, 71, true, true},
		{"split JSON", "split", common.RoleCommonUser, 71, true, true},
		{"user API key", "json", 0, 71, false, true},
		{"admin JSON", "json", common.RoleAdminUser, 72, true, false},
		{"admin API key", "json", 0, 72, false, false},
		{"root JSON", "json", common.RoleRootUser, 72, true, false},
		{"user SSE", "sse", common.RoleCommonUser, 71, true, true},
		{"admin SSE", "sse", common.RoleAdminUser, 72, true, false},
		{"user Responses SSE", "responses", common.RoleCommonUser, 71, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(middleware.UserErrorResponses(), middleware.RouteTag("relay"))
			engine.POST("/v1/chat/completions", func(c *gin.Context) {
				if tc.statedRole {
					c.Set("role", tc.role)
				}
				c.Set("id", tc.userID)
				c.Set("channel_id", 41)
				c.Set("original_model", "public-alias")
				c.Set(common.RequestIdKey, "local-id")
				switch tc.mode {
				case "sse":
					helper.SetEventStreamHeaders(c)
					require.NoError(t, helper.StringData(c, raw))
				case "responses":
					helper.SetEventStreamHeaders(c)
					require.NoError(t, helper.StringData(c, stream))
				case "split":
					c.Status(http.StatusServiceUnavailable)
					split := strings.Index(raw, "secret-model") + 3
					_, err := c.Writer.Write([]byte(raw[:split]))
					require.NoError(t, err)
					_, err = c.Writer.Write([]byte(raw[split:]))
					require.NoError(t, err)
				default:
					c.Data(http.StatusServiceUnavailable, "application/json", []byte(raw))
				}
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
			body := recorder.Body.String()
			if tc.masked {
				assert.NotContains(t, body, "provider-secret-model")
				assert.NotContains(t, body, "provider-returned-model")
				assert.NotContains(t, body, `\u0070rovider-secret-model`)
				assert.Contains(t, body, "friendly-model")
			} else {
				assert.Contains(t, body, "provider-secret-model")
				assert.Contains(t, body, "provider-returned-model")
			}
			if tc.mode == "sse" || tc.mode == "responses" {
				assert.Equal(t, http.StatusOK, recorder.Code)
				assert.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
				assert.True(t, strings.HasPrefix(body, "data: "))
			} else {
				assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				var decoded struct {
					Error types.OpenAIError `json:"error"`
				}
				require.NoError(t, common.UnmarshalJsonStr(body, &decoded))
				assert.Equal(t, "model_not_found", decoded.Error.Code)
				assert.Equal(t, "new_api_error", decoded.Error.Type)
				assert.Contains(t, decoded.Error.Message, "private-group")
				assert.Contains(t, decoded.Error.Message, "upstream-id")
				assert.Contains(t, body, "9007199254740993")
			}
		})
	}
	t.Run("unconfigured display falls back to request alias", func(t *testing.T) {
		plainChannel := model.Channel{Id: 42, Key: "test-key", ModelMapping: common.GetPointer(`{"public-alias":"provider-secret-model"}`)}
		require.NoError(t, db.Create(&plainChannel).Error)
		for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
			engine := gin.New()
			engine.Use(middleware.UserErrorResponses(), middleware.RouteTag("relay"))
			engine.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set("role", role)
				c.Set("id", 71)
				c.Set("channel_id", plainChannel.Id)
				c.Set("original_model", "public-alias")
				c.Data(http.StatusServiceUnavailable, "application/json", []byte(raw))
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
			body := recorder.Body.String()
			if role == common.RoleCommonUser {
				assert.NotContains(t, body, "provider-secret-model")
				assert.NotContains(t, body, "provider-returned-model")
				assert.Contains(t, body, "public-alias")
			} else {
				assert.Contains(t, body, "provider-secret-model")
				assert.Contains(t, body, "provider-returned-model")
			}
			assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		}
	})
	t.Run("success is unchanged", func(t *testing.T) {
		presentation := model.NewUserErrorPresentation(41, "public-alias")
		success := []byte(`{"model":"provider-secret-model","choices":[{"message":{"content":"text"}}]}`)
		assert.Equal(t, success, presentation.PresentJSON(success, http.StatusOK))
	})
	t.Run("public prefix and short names are preserved", func(t *testing.T) {
		presentation := model.NewUserErrorPresentation(0, "a-public", "a")
		assert.Equal(t, "failed a-public a-public", presentation.MaskText("failed a a-public"))
	})
	t.Run("websocket error", func(t *testing.T) {
		for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
			engine := gin.New()
			engine.GET("/ws", func(c *gin.Context) {
				c.Set("role", role)
				c.Set("channel_id", 41)
				c.Set("original_model", "public-alias")
				socket, err := upgrader.Upgrade(c.Writer, c.Request, nil)
				require.NoError(t, err)
				defer socket.Close()
				helper.WssError(c, socket, types.OpenAIError{Message: "provider-secret-model unavailable", Code: "model_not_found"})
			})
			server := httptest.NewServer(engine)
			socket, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
			require.NoError(t, err)
			_, payload, err := socket.ReadMessage()
			require.NoError(t, err)
			if role == common.RoleCommonUser {
				assert.NotContains(t, string(payload), "provider-secret-model")
				assert.Contains(t, string(payload), "friendly-model")
			} else {
				assert.Contains(t, string(payload), "provider-secret-model")
			}
			require.NoError(t, socket.Close())
			server.Close()
		}
	})
}
