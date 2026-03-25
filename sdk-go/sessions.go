package claudeagentsdk

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	internalsessions "github.com/anthropics/claude-agent-sdk-python/sdk-go/internal/sessions"
)

var sessionUUIDRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type SDKSessionInfo struct {
	SessionID    string
	Summary      string
	LastModified int64
	FileSize     int64
	CustomTitle  *string
	FirstPrompt  *string
	GitBranch    *string
	Cwd          *string
	Tag          *string
	CreatedAt    *int64
}

type SessionMessage struct {
	Type            string
	UUID            string
	SessionID       string
	Message         any
	ParentToolUseID *string
}

type ListSessionsOptions struct {
	Directory        string
	Limit            int
	IncludeWorktrees *bool
}

type SessionQueryOptions struct {
	Directory string
	Limit     int
	Offset    int
}

type SessionMutationOptions struct {
	Directory string
}

func ListSessions(options ListSessionsOptions) ([]SDKSessionInfo, error) {
	includeWorktrees := false
	if options.Directory != "" {
		includeWorktrees = true
		if options.IncludeWorktrees != nil {
			includeWorktrees = *options.IncludeWorktrees
		}
	}

	sessions, err := internalsessions.ListSessions(options.Directory, options.Limit, includeWorktrees)
	if err != nil {
		return nil, err
	}
	return convertSessionInfos(sessions), nil
}

func GetSessionInfo(sessionID string, options SessionQueryOptions) (*SDKSessionInfo, error) {
	if !isValidSessionUUID(sessionID) {
		return nil, fmt.Errorf("invalid session ID %q", sessionID)
	}
	info, err := internalsessions.GetSessionInfo(sessionID, options.Directory)
	if err != nil || info == nil {
		return nil, err
	}
	converted := convertSessionInfo(*info)
	return &converted, nil
}

func GetSessionMessages(sessionID string, options SessionQueryOptions) ([]SessionMessage, error) {
	if !isValidSessionUUID(sessionID) {
		return nil, fmt.Errorf("invalid session ID %q", sessionID)
	}
	messages, err := internalsessions.GetSessionMessages(sessionID, options.Directory, options.Limit, options.Offset)
	if err != nil {
		return nil, err
	}
	return convertSessionMessages(messages), nil
}

func RenameSession(sessionID string, title string, options SessionMutationOptions) error {
	if !isValidSessionUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	if strings.TrimSpace(title) == "" {
		return errors.New("title must be non-empty")
	}
	return internalsessions.RenameSession(sessionID, title, options.Directory)
}

func TagSession(sessionID string, tag *string, options SessionMutationOptions) error {
	if !isValidSessionUUID(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	return internalsessions.TagSession(sessionID, tag, options.Directory)
}

func isValidSessionUUID(value string) bool {
	return sessionUUIDRe.MatchString(value)
}

func convertSessionInfos(items []internalsessions.SessionInfo) []SDKSessionInfo {
	converted := make([]SDKSessionInfo, 0, len(items))
	for _, item := range items {
		converted = append(converted, convertSessionInfo(item))
	}
	return converted
}

func convertSessionInfo(item internalsessions.SessionInfo) SDKSessionInfo {
	return SDKSessionInfo{
		SessionID:    item.SessionID,
		Summary:      item.Summary,
		LastModified: item.LastModified,
		FileSize:     item.FileSize,
		CustomTitle:  item.CustomTitle,
		FirstPrompt:  item.FirstPrompt,
		GitBranch:    item.GitBranch,
		Cwd:          item.Cwd,
		Tag:          item.Tag,
		CreatedAt:    item.CreatedAt,
	}
}

func convertSessionMessages(items []internalsessions.SessionMessage) []SessionMessage {
	converted := make([]SessionMessage, 0, len(items))
	for _, item := range items {
		converted = append(converted, SessionMessage{
			Type:            item.Type,
			UUID:            item.UUID,
			SessionID:       item.SessionID,
			Message:         item.Message,
			ParentToolUseID: item.ParentToolUseID,
		})
	}
	return converted
}
