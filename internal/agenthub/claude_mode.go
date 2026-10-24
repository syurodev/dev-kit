package agenthub

import (
	"encoding/json"

	"devkit/internal/acp"
)

// Claude ACP 0.95 sends current_mode_update from set_mode only when it clamps
// the requested mode (e.g. auto → acceptEdits). Ordinary successes need our own
// announcement. If notifications race the request, keep the stricter mode.
// An echo arriving after our announcement must not record the change twice.
func (r *acpRuntime) recordSetMode(id string, revision uint64) {
	r.mu.Lock()
	if r.modeRevision != revision && r.modes != nil {
		// An unknown id ranks -1, so it is treated as strictest here. On a
		// tie keep the observed id. Synthetic changes also move the revision
		// so overlapping SetMode completions cannot overwrite a stricter mode.
		if modeRank(r.modes.CurrentModeID) <= modeRank(id) {
			id = r.modes.CurrentModeID
		}
	}
	if !r.changeModeLocked(id) {
		r.mu.Unlock()
		return
	}
	r.modeRevision++
	r.pendingMode = true
	update := r.takePendingModeLocked()
	onUpdate := r.handlers.OnUpdate
	r.mu.Unlock()
	if update != nil {
		onUpdate(update, nil)
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
	changed := r.changeModeLocked(parsed.Mode)
	if len(parsed.AvailableModes) > 0 {
		next := *r.modes
		next.AvailableModes = parsed.AvailableModes
		r.modes = &next
		changed = true
	}
	if r.handlers.OnUpdate == nil {
		// Coalesce changes before attachment to the latest effective mode.
		// SetHandlers publishes it once, including the available mode list.
		r.pendingMode = true
		return false
	}
	return changed
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

// takePendingModeLocked consumes the buffered announcement only when it can
// be delivered. The service recognises this update instead of seeding it again.
func (r *acpRuntime) takePendingModeLocked() json.RawMessage {
	if !r.pendingMode || r.handlers.OnUpdate == nil || r.modes == nil {
		return nil
	}
	r.pendingMode = false
	return marshalUpdate(map[string]any{
		"sessionUpdate":  string(UpdateCurrentMode),
		"currentModeId":  r.modes.CurrentModeID,
		"availableModes": r.modes.AvailableModes,
	})
}
