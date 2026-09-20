package apiclient

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"devkit/internal/vaultitem"
)

// maxCollectionDepth bounds the parent walk. A cycle is refused outright, but
// a deep tree still has to terminate, and nothing useful is 32 folders deep.
const maxCollectionDepth = 32

// SaveCollection creates or renames a folder of requests.
//
// A parent that does not exist, a folder that is its own parent, and a cycle
// are all refused. A cycle is not a cosmetic problem: everything that walks
// this tree — the sidebar, the delete check below — would walk it forever.
func (s *Service) SaveCollection(c Collection) (Collection, error) {
	if strings.TrimSpace(c.Name) == "" {
		return Collection{}, ErrInvalid
	}
	if c.ParentID != "" {
		if c.ParentID == c.ID {
			return Collection{}, ErrInvalid
		}
		existing, err := s.Collections()
		if err != nil {
			return Collection{}, err
		}
		byID := map[string]Collection{}
		for _, candidate := range existing {
			byID[candidate.ID] = candidate
		}
		if _, ok := byID[c.ParentID]; !ok {
			return Collection{}, ErrNotFound
		}
		// Walk up from the proposed parent: reaching this folder means the
		// move would close a loop.
		for at, depth := c.ParentID, 0; at != "" && depth < maxCollectionDepth; depth++ {
			if at == c.ID {
				return Collection{}, ErrInvalid
			}
			at = byID[at].ParentID
		}
	}

	raw, err := json.Marshal(collectionMetadata{
		Name: c.Name, ParentID: c.ParentID, Order: c.Order,
	})
	if err != nil {
		return Collection{}, err
	}
	var record vaultitem.Record
	if c.ID == "" {
		// A folder holds no secret, but a vault item must carry a valid
		// envelope, so it seals an empty one rather than becoming a special
		// case in sync.
		record, err = s.items.Create(vaultitem.ItemTypeAPICollection, KeyIDCollection, raw, []byte{})
	} else {
		record, err = s.items.Update(c.ID, KeyIDCollection, raw, []byte{})
	}
	if err != nil {
		return Collection{}, err
	}
	c.ID = record.ID
	return c, nil
}

// Collections lists the folders, by explicit order then name.
func (s *Service) Collections() ([]Collection, error) {
	records, err := s.items.ListByType(vaultitem.ItemTypeAPICollection)
	if err != nil {
		return nil, err
	}
	out := make([]Collection, 0, len(records))
	for _, record := range records {
		var meta collectionMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		out = append(out, Collection{
			ID: record.ID, Name: meta.Name, ParentID: meta.ParentID, Order: meta.Order,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// DeleteCollection removes an empty folder.
//
// It refuses while anything is still inside and says what, rather than taking
// the requests with it. Deleting a folder is a tidying action; losing every
// saved request in it is not, and a vault item has no undo. It is the same
// rule accounts keeps for an account something still binds.
func (s *Service) DeleteCollection(id string) error {
	if id == "" {
		return ErrInvalid
	}
	holding, err := s.CollectionContents(id)
	if err != nil {
		return err
	}
	if len(holding) > 0 {
		return fmt.Errorf("%w: %s", ErrCollectionNotEmpty, strings.Join(holding, ", "))
	}
	return s.items.Delete(id)
}

// CollectionContents describes what a folder directly holds, for the message
// a refused delete carries.
func (s *Service) CollectionContents(id string) ([]string, error) {
	if id == "" {
		return nil, nil
	}
	var holding []string
	children, err := s.Collections()
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if child.ParentID == id {
			holding = append(holding, "folder "+child.Name)
		}
	}
	requests, err := s.Requests()
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		if request.CollectionID == id {
			holding = append(holding, request.Name)
		}
	}
	return holding, nil
}
