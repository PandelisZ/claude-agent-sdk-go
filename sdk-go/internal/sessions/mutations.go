package sessions

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type ForkSessionResult struct {
	SessionID string
}

func RenameSession(sessionID string, title string, directory string) error {
	if !validateUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return errors.New("title must be non-empty")
	}

	entry, err := json.Marshal(struct {
		Type        string `json:"type"`
		CustomTitle string `json:"customTitle"`
		SessionID   string `json:"sessionId"`
	}{
		Type:        "custom-title",
		CustomTitle: trimmed,
		SessionID:   sessionID,
	})
	if err != nil {
		return err
	}
	return appendToSession(sessionID, string(entry)+"\n", directory)
}

func TagSession(sessionID string, tag *string, directory string) error {
	if !validateUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}

	value := ""
	if tag != nil {
		sanitized := strings.TrimSpace(sanitizeUnicode(*tag))
		if sanitized == "" {
			return errors.New("tag must be non-empty after sanitization (use nil to clear)")
		}
		value = sanitized
	}

	entry, err := json.Marshal(struct {
		Type      string `json:"type"`
		Tag       string `json:"tag"`
		SessionID string `json:"sessionId"`
	}{
		Type:      "tag",
		Tag:       value,
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}
	return appendToSession(sessionID, string(entry)+"\n", directory)
}

func DeleteSession(sessionID string, directory string) error {
	if !validateUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	path := resolveSessionFilePath(sessionID, directory)
	if path == "" {
		return fmt.Errorf("session %s not found: %w", sessionID, os.ErrNotExist)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(filepath.Dir(path), sessionID))
}

func ForkSession(sessionID string, directory string, upToMessageID string, title string) (ForkSessionResult, error) {
	if !validateUUID(sessionID) {
		return ForkSessionResult{}, fmt.Errorf("invalid session ID %q", sessionID)
	}
	if upToMessageID != "" && !validateUUID(upToMessageID) {
		return ForkSessionResult{}, fmt.Errorf("invalid up_to_message_id %q", upToMessageID)
	}
	path := resolveSessionFilePath(sessionID, directory)
	if path == "" {
		return ForkSessionResult{}, fmt.Errorf("session %s not found: %w", sessionID, os.ErrNotExist)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ForkSessionResult{}, err
	}

	entries, err := parseForkEntries(content, sessionID, upToMessageID)
	if err != nil {
		return ForkSessionResult{}, err
	}
	forkedSessionID, err := newUUID()
	if err != nil {
		return ForkSessionResult{}, err
	}
	lines, err := buildForkLines(entries, sessionID, forkedSessionID, title)
	if err != nil {
		return ForkSessionResult{}, err
	}

	forkPath := filepath.Join(filepath.Dir(path), forkedSessionID+".jsonl")
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(forkPath, []byte(data), 0o600); err != nil {
		return ForkSessionResult{}, err
	}
	return ForkSessionResult{SessionID: forkedSessionID}, nil
}

func appendToSession(sessionID string, data string, directory string) error {
	fileName := sessionID + ".jsonl"
	if directory != "" {
		canonicalDir := canonicalizePath(directory)
		if projectDir, ok := findProjectDir(canonicalDir); ok {
			appended, err := tryAppend(filepath.Join(projectDir, fileName), data)
			if err != nil {
				return err
			}
			if appended {
				return nil
			}
		}
		for _, worktree := range getWorktreePaths(canonicalDir) {
			if worktree == canonicalDir {
				continue
			}
			if projectDir, ok := findProjectDir(worktree); ok {
				appended, err := tryAppend(filepath.Join(projectDir, fileName), data)
				if err != nil {
					return err
				}
				if appended {
					return nil
				}
			}
		}
		return fmt.Errorf("session %s not found in project directory for %s: %w", sessionID, directory, os.ErrNotExist)
	}

	entries, err := os.ReadDir(getProjectsDir())
	if err != nil {
		return fmt.Errorf("session %s not found (no projects directory): %w", sessionID, os.ErrNotExist)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		appended, err := tryAppend(filepath.Join(getProjectsDir(), entry.Name(), fileName), data)
		if err != nil {
			return err
		}
		if appended {
			return nil
		}
	}
	return fmt.Errorf("session %s not found in any project directory: %w", sessionID, os.ErrNotExist)
}

func tryAppend(path string, data string) (bool, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && (errors.Is(pathErr.Err, os.ErrNotExist) || errors.Is(pathErr.Err, syscall.ENOTDIR)) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return false, err
	}
	_, err = file.WriteString(data)
	if err != nil {
		return false, err
	}
	return true, nil
}

