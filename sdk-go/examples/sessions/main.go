package main

import (
	"fmt"
	"log"
	"os"

	claudeagentsdk "github.com/anthropics/claude-agent-sdk-python/sdk-go"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	sessions, err := claudeagentsdk.ListSessions(claudeagentsdk.ListSessionsOptions{
		Directory: cwd,
		Limit:     10,
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(sessions) == 0 {
		fmt.Println("no local sessions found")
		return
	}

	sessionID := sessions[0].SessionID
	info, err := claudeagentsdk.GetSessionInfo(sessionID, claudeagentsdk.SessionQueryOptions{Directory: cwd})
	if err != nil {
		log.Fatal(err)
	}
	messages, err := claudeagentsdk.GetSessionMessages(sessionID, claudeagentsdk.SessionQueryOptions{
		Directory: cwd,
		Limit:     20,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("session=%s summary=%s messages=%d\n", info.SessionID, info.Summary, len(messages))
}
