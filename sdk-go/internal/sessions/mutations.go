package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

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
