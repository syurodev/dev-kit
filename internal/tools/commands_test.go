package tools

import (
	"regexp"
	"testing"
)

// The same rule internal/modules/registry.go enforces. Duplicated here on
// purpose: this package cannot import modules without a cycle, and the point is
// to fail in the package that owns the ids rather than at app startup.
var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

func TestCommandIDsAreValidIdentifiers(t *testing.T) {
	for _, command := range Commands {
		if !identifierPattern.MatchString(command.ID) {
			t.Errorf("command id %q would be rejected by the module registry", command.ID)
		}
	}
}

func TestCommandsAreCompleteAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, command := range Commands {
		if seen[command.ID] {
			t.Errorf("duplicate command id %q", command.ID)
		}
		seen[command.ID] = true
		if command.Title == "" {
			t.Errorf("command %q has no title; the registry refuses it", command.ID)
		}
		if command.Capability == "" {
			t.Errorf("command %q has no capability, so nothing guards it", command.ID)
		}
	}
	if len(Commands) != 7 {
		t.Errorf("Commands has %d entries, want 7 (hash + the six utils)", len(Commands))
	}
}

// tools.mdx: the util group shares one capability, and it is NOT the hash one.
// Reusing tools:hash would silently widen a grant written for something else.
func TestCapabilitySplit(t *testing.T) {
	for _, command := range Commands {
		capability, known := CapabilityFor(command.ID)
		if !known {
			t.Fatalf("CapabilityFor(%q) reported unknown for a listed command", command.ID)
		}
		want := CapabilityUtil
		if command.ID == CommandHash {
			want = CapabilityHash
		}
		if capability != want {
			t.Errorf("CapabilityFor(%q) = %q, want %q", command.ID, capability, want)
		}
	}
}

// An unknown id must not resolve to a capability. The bridge refuses to execute
// on false, so answering with a default here would authorise anything.
func TestCapabilityForRejectsUnknown(t *testing.T) {
	for _, id := range []string{"", "tools.nope", "notes.create", "tools"} {
		if capability, known := CapabilityFor(id); known {
			t.Errorf("CapabilityFor(%q) = %q, true; want unknown", id, capability)
		}
	}
}

func TestCapabilityIDsAreDistinct(t *testing.T) {
	ids := CapabilityIDs()
	if len(ids) != 2 || ids[0] != CapabilityHash || ids[1] != CapabilityUtil {
		t.Errorf("CapabilityIDs() = %v, want [%s %s]", ids, CapabilityHash, CapabilityUtil)
	}
}

func TestCommandIDsMatchTable(t *testing.T) {
	ids := CommandIDs()
	if len(ids) != len(Commands) {
		t.Fatalf("CommandIDs() has %d entries, Commands has %d", len(ids), len(Commands))
	}
	for i, id := range ids {
		if id != Commands[i].ID {
			t.Errorf("CommandIDs()[%d] = %q, want %q", i, id, Commands[i].ID)
		}
	}
}
