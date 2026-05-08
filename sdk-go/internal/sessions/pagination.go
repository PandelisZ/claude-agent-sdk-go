package sessions

import "sort"

func applySortLimitOffset(sessions []SessionInfo, limit int, offset int) []SessionInfo {
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].LastModified == sessions[j].LastModified {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].LastModified > sessions[j].LastModified
	})
	if offset < 0 {
		offset = 0
	}
	if offset >= len(sessions) {
		return []SessionInfo{}
	}
	if offset > 0 {
		sessions = sessions[offset:]
	}
	if limit > 0 && len(sessions) > limit {
		return sessions[:limit]
	}
	return sessions
}
