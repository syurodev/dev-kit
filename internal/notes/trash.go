package notes

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"devkit/internal/vaultitem"
)

// TrashRetention is how long a trashed note survives before it is purged for
// good. Fixed, not configurable — see the Known limitations in the design page.
const TrashRetention = 30 * 24 * time.Hour

// TrashedNote is a summary plus when it goes away, which is the only thing the
// trash screen asks that a live note never does.
type TrashedNote struct {
	Summary
	DeletedAt time.Time `json:"deleted_at"`
	PurgesAt  time.Time `json:"purges_at"`
}

// Delete moves a note to the trash. It is a metadata-only write, the same path
// SetOrganization takes, so the body is never resealed.
//
// Organisation is deliberately left alone. DeleteFolder copies its fields down
// onto the notes it held because the folder is really gone; trash is reversible,
// so folder, tags and colour stay put and restore returns the note to where it
// actually was.
func (s *Service) Delete(id string) error {
	meta, err := s.trashableMeta(id)
	if err != nil {
		return err
	}
	if meta.DeletedAt != "" {
		return nil
	}
	meta.DeletedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.writeMeta(id, meta); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// Restore takes a note back out of the trash.
func (s *Service) Restore(id string) error {
	meta, err := s.trashableMeta(id)
	if err != nil {
		return err
	}
	if meta.DeletedAt == "" {
		return nil
	}
	meta.DeletedAt = ""
	if err := s.writeMeta(id, meta); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// Purge deletes a note for good. It does not check whether the note was in the
// trash: "Delete forever" is reachable from the trash screen, and a caller that
// means it should not have to stage the note first.
func (s *Service) Purge(id string) error {
	record, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if record.Type != itemType {
		return errors.New("note not found")
	}
	// A purged note must not leave its history behind — orphan versions are
	// unreachable and would sync forever.
	if err := s.deleteVersionsOf(id); err != nil {
		return err
	}
	// Both directions: a row pointing at an id that no longer exists is as
	// orphaned as one leaving it.
	if s.links != nil {
		if err := s.links.DeleteLinksFor(id); err != nil {
			return err
		}
	}
	if err := s.store.Delete(id); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// EmptyTrash purges every trashed note.
func (s *Service) EmptyTrash() (int, error) {
	trashed, err := s.trashedRecords()
	if err != nil {
		return 0, err
	}
	for _, entry := range trashed {
		if err := s.Purge(entry.id); err != nil {
			return 0, err
		}
	}
	return len(trashed), nil
}

// ListTrash reports what is in the trash, newest deletion first, after sweeping
// anything past retention.
func (s *Service) ListTrash() ([]TrashedNote, error) {
	if err := s.sweepExpired(); err != nil {
		return nil, err
	}
	trashed, err := s.trashedRecords()
	if err != nil {
		return nil, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return nil, err
	}

	out := make([]TrashedNote, 0, len(trashed))
	for _, entry := range trashed {
		summary, err := s.toSummary(entry.record, folderMap)
		if err != nil {
			return nil, err
		}
		out = append(out, TrashedNote{
			Summary:   summary,
			DeletedAt: entry.deletedAt,
			PurgesAt:  entry.deletedAt.Add(TrashRetention),
		})
	}
	sortByDeletedAtDesc(out)
	return out, nil
}

// sweepExpired hard-deletes notes past retention.
//
// A desktop app is not running in the background, so there is no cron to hang
// this on. It rides along with List and ListTrash instead — opening the
// dashboard is enough, nobody has to visit the trash for the sweep to happen.
func (s *Service) sweepExpired() error {
	trashed, err := s.trashedRecords()
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-TrashRetention)
	for _, entry := range trashed {
		if entry.deletedAt.After(cutoff) {
			continue
		}
		if err := s.Purge(entry.id); err != nil {
			return err
		}
	}
	return nil
}

type trashEntry struct {
	id        string
	deletedAt time.Time
	record    vaultitem.Record
}

// trashedRecords returns every note carrying a deleted_at, with the timestamp
// already parsed. A deleted_at that will not parse is treated as trashed with a
// zero time, so a corrupt value ages out rather than pinning a note in the
// trash forever where nothing can reach it.
func (s *Service) trashedRecords() ([]trashEntry, error) {
	records, err := s.store.ListByType(itemType)
	if err != nil {
		return nil, err
	}
	out := make([]trashEntry, 0)
	for _, record := range records {
		meta, err := decodeMetadata(record)
		if err != nil {
			return nil, err
		}
		if meta.DeletedAt == "" {
			continue
		}
		when, err := time.Parse(time.RFC3339, meta.DeletedAt)
		if err != nil {
			when = time.Time{}
		}
		out = append(out, trashEntry{id: record.ID, deletedAt: when.UTC(), record: record})
	}
	return out, nil
}

func (s *Service) trashableMeta(id string) (metadata, error) {
	record, err := s.store.Get(id)
	if err != nil {
		return metadata{}, err
	}
	if record.Type != itemType {
		return metadata{}, errors.New("note not found")
	}
	return decodeMetadata(record)
}

func (s *Service) writeMeta(id string, meta metadata) error {
	encoded, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.store.UpdateMetadata(id, encoded)
	return err
}

func sortByDeletedAtDesc(out []TrashedNote) {
	sort.Slice(out, func(i, j int) bool {
		return out[i].DeletedAt.After(out[j].DeletedAt)
	})
}
