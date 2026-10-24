package agenthub

import (
	"context"
	"testing"
)

func init() {
	conformanceCases = append(conformanceCases,
		ConformanceCase{
			Name: "open",
			Run: func(t *testing.T, v VendorQuirks) {
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				if rt.ACPSessionID() == "" {
					t.Fatal("ACPSessionID is empty")
				}
				_ = rt.Close(context.Background())
			},
		},
		ConformanceCase{Name: "deferred_resume_never_prompted_service_followup_resume", Run: func(t *testing.T, _ VendorQuirks) {
			t.Skip("harness not implemented, tracked as follow-up B3-FOLLOWUP-RESUME")
		}},
		ConformanceCase{Name: "deferred_opencode_process_death_followup_process_death", SkipIf: func(v VendorQuirks) (bool, string) {
			if v.Name != "opencode" {
				return true, "only opencode process-death harness is deferred"
			}
			return false, ""
		}, Run: func(t *testing.T, _ VendorQuirks) {
			t.Skip("harness not implemented, tracked as follow-up B3-FOLLOWUP-OPENCODE-DEATH")
		}},
		ConformanceCase{
			Name: "load_resume",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.LoadResume {
					return true, "vendor has no SessionLoader"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				// First open a session to get an ACP session ID
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open for load: %v", err)
				}
				id := rt.ACPSessionID()
				_ = rt.Close(context.Background())

				// Now load/resume it
				rt2, err := v.Load(t, id)
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if rt2.ACPSessionID() != id {
					t.Fatalf("ACPSessionID = %q, want %q", rt2.ACPSessionID(), id)
				}
				_ = rt2.Close(context.Background())
			},
			RequiredCap: CapLoadResume,
		},
		ConformanceCase{
			Name: "load_resume_never_prompted",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.LoadResume {
					return true, "vendor has no SessionLoader"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				// A session that was never prompted (no turns) must be opened with Open,
				// not Load. The service does this; here we verify the adapter doesn't
				// require a session ID for a never-prompted session.
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open (never prompted): %v", err)
				}
				if rt.ACPSessionID() == "" {
					t.Fatal("ACPSessionID is empty")
				}
				_ = rt.Close(context.Background())
			},
			RequiredCap: CapLoadResume,
		},
	)
}
