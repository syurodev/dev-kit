package app

import (
	"time"

	"devkit/internal/audit"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
)

// Fixed audit identity of a harness proposal outcome. It follows the other
// built-in rows (subject "builtin.<module>", capability "<module>:<verb>").
const (
	harnessAuditSubject    = "builtin.harness"
	harnessAuditCapability = "harness:configure"
)

// HarnessAuditSink adapts the audit sink the other services write to into the
// assembly's Audit callback. A harnessconfig.Event is codes, ids, revisions and
// a tier by construction; this mapping copies exactly those fields and nothing
// else, so no profile value, command, path, rationale or diff text can reach the
// trail through it. A nil sink gives a nil callback, which the service accepts.
func HarnessAuditSink(sink audit.Sink) func(harnessconfig.Event) {
	if sink == nil {
		return nil
	}
	return func(ev harnessconfig.Event) {
		caller := ev.Caller
		if caller == "" {
			caller = identity.Unknown
		}
		scope := "workspace:" + ev.WorkspaceID
		if ev.SessionID != "" {
			scope = "session:" + ev.SessionID
		}
		// The audit package has two outcomes, both about authorization. A commit
		// whose result is unknown (the write may have landed) was approved by the
		// person and is not a refusal, so it is recorded as allowed, and the row
		// says in its own fields ("result" and "commit" are "unknown") that the
		// write is unconfirmed. Recording it as denied would claim nothing was
		// written. No new top-level outcome is warranted for that.
		outcome, commit := audit.OutcomeDenied, "none"
		switch ev.Outcome {
		case "applied":
			outcome, commit = audit.OutcomeAllowed, "committed"
		case "unknown":
			outcome, commit = audit.OutcomeAllowed, "unknown"
		}
		action := "proposal"
		if ev.Target == harnessconfig.TargetRepair || ev.Target == harnessconfig.TargetSessionRepair {
			action = "stale_repair"
		}
		sink.Record(audit.Event{
			Caller: caller, At: time.Now().UTC(),
			SubjectID: harnessAuditSubject, Capability: harnessAuditCapability,
			Scope: scope, Outcome: outcome,
			Metadata: map[string]any{
				"action": action, "result": ev.Outcome, "commit": commit, "tier": ev.Tier, "target": string(ev.Target),
				"workspace_id": ev.WorkspaceID, "session_id": ev.SessionID,
				"profile_revision": ev.Profile, "override_revision": ev.Override, "publication": ev.Publication,
			},
		})
	}
}
