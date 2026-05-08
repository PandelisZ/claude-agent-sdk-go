package claudeagentsdk

// FoldSessionSummary folds a batch of appended store entries into a reusable
// summary sidecar. It mirrors Python's fold_session_summary shape: mtime is
// preserved from prev and should be stamped by the store after persisting.
func FoldSessionSummary(prev *SessionSummaryEntry, key SessionKey, entries []SessionStoreEntry) SessionSummaryEntry {
	summary := SessionSummaryEntry{
		SessionID: key.SessionID,
		Data:      map[string]any{},
	}
	if prev != nil {
		summary.MTime = prev.MTime
		summary.Data = map[string]any{}
		for k, v := range prev.Data {
			summary.Data[k] = v
		}
	}

	for _, entry := range entries {
		if _, ok := summary.Data["is_sidechain"]; !ok {
			summary.Data["is_sidechain"] = boolFromAny(entry["isSidechain"])
		}
		if _, ok := summary.Data["created_at"]; !ok {
			if value := epochMillisFromAny(entry["timestamp"]); value != nil {
				summary.Data["created_at"] = *value
			}
		}
		if _, ok := summary.Data["cwd"]; !ok {
			if value := stringFromAny(entry["cwd"]); value != "" {
				summary.Data["cwd"] = value
			}
		}
		foldSummaryFirstPrompt(summary.Data, entry)
		for src, dst := range map[string]string{
			"customTitle": "custom_title",
			"aiTitle":     "ai_title",
			"lastPrompt":  "last_prompt",
			"summary":     "summary_hint",
			"gitBranch":   "git_branch",
		} {
			if value := stringFromAny(entry[src]); value != "" {
				summary.Data[dst] = value
			}
		}
		if stringFromAny(entry["type"]) == "tag" {
			if value, ok := entry["tag"].(string); ok && value != "" {
				summary.Data["tag"] = value
			} else {
				delete(summary.Data, "tag")
			}
		}
	}

	return summary
}

func SummaryEntryToSDKInfo(entry SessionSummaryEntry, projectPath string) *SDKSessionInfo {
	if boolFromAny(entry.Data["is_sidechain"]) {
		return nil
	}
	firstPrompt := stringFromAny(entry.Data["first_prompt"])
	if !boolFromAny(entry.Data["first_prompt_locked"]) {
		firstPrompt = stringFromAny(entry.Data["command_fallback"])
	}
	customTitle := stringFromAny(entry.Data["custom_title"])
	if customTitle == "" {
		customTitle = stringFromAny(entry.Data["ai_title"])
	}
	summary := firstNonEmpty(
		customTitle,
		stringFromAny(entry.Data["last_prompt"]),
		stringFromAny(entry.Data["summary_hint"]),
		firstPrompt,
	)
	if summary == "" {
		return nil
	}
	info := SDKSessionInfo{
		SessionID:    entry.SessionID,
		Summary:      summary,
		LastModified: entry.MTime,
		GitBranch:    optionalStringValue(stringFromAny(entry.Data["git_branch"])),
		Cwd:          optionalStringValue(firstNonEmpty(stringFromAny(entry.Data["cwd"]), projectPath)),
		Tag:          optionalStringValue(stringFromAny(entry.Data["tag"])),
	}
	if customTitle != "" {
		info.CustomTitle = &customTitle
	}
	if firstPrompt != "" {
		info.FirstPrompt = &firstPrompt
	}
	if createdAt, ok := int64FromAny(entry.Data["created_at"]); ok {
		info.CreatedAt = &createdAt
	}
	return &info
}

func foldSummaryFirstPrompt(data map[string]any, entry SessionStoreEntry) {
	if boolFromAny(data["first_prompt_locked"]) || stringFromAny(entry["type"]) != "user" || boolFromAny(entry["isMeta"]) || boolFromAny(entry["isCompactSummary"]) {
		return
	}
	prompt := firstPromptFromStoreEntry(entry)
	if prompt == "" {
		return
	}
	data["first_prompt"] = prompt
	data["first_prompt_locked"] = true
}

func optionalStringValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func int64FromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}
