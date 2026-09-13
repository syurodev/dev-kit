package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
A query console's text, as a file.

DataGrip's model, and for DataGrip's reason: a console is a scratch file you
keep, not a record you saved. It survives a relaunch without being named,
without being asked for, and it is a real .sql on disk that any editor can
open. Saved queries are the other thing and stay where they are — a vault
item, named, synced, deliberate. Nothing here touches them.

The connection it belongs to travels in the file, on one comment line at the
top:

	-- devkit-console {"profile_id":"…","database":"public"}
	SELECT * FROM orders;

An index file beside the statements would be a second thing to keep in step,
and the first crash between the two writes leaves a console whose text says
one connection and whose index says another. A header cannot disagree with
the file it is in. It is also the answer when the file is opened anywhere
else: the line says what it is for, in a syntax every SQL editor already
ignores.

It is JSON on one line so that no value can end the line early — a database
named with a newline is absurd and is exactly the kind of absurd that writes
half a header. And it is stripped on read, so it never reaches the editor:
what the user typed is what comes back.
*/

// Console is one scratch console: its text and where that text belongs.
type Console struct {
	ID        string    `json:"id"`
	ProfileID string    `json:"profile_id"`
	Database  string    `json:"database"`
	SQL       string    `json:"sql"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ErrConsoleStoreUnavailable means the service was built without a place to
// put them. Consoles still work; they just do not survive the window closing.
var ErrConsoleStoreUnavailable = errors.New("query consoles are not stored")

// ErrConsoleStore marks a failure to read or write the files — a full disk, a
// directory someone chmod'ed, an id that came from the wrong place. Marked so
// the bridge can say what actually went wrong: without it every one of these
// reached the window as "Connection failed.", which is the same wrong answer
// a statement error used to get, about a connection that is fine.
var ErrConsoleStore = errors.New("query console storage failed")

// ErrConsoleDirRejected is a folder the store will not use, with a reason
// the user can act on. Separate from ErrConsoleStore because it is not a
// failure of the disk — it is an answer to something the user just chose,
// and it belongs in the dialog they chose it in.
var ErrConsoleDirRejected = errors.New("that folder cannot hold the consoles")

const consoleHeader = "-- devkit-console "

// A console id is a file name, so it is checked as one. Anything with a
// separator, a drive letter or a dot-dot in it is rejected outright rather
// than cleaned: an id that needed cleaning is an id that came from somewhere
// it should not have, and writing to the "fixed" path would be the bug.
var consoleID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type ConsoleStore struct {
	dir string
	// Where they go when nobody has chosen: beside the database, under the
	// app's own directory. Kept so "Use the default" is a real option rather
	// than a path the window has to remember and retype.
	defaultDir string
	// Save and Delete both rename into the same directory, and List reads it.
	// One process, but several UI calls in flight at once.
	mu sync.Mutex
}

func NewConsoleStore(defaultDir string) *ConsoleStore {
	return &ConsoleStore{dir: defaultDir, defaultDir: defaultDir}
}

func (s *ConsoleStore) Dir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir
}

func (s *ConsoleStore) DefaultDir() string { return s.defaultDir }

/*
SetDir points the consoles somewhere else, and takes them with it.

Leaving them behind is the version that generates the bug report: the
setting changes, the next launch reads an empty folder, and every console
the user had is gone as far as the window is concerned — still on disk,
in a place they now have to remember.

The files are COPIED and then unlinked rather than renamed. A rename cannot
cross a filesystem, and crossing one is most of the point of this setting:
the reason to move consoles is to put them in a synced folder, a project
directory, an external disk. Copy-then-unlink also fails safe — the source
is only removed once its copy is on disk.

A name that already exists at the destination is left alone, and its source
left alone with it. Nothing here is worth overwriting somebody's file for,
and a console that did not move is recoverable; one that was overwritten is
not.
*/
func (s *ConsoleStore) SetDir(dir string) error {
	if dir == "" {
		dir = s.defaultDir
	}
	if !filepath.IsAbs(dir) {
		// Relative to what? The app's working directory is not a place the
		// user can see, and it is not the same on every launch.
		return fmt.Errorf("%w: the folder must be an absolute path", ErrConsoleDirRejected)
	}
	dir = filepath.Clean(dir)

	s.mu.Lock()
	defer s.mu.Unlock()
	if dir == s.dir {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: that is a file, not a folder", ErrConsoleDirRejected)
	}
	// Before the switch, not after: a move that fails leaves the setting
	// where it was, so the user is looking at the folder their consoles are
	// actually in.
	if err := moveConsoles(s.dir, dir); err != nil {
		return err
	}
	s.dir = dir
	return nil
}

/*
moveConsoles copies the consoles in from to to, and removes the originals only
once every copy has landed (#50).

Copy-then-remove one file at a time meant a failure halfway left some consoles
in the new folder while the setting — which SetDir only changes on success —
still named the old one. And only consoles: the folder may be a project's own,
and a migrations.sql sitting in it is not the app's to move. A console is a file
this app wrote, which is to say one that starts with its header.
*/
func moveConsoles(from, to string) error {
	entries, err := os.ReadDir(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var sources, copies []string
	undo := func() {
		for _, copied := range copies {
			_ = os.Remove(copied)
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		source := filepath.Join(from, name)
		target := filepath.Join(to, name)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		text, err := os.ReadFile(source)
		if err != nil {
			undo()
			return err
		}
		if !strings.HasPrefix(string(text), consoleHeader) {
			continue
		}
		if err := os.WriteFile(target, text, 0o600); err != nil {
			_ = os.Remove(target)
			undo()
			return err
		}
		sources = append(sources, source)
		copies = append(copies, target)
	}
	for _, source := range sources {
		if err := os.Remove(source); err != nil {
			return err
		}
	}
	return nil
}

type consoleMeta struct {
	ProfileID string    `json:"profile_id,omitempty"`
	Database  string    `json:"database,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (s *ConsoleStore) path(id string) (string, error) {
	if !consoleID.MatchString(id) {
		return "", fmt.Errorf("invalid console id %q", id)
	}
	return filepath.Join(s.dir, id+".sql"), nil
}

