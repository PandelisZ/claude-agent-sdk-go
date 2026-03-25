package sessions

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type liteSessionFile struct {
	mtime int64
	size  int64
	head  string
	tail  string
}

var (
	skipFirstPromptPattern = regexp.MustCompile(`(?s)^(?:<local-command-stdout>|<session-start-hook>|<tick>|<goal>|\[Request interrupted by user[^\]]*\]|\s*<ide_opened_file>.*</ide_opened_file>\s*$|\s*<ide_selection>.*</ide_selection>\s*$)`)
	commandNamePattern     = regexp.MustCompile(`<command-name>(.*?)</command-name>`)
)

func readSessionLite(path string) (*liteSessionFile, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return nil, false
	}

	headBuf := make([]byte, liteReadBufSize)
	headN, err := file.Read(headBuf)
	if err != nil && err != io.EOF || headN == 0 {
		return nil, false
	}
	head := string(headBuf[:headN])

	tailOffset := stat.Size() - liteReadBufSize
	if tailOffset < 0 {
		tailOffset = 0
	}

	tail := head
	if tailOffset > 0 {
		if _, err := file.Seek(tailOffset, 0); err != nil {
			return nil, false
		}
		tailBuf := make([]byte, liteReadBufSize)
		tailN, err := file.Read(tailBuf)
		if err != nil && err != io.EOF || tailN == 0 {
			return nil, false
		}
		tail = string(tailBuf[:tailN])
	}

	return &liteSessionFile{
		mtime: stat.ModTime().UnixMilli(),
		size:  stat.Size(),
		head:  head,
		tail:  tail,
	}, true
}

func unescapeJSONString(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var decoded string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &decoded); err == nil {
		return decoded
	}
	return raw
}

func extractJSONStringField(text string, key string) *string {
	patterns := []string{`"` + key + `":"`, `"` + key + `": "`}
	for _, pattern := range patterns {
		idx := strings.Index(text, pattern)
		if idx < 0 {
			continue
		}
		valueStart := idx + len(pattern)
		for i := valueStart; i < len(text); i++ {
			switch text[i] {
			case '\\':
				i++
			case '"':
				value := unescapeJSONString(text[valueStart:i])
				return &value
			}
		}
	}
	return nil
}

func extractLastJSONStringField(text string, key string) *string {
	patterns := []string{`"` + key + `":"`, `"` + key + `": "`}
	var result *string
	for _, pattern := range patterns {
		searchFrom := 0
		for {
			idx := strings.Index(text[searchFrom:], pattern)
			if idx < 0 {
				break
			}
			idx += searchFrom
			valueStart := idx + len(pattern)
			for i := valueStart; i < len(text); i++ {
				switch text[i] {
				case '\\':
					i++
				case '"':
					value := unescapeJSONString(text[valueStart:i])
					copied := value
					result = &copied
					searchFrom = i + 1
					goto nextMatch
				}
			}
			searchFrom = len(text)
		nextMatch:
			if searchFrom >= len(text) {
				break
			}
		}
	}
	return result
}

