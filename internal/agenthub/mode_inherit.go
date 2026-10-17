package agenthub

import (
	"context"
	"errors"
	"fmt"
	"log"

	"devkit/internal/acp"
)

// A session an agent creates starts in its parent's permission mode, never a
// more permissive one. Model and effort are inherited the same way
// (model_choice.go); a mode used to be left out, so a child opened in its
// vendor's default (Auto) whatever the parent was in — an Ask parent could
// spawn an Auto child, and a Full auto parent got an Auto one.
//
// The mode comes only from the parent's recorded state, read here in Go. The
// create tool has no mode input and an agent cannot name one.

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

// currentMode reads the mode a session's agent last announced (the stored
// current_mode_update row, seeded from the handshake and moved by every mode
// change). False when the session has none — a vendor without modes.
func (s *Service) currentMode(ctx context.Context, sess AgentSession) (parentMode, bool) {
	// The newest announcement may still be in the recorder's buffer.
	_ = s.recorder.Flush(ctx, sess.ID)
	entry, found, err := s.store.FindTranscriptKind(ctx, sess.ID, UpdateCurrentMode)
	if err != nil || !found {
		return parentMode{}, false
	}
	id := storedUpdate(entry).Mode
	return parentMode{Vendor: sess.AgentType, ID: id}, id != ""
}

// inheritedModeID picks the child's mode for a parent's. A same-vendor child
// takes the parent's id. Another vendor takes the same id when it advertises
// it, else the equivalent name (Claude default ↔ Codex ask); a parent stricter
// than that vendor's default (Claude plan or acceptEdits, which Codex has no
// match for) gets the child's asking mode rather than its more permissive
// default. "" with a reason when nothing fits.
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
