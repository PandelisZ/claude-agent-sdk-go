package claudeagentsdk

import (
	"encoding/json"
	"reflect"
)

func effectiveCanUseTool(options ClientOptions) CanUseToolCallback {
	if options.CanUseTool != nil {
		return options.CanUseTool
	}
	return options.ClaudeAgentOptions.CanUseTool
}

func effectiveHooks(options ClientOptions) map[HookEvent][]HookMatcher {
	if len(options.Hooks) > 0 {
		return options.Hooks
	}
	return options.ClaudeAgentOptions.Hooks
}

func ParseContextUsageResponse(raw map[string]any) (ContextUsageResponse, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ContextUsageResponse{}, err
	}
	var response ContextUsageResponse
	if err := json.Unmarshal(encoded, &response); err != nil {
		return ContextUsageResponse{}, err
	}
	return response, nil
}

func extractInitializeAgents(options ClaudeAgentOptions) map[string]any {
	field := reflect.ValueOf(options).FieldByName("Agents")
	if !field.IsValid() || field.IsNil() {
		return nil
	}

	raw := field.Interface()
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var agents map[string]any
	if err := json.Unmarshal(encoded, &agents); err != nil {
		return nil
	}
	return pruneNilValues(agents)
}

func extractExcludeDynamicSections(options ClaudeAgentOptions) *bool {
	field := reflect.ValueOf(options).FieldByName("SystemPromptPreset")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	preset := field.Elem()
	excludeField := preset.FieldByName("ExcludeDynamicSections")
	if !excludeField.IsValid() || excludeField.IsNil() || excludeField.Elem().Kind() != reflect.Bool {
		return nil
	}
	value := excludeField.Elem().Bool()
	return &value
}

func extractInitializeSkills(options ClaudeAgentOptions) []string {
	field := reflect.ValueOf(options).FieldByName("Skills")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	if field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.String {
		return nil
	}
	skills := make([]string, 0, field.Len())
	for index := 0; index < field.Len(); index++ {
		skills = append(skills, field.Index(index).String())
	}
	return skills
}

func pruneNilValues(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case nil:
			continue
		case map[string]any:
			result[key] = pruneNilValues(typed)
		case []any:
			result[key] = pruneNilSliceValues(typed)
		default:
			result[key] = value
		}
	}
	return result
}

func pruneNilSliceValues(input []any) []any {
	result := make([]any, 0, len(input))
	for _, value := range input {
		switch typed := value.(type) {
		case nil:
			continue
		case map[string]any:
			result = append(result, pruneNilValues(typed))
		case []any:
			result = append(result, pruneNilSliceValues(typed))
		default:
			result = append(result, value)
		}
	}
	return result
}
