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
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const modelMonitorMaxModels = perfmetrics.MaxRecentModels
const modelMonitorMaxDescriptionLength = 300

type modelMonitorModel struct {
	ModelName   string `json:"model_name"`
	Description string `json:"description"`
}

type modelMonitorPoint struct {
	Ts           int64   `json:"ts"`
	RequestCount int64   `json:"request_count"`
	SuccessCount int64   `json:"success_count"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
}

type modelMonitorMetric struct {
	ModelName    string              `json:"model_name"`
	RequestCount int64               `json:"request_count"`
	SuccessCount int64               `json:"success_count"`
	AvgLatencyMs int64               `json:"avg_latency_ms"`
	SuccessRate  float64             `json:"success_rate"`
	AvgTps       float64             `json:"avg_tps"`
	Series       []modelMonitorPoint `json:"series"`
}

type modelMonitorResponse struct {
	Models          []modelMonitorModel  `json:"models"`
	AvailableModels []string             `json:"available_models"`
	Metrics         []modelMonitorMetric `json:"metrics"`
	WindowStart     int64                `json:"window_start"`
	WindowEnd       int64                `json:"window_end"`
}

func GetModelMonitor(c *gin.Context) {
	minutes, err := parseModelMonitorMinutes(c.Query("minutes"))
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}

	configuredModels, err := getModelMonitorModels()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to load the model monitor configuration"))
		return
	}
	availableModels := getModelMonitorAvailableModels()
	configuredModels = filterModelMonitorModels(configuredModels, availableModels)
	requestedModels := make([]string, 0, len(configuredModels))
	for _, item := range configuredModels {
		requestedModels = append(requestedModels, item.ModelName)
	}
	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QueryRollingSummary(minutes, activeGroups, requestedModels...)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	metrics := make([]modelMonitorMetric, 0, len(result.Models))
	for _, item := range result.Models {
		series := make([]modelMonitorPoint, 0, len(item.Series))
		for _, point := range item.Series {
			series = append(series, modelMonitorPoint{
				Ts:           point.Ts,
				RequestCount: point.RequestCount,
				SuccessCount: point.SuccessCount,
				AvgLatencyMs: point.AvgLatencyMs,
				SuccessRate:  point.SuccessRate,
				AvgTps:       point.AvgTps,
			})
		}
		metrics = append(metrics, modelMonitorMetric{
			ModelName:    item.ModelName,
			RequestCount: item.RequestCount,
			SuccessCount: item.SuccessCount,
			AvgLatencyMs: item.AvgLatencyMs,
			SuccessRate:  item.SuccessRate,
			AvgTps:       item.AvgTps,
			Series:       series,
		})
	}

	common.ApiSuccess(c, modelMonitorResponse{
		Models:          configuredModels,
		AvailableModels: availableModels,
		Metrics:         metrics,
		WindowStart:     result.WindowStart,
		WindowEnd:       result.WindowEnd,
	})
}

func UpdateModelMonitorModels(c *gin.Context) {
	var request struct {
		Models json.RawMessage `json:"models"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	requestedModels, err := parseModelMonitorUpdateModels(request.Models)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	models, err := normalizeModelMonitorModels(requestedModels, getModelMonitorAvailableModels())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	encoded, err := common.Marshal(models)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to save the model monitor configuration"))
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{model.ModelMonitorModelsOptionKey: string(encoded)}); err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to save the model monitor configuration"))
		return
	}
	common.ApiSuccess(c, models)
}

func parseModelMonitorMinutes(raw string) (int, error) {
	if raw == "" {
		return 60, nil
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil || (minutes != 60 && minutes != 360 && minutes != 720 && minutes != 1440) {
		return 0, common.NewMessage("Invalid parameters")
	}
	return minutes, nil
}

func getModelMonitorAvailableModels() []string {
	pricing := model.GetPricing()
	seen := make(map[string]struct{}, len(pricing))
	models := make([]string, 0, len(pricing))
	for _, item := range pricing {
		name := strings.TrimSpace(item.ModelName)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, name)
	}
	slices.Sort(models)
	return models
}

func getModelMonitorModels() ([]modelMonitorModel, error) {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[model.ModelMonitorModelsOptionKey]
	common.OptionMapRWMutex.RUnlock()
	return parseModelMonitorModels(raw)
}

func parseModelMonitorUpdateModels(raw json.RawMessage) ([]modelMonitorModel, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, common.NewMessage("Invalid parameters")
	}
	return parseModelMonitorModels(trimmed)
}
func parseModelMonitorModels(raw string) ([]modelMonitorModel, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "null" {
		return []modelMonitorModel{}, nil
	}
	var entries []json.RawMessage
	if err := common.UnmarshalJsonStr(raw, &entries); err != nil {
		return nil, err
	}
	models := make([]modelMonitorModel, 0, len(entries))
	for _, entry := range entries {
		var legacyName string
		if err := common.Unmarshal(entry, &legacyName); err == nil {
			name := strings.TrimSpace(legacyName)
			if name != "" {
				models = append(models, modelMonitorModel{ModelName: name})
			}
			continue
		}
		var modelItem modelMonitorModel
		if err := common.Unmarshal(entry, &modelItem); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(modelItem.ModelName)
		if name == "" {
			continue
		}
		models = append(models, modelMonitorModel{ModelName: name, Description: strings.TrimSpace(modelItem.Description)})
	}
	return models, nil
}

func filterModelMonitorModels(configured []modelMonitorModel, available []string) []modelMonitorModel {
	availableSet := make(map[string]struct{}, len(available))
	for _, name := range available {
		availableSet[name] = struct{}{}
	}
	models := make([]modelMonitorModel, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))
	for _, item := range configured {
		name := strings.TrimSpace(item.ModelName)
		if name == "" {
			continue
		}
		if _, ok := availableSet[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, modelMonitorModel{ModelName: name, Description: strings.TrimSpace(item.Description)})
	}
	return models
}

func normalizeModelMonitorModels(requested []modelMonitorModel, available []string) ([]modelMonitorModel, error) {
	availableSet := make(map[string]struct{}, len(available))
	for _, name := range available {
		availableSet[name] = struct{}{}
	}
	for _, item := range requested {
		name := strings.TrimSpace(item.ModelName)
		description := strings.TrimSpace(item.Description)
		if name == "" {
			if description != "" {
				return nil, common.NewMessage("Invalid parameters")
			}
			continue
		}
		if _, ok := availableSet[name]; !ok {
			return nil, common.NewMessage("Model {{model}} is not available in the model square", map[string]any{"model": name})
		}
		if utf8.RuneCountInString(description) > modelMonitorMaxDescriptionLength {
			return nil, common.NewMessage("Description for model {{model}} must be at most {{count}} characters", map[string]any{"model": name, "count": modelMonitorMaxDescriptionLength})
		}
	}
	models := filterModelMonitorModels(requested, available)
	if len(models) > modelMonitorMaxModels {
		return nil, common.NewMessage("The model monitor supports up to {{count}} models", map[string]any{"count": modelMonitorMaxModels})
	}
	return models, nil
}