func extractFirstPromptFromHead(head string) *string {
	var commandFallback string
	for _, line := range strings.Split(head, "\n") {
		if !strings.Contains(line, `"type":"user"`) && !strings.Contains(line, `"type": "user"`) {
			continue
		}
		if strings.Contains(line, `"tool_result"`) {
			continue
		}
		if strings.Contains(line, `"isMeta":true`) || strings.Contains(line, `"isMeta": true`) {
			continue
		}
		if strings.Contains(line, `"isCompactSummary":true`) || strings.Contains(line, `"isCompactSummary": true`) {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["type"] != "user" {
			continue
		}
		message, ok := entry["message"].(map[string]any)
		if !ok {
			continue
		}

		texts := make([]string, 0, 2)
		switch content := message["content"].(type) {
		case string:
			texts = append(texts, content)
		case []any:
			for _, block := range content {
				blockMap, ok := block.(map[string]any)
				if !ok {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				blockText, _ := blockMap["text"].(string)
				if blockType == "text" && blockText != "" {
					texts = append(texts, blockText)
				}
			}
		}

		for _, raw := range texts {
			candidate := strings.TrimSpace(strings.ReplaceAll(raw, "\n", " "))
			if candidate == "" {
				continue
			}
			if matches := commandNamePattern.FindStringSubmatch(candidate); len(matches) == 2 {
				if commandFallback == "" {
					commandFallback = matches[1]
				}
				continue
			}
			if skipFirstPromptPattern.MatchString(candidate) {
				continue
			}
			runes := []rune(candidate)
			if len(runes) > 200 {
				value := strings.TrimRight(string(runes[:200]), " \t\r\n") + "…"
				return &value
			}
			value := candidate
			return &value
		}
	}

	if commandFallback != "" {
		return &commandFallback
	}
	return nil
}

func parseSessionInfoFromLite(sessionID string, lite *liteSessionFile, projectPath string) *SessionInfo {
	if lite == nil {
		return nil
	}

	firstLine, _, _ := strings.Cut(lite.head, "\n")
	if strings.Contains(firstLine, `"isSidechain":true`) || strings.Contains(firstLine, `"isSidechain": true`) {
		return nil
	}

	customTitle := extractLastJSONStringField(lite.tail, "customTitle")
	if customTitle == nil {
		customTitle = extractLastJSONStringField(lite.head, "customTitle")
	}
	if customTitle == nil {
		customTitle = extractLastJSONStringField(lite.tail, "aiTitle")
	}
	if customTitle == nil {
		customTitle = extractLastJSONStringField(lite.head, "aiTitle")
	}

	firstPrompt := extractFirstPromptFromHead(lite.head)

	summary := customTitle
	if summary == nil {
		summary = extractLastJSONStringField(lite.tail, "lastPrompt")
	}
	if summary == nil {
		summary = extractLastJSONStringField(lite.tail, "summary")
	}
	if summary == nil {
		summary = firstPrompt
	}
	if summary == nil || *summary == "" {
		return nil
	}

	gitBranch := extractLastJSONStringField(lite.tail, "gitBranch")
	if gitBranch == nil {
		gitBranch = extractJSONStringField(lite.head, "gitBranch")
	}
	cwd := extractJSONStringField(lite.head, "cwd")
	if cwd == nil && projectPath != "" {
		copied := projectPath
		cwd = &copied
	}

	var tag *string
	tailLines := strings.Split(lite.tail, "\n")
	for i := len(tailLines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(tailLines[i])
		if strings.HasPrefix(line, `{"type":"tag"`) {
			tag = extractLastJSONStringField(line, "tag")
			if tag != nil && *tag == "" {
				tag = nil
			}
			break
		}
	}

	var createdAt *int64
	if timestamp := extractJSONStringField(firstLine, "timestamp"); timestamp != nil {
		if parsed, err := time.Parse(time.RFC3339Nano, *timestamp); err == nil {
			value := parsed.UnixMilli()
			createdAt = &value
		}
	}

	return &SessionInfo{
		SessionID:    sessionID,
		Summary:      *summary,
		LastModified: lite.mtime,
		FileSize:     lite.size,
		CustomTitle:  customTitle,
		FirstPrompt:  firstPrompt,
		GitBranch:    gitBranch,
		Cwd:          cwd,
		Tag:          tag,
		CreatedAt:    createdAt,
	}
}

func readSessionsFromDir(projectDir string, projectPath string) []SessionInfo {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}

	results := make([]SessionInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		if !validateUUID(sessionID) {
			continue
		}
		lite, ok := readSessionLite(filepath.Join(projectDir, entry.Name()))
		if !ok {
			continue
		}
		info := parseSessionInfoFromLite(sessionID, lite, projectPath)
		if info != nil {
			results = append(results, *info)
		}
	}
	return results
}

func deduplicateBySessionID(sessions []SessionInfo) []SessionInfo {
	byID := make(map[string]SessionInfo, len(sessions))
	for _, session := range sessions {
		existing, ok := byID[session.SessionID]
		if !ok || session.LastModified > existing.LastModified {
			byID[session.SessionID] = session
		}
	}
	deduped := make([]SessionInfo, 0, len(byID))
	for _, session := range byID {
		deduped = append(deduped, session)
	}
	return deduped
}

func applySortAndLimit(sessions []SessionInfo, limit int) []SessionInfo {
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastModified == sessions[j].LastModified {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].LastModified > sessions[j].LastModified
	})
	if limit > 0 && len(sessions) > limit {
		return sessions[:limit]
	}
	return sessions
}

