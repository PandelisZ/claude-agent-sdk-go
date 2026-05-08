package claudeagentsdk

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func deriveStoreSessionInfo(sessionID string, directory string, entries []SessionStoreEntry) SDKSessionInfo {
	var customTitle, aiTitle, summaryHint, firstPrompt, gitBranch, cwd, tag *string
	var createdAt *int64
	for _, entry := range entries {
		if createdAt == nil {
			createdAt = epochMillisFromAny(entry["timestamp"])
		}
		if value := stringFromAny(entry["customTitle"]); value != "" {
			customTitle = &value
		}
		if value := stringFromAny(entry["aiTitle"]); value != "" {
			aiTitle = &value
		}
		if value := stringFromAny(entry["summary"]); value != "" {
			summaryHint = &value
		}
		if value := stringFromAny(entry["gitBranch"]); value != "" {
			gitBranch = &value
		}
		if value := stringFromAny(entry["cwd"]); value != "" {
			cwd = &value
		}
		if value, ok := entry["tag"].(string); ok {
			tagValue := value
			tag = &tagValue
		}
		if firstPrompt == nil {
			if prompt := firstPromptFromStoreEntry(entry); prompt != "" {
				firstPrompt = &prompt
			}
		}
	}
	if cwd == nil && directory != "" {
		value := directory
		cwd = &value
	}
	summary := sessionID
	for _, candidate := range []*string{customTitle, aiTitle, summaryHint, firstPrompt} {
		if candidate != nil && *candidate != "" {
			summary = *candidate
			break
		}
	}
	return SDKSessionInfo{
		SessionID:    sessionID,
		Summary:      summary,
		CustomTitle:  customTitle,
		FirstPrompt:  firstPrompt,
		GitBranch:    gitBranch,
		Cwd:          cwd,
		Tag:          tag,
		CreatedAt:    createdAt,
		LastModified: storeLastModified(entries),
	}
}

