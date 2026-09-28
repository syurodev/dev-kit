package agenthub

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// devkitAgentLogEnv, when set to a file path, turns on a raw capture of every
// session/update this process receives, from any vendor and any session —
// nothing more than a way to gather real traffic for UI/design work without
// scripting a session by hand. Unset by default: logAgentUpdate is a no-op
// until a person opts in.
const devkitAgentLogEnv = "DEVKIT_AGENT_LOG_FILE"

var agentLogMu sync.Mutex

// logAgentUpdate appends one JSON line to DEVKIT_AGENT_LOG_FILE, if set.
// Errors are swallowed: a debug capture must never break a real turn, and
// there is nobody in this path to report them to anyway.
func logAgentUpdate(sessionID string, update json.RawMessage) {
	path := os.Getenv(devkitAgentLogEnv)
	if path == "" {
		return
	}
	line, err := json.Marshal(struct {
		Time      string          `json:"time"`
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}{
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		Update:    update,
	})
	if err != nil {
		return
	}

	agentLogMu.Lock()
	defer agentLogMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}
