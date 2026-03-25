package sessions

type SessionInfo struct {
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
