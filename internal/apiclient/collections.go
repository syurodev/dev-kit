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

	if c.ID == "" {
		raw, err := json.Marshal(collectionMetadata{Name: c.Name, ParentID: c.ParentID, Order: c.Order})
		if err != nil {
			return Collection{}, err
		}
		// Secrets are set through SetCollectionVariables; a new folder starts
		// with none. A folder with no variables still seals a payload rather
		// than becoming a special case in sync.
		payload, err := json.Marshal(collectionPayload{Vars: c.Vars})
		if err != nil {
			return Collection{}, err
		}
		record, err := s.items.Create(vaultitem.ItemTypeAPICollection, KeyIDCollection, raw, payload)
		if err != nil {
			return Collection{}, err
		}
		c.ID = record.ID
		c.SecretVars = nil
		return c, nil
	}

	// A rename or a move touches the metadata only: the variables and the
	// sealed secrets stay what they were, so renaming a folder cannot empty it.
	record, err := s.items.Get(c.ID)
	if err != nil || record.Type != vaultitem.ItemTypeAPICollection {
		return Collection{}, ErrNotFound
	}
	var meta collectionMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Collection{}, err
	}
	meta.Name, meta.ParentID, meta.Order = c.Name, c.ParentID, c.Order
	raw, err := json.Marshal(meta)
	if err != nil {
		return Collection{}, err
	}
	if _, err := s.items.UpdateMetadata(c.ID, raw); err != nil {
		return Collection{}, err
	}
	if sealed, openErr := s.collectionVars(record); openErr == nil {
		c.Vars, c.SecretVars = sealed.Vars, sortedKeys(sealed.Secrets)
	}
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
		collection := Collection{ID: record.ID, Name: meta.Name, ParentID: meta.ParentID, Order: meta.Order}
		if sealed, openErr := s.collectionVars(record); openErr == nil {
			collection.Vars, collection.SecretVars = sealed.Vars, sortedKeys(sealed.Secrets)
		}
		out = append(out, collection)
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

// DeleteCollectionTree removes a folder with everything in it: its requests,
// its subfolders and theirs. DeleteCollection refuses a full folder so a
// tidying click cannot lose work; this is the other door, for the person who
// has been shown what is inside and confirmed. It returns how many requests
// went with it.
func (s *Service) DeleteCollectionTree(id string) (int, error) {
	if id == "" {
		return 0, ErrInvalid
	}
	collections, err := s.Collections()
	if err != nil {
		return 0, err
	}
	found := false
	children := map[string][]string{}
	for _, collection := range collections {
		children[collection.ParentID] = append(children[collection.ParentID], collection.ID)
		if collection.ID == id {
			found = true
		}
	}
	if !found {
		return 0, ErrNotFound
	}
	// Breadth-first from the folder; the visited set ends a cycle a remote
	// snapshot might still carry.
	doomed := map[string]bool{}
	order := []string{id}
	for i := 0; i < len(order); i++ {
		at := order[i]
		if doomed[at] {
			continue
		}
		doomed[at] = true
		order = append(order, children[at]...)
	}
	requests, err := s.Requests()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, request := range requests {
		if doomed[request.CollectionID] {
			if err := s.DeleteRequest(request.ID); err != nil {
				return removed, err
			}
			removed++
		}
	}
	// Leaves first, so an interruption never leaves a child whose parent is
	// gone — folderPath copes, but there is no reason to make it.
	for i := len(order) - 1; i >= 0; i-- {
		if !doomed[order[i]] {
			continue
		}
		delete(doomed, order[i])
		if err := s.items.Delete(order[i]); err != nil {
			return removed, err
		}
	}
	return removed, nil
}
