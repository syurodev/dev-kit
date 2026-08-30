package notes

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"devkit/internal/vaultitem"
)

const (
	versionType   = vaultitem.ItemTypeNoteVersion
	versionKeyID  = "note-version-body"
	maxVersions   = 50
	idleThreshold = 5 * time.Minute
)

// Trigger values are display only — nothing branches on them.
const (
	TriggerCheckpoint = "checkpoint"
	TriggerPreRestore = "pre-restore"
)

type versionMetadata struct {
	NoteID  string `json:"note_id"`
	Trigger string `json:"trigger"`
}

// versionPayload is what gets sealed. Only title and body are versioned, the
// same fields Update writes — folder, tags and colour belong to
// SetOrganization, and restoring content must not rearrange a note.
type versionPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Version is one snapshot, without its body.
type Version struct {
	ID        string    `json:"id"`
	NoteID    string    `json:"note_id"`
	Trigger   string    `json:"trigger"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// Checkpoint snapshots a note's current title and body.
//
// No-op when the note is unchanged since the last snapshot: an editor that
// checkpoints on idle would otherwise fill the history with identical entries
// for a note nobody is editing.
func (s *Service) Checkpoint(id, trigger string) (Version, error) {
	note, err := s.Get(id)
	if err != nil {
		return Version{}, err
	}

	existing, err := s.versionRecords(id)
	if err != nil {
		return Version{}, err
	}
	if len(existing) > 0 {
		latest, err := s.openVersion(existing[0])
		if err == nil && latest.Title == note.Title && latest.Body == note.Body {
			return Version{}, nil
		}
	}

	payload, err := json.Marshal(versionPayload{Title: note.Title, Body: note.Body})
	if err != nil {
		return Version{}, err
	}
	meta, err := json.Marshal(versionMetadata{NoteID: id, Trigger: triggerOrDefault(trigger)})
	if err != nil {
		return Version{}, err
	}
	record, err := s.store.Create(versionType, versionKeyID, meta, payload)
	if err != nil {
		return Version{}, err
	}

	if err := s.trimVersions(id); err != nil {
		return Version{}, err
	}
	return Version{
		ID:        record.ID,
		NoteID:    id,
		Trigger:   triggerOrDefault(trigger),
		Title:     note.Title,
		CreatedAt: record.CreatedAt,
	}, nil
}

// ListVersions returns a note's history, newest first.
func (s *Service) ListVersions(id string) ([]Version, error) {
	records, err := s.versionRecords(id)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(records))
	for _, record := range records {
		meta, err := decodeVersionMetadata(record)
		if err != nil {
			return nil, err
		}
		version := Version{
			ID:        record.ID,
			NoteID:    meta.NoteID,
			Trigger:   meta.Trigger,
			CreatedAt: record.CreatedAt,
		}
		if payload, err := s.openVersion(record); err == nil {
			version.Title = payload.Title
		}
		out = append(out, version)
	}
	return out, nil
}

// GetVersion returns one snapshot's title and body.
func (s *Service) GetVersion(versionID string) (string, string, error) {
	record, err := s.store.Get(versionID)
	if err != nil {
		return "", "", err
	}
	if record.Type != versionType {
		return "", "", errors.New("version not found")
	}
	payload, err := s.openVersion(record)
	if err != nil {
		return "", "", err
	}
	return payload.Title, payload.Body, nil
}

// RestoreVersion writes a snapshot back over the note.
//
// It checkpoints the current content first, tagged pre-restore. Without that,
// restoring would destroy whatever is on screen whenever the note has not been
// idle long enough to have been checkpointed on its own — which is exactly
// when someone reaches for the history.
func (s *Service) RestoreVersion(id, versionID string) (Note, error) {
	record, err := s.store.Get(versionID)
	if err != nil {
		return Note{}, err
	}
	meta, err := decodeVersionMetadata(record)
	if err != nil {
		return Note{}, err
	}
	if meta.NoteID != id {
		return Note{}, errors.New("version belongs to another note")
	}
	payload, err := s.openVersion(record)
	if err != nil {
		return Note{}, err
	}

	if _, err := s.Checkpoint(id, TriggerPreRestore); err != nil {
		return Note{}, err
	}
	return s.Update(id, payload.Title, payload.Body)
}

// deleteVersionsOf removes every snapshot of a note. Purging a note must not
// leave its history behind as orphans nothing can reach.
func (s *Service) deleteVersionsOf(id string) error {
	records, err := s.versionRecords(id)
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := s.store.Delete(record.ID); err != nil {
			return err
		}
	}
	return nil
}

// versionRecords returns one note's snapshots, newest first.
func (s *Service) versionRecords(id string) ([]vaultitem.Record, error) {
	records, err := s.store.ListByTypeAndMetaField(versionType, "note_id", id)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records, nil
}

// trimVersions drops everything past the cap, oldest first. The set is one
// note's history, so sorting and cutting in Go is cheaper than pushing it down
// into SQL.
func (s *Service) trimVersions(id string) error {
	records, err := s.versionRecords(id)
	if err != nil {
		return err
	}
	for _, record := range records[min(len(records), maxVersions):] {
		if err := s.store.Delete(record.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) openVersion(record vaultitem.Record) (versionPayload, error) {
	plain, err := s.store.OpenPayload(record, versionKeyID)
	if err != nil {
		return versionPayload{}, err
	}
	var payload versionPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return versionPayload{}, err
	}
	return payload, nil
}

func decodeVersionMetadata(record vaultitem.Record) (versionMetadata, error) {
	var meta versionMetadata
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		return versionMetadata{}, err
	}
	return meta, nil
}

func triggerOrDefault(trigger string) string {
	if trigger == TriggerPreRestore {
		return TriggerPreRestore
	}
	return TriggerCheckpoint
}
