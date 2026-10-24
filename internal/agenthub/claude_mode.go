package agenthub

import (
	"encoding/json"

	"devkit/internal/acp"
)

// Claude ACP 0.95 sends current_mode_update from set_mode only when it clamps
// the requested mode (e.g. auto → acceptEdits). Ordinary successes need our own
// announcement. A vendor notification wins over the requested id, and an echo
// arriving after our announcement must not record the same change twice.
func (r *acpRuntime) recordSetMode(id string, revision uint64) {
	r.mu.Lock()
	if r.modeRevision != revision || !r.changeModeLocked(id) {
		r.mu.Unlock()
		return
	}
	onUpdate := r.handlers.OnUpdate
	r.mu.Unlock()
	if onUpdate != nil {
		onUpdate(marshalUpdate(map[string]any{
			"sessionUpdate": string(UpdateCurrentMode), "currentModeId": id,
		}), nil)
	}
}

// observeModeUpdate keeps Modes current for vendor-initiated changes too. Only
// the runtime's own notifications pass here, never a vendor subagent's mode.
func (r *acpRuntime) observeModeUpdate(update json.RawMessage) bool {
	parsed := ParseSessionUpdate(update)
	if parsed.Kind != UpdateCurrentMode || parsed.Mode == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modeRevision++
	return r.changeModeLocked(parsed.Mode)
}

func (r *acpRuntime) changeModeLocked(id string) bool {
	if r.modes == nil {
		r.modes = &acp.SessionModeState{CurrentModeID: id}
		return true
	}
	if r.modes.CurrentModeID == id {
		return false
	}
	next := *r.modes
	next.CurrentModeID = id
	r.modes = &next
	return true
}