// Save writes the console, replacing whatever was there.
//
// Through a temporary file and a rename, because the thing being replaced is
// the only copy of something the user typed. A write straight over it that
// fails halfway leaves a truncated query; a rename either happened or did
// not.
func (s *ConsoleStore) Save(console Console) error {
	path, err := s.path(console.ID)
	if err != nil {
		return err
	}
	header, err := json.Marshal(consoleMeta{
		ProfileID: console.ProfileID,
		Database:  console.Database,
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	body := consoleHeader + string(header) + "\n" + console.SQL

	s.mu.Lock()
	defer s.mu.Unlock()
	// 0700: the statements carry literals, and a literal is as often a token
	// as it is an order number.
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.dir, console.ID+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.WriteString(body); err != nil {
		temp.Close()
		return err
	}
	// On the disk before the rename (#50): the rename can be recorded before
	// the data it points at, and a crash in between leaves an empty file where
	// the only copy of the console was.
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceFile(temp.Name(), path)
}

// replaceFile renames from over to, retrying on Windows (#50). There a rename
// over a file another program holds open — an editor showing the console, a
// virus scanner reading it — fails, and usually works a moment later.
func replaceFile(from, to string) error {
	err := os.Rename(from, to)
	for delay := 20 * time.Millisecond; err != nil && runtime.GOOS == "windows" && delay <= 320*time.Millisecond; delay *= 2 {
		time.Sleep(delay)
		err = os.Rename(from, to)
	}
	return err
}

// List returns every console on disk, newest first.
//
// A file it cannot parse is still returned, with its whole text as the SQL:
// the header is metadata about the statement, and losing the statement
// because the metadata went bad would be the worse half to keep.
func (s *ConsoleStore) List() ([]Console, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	consoles := make([]Console, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		id := strings.TrimSuffix(name, ".sql")
		if !consoleID.MatchString(id) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			return nil, err
		}
		consoles = append(consoles, parseConsole(id, string(raw)))
	}
	sort.Slice(consoles, func(i, j int) bool {
		return consoles[i].UpdatedAt.After(consoles[j].UpdatedAt)
	})
	return consoles, nil
}

func (s *ConsoleStore) Delete(id string) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

/*
parseConsole splits the header off the statement.

Only a first line that is exactly our own header and holds valid JSON counts
as one. A user whose query genuinely begins with a comment keeps it — the
alternative is an editor that silently eats the first line of somebody's SQL,
which is a far worse failure than a console that does not remember which
connection it was for.
*/
func parseConsole(id, raw string) Console {
	console := Console{ID: id, SQL: raw}
	if !strings.HasPrefix(raw, consoleHeader) {
		return console
	}
	line, rest, found := strings.Cut(raw, "\n")
	var meta consoleMeta
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, consoleHeader)), &meta); err != nil {
		return console
	}
	console.ProfileID = meta.ProfileID
	console.Database = meta.Database
	console.UpdatedAt = meta.UpdatedAt
	if found {
		console.SQL = rest
	} else {
		console.SQL = ""
	}
	return console
}

// SetConsoleStore gives the service somewhere to keep console text. Without
// it every console call reports ErrConsoleStoreUnavailable and nothing else
// changes: a console with nowhere to be written is still a console.
func (s *Service) SetConsoleStore(store *ConsoleStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consoles = store
}

func (s *Service) consoleStore() *ConsoleStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consoles
}

func (s *Service) ListConsoles() ([]Console, error) {
	store := s.consoleStore()
	if store == nil {
		return nil, ErrConsoleStoreUnavailable
	}
	list, err := store.List()
	return list, markConsoleError(err)
}

func (s *Service) SaveConsole(console Console) error {
	store := s.consoleStore()
	if store == nil {
		return ErrConsoleStoreUnavailable
	}
	return markConsoleError(store.Save(console))
}

func (s *Service) DeleteConsole(id string) error {
	store := s.consoleStore()
	if store == nil {
		return ErrConsoleStoreUnavailable
	}
	return markConsoleError(store.Delete(id))
}

// ConsoleDirectory is where consoles are kept, and where they would be kept
// if the setting were cleared.
func (s *Service) ConsoleDirectory() (current string, fallback string, err error) {
	store := s.consoleStore()
	if store == nil {
		return "", "", ErrConsoleStoreUnavailable
	}
	return store.Dir(), store.DefaultDir(), nil
}

// SetConsoleDirectory moves the consoles to dir, or back to the default when
// dir is empty.
func (s *Service) SetConsoleDirectory(dir string) error {
	store := s.consoleStore()
	if store == nil {
		return ErrConsoleStoreUnavailable
	}
	if err := store.SetDir(dir); err != nil {
		if errors.Is(err, ErrConsoleDirRejected) {
			return err
		}
		return markConsoleError(err)
	}
	return nil
}

func markConsoleError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrConsoleStore, err)
}