func storeEntriesToMessages(entries []SessionStoreEntry, limit int, offset int, subagent bool) []SessionMessage {
	parsed := parseStoreEntries(entries)
	var chain []storeEntry
	if subagent {
		chain = buildStoreSubagentChain(parsed)
	} else {
		chain = buildStoreConversationChain(parsed)
	}
	messages := make([]SessionMessage, 0, len(chain))
	for _, entry := range chain {
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		if !subagent && (entry.IsMeta || entry.IsSidechain || entry.TeamName != "") {
			continue
		}
		messages = append(messages, SessionMessage{
			Type:      entry.Type,
			UUID:      entry.UUID,
			SessionID: entry.SessionID,
			Message:   entry.Message,
		})
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(messages) {
		return []SessionMessage{}
	}
	if limit > 0 && offset+limit < len(messages) {
		return messages[offset : offset+limit]
	}
	return messages[offset:]
}

func parseStoreEntries(entries []SessionStoreEntry) []storeEntry {
	parsed := make([]storeEntry, 0, len(entries))
	for _, raw := range entries {
		uuid := stringFromAny(raw["uuid"])
		if uuid == "" {
			continue
		}
		parsed = append(parsed, storeEntry{
			Type:        stringFromAny(raw["type"]),
			UUID:        uuid,
			ParentUUID:  stringFromAny(raw["parentUuid"]),
			SessionID:   stringFromAny(raw["sessionId"]),
			Message:     raw["message"],
			IsSidechain: boolFromAny(raw["isSidechain"]),
			IsMeta:      boolFromAny(raw["isMeta"]),
			TeamName:    stringFromAny(raw["teamName"]),
			Raw:         raw,
		})
	}
	return parsed
}

func buildStoreConversationChain(entries []storeEntry) []storeEntry {
	if len(entries) == 0 {
		return nil
	}
	byUUID := make(map[string]storeEntry, len(entries))
	parentUUIDs := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		byUUID[entry.UUID] = entry
		if entry.ParentUUID != "" {
			parentUUIDs[entry.ParentUUID] = struct{}{}
		}
	}
	var leaf storeEntry
	found := false
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		if entry.IsMeta || entry.IsSidechain || entry.TeamName != "" {
			continue
		}
		if _, hasChild := parentUUIDs[entry.UUID]; !hasChild {
			leaf = entry
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	return walkStoreChain(leaf, byUUID)
}

func buildStoreSubagentChain(entries []storeEntry) []storeEntry {
	byUUID := make(map[string]storeEntry, len(entries))
	for _, entry := range entries {
		byUUID[entry.UUID] = entry
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "user" || entries[i].Type == "assistant" {
			return walkStoreChain(entries[i], byUUID)
		}
	}
	return nil
}

func walkStoreChain(leaf storeEntry, byUUID map[string]storeEntry) []storeEntry {
	chain := make([]storeEntry, 0)
	current := leaf
	seen := map[string]struct{}{}
	for current.UUID != "" {
		if _, ok := seen[current.UUID]; ok {
			break
		}
		seen[current.UUID] = struct{}{}
		chain = append(chain, current)
		if current.ParentUUID == "" {
			break
		}
		parent, ok := byUUID[current.ParentUUID]
		if !ok {
			break
		}
		current = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

func buildStoreForkLines(entries []SessionStoreEntry, sessionID string, forkedSessionID string, upToMessageID string, title string) ([]string, error) {
	transcript := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryType := stringFromAny(entry["type"])
		if entryType != "user" && entryType != "assistant" && entryType != "system" && entryType != "attachment" && entryType != "progress" {
			continue
		}
		if stringFromAny(entry["uuid"]) == "" || boolFromAny(entry["isSidechain"]) {
			continue
		}
		transcript = append(transcript, mapFromSessionStoreEntry(entry))
		if upToMessageID != "" && stringFromAny(entry["uuid"]) == upToMessageID {
			break
		}
	}
	if len(transcript) == 0 {
		return nil, fmt.Errorf("session %s has no messages to fork", sessionID)
	}
	if upToMessageID != "" && stringFromAny(transcript[len(transcript)-1]["uuid"]) != upToMessageID {
		return nil, fmt.Errorf("message %s not found in session %s", upToMessageID, sessionID)
	}
	uuidMapping := map[string]string{}
	for _, entry := range transcript {
		uuidMapping[stringFromAny(entry["uuid"])] = mustStoreUUID()
	}
	lines := make([]string, 0, len(transcript)+1)
	for _, entry := range transcript {
		if entry["type"] == "progress" {
			continue
		}
		originalUUID := stringFromAny(entry["uuid"])
		forked := mapFromAny(entry)
		forked["uuid"] = uuidMapping[originalUUID]
		forked["sessionId"] = forkedSessionID
		forked["isSidechain"] = false
		forked["forkedFrom"] = map[string]any{"sessionId": sessionID, "messageUuid": originalUUID}
		if parentUUID := stringFromAny(entry["parentUuid"]); parentUUID != "" {
			if remapped, ok := uuidMapping[parentUUID]; ok {
				forked["parentUuid"] = remapped
			} else {
				delete(forked, "parentUuid")
			}
		}
		encoded, err := json.Marshal(forked)
		if err != nil {
			return nil, err
		}
		lines = append(lines, string(encoded))
	}
	titleValue := strings.TrimSpace(title)
	if titleValue == "" {
		titleValue = "Forked session"
	}
	encoded, err := json.Marshal(map[string]any{
		"type":        "custom-title",
		"sessionId":   forkedSessionID,
		"customTitle": titleValue,
		"uuid":        mustStoreUUID(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	lines = append(lines, string(encoded))
	return lines, nil
}

func appendJSONLFileToStore(ctx context.Context, store SessionStore, key SessionKey, path string, batchSize int) error {
	if batchSize <= 0 {
		batchSize = 500
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	batch := make([]SessionStoreEntry, 0, batchSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry SessionStoreEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return err
		}
		batch = append(batch, entry)
		if len(batch) >= batchSize {
			if err := store.Append(ctx, key, batch); err != nil {
				return err
			}
			batch = nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(batch) > 0 {
		return store.Append(ctx, key, batch)
	}
	return nil
}

func localSessionPathForImport(sessionID string, directory string) string {
	if directory != "" {
		path := filepath.Join(getClaudeConfigDirForStoreHelpers(), "projects", ProjectKeyForDirectory(directory), sessionID+".jsonl")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
		return ""
	}
	projectsDir := filepath.Join(getClaudeConfigDirForStoreHelpers(), "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(projectsDir, entry.Name(), sessionID+".jsonl")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func getClaudeConfigDirForStoreHelpers() string {
	if value := os.Getenv("CLAUDE_CONFIG_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(string(filepath.Separator), ".claude")
	}
	return filepath.Join(home, ".claude")
}

func firstPromptFromStoreEntry(entry SessionStoreEntry) string {
	if stringFromAny(entry["type"]) != "user" || boolFromAny(entry["isMeta"]) || boolFromAny(entry["isCompactSummary"]) {
		return ""
	}
	message, _ := entry["message"].(map[string]any)
	content := message["content"]
	if text, ok := content.(string); ok {
		return strings.TrimSpace(text)
	}
	items, _ := content.([]any)
	for _, item := range items {
		block, _ := item.(map[string]any)
		if stringFromAny(block["type"]) == "text" {
			return strings.TrimSpace(stringFromAny(block["text"]))
		}
	}
	return ""
}

func storeLastModified(entries []SessionStoreEntry) int64 {
	for i := len(entries) - 1; i >= 0; i-- {
		if value := epochMillisFromAny(entries[i]["timestamp"]); value != nil {
			return *value
		}
	}
	return 0
}

func epochMillisFromAny(raw any) *int64 {
	text, ok := raw.(string)
	if !ok || text == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil
	}
	value := parsed.UnixMilli()
	return &value
}

func applyStoreSessionPaging(items []SDKSessionInfo, limit int, offset int) []SDKSessionInfo {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []SDKSessionInfo{}
	}
	if limit > 0 && offset+limit < len(items) {
		return items[offset : offset+limit]
	}
	return items[offset:]
}

func mapFromSessionStoreEntry(entry SessionStoreEntry) map[string]any {
	out := make(map[string]any, len(entry))
	for key, value := range entry {
		out[key] = value
	}
	return out
}

func mapFromAny(entry map[string]any) map[string]any {
	out := make(map[string]any, len(entry))
	for key, value := range entry {
		out[key] = value
	}
	return out
}

func stringFromAny(raw any) string {
	value, _ := raw.(string)
	return value
}

func boolFromAny(raw any) bool {
	value, _ := raw.(bool)
	return value
}

func mustStoreUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
