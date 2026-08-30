package notes

import (
	"testing"
	"time"
)

func TestRestorePutsANoteBackWhereItWas(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("Standup", "body", folder.ID, []string{"standup"})

	if err := s.Delete(summary.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Restore(summary.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want the note back, got %d", len(list))
	}
	// DeleteFolder copies its fields down because the folder is really gone.
	// Trash is reversible, so it must not strip anything on the way through.
	if list[0].FolderID != folder.ID {
		t.Fatalf("folder lost through the trash: %+v", list[0])
	}
	if len(list[0].Tags) != 1 || list[0].Tags[0] != "standup" {
		t.Fatalf("tags lost through the trash: %+v", list[0].Tags)
	}
}

func TestListTrashReportsWhenEachNotePurges(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("Doomed", "body", "", nil)
	if err := s.Delete(summary.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	trash, err := s.ListTrash()
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if len(trash) != 1 {
		t.Fatalf("want one trashed note, got %d", len(trash))
	}
	gap := trash[0].PurgesAt.Sub(trash[0].DeletedAt)
	if gap != TrashRetention {
		t.Fatalf("purge date is %v after deletion, want %v", gap, TrashRetention)
	}
}

// The sweep is the only thing that enforces retention — there is no background
// job — so it has to run off the dashboard, not just off the trash screen.
func TestListSweepsNotesPastRetention(t *testing.T) {
	s := newService(t)
	expired, _ := s.Create("Old", "body", "", nil)
	fresh, _ := s.Create("Recent", "body", "", nil)

	if err := s.Delete(expired.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Delete(fresh.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdate(t, s, expired.ID, time.Now().UTC().Add(-TrashRetention-time.Hour))

	if _, err := s.List(); err != nil {
		t.Fatalf("list: %v", err)
	}

	if _, err := s.Get(expired.ID); err == nil {
		t.Fatal("a note past retention survived the sweep")
	}
	if _, err := s.Get(fresh.ID); err != nil {
		t.Fatalf("a note inside retention was swept: %v", err)
	}
}

func TestEmptyTrashPurgesOnlyTrashedNotes(t *testing.T) {
	s := newService(t)
	live, _ := s.Create("Live", "body", "", nil)
	trashed, _ := s.Create("Trashed", "body", "", nil)
	if err := s.Delete(trashed.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	count, err := s.EmptyTrash()
	if err != nil {
		t.Fatalf("empty trash: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 purged, got %d", count)
	}
	if _, err := s.Get(live.ID); err != nil {
		t.Fatalf("emptying the trash took a live note: %v", err)
	}
}

func backdate(t *testing.T, s *Service, id string, when time.Time) {
	t.Helper()
	meta, err := s.trashableMeta(id)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	meta.DeletedAt = when.Format(time.RFC3339)
	if err := s.writeMeta(id, meta); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}
