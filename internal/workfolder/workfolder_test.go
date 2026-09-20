package workfolder

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func record(s *Service) *[]State {
	var seen []State
	s.Subscribe(func(state State) { seen = append(seen, state) })
	return &seen
}

func TestUnpinnedModuleFollowsTheCurrentFolder(t *testing.T) {
	s := NewService()
	s.SetCurrent("/work/a")
	if got := s.Effective(ModuleGit); got != "/work/a" {
		t.Fatalf("git = %q, want /work/a", got)
	}
	s.SetCurrent("/work/b")
	if got := s.Effective(ModuleGit); got != "/work/b" {
		t.Fatalf("git after the folder moved = %q, want /work/b", got)
	}
}

func TestPinnedModuleStaysWhileTheCurrentFolderMoves(t *testing.T) {
	s := NewService()
	pinned := t.TempDir()
	s.SetCurrent("/work/a")
	if err := s.Pin(ModuleGit, pinned); err != nil {
		t.Fatal(err)
	}
	s.SetCurrent("/work/b")

	if got := s.Effective(ModuleGit); got != pinned {
		t.Fatalf("git = %q, want it to stay on %q", got, pinned)
	}
	if got := s.Effective(ModuleAgentHub); got != "/work/b" {
		t.Fatalf("agenthub = %q, want it to follow to /work/b", got)
	}

	s.Follow(ModuleGit)
	if got := s.Effective(ModuleGit); got != "/work/b" {
		t.Fatalf("git after following = %q, want /work/b", got)
	}
}

func TestPinRefusesWhatIsNotAnAbsoluteFolder(t *testing.T) {
	s := NewService()
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		module, folder string
		want           error
	}{
		{ModuleGit, "relative/dir", ErrNotAbsolute},
		{ModuleGit, filepath.Join(t.TempDir(), "missing"), ErrNotDirectory},
		{ModuleGit, file, ErrNotDirectory},
		{"", t.TempDir(), ErrNoModule},
	}
	for _, c := range cases {
		if err := s.Pin(c.module, c.folder); !errors.Is(err, c.want) {
			t.Errorf("Pin(%q, %q) = %v, want %v", c.module, c.folder, err, c.want)
		}
	}
	if pins := s.State().Pins; len(pins) != 0 {
		t.Fatalf("a refused pin left %v behind", pins)
	}
}

func TestSubscribersHearOnlyRealChanges(t *testing.T) {
	s := NewService()
	seen := record(s)
	folder := t.TempDir()

	s.SetCurrent("/work/a")
	s.SetCurrent("/work/a") // a restore announcing what an open already did
	s.Follow(ModuleGit)     // not pinned: already following
	if err := s.Pin(ModuleGit, folder); err != nil {
		t.Fatal(err)
	}
	if err := s.Pin(ModuleGit, folder); err != nil { // same pin again
		t.Fatal(err)
	}
	s.Follow(ModuleGit)

	if len(*seen) != 3 {
		t.Fatalf("subscriber heard %d changes, want 3: %+v", len(*seen), *seen)
	}
	if last := (*seen)[2]; last.Current != "/work/a" || len(last.Pins) != 0 {
		t.Fatalf("last state = %+v", last)
	}
}

func TestStateIsACopy(t *testing.T) {
	s := NewService()
	if err := s.Pin(ModuleGit, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	state := s.State()
	state.Pins[ModuleGit] = "/tampered"
	if s.Effective(ModuleGit) == "/tampered" {
		t.Fatal("State handed out the live map")
	}
}

func TestSubscriberMayCallBackIntoTheService(t *testing.T) {
	s := NewService()
	done := make(chan struct{})
	s.Subscribe(func(State) {
		_ = s.State() // would deadlock if callbacks ran under the lock
		close(done)
	})
	s.SetCurrent("/work/a")
	<-done
}