func sanitizeUnicode(value string) string {
	current := value
	for i := 0; i < maxSanitizePasses; i++ {
		previous := current
		current = strings.Map(func(r rune) rune {
			switch {
			case r == '\uFEFF':
				return -1
			case r >= '\u200B' && r <= '\u200F':
				return -1
			case r >= '\u202A' && r <= '\u202E':
				return -1
			case r >= '\u2066' && r <= '\u2069':
				return -1
			case r >= '\uE000' && r <= '\uF8FF':
				return -1
			case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Co, r):
				return -1
			case r == '\u3000':
				return ' '
			case r >= '\uFF01' && r <= '\uFF5E':
				return r - 0xFEE0
			default:
				return r
			}
		}, current)
		if current == previous {
			break
		}
	}
	return current
}

var forkEntryTypes = map[string]struct{}{
	"user":       {},
	"assistant":  {},
	"attachment": {},
	"system":     {},
	"progress":   {},
}

func parseForkEntries(content []byte, sessionID string, upToMessageID string) ([]map[string]any, error) {
	entries := make([]map[string]any, 0)
	for _, line := range splitLines(content) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entryType, _ := entry["type"].(string)
		if _, ok := forkEntryTypes[entryType]; !ok {
			continue
		}
		if _, ok := entry["uuid"].(string); !ok {
			continue
		}
		if isSidechain, _ := entry["isSidechain"].(bool); isSidechain {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("session %s has no messages to fork", sessionID)
	}
	if upToMessageID == "" {
		return entries, nil
	}
	cutoff := -1
	for i, entry := range entries {
		if entry["uuid"] == upToMessageID {
			cutoff = i
			break
		}
	}
	if cutoff < 0 {
		return nil, fmt.Errorf("message %s not found in session %s", upToMessageID, sessionID)
	}
	return entries[:cutoff+1], nil
}

func buildForkLines(entries []map[string]any, sessionID string, forkedSessionID string, title string) ([]string, error) {
	uuidMapping := make(map[string]string, len(entries))
	byUUID := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		uuid, _ := entry["uuid"].(string)
		remapped, err := newUUID()
		if err != nil {
			return nil, err
		}
		uuidMapping[uuid] = remapped
		byUUID[uuid] = entry
	}

	writable := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry["type"] != "progress" {
			writable = append(writable, entry)
		}
	}
	if len(writable) == 0 {
		return nil, fmt.Errorf("session %s has no messages to fork", sessionID)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	lines := make([]string, 0, len(writable)+1)
	for i, original := range writable {
		originalUUID, _ := original["uuid"].(string)
		forked := cloneForkEntry(original)
		forked["uuid"] = uuidMapping[originalUUID]
		forked["parentUuid"] = remapForkParent(original, byUUID, uuidMapping)
		if logicalParent, _ := original["logicalParentUuid"].(string); logicalParent != "" {
			if remapped, ok := uuidMapping[logicalParent]; ok {
				forked["logicalParentUuid"] = remapped
			}
		}
		forked["sessionId"] = forkedSessionID
		if i == len(writable)-1 {
			forked["timestamp"] = now
		} else if _, ok := forked["timestamp"]; !ok {
			forked["timestamp"] = now
		}
		forked["isSidechain"] = false
		forked["forkedFrom"] = map[string]any{
			"sessionId":   sessionID,
			"messageUuid": originalUUID,
		}
		delete(forked, "teamName")
		delete(forked, "agentName")
		delete(forked, "slug")
		delete(forked, "sourceToolAssistantUUID")

		encoded, err := json.Marshal(forked)
		if err != nil {
			return nil, err
		}
		lines = append(lines, string(encoded))
	}

	forkTitle := strings.TrimSpace(title)
	if forkTitle == "" {
		forkTitle = "Forked session"
	}
	titleUUID, err := newUUID()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(map[string]any{
		"type":        "custom-title",
		"sessionId":   forkedSessionID,
		"customTitle": forkTitle,
		"uuid":        titleUUID,
		"timestamp":   now,
	})
	if err != nil {
		return nil, err
	}
	lines = append(lines, string(encoded))
	return lines, nil
}

func remapForkParent(original map[string]any, byUUID map[string]map[string]any, uuidMapping map[string]string) any {
	parentID, _ := original["parentUuid"].(string)
	for parentID != "" {
		parent := byUUID[parentID]
		if parent == nil {
			return nil
		}
		if parent["type"] != "progress" {
			if remapped, ok := uuidMapping[parentID]; ok {
				return remapped
			}
			return nil
		}
		parentID, _ = parent["parentUuid"].(string)
	}
	return nil
}

func cloneForkEntry(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
