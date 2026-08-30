package notes

import (
	"regexp"
	"sort"
)

// LinkIndex records which note references which. The notes service declares
// what it needs rather than importing a storage type.
type LinkIndex interface {
	ReplaceLinks(fromNoteID string, toNoteIDs []string) error
	ListBacklinks(toNoteID string) ([]string, error)
	DeleteLinksFor(noteID string) error
}

// SetLinkIndex wires the index. Without one the service still works — links
// simply are not indexed — which is what keeps every existing test and any
// caller that does not care about backlinks running unchanged.
func (s *Service) SetLinkIndex(index LinkIndex) {
	s.links = index
}

// noteLinkPattern matches the serialized form of a note-link node.
//
// Bodies are markdown, so the Plate inline void has to survive a round trip
// through text. `[[note:<id>]]` carries the id and no label — the label is
// resolved from the target's current title at render time, so renaming a note
// updates every link to it without rewriting a single body.
var noteLinkPattern = regexp.MustCompile(`\[\[note:([0-9a-fA-F]{8,64})\]\]`)

// LinkedNoteIDs pulls the note ids a body references, de-duplicated and in a
// stable order.
func LinkedNoteIDs(body string) []string {
	matches := noteLinkPattern.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		id := match[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// LinkTarget is what a link renders as: the target's title now, and whether it
// is in the trash.
type LinkTarget struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Deleted bool   `json:"deleted"`
	Missing bool   `json:"missing"`
}

// ResolveLinks reports the current title of each referenced note.
//
// Unlike List and Search this does not hide trashed notes. A link to a trashed
// note keeps its place in the source body and renders struck through; deleting
// the text would silently edit a note the user never touched, and the link
// comes back on its own if the target is restored.
func (s *Service) ResolveLinks(ids []string) ([]LinkTarget, error) {
	out := make([]LinkTarget, 0, len(ids))
	for _, id := range ids {
		record, err := s.store.Get(id)
		if err != nil {
			out = append(out, LinkTarget{ID: id, Missing: true})
			continue
		}
		if record.Type != itemType {
			out = append(out, LinkTarget{ID: id, Missing: true})
			continue
		}
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		out = append(out, LinkTarget{
			ID:      id,
			Title:   meta.Title,
			Deleted: meta.DeletedAt != "",
		})
	}
	return out, nil
}

// ListBacklinks reports the notes linking to this one, newest first. It reads
// the index rather than scanning bodies, so it costs the same at any vault size.
func (s *Service) ListBacklinks(id string) ([]Summary, error) {
	if s.links == nil {
		return []Summary{}, nil
	}
	fromIDs, err := s.links.ListBacklinks(id)
	if err != nil {
		return nil, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return nil, err
	}

	out := make([]Summary, 0, len(fromIDs))
	for _, fromID := range fromIDs {
		record, err := s.store.Get(fromID)
		if err != nil {
			continue
		}
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		// A trashed note is not a backlink anyone can act on.
		if meta.DeletedAt != "" {
			continue
		}
		summary, err := s.toSummary(record, folderMap)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// indexLinks records what a body references. Called from Create and Update,
// which already hold the plaintext body before it is sealed.
func (s *Service) indexLinks(fromID, body string) error {
	if s.links == nil {
		return nil
	}
	return s.links.ReplaceLinks(fromID, LinkedNoteIDs(body))
}
