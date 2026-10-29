package app

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/audit"
	"devkit/internal/harnessconfig"
)

// The adapter copies a proposal outcome's codes, ids and revisions into the
// audit trail under one fixed subject and capability, and nothing else.
func TestHarnessAuditSinkMapsAnOutcomeToFixedAuditFields(t *testing.T) {
	sink := audit.NewMemorySink()
	record := HarnessAuditSink(sink)
	if record == nil {
		t.Fatal("a sink must give a callback")
	}
	record(harnessconfig.Event{Outcome: "applied", Tier: "ordinary", Target: harnessconfig.TargetSession,
		Caller: "agent:s1", WorkspaceID: "w1", SessionID: "s1", Profile: 4, Override: 2, Publication: "installed"})
	record(harnessconfig.Event{Outcome: "harness.proposalStale", Target: harnessconfig.TargetWorkspace, WorkspaceID: "w1"})

	events := sink.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	ok, refused := events[0], events[1]
	if ok.SubjectID != "builtin.harness" || ok.Capability != "harness:configure" || ok.Scope != "session:s1" ||
		ok.Outcome != audit.OutcomeAllowed || ok.Caller != "agent:s1" || ok.At.IsZero() {
		t.Errorf("applied row = %+v", ok)
	}
	want := map[string]any{"action": "proposal", "result": "applied", "tier": "ordinary", "target": "session",
		"workspace_id": "w1", "session_id": "s1", "profile_revision": int64(4), "override_revision": int64(2), "publication": "installed"}
	if !reflect.DeepEqual(ok.Metadata, want) {
		t.Errorf("applied metadata = %v", ok.Metadata)
	}
	if refused.Outcome != audit.OutcomeDenied || refused.Scope != "workspace:w1" || refused.Caller != "unknown" ||
		refused.Metadata["result"] != "harness.proposalStale" {
		t.Errorf("refused row = %+v", refused)
	}
	if HarnessAuditSink(nil) != nil {
		t.Error("no sink must give no callback")
	}
}

// The metadata carries a fixed set of keys: adding a free-form field to the audit
// mapping is a visible change here. The keys are checked against the redaction
// list too, so none of them is blanked by it.
func TestHarnessAuditMetadataKeysAreFixedAndNeverRedacted(t *testing.T) {
	sink := audit.NewMemorySink()
	HarnessAuditSink(sink)(harnessconfig.Event{Outcome: "applied", WorkspaceID: "w", SessionID: "s"})
	keys := []string{}
	for k, v := range sink.Events()[0].Metadata {
		keys = append(keys, k)
		if v == audit.RedactedValue {
			t.Errorf("%s was redacted", k)
		}
	}
	sort.Strings(keys)
	want := []string{"action", "override_revision", "profile_revision", "publication", "result", "session_id", "target", "tier", "workspace_id"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v", keys)
	}
}

// Through the composed graph: an approved change and a refused one each leave one
// row in the trail the application hands the assembly, with ids and revisions
// and no group name, value or approval text.
func TestComposedProposalOutcomesReachTheAuditSink(t *testing.T) {
	sink := audit.NewMemorySink()
	r := newCompRig(t, compOptions{auditSink: sink})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)

	r.prompter.answer = "allow"
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	r.prompter.answer = "deny"
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err == nil {
		t.Fatal("the denied change must be refused")
	}

	rows := []audit.Event{}
	for _, e := range sink.Events() {
		if e.SubjectID == "builtin.harness" {
			rows = append(rows, e)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("harness rows = %d: %+v", len(rows), sink.Events())
	}
	if rows[0].Outcome != audit.OutcomeAllowed || rows[0].Metadata["result"] != "applied" || rows[0].Scope != "session:"+sess.ID ||
		rows[0].Metadata["session_id"] != sess.ID || rows[0].Metadata["override_revision"] == int64(0) {
		t.Errorf("applied row = %+v", rows[0])
	}
	if rows[1].Outcome != audit.OutcomeDenied || rows[1].Metadata["result"] == "applied" {
		t.Errorf("denied row = %+v", rows[1])
	}
	for _, row := range rows {
		for _, v := range row.Metadata {
			if s, ok := v.(string); ok && strings.Contains(s, "vault") {
				t.Errorf("a group name reached the trail: %+v", row)
			}
		}
	}
}
