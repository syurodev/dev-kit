package agenthub

import (
	"context"
	"errors"
	"fmt"
	"log"

	"devkit/internal/acp"
)

// A session an agent creates starts in its parent's permission mode (with the
// explicit cross-vendor acceptEdits → auto mapping below). Model and effort
// are inherited the same way (model_choice.go). Previously a child opened
// in its vendor's default (Auto) whatever the parent was in — an Ask parent could
// spawn an Auto child, and a Full auto parent got an Auto one.
//
// The mode comes only from the parent's live or recorded state, read here
// in Go. The create tool has no mode input and an agent cannot name one.

// Mode ids, ordered by how much each lets the agent do without asking. Claude
// and Codex share ids where they mean the same thing (auto, bypassPermissions);
// Claude's default and Codex's ask are the same strictness under two names.
// An id that is not here is unranked: it is carried over to a same-vendor child
// as it is, but never reasoned about.
const (
	modeRankPlan = iota // read only
	modeRankAsk         // asks before anything that changes something
	modeRankEdits
	modeRankAuto
	modeRankFull
)

var modeRanks = map[string]int{
	"plan":        modeRankPlan,
	"default":     modeRankAsk,
	codexModeAsk:  modeRankAsk,
	"acceptEdits": modeRankEdits,
	// Claude ACP parses dontAsk but does not advertise it. It refuses anything
	// outside pre-approved permissions; map conservatively to ask across vendors.
	"dontAsk":     modeRankAsk,
	"auto":        modeRankAuto,
	codexModeFull: modeRankFull, // "bypassPermissions", Claude's id too
}

// modeEquivalent pairs the two names for the strictest asking mode.
var modeEquivalent = map[string]string{"default": codexModeAsk, codexModeAsk: "default"}

func modeRank(id string) int {
	if r, ok := modeRanks[id]; ok {
		return r
	}
	return -1
}

// parentMode is a parent session's vendor and the mode it is in now.
type parentMode struct {
	Vendor AgentType
	ID     string
}

// currentMode prefers the live runtime, whose mode may be newer than the
// transcript. Without one, use the last recorded current_mode_update (also
// seeded from the handshake). False for a vendor without modes.
func (s *Service) currentMode(ctx context.Context, sess AgentSession) (parentMode, bool) {
	if rt := s.liveRuntime(ctx, sess.ID); rt != nil {
		if withModes, ok := rt.(RuntimeModes); ok {
			if modes := withModes.Modes(); modes != nil && modes.CurrentModeID != "" {
				return parentMode{Vendor: sess.AgentType, ID: modes.CurrentModeID}, true
			}
		}
	}
	// The newest announcement may still be in the recorder's buffer.
	_ = s.recorder.Flush(ctx, sess.ID)
	entry, found, err := s.store.FindTranscriptKind(ctx, sess.ID, UpdateCurrentMode)
	if err != nil || !found {
		return parentMode{}, false
	}
	id := storedUpdate(entry).Mode
	return parentMode{Vendor: sess.AgentType, ID: id}, id != ""
}

// inheritedModeID preserves a same-vendor id. Across vendors, Claude plan /
// default → ask, acceptEdits / auto → auto, bypassPermissions → itself; the
// reverse maps ask → default and auto → auto (acceptEdits on older Claude).
// acceptEdits → auto is an explicit product choice despite the higher rank:
// Codex and opencode have no edits-only mode. Unknown ids remain unranked.
func inheritedModeID(parent parentMode, child AgentType, advertised []acp.SessionMode) (id, why string) {
	if parent.Vendor == child {
		return parent.ID, ""
	}
	has := func(id string) bool {
		for _, m := range advertised {
			if m.ID == id {
				return true
			}
		}
		return false
	}
	if parent.Vendor == AgentTypeClaude && parent.ID == "acceptEdits" && has("auto") {
		return "auto", ""
	}
	if child == AgentTypeClaude && parent.ID == "auto" && !has("auto") && has("acceptEdits") {
		log.Printf("agenthub: Claude child does not advertise auto; inheriting acceptEdits instead")
		return "acceptEdits", ""
	}
	if has(parent.ID) {
		return parent.ID, ""
	}
	if other := modeEquivalent[parent.ID]; other != "" && has(other) {
		return other, ""
	}
	if rank := modeRank(parent.ID); rank >= 0 && rank < modeRankAuto {
		for _, safer := range []string{codexModeAsk, "default"} {
			if has(safer) {
				return safer, ""
			}
		}
	}
	return "", fmt.Sprintf("%s has no mode matching %s's %q", child, parent.Vendor, parent.ID)
}

// applyInheritedMode puts a freshly opened child in its parent's mode.
//
// The rule: a child is never left MORE permissive than its parent when a safer
// choice exists. A mode the child's runtime refuses does not fail the creation
// when the parent was not strict — the child starts on its vendor default and
// the log says so, as for an inherited model. When the parent is Ask / default
// or read only, the creation fails instead (ErrModeNotInherited) unless the
// child is already as strict: starting it more permissive would hand a
// restricted session a way past its own limit.
func applyInheritedMode(ctx context.Context, rt SessionRuntime, child AgentType, parent parentMode) error {
	var advertised []acp.SessionMode
	childNow := ""
	if withModes, ok := rt.(RuntimeModes); ok {
		if m := withModes.Modes(); m != nil {
			advertised, childNow = m.AvailableModes, m.CurrentModeID
		}
	}
	if len(advertised) == 0 && parent.Vendor != child {
		return nil // the child has no permission modes to be too permissive in
	}
	parentRank := modeRank(parent.ID)
	strict := parentRank >= 0 && parentRank <= modeRankAsk
	// Already as strict as the parent (or the very mode asked): nothing to set.
	asIsSafe := func() bool {
		childRank := modeRank(childNow)
		return childRank >= 0 && parentRank >= 0 && childRank <= parentRank
	}

	id, why := inheritedModeID(parent, child, advertised)
	if id == "" {
		log.Printf("agenthub: %s child session: parent's mode %q not applied: %s", child, parent.ID, why)
		if strict && !asIsSafe() {
			return fmt.Errorf("%w: %s", ErrModeNotInherited, why)
		}
		return nil
	}
	if id == childNow {
		return nil
	}
	err := errors.New("the runtime has no mode control")
	if configurator, ok := rt.(RuntimeConfigurator); ok {
		err = configurator.SetMode(ctx, id)
	}
	if err == nil {
		return nil
	}
	log.Printf("agenthub: %s child session: parent's mode %q not applied: %v", child, id, err)
	if strict && !asIsSafe() {
		return fmt.Errorf("%w: %q: %v", ErrModeNotInherited, id, err)
	}
	return nil
}
