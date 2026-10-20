/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeModelMonitorModels(t *testing.T) {
	available := []string{"alpha", "beta", "gamma"}
	models, err := normalizeModelMonitorModels([]modelMonitorModel{
		{ModelName: " beta ", Description: "Stable production endpoint"},
		{ModelName: "alpha"},
		{ModelName: "beta", Description: "Ignored duplicate"},
	}, available)
	require.NoError(t, err)
	assert.Equal(t, []modelMonitorModel{
		{ModelName: "beta", Description: "Stable production endpoint"},
		{ModelName: "alpha", Description: ""},
	}, models)

	_, err = normalizeModelMonitorModels([]modelMonitorModel{{ModelName: "missing"}}, available)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")

	_, err = normalizeModelMonitorModels([]modelMonitorModel{{
		ModelName:   "alpha",
		Description: strings.Repeat("x", modelMonitorMaxDescriptionLength+1),
	}}, available)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "300")

	tooMany := make([]modelMonitorModel, 0, modelMonitorMaxModels+1)
	tooManyAvailable := make([]string, 0, modelMonitorMaxModels+1)
	for i := range modelMonitorMaxModels + 1 {
		name := fmt.Sprintf("model-%d", i)
		tooMany = append(tooMany, modelMonitorModel{ModelName: name})
		tooManyAvailable = append(tooManyAvailable, name)
	}
	_, err = normalizeModelMonitorModels(tooMany, tooManyAvailable)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%d", modelMonitorMaxModels))
}

func TestParseModelMonitorModelsSupportsLegacyStrings(t *testing.T) {
	models, err := parseModelMonitorModels(`["beta","alpha"]`)
	require.NoError(t, err)
	assert.Equal(t, []modelMonitorModel{
		{ModelName: "beta", Description: ""},
		{ModelName: "alpha", Description: ""},
	}, models)
}

func TestParseModelMonitorUpdateModelsRejectsMissingModels(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null")} {
		_, err := parseModelMonitorUpdateModels(raw)
		require.Error(t, err)
	}

	models, err := parseModelMonitorUpdateModels(json.RawMessage(`[{"model_name":"alpha"}]`))
	require.NoError(t, err)
	assert.Equal(t, []modelMonitorModel{{ModelName: "alpha"}}, models)

	models, err = parseModelMonitorUpdateModels(json.RawMessage(`[]`))
	require.NoError(t, err)
	assert.Empty(t, models)
}
func TestParseModelMonitorMinutes(t *testing.T) {
	for _, value := range []int{60, 360, 720, 1440} {
		minutes, err := parseModelMonitorMinutes(fmt.Sprintf("%d", value))
		require.NoError(t, err)
		assert.Equal(t, value, minutes)
	}
	minutes, err := parseModelMonitorMinutes("")
	require.NoError(t, err)
	assert.Equal(t, 60, minutes)
	for _, raw := range []string{"0", "15", "30", "59", "61", "359", "361", "719", "721", "1439", "1441", "invalid"} {
		_, err := parseModelMonitorMinutes(raw)
		require.Error(t, err)
	}
}

func TestModelMonitorModelsPersist(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Option{}))
	previousDB, previousLogDB, previousOptions := model.DB, model.LOG_DB, common.OptionMap
	model.DB, model.LOG_DB, common.OptionMap = database, database, map[string]string{}
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.OptionMap = previousDB, previousLogDB, previousOptions
		sqlDB, err := database.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	expected := []modelMonitorModel{
		{ModelName: "beta", Description: "Stable production endpoint"},
		{ModelName: "alpha", Description: ""},
	}
	encoded, err := common.Marshal(expected)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOptionsBulk(map[string]string{
		model.ModelMonitorModelsOptionKey: string(encoded),
	}))

	models, err := getModelMonitorModels()
	require.NoError(t, err)
	assert.Equal(t, expected, models)

	var stored model.Option
	require.NoError(t, database.First(&stored, "key = ?", model.ModelMonitorModelsOptionKey).Error)
	assert.JSONEq(t, string(encoded), stored.Value)
}
