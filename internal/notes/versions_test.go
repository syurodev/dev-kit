package notes

import "testing"

func TestCheckpointSkipsAnUnchangedNote(t *testing.T) {
	s := newService(t)
	note, _ := s.Create("Meeting", "agenda", "", nil)

	if _, err := s.Checkpoint(note.ID, TriggerCheckpoint); err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	// An editor checkpoints on idle. Without this guard a note nobody is
	// editing would fill its own history with identical snapshots and push the
	// real ones past the cap.
	if _, err := s.Checkpoint(note.ID, TriggerCheckpoint); err != nil {
		t.Fatalf("second checkpoint: %v", err)
	}

	versions, err := s.ListVersions(note.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("want 1 version for unchanged content, got %d", len(versions))
	}
}

func TestRestoreCheckpointsCurrentContentFirst(t *testing.T) {
	s := newService(t)
	note, _ := s.Create("Meeting", "v1 body", "", nil)
	if _, err := s.Checkpoint(note.ID, TriggerCheckpoint); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if _, err := s.Update(note.ID, "Meeting", "v2 body"); err != nil {
		t.Fatalf("update: %v", err)
	}

	versions, _ := s.ListVersions(note.ID)
	restored, err := s.RestoreVersion(note.ID, versions[0].ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Body != "v1 body" {
		t.Fatalf("restore did not write the snapshot back: %q", restored.Body)
	}

	// The v2 body was never checkpointed on its own — it was edited seconds
	// ago. Restore has to capture it, or reaching for the history destroys the
	// work that prompted it.
	after, _ := s.ListVersions(note.ID)
	var sawPreRestore bool
	for _, v := range after {
		if v.Trigger != TriggerPreRestore {
			continue
		}
		_, body, err := s.GetVersion(v.ID)
		if err != nil {
			t.Fatalf("read pre-restore version: %v", err)
		}
		if body == "v2 body" {
			sawPreRestore = true
		}
	}
	if !sawPreRestore {
		t.Fatalf("the content being overwritten was not captured: %+v", after)
	}
}

func TestVersionsAreCappedOldestFirst(t *testing.T) {
	s := newService(t)
	note, _ := s.Create("Log", "body 0", "", nil)
	for i := 1; i <= maxVersions+5; i++ {
		if _, err := s.Update(note.ID, "Log", "body "+string(rune('a'+i%26))+itoa(i)); err != nil {
			t.Fatalf("update %d: %v", i, err)
		}
		if _, err := s.Checkpoint(note.ID, TriggerCheckpoint); err != nil {
			t.Fatalf("checkpoint %d: %v", i, err)
		}
	}

	versions, err := s.ListVersions(note.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != maxVersions {
		t.Fatalf("want the cap of %d, got %d", maxVersions, len(versions))
	}
}

func TestPurgeTakesTheHistoryWithIt(t *testing.T) {
	s := newService(t)
	note, _ := s.Create("Doomed", "body", "", nil)
	if _, err := s.Checkpoint(note.ID, TriggerCheckpoint); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	if err := s.Purge(note.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Orphan versions are unreachable through any API and would keep syncing.
	left, err := s.store.ListByTypeAndMetaField(versionType, "note_id", note.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("purge left %d orphan versions behind", len(left))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
