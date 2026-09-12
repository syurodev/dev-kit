package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

const consoleHeader = "-- devkit-console "

// A console id is a file name, so it is checked as one. Anything with a
// separator, a drive letter or a dot-dot in it is rejected outright rather
// than cleaned: an id that needed cleaning is an id that came from somewhere
// it should not have, and writing to the "fixed" path would be the bug.
var consoleID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type ConsoleStore struct {
	dir string
	// Save and Delete both rename into the same directory, and List reads it.
	// One process, but several UI calls in flight at once.
	mu sync.Mutex
}

func NewConsoleStore(dir string) *ConsoleStore {
	return &ConsoleStore{dir: dir}
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
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
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

func markConsoleError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrConsoleStore, err)
}
