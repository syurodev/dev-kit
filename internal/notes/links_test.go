package notes

import (
	"reflect"
	"testing"
)

func TestLinkedNoteIDsReadsTheSerializedForm(t *testing.T) {
	body := "See [[note:aabbccddeeff001122334455]] and [[note:0011223344556677889900aa]].\n" +
		"The first one again: [[note:aabbccddeeff001122334455]]"

	got := LinkedNoteIDs(body)

	// De-duplicated: two mentions of the same note are one edge, not two rows
	// that would both have to be cleaned up on purge.
	want := []string{"0011223344556677889900aa", "aabbccddeeff001122334455"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A plain markdown link and a wiki-style link to something that is not a note
// must not become edges — the index would then point at ids that never existed.
func TestLinkedNoteIDsIgnoresEverythingElse(t *testing.T) {
	body := "[a link](https://example.com) and [[some page]] and [[note:zzz]]"
	if got := LinkedNoteIDs(body); len(got) != 0 {
		t.Fatalf("want no links, got %v", got)
	}
}

func TestResolveLinksReportsTrashedTargetsInsteadOfHidingThem(t *testing.T) {
	s := newService(t)
	target, _ := s.Create("Q3 Planning", "body", "", nil)
	if err := s.Delete(target.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	resolved, err := s.ResolveLinks([]string{target.ID, "0011223344556677889900aa"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("want both ids answered, got %d", len(resolved))
	}
	// List and Search hide trashed notes. Resolve must not: the link keeps its
	// place in a body the user never edited, and renders struck through.
	if !resolved[0].Deleted || resolved[0].Title != "Q3 Planning" {
		t.Fatalf("trashed target lost its title or its flag: %+v", resolved[0])
	}
	if !resolved[1].Missing {
		t.Fatalf("an id that resolves to nothing must say so: %+v", resolved[1])
	}
}

type memoryLinkIndex struct{ edges map[string][]string }

func (m *memoryLinkIndex) ReplaceLinks(from string, to []string) error {
	if m.edges == nil {
		m.edges = map[string][]string{}
	}
	m.edges[from] = to
	return nil
}

func (m *memoryLinkIndex) ListBacklinks(to string) ([]string, error) {
	var out []string
	for from, targets := range m.edges {
		for _, target := range targets {
			if target == to {
				out = append(out, from)
			}
		}
	}
	return out, nil
}

func (m *memoryLinkIndex) DeleteLinksFor(id string) error {
	delete(m.edges, id)
	for from, targets := range m.edges {
		kept := targets[:0]
		for _, target := range targets {
			if target != id {
				kept = append(kept, target)
			}
		}
		m.edges[from] = kept
	}
	return nil
}

func TestBacklinksComeFromTheIndexAndSkipTrashedSources(t *testing.T) {
	s := newService(t)
	s.SetLinkIndex(&memoryLinkIndex{})

	target, _ := s.Create("Target", "body", "", nil)
	live, _ := s.Create("Live source", "see [[note:"+target.ID+"]]", "", nil)
	trashed, _ := s.Create("Trashed source", "see [[note:"+target.ID+"]]", "", nil)
	if err := s.Delete(trashed.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	backlinks, err := s.ListBacklinks(target.ID)
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0].ID != live.ID {
		t.Fatalf("want only the live source, got %+v", backlinks)
	}
}
