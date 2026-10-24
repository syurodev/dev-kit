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
	r.mu.Unlock()
	r.emitMode(update)
}

// observeModeUpdate keeps Modes current for vendor-initiated changes too. Only
// the runtime's own notifications pass here, never a vendor subagent's mode.
func (r *acpRuntime) observeModeUpdate(update, meta json.RawMessage) bool {
	parsed := ParseSessionUpdate(update)
	if parsed.Kind != UpdateCurrentMode {
		return true
	}
	if r.vendorType() != AgentTypeClaude {
		return false
	}
	if parsed.Mode == "" {
		return true
	}
	r.mu.Lock()
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
		r.mu.Unlock()
		return false
	}
	var announcement *modeAnnouncement
	if changed {
		announcement = &modeAnnouncement{r.modeRevision, update, meta, r.handlers.OnUpdate}
	}
	r.mu.Unlock()
	r.emitMode(announcement)
	return false // own mode updates are forwarded only through emitMode
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
func (r *acpRuntime) takePendingModeLocked() *modeAnnouncement {
	if r.vendorType() != AgentTypeClaude {
		r.pendingMode = false
		return nil
	}
	if !r.pendingMode || r.handlers.OnUpdate == nil || r.modes == nil {
		return nil
	}
	r.pendingMode = false
	return &modeAnnouncement{
		revision: r.modeRevision,
		update: marshalUpdate(map[string]any{
			"sessionUpdate":  string(UpdateCurrentMode),
			"currentModeId":  r.modes.CurrentModeID,
			"availableModes": r.modes.AvailableModes,
		}),
		onUpdate: r.handlers.OnUpdate,
	}
}

// Snapshot each announcement and its revision under mu. All mode callbacks
// share this sequencer: an older snapshot can run first, or be discarded after
// a newer one, but cannot overwrite a newer record. Keep mu free for callbacks
// that read Modes or other runtime state.
type modeAnnouncement struct {
	revision uint64
	update   json.RawMessage
	meta     json.RawMessage
	onUpdate func(json.RawMessage, json.RawMessage)
}

func (r *acpRuntime) emitMode(announcement *modeAnnouncement) {
	if announcement == nil {
		return
	}
	r.modeEmitMu.Lock()
	defer r.modeEmitMu.Unlock()
	if announcement.revision <= r.modeEmittedRevision {
		return
	}
	r.modeEmittedRevision = announcement.revision
	announcement.onUpdate(announcement.update, announcement.meta)
}
