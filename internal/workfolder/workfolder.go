// Package workfolder is the one folder the app is working in, and which
// modules follow it.
//
// Opening a folder in Code Graph used to be a fact only Code Graph knew. Git
// had no way to be pointed anywhere, Agent Hub asked Code Graph once when a
// draft began, and Tasks and MCP install each kept a path of their own. The
// person had opened a folder and then had to say so again to everything else.
//
// The model is deliberately small: a current folder, and per-module pins.
// A module without a pin uses the current folder, so it moves when the current
// one does. A module the person points somewhere else is pinned and stays
// there until they send it back to following — which is how a repo can be read
// in Git without dragging Code Graph, which would go and index it, along too.
//
// It holds paths and nothing else. What a module does when its folder changes
// — Git reopening a repository — is the module's business, wired where the
// module is built, so this package imports none of them.
package workfolder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Module names are the keys a pin is filed under. They are the frontend's
// module ids where a module has one, so a pin reads the way the tab does.
const (
	ModuleGit      = "git"
	ModuleAgentHub = "agenthub"
	ModuleTasks    = "tasks"
)

var (
	// ErrNotAbsolute is a folder that names no particular place. A relative
	// path would mean whatever the process's working directory happens to be.
	ErrNotAbsolute = errors.New("workfolder: folder must be an absolute path")
	// ErrNotDirectory is a path that is missing or is a file.
	ErrNotDirectory = errors.New("workfolder: not a folder")
	// ErrNoModule is a pin with no module to file it under.
	ErrNoModule = errors.New("workfolder: module is required")
)

// State is everything a subscriber needs: the current folder and the pins.
// It is a copy; changing it changes nothing.
type State struct {
	Current string            `json:"current"`
	Pins    map[string]string `json:"pins"`
}

// Effective is the folder a module works in: its pin, else the current one.
func (s State) Effective(module string) string {
	if pinned, ok := s.Pins[module]; ok {
		return pinned
	}
	return s.Current
}

// Service holds the state and tells subscribers when it changes.
type Service struct {
	mu      sync.RWMutex
	current string
	pins    map[string]string
	subs    []func(State)
}

// NewService returns a Service with no folder open.
func NewService() *Service {
	return &Service{pins: map[string]string{}}
}

// State returns a copy of the current folder and pins.
func (s *Service) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot()
}

// Effective is the folder module works in right now.
func (s *Service) Effective(module string) string {
	return s.State().Effective(module)
}

// Subscribe registers fn to run after every change, with the state as it
// became. Callbacks run on the goroutine that made the change and outside the
// lock, so a subscriber may call back into the Service. There is no
// unsubscribe: every subscriber lives as long as the app does.
func (s *Service) Subscribe(fn func(State)) {
	s.mu.Lock()
	s.subs = append(s.subs, fn)
	s.mu.Unlock()
}

// SetCurrent makes root the folder every unpinned module follows. Setting the
// folder already current is not a change and tells nobody, so the callers that
// announce the same folder twice — a restore and then an open — cost nothing.
// An empty root clears it.
func (s *Service) SetCurrent(root string) {
	s.mu.Lock()
	if s.current == root {
		s.mu.Unlock()
		return
	}
	s.current = root
	state, subs := s.snapshot(), s.subs
	s.mu.Unlock()
	notify(subs, state)
}

// Pin points one module at folder, whatever the current folder is.
func (s *Service) Pin(module, folder string) error {
	if module == "" {
		return ErrNoModule
	}
	if !filepath.IsAbs(folder) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, folder)
	}
	folder = filepath.Clean(folder)
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %q", ErrNotDirectory, folder)
	}

	s.mu.Lock()
	if pinned, ok := s.pins[module]; ok && pinned == folder {
		s.mu.Unlock()
		return nil
	}
	s.pins[module] = folder
	state, subs := s.snapshot(), s.subs
	s.mu.Unlock()
	notify(subs, state)
	return nil
}

// Follow sends a module back to the current folder. A module that is not
// pinned is already following, and nothing changes.
func (s *Service) Follow(module string) {
	s.mu.Lock()
	if _, ok := s.pins[module]; !ok {
		s.mu.Unlock()
		return
	}
	delete(s.pins, module)
	state, subs := s.snapshot(), s.subs
	s.mu.Unlock()
	notify(subs, state)
}

// snapshot copies the state. The caller holds mu.
func (s *Service) snapshot() State {
	pins := make(map[string]string, len(s.pins))
	for module, folder := range s.pins {
		pins[module] = folder
	}
	return State{Current: s.current, Pins: pins}
}

func notify(subs []func(State), state State) {
	for _, fn := range subs {
		fn(state)
	}
}
