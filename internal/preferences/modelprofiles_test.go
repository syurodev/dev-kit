package preferences

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeModelProfilesFillsIDsAndRefusesWhatCannotBeStored(t *testing.T) {
	got, err := NormalizeModelProfiles([]AgentModelProfile{
		{Name: " Code review ", Vendor: "claude", Model: "sonnet", Effort: "low"},
		{Name: "Code review!", Vendor: "claude", Model: "haiku"},
		{ID: "docs", Name: "Docs", Vendor: "codex", Model: "gpt-x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "code-review" || got[1].ID != "code-review-2" || got[2].ID != "docs" || got[0].Name != "Code review" {
		t.Fatalf("normalized = %+v", got)
	}

	long := strings.Repeat("x", 801)
	for name, in := range map[string][]AgentModelProfile{
		"no model":     {{Name: "A", Vendor: "claude"}},
		"no vendor":    {{Name: "A", Model: "m"}},
		"no name":      {{Vendor: "claude", Model: "m"}},
		"control char": {{Name: "A\x07", Vendor: "claude", Model: "m"}},
		"long notes":   {{Name: "A", Vendor: "claude", Model: "m", Notes: long}},
		"same id":      {{ID: "a", Name: "A", Vendor: "c", Model: "m"}, {ID: "a", Name: "B", Vendor: "c", Model: "m"}},
		"same name":    {{Name: "A", Vendor: "c", Model: "m"}, {Name: "a", Vendor: "c", Model: "m"}},
	} {
		if _, err := NormalizeModelProfiles(in); !errors.Is(err, ErrInvalidModelProfiles) {
			t.Errorf("%s: err = %v, want ErrInvalidModelProfiles", name, err)
		}
	}
	many := make([]AgentModelProfile, 25)
	for i := range many {
		many[i] = AgentModelProfile{Name: string(rune('a' + i)), Vendor: "c", Model: "m"}
	}
	if _, err := NormalizeModelProfiles(many); !errors.Is(err, ErrInvalidModelProfiles) {
		t.Errorf("25 profiles: %v", err)
	}
	// A description may run to several lines; a name may not.
	if _, err := NormalizeModelProfiles([]AgentModelProfile{{Name: "A", Vendor: "c", Model: "m", Description: "line one\nline two"}}); err != nil {
		t.Errorf("multi-line description: %v", err)
	}
}

func TestModelProfilesAreStoredWithARevision(t *testing.T) {
	svc := NewService(NewMemoryRepository())
	first, err := svc.SetAgentModelProfiles(AgentModelProfiles{Profiles: []AgentModelProfile{{Name: "Docs", Vendor: "codex", Model: "m"}}})
	if err != nil || first.Revision != 1 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := svc.SetAgentModelProfiles(AgentModelProfiles{Revision: 0}); !errors.Is(err, ErrModelProfilesConflict) {
		t.Fatalf("stale write: %v", err)
	}
	got, err := svc.AgentModelProfiles()
	if err != nil || len(got.Profiles) != 1 || got.Profiles[0].ID != "docs" {
		t.Fatalf("read = %+v, %v", got, err)
	}
	// What a caller holds is its own copy.
	got.Profiles[0].Name = "changed"
	if again, _ := svc.AgentModelProfiles(); again.Profiles[0].Name != "Docs" {
		t.Fatal("the stored list was reachable through a returned copy")
	}
}
