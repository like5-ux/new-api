package model

import (
	"bytes"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// UserErrorPresentation replaces only internal model names. It never changes
// error categories, status codes, correlation IDs, or non-model diagnostics.
type UserErrorPresentation struct {
	ModelName string
	requested string
	names     []string
}

func NewUserErrorPresentation(channelID int, requested string, upstreamModels ...string) *UserErrorPresentation {
	p := &UserErrorPresentation{ModelName: requested, requested: requested}
	names := map[string]bool{}
	for _, name := range upstreamModels {
		if name != "" {
			names[name] = true
		}
	}
	if channelID > 0 {
		if channel, err := CacheGetChannel(channelID); err == nil && channel != nil {
			var settings struct {
				Models map[string]string `json:"user_model_mapping"`
			}
			if common.UnmarshalJsonStr(channel.OtherSettings, &settings) == nil {
				if display := strings.TrimSpace(settings.Models[requested]); display != "" {
					p.ModelName = display
				}
			}
			var mapping map[string]string
			if channel.ModelMapping != nil && common.UnmarshalJsonStr(*channel.ModelMapping, &mapping) == nil {
				visited := map[string]bool{requested: true}
				for name := mapping[requested]; name != "" && !visited[name]; name = mapping[name] {
					visited[name] = true
					names[name] = true
				}
			}
		}
	}
	if requested == "" {
		return p
	}
	// Public names are identity matches, so an upstream prefix cannot rewrite
	// the request/display name when one is contained in the other.
	names[requested], names[p.ModelName] = true, true
	for name := range names {
		p.names = append(p.names, name)
	}
	slices.SortFunc(p.names, func(a, b string) int { return len(b) - len(a) })
	return p
}

func modelNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.'
}

func hasModelNameContinuationBefore(text string, offset int) bool {
	if offset == 0 {
		return false
	}
	previous := text[offset-1]
	if previous != '.' {
		return modelNameByte(previous)
	}
	return offset > 1 && modelNameByte(text[offset-2])
}

func hasModelNameContinuationAfter(text string, offset int) bool {
	if offset >= len(text) {
		return false
	}
	next := text[offset]
	if next != '.' {
		return modelNameByte(next)
	}
	return offset+1 < len(text) && modelNameByte(text[offset+1])
}

func (p *UserErrorPresentation) MaskText(text string) string {
	if p == nil || len(p.names) == 0 || text == "" {
		return text
	}
	var result strings.Builder
	for offset := 0; offset < len(text); {
		matched := false
		for _, name := range p.names {
			end := offset + len(name)
			if !strings.HasPrefix(text[offset:], name) {
				continue
			}
			if hasModelNameContinuationBefore(text, offset) || hasModelNameContinuationAfter(text, end) {
				continue
			}
			if name == p.requested || name == p.ModelName {
				result.WriteString(name)
			} else {
				result.WriteString(p.ModelName)
			}
			offset, matched = end, true
			break
		}
		if !matched {
			result.WriteByte(text[offset])
			offset++
		}
	}
	return result.String()
}

// MaskJSON visits string values only, preserving numeric lexemes and keys.
func (p *UserErrorPresentation) MaskJSON(data []byte) []byte {
	if p == nil || len(p.names) == 0 {
		return data
	}
	changed := false
	switch common.GetJsonType(data) {
	case "string":
		var text string
		if common.Unmarshal(data, &text) != nil {
			return data
		}
		masked := p.MaskText(text)
		if masked == text {
			return data
		}
		encoded, err := common.Marshal(masked)
		if err == nil {
			return encoded
		}
	case "object":
		var object map[string]common.RawMessage
		if common.Unmarshal(data, &object) != nil {
			return data
		}
		for key, raw := range object {
			masked := p.MaskJSON(raw)
			if !bytes.Equal(masked, raw) {
				object[key], changed = masked, true
			}
		}
		if changed {
			if encoded, err := common.Marshal(object); err == nil {
				return encoded
			}
		}
	case "array":
		var array []common.RawMessage
		if common.Unmarshal(data, &array) != nil {
			return data
		}
		for index, raw := range array {
			masked := p.MaskJSON(raw)
			if !bytes.Equal(masked, raw) {
				array[index], changed = masked, true
			}
		}
		if changed {
			if encoded, err := common.Marshal(array); err == nil {
				return encoded
			}
		}
	}
	return data
}

func IsRelayErrorJSON(data []byte) bool {
	if !bytes.Contains(data, []byte(`"error"`)) && !bytes.Contains(data, []byte(`"response.failed"`)) && !bytes.Contains(data, []byte(`"fail_reason"`)) {
		return false
	}
	var root map[string]common.RawMessage
	if common.Unmarshal(data, &root) != nil {
		return false
	}
	var eventType string
	_ = common.Unmarshal(root["type"], &eventType)
	if eventType == "error" || eventType == "response.failed" {
		return true
	}
	if raw := root["error"]; len(raw) > 0 && string(raw) != "null" {
		return true
	}
	var status string
	_ = common.Unmarshal(root["status"], &status)
	if (status == "FAILURE" || status == "FAILED" || status == "failed") && len(root["fail_reason"]) > 0 {
		return true
	}
	for _, key := range []string{"response", "data"} {
		if raw := root[key]; len(raw) > 0 && IsRelayErrorJSON(raw) {
			return true
		}
	}
	return false
}

func isModelJSONKey(key string) bool {
	switch key {
	case "model", "upstream_model", "upstream_model_name", "returned_model", "billing_model":
		return true
	default:
		return false
	}
}

func collectModelNames(data []byte, names map[string]bool) {
	switch common.GetJsonType(data) {
	case "object":
		var object map[string]common.RawMessage
		if common.Unmarshal(data, &object) != nil {
			return
		}
		for key, raw := range object {
			if isModelJSONKey(key) {
				var name string
				if common.Unmarshal(raw, &name) == nil && strings.TrimSpace(name) != "" {
					names[name] = true
				}
			}
			collectModelNames(raw, names)
		}
	case "array":
		var array []common.RawMessage
		if common.Unmarshal(data, &array) != nil {
			return
		}
		for _, raw := range array {
			collectModelNames(raw, names)
		}
	}
}

func (p *UserErrorPresentation) withAdditionalNames(extra map[string]bool) *UserErrorPresentation {
	names := make(map[string]bool, len(p.names)+len(extra))
	for _, name := range p.names {
		names[name] = true
	}
	for name := range extra {
		if name != "" {
			names[name] = true
		}
	}
	result := &UserErrorPresentation{ModelName: p.ModelName, requested: p.requested}
	for name := range names {
		result.names = append(result.names, name)
	}
	slices.SortFunc(result.names, func(a, b string) int { return len(b) - len(a) })
	return result
}

func (p *UserErrorPresentation) PresentJSON(data []byte, status int) []byte {
	if p == nil || p.requested == "" || p.ModelName == "" || status < 400 && !IsRelayErrorJSON(data) {
		return data
	}
	modelNames := map[string]bool{}
	collectModelNames(data, modelNames)
	presentation := p
	if len(modelNames) > 0 {
		presentation = p.withAdditionalNames(modelNames)
	}
	switch common.GetJsonType(data) {
	case "object", "array", "string":
		return presentation.MaskJSON(data)
	case "number", "boolean", "null":
		return data
	default:
		return []byte(presentation.MaskText(string(data)))
	}
}
