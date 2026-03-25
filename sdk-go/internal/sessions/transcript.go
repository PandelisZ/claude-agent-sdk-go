package sessions

import (
	"encoding/json"
)

var transcriptEntryTypes = map[string]struct{}{
	"user":       {},
	"assistant":  {},
	"progress":   {},
	"system":     {},
	"attachment": {},
}

type transcriptEntry struct {
	Type             string
	UUID             string
	ParentUUID       string
	SessionID        string
	Message          any
	IsSidechain      bool
	IsMeta           bool
	IsCompactSummary bool
	TeamName         string
}

func parseTranscriptEntries(content []byte) []transcriptEntry {
	lines := splitLines(content)
	entries := make([]transcriptEntry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		entryType, _ := raw["type"].(string)
		if _, ok := transcriptEntryTypes[entryType]; !ok {
			continue
		}
		uuid, _ := raw["uuid"].(string)
		if uuid == "" {
			continue
		}
		parentUUID, _ := raw["parentUuid"].(string)
		sessionID, _ := raw["sessionId"].(string)
		isSidechain, _ := raw["isSidechain"].(bool)
		isMeta, _ := raw["isMeta"].(bool)
		isCompactSummary, _ := raw["isCompactSummary"].(bool)
		teamName, _ := raw["teamName"].(string)
		entries = append(entries, transcriptEntry{
			Type:             entryType,
			UUID:             uuid,
			ParentUUID:       parentUUID,
			SessionID:        sessionID,
			Message:          raw["message"],
			IsSidechain:      isSidechain,
			IsMeta:           isMeta,
			IsCompactSummary: isCompactSummary,
			TeamName:         teamName,
		})
	}
	return entries
}

func buildConversationChain(entries []transcriptEntry) []transcriptEntry {
	if len(entries) == 0 {
		return nil
	}

	byUUID := make(map[string]transcriptEntry, len(entries))
	indexByUUID := make(map[string]int, len(entries))
	parentUUIDs := make(map[string]struct{}, len(entries))
	for i, entry := range entries {
		byUUID[entry.UUID] = entry
		indexByUUID[entry.UUID] = i
		if entry.ParentUUID != "" {
			parentUUIDs[entry.ParentUUID] = struct{}{}
		}
	}

	terminals := make([]transcriptEntry, 0, len(entries))
	for _, entry := range entries {
		if _, hasChild := parentUUIDs[entry.UUID]; !hasChild {
			terminals = append(terminals, entry)
		}
	}

	leaves := make([]transcriptEntry, 0, len(terminals))
	for _, terminal := range terminals {
		current := terminal
		seen := map[string]struct{}{}
		for current.UUID != "" {
			if _, ok := seen[current.UUID]; ok {
				break
			}
			seen[current.UUID] = struct{}{}
			if current.Type == "user" || current.Type == "assistant" {
				leaves = append(leaves, current)
				break
			}
			if current.ParentUUID == "" {
				break
			}
			parent, ok := byUUID[current.ParentUUID]
			if !ok {
				break
			}
			current = parent
		}
	}
	if len(leaves) == 0 {
		return nil
	}

	mainLeaves := make([]transcriptEntry, 0, len(leaves))
	for _, leaf := range leaves {
		if !leaf.IsSidechain && !leaf.IsMeta && leaf.TeamName == "" {
			mainLeaves = append(mainLeaves, leaf)
		}
	}
	candidates := leaves
	if len(mainLeaves) > 0 {
		candidates = mainLeaves
	}

	best := candidates[0]
	bestIdx := indexByUUID[best.UUID]
	for _, candidate := range candidates[1:] {
		if idx := indexByUUID[candidate.UUID]; idx > bestIdx {
			best = candidate
			bestIdx = idx
		}
	}

	chain := make([]transcriptEntry, 0, len(entries))
	current := best
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

func isVisibleMessage(entry transcriptEntry) bool {
	if entry.Type != "user" && entry.Type != "assistant" {
		return false
	}
	if entry.IsMeta || entry.IsSidechain || entry.TeamName != "" {
		return false
	}
	return true
}

func toSessionMessage(entry transcriptEntry) SessionMessage {
	return SessionMessage{
		Type:            entry.Type,
		UUID:            entry.UUID,
		SessionID:       entry.SessionID,
		Message:         entry.Message,
		ParentToolUseID: nil,
	}
}

func GetSessionMessages(sessionID string, directory string, limit int, offset int) ([]SessionMessage, error) {
	if !validateUUID(sessionID) {
		return nil, nil
	}
	content, err := readSessionFile(sessionID, directory)
	if err != nil || len(content) == 0 {
		return nil, err
	}
	entries := parseTranscriptEntries(content)
	chain := buildConversationChain(entries)
	visible := make([]SessionMessage, 0, len(chain))
	for _, entry := range chain {
		if isVisibleMessage(entry) {
			visible = append(visible, toSessionMessage(entry))
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(visible) {
		return []SessionMessage{}, nil
	}
	if limit > 0 && offset+limit < len(visible) {
		return visible[offset : offset+limit], nil
	}
	return visible[offset:], nil
}