func listSessionsForProject(directory string, limit int, includeWorktrees bool) ([]SessionInfo, error) {
	canonicalDir := canonicalizePath(directory)
	worktreePaths := []string(nil)
	if includeWorktrees {
		worktreePaths = getWorktreePaths(canonicalDir)
	}

	if len(worktreePaths) <= 1 {
		projectDir, ok := findProjectDir(canonicalDir)
		if !ok {
			return nil, nil
		}
		return applySortAndLimit(readSessionsFromDir(projectDir, canonicalDir), limit), nil
	}

	projectsDir := getProjectsDir()
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		projectDir, ok := findProjectDir(canonicalDir)
		if !ok {
			return nil, nil
		}
		return applySortAndLimit(readSessionsFromDir(projectDir, canonicalDir), limit), nil
	}

	type indexedWorktree struct {
		path   string
		prefix string
	}
	indexed := make([]indexedWorktree, 0, len(worktreePaths))
	for _, worktree := range worktreePaths {
		indexed = append(indexed, indexedWorktree{
			path:   worktree,
			prefix: sanitizePath(worktree),
		})
	}
	sort.Slice(indexed, func(i, j int) bool {
		return len(indexed[i].prefix) > len(indexed[j].prefix)
	})

	allSessions := make([]SessionInfo, 0)
	seen := map[string]struct{}{}

	if projectDir, ok := findProjectDir(canonicalDir); ok {
		seen[filepath.Base(projectDir)] = struct{}{}
		allSessions = append(allSessions, readSessionsFromDir(projectDir, canonicalDir)...)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, alreadySeen := seen[entry.Name()]; alreadySeen {
			continue
		}
		for _, worktree := range indexed {
			if entry.Name() == worktree.prefix || (len(worktree.prefix) >= maxSanitizedLength && strings.HasPrefix(entry.Name(), worktree.prefix+"-")) {
				seen[entry.Name()] = struct{}{}
				allSessions = append(allSessions, readSessionsFromDir(filepath.Join(projectsDir, entry.Name()), worktree.path)...)
				break
			}
		}
	}

	return applySortAndLimit(deduplicateBySessionID(allSessions), limit), nil
}

func listAllSessions(limit int) ([]SessionInfo, error) {
	entries, err := os.ReadDir(getProjectsDir())
	if err != nil {
		return nil, nil
	}
	allSessions := make([]SessionInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			allSessions = append(allSessions, readSessionsFromDir(filepath.Join(getProjectsDir(), entry.Name()), "")...)
		}
	}
	return applySortAndLimit(deduplicateBySessionID(allSessions), limit), nil
}

func ListSessions(directory string, limit int, includeWorktrees bool) ([]SessionInfo, error) {
	if directory != "" {
		return listSessionsForProject(directory, limit, includeWorktrees)
	}
	return listAllSessions(limit)
}

func GetSessionInfo(sessionID string, directory string) (*SessionInfo, error) {
	if !validateUUID(sessionID) {
		return nil, nil
	}
	fileName := sessionID + ".jsonl"
	if directory != "" {
		canonicalDir := canonicalizePath(directory)
		if projectDir, ok := findProjectDir(canonicalDir); ok {
			if lite, ok := readSessionLite(filepath.Join(projectDir, fileName)); ok {
				return parseSessionInfoFromLite(sessionID, lite, canonicalDir), nil
			}
		}
		for _, worktree := range getWorktreePaths(canonicalDir) {
			if worktree == canonicalDir {
				continue
			}
			if projectDir, ok := findProjectDir(worktree); ok {
				if lite, ok := readSessionLite(filepath.Join(projectDir, fileName)); ok {
					return parseSessionInfoFromLite(sessionID, lite, worktree), nil
				}
			}
		}
		return nil, nil
	}

	entries, err := os.ReadDir(getProjectsDir())
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if lite, ok := readSessionLite(filepath.Join(getProjectsDir(), entry.Name(), fileName)); ok {
			return parseSessionInfoFromLite(sessionID, lite, ""), nil
		}
	}
	return nil, nil
}

func readSessionFile(sessionID string, directory string) ([]byte, error) {
	fileName := sessionID + ".jsonl"
	if directory != "" {
		canonicalDir := canonicalizePath(directory)
		if projectDir, ok := findProjectDir(canonicalDir); ok {
			if content, err := os.ReadFile(filepath.Join(projectDir, fileName)); err == nil && len(content) > 0 {
				return content, nil
			}
		}
		for _, worktree := range getWorktreePaths(canonicalDir) {
			if worktree == canonicalDir {
				continue
			}
			if projectDir, ok := findProjectDir(worktree); ok {
				if content, err := os.ReadFile(filepath.Join(projectDir, fileName)); err == nil && len(content) > 0 {
					return content, nil
				}
			}
		}
		return nil, nil
	}

	entries, err := os.ReadDir(getProjectsDir())
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(getProjectsDir(), entry.Name(), fileName))
		if err == nil && len(content) > 0 {
			return content, nil
		}
	}
	return nil, nil
}

func splitLines(content []byte) []string {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(content, []byte("\n"))
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, string(line))
	}
	return out
}
