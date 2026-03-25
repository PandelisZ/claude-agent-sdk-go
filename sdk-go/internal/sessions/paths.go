package sessions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	liteReadBufSize     = 65536
	maxSanitizedLength  = 200
	maxSanitizePasses   = 10
	projectsDirName     = "projects"
	defaultClaudeSubdir = ".claude"
)

var (
	uuidRe     = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

func validateUUID(value string) bool {
	return uuidRe.MatchString(value)
}

func simpleHash(value string) string {
	var hash int32
	for _, r := range value {
		hash = int32((int64(hash) << 5) - int64(hash) + int64(r))
	}
	if hash < 0 {
		hash = -hash
	}
	if hash == 0 {
		return "0"
	}

	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, 8)
	for hash > 0 {
		out = append(out, digits[hash%36])
		hash /= 36
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func sanitizePath(name string) string {
	sanitized := sanitizeRe.ReplaceAllString(name, "-")
	if len(sanitized) <= maxSanitizedLength {
		return sanitized
	}
	return sanitized[:maxSanitizedLength] + "-" + simpleHash(name)
}

func getClaudeConfigDir() string {
	if value := os.Getenv("CLAUDE_CONFIG_DIR"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(string(filepath.Separator), defaultClaudeSubdir)
	}
	return filepath.Join(home, defaultClaudeSubdir)
}

func getProjectsDir() string {
	return filepath.Join(getClaudeConfigDir(), projectsDirName)
}

func canonicalizePath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		if absolute, err := filepath.Abs(resolved); err == nil {
			return absolute
		}
		return resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

func findProjectDir(projectPath string) (string, bool) {
	projectsDir := getProjectsDir()
	exact := filepath.Join(projectsDir, sanitizePath(projectPath))
	if info, err := os.Stat(exact); err == nil && info.IsDir() {
		return exact, true
	}

	sanitized := sanitizePath(projectPath)
	if len(sanitized) <= maxSanitizedLength {
		return "", false
	}

	prefix := sanitized[:maxSanitizedLength] + "-"
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(projectsDir, entry.Name()), true
		}
	}
	return "", false
}

func getWorktreePaths(cwd string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, line[len("worktree "):])
		}
	}
	return paths
}
