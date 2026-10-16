package preferences

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

// Model profiles are the person's answer to "which model fits which job":
// "review and reading — the cheap one, low effort", "architecture — the strong
// one, high effort". They are local, UI-owned configuration. An agent that is
// about to start a child session reads them (agenthub.listModelProfiles) and
// picks by the description; it cannot change them, and a profile carries no
// permission — a model and an effort, nothing else.
const (
	maxModelProfiles          = 24
	maxModelProfileName       = 60
	maxModelProfileDesc       = 400
	maxModelProfileNotes      = 800
	maxModelProfileValue      = 120
	maxModelProfileVendorSize = 40
)

var (
	// ErrInvalidModelProfiles rejects a list the store cannot hold.
	ErrInvalidModelProfiles = errors.New("preferences: invalid model profiles")
	// ErrModelProfilesConflict: the list changed since the caller read it.
	ErrModelProfilesConflict = errors.New("model profiles revision conflict")
)

// AgentModelProfile is one job and the model to run it on.
type AgentModelProfile struct {
	// ID is stable once made (the name may be edited); an agent may name a
	// profile by either. Generated from the name when empty.
	ID string `json:"id"`
	// Name is what the person calls the job — "Code review".
	Name string `json:"name"`
	// Description tells an agent when the profile applies. It is read as data
	// the person wrote, never as an instruction to the agent.
	Description string `json:"description"`
	// Vendor is the agent type ("claude", "codex").
	Vendor string `json:"vendor"`
	// Model is the vendor's model id, as its picker lists it.
	Model string `json:"model"`
	// Effort is the reasoning effort; empty leaves the model's own default.
	Effort string `json:"effort,omitempty"`
	// Notes is for the person (and the agent) — why this model, what to avoid.
	Notes string `json:"notes,omitempty"`
}

// AgentModelProfiles is the whole list, versioned like AgentCustomization so
// a stale Settings form cannot overwrite a newer list.
type AgentModelProfiles struct {
	Revision int64               `json:"revision"`
	Profiles []AgentModelProfile `json:"profiles"`
}

func (p AgentModelProfiles) clone() AgentModelProfiles {
	out := AgentModelProfiles{Revision: p.Revision, Profiles: make([]AgentModelProfile, len(p.Profiles))}
	copy(out.Profiles, p.Profiles)
	return out
}

// NormalizeModelProfiles trims, fills missing ids and refuses what cannot be
// stored: a blank name, vendor or model, control characters, an over-long
// field, a duplicate name or id, or more than 24 profiles. Whether the model
// still exists is NOT checked here — a model leaves a vendor's list without
// the profile changing — and is reported when the profile is read.
func NormalizeModelProfiles(in []AgentModelProfile) ([]AgentModelProfile, error) {
	if len(in) > maxModelProfiles {
		return nil, fmt.Errorf("%w: at most %d profiles", ErrInvalidModelProfiles, maxModelProfiles)
	}
	out := make([]AgentModelProfile, 0, len(in))
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, raw := range in {
		p := AgentModelProfile{
			ID: strings.TrimSpace(raw.ID), Name: strings.TrimSpace(raw.Name),
			Description: strings.TrimSpace(raw.Description), Vendor: strings.TrimSpace(raw.Vendor),
			Model: strings.TrimSpace(raw.Model), Effort: strings.TrimSpace(raw.Effort), Notes: strings.TrimSpace(raw.Notes),
		}
		for field, limit := range map[string]struct {
			value string
			max   int
			multi bool
		}{
			"name":        {p.Name, maxModelProfileName, false},
			"vendor":      {p.Vendor, maxModelProfileVendorSize, false},
			"model":       {p.Model, maxModelProfileValue, false},
			"effort":      {p.Effort, maxModelProfileValue, false},
			"id":          {p.ID, maxModelProfileName, false},
			"description": {p.Description, maxModelProfileDesc, true},
			"notes":       {p.Notes, maxModelProfileNotes, true},
		} {
			if len(limit.value) > limit.max {
				return nil, fmt.Errorf("%w: %s is longer than %d bytes", ErrInvalidModelProfiles, field, limit.max)
			}
			if strings.IndexFunc(limit.value, func(r rune) bool {
				return unicode.IsControl(r) && !(limit.multi && (r == '\n' || r == '\t'))
			}) >= 0 {
				return nil, fmt.Errorf("%w: %s holds a control character", ErrInvalidModelProfiles, field)
			}
		}
		if p.Name == "" || p.Vendor == "" || p.Model == "" {
			return nil, fmt.Errorf("%w: a profile needs a name, an agent and a model", ErrInvalidModelProfiles)
		}
		key := strings.ToLower(p.Name)
		if names[key] {
			return nil, fmt.Errorf("%w: two profiles are named %q", ErrInvalidModelProfiles, p.Name)
		}
		names[key] = true
		out = append(out, p)
	}
	// Ids last: an id-less profile takes a slug of its name that no other
	// profile (already named or not) holds.
	for _, p := range out {
		if p.ID != "" {
			if ids[p.ID] {
				return nil, fmt.Errorf("%w: id %q is used twice", ErrInvalidModelProfiles, p.ID)
			}
			ids[p.ID] = true
		}
	}
	for i := range out {
		if out[i].ID != "" {
			continue
		}
		base := slugOf(out[i].Name)
		id := base
		for n := 2; ids[id]; n++ {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		ids[id] = true
		out[i].ID = id
	}
	return out, nil
}

func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "profile"
	}
	return slug
}

var modelProfilesMu sync.Mutex

// AgentModelProfiles reads the list. Unset is an empty list.
func (s *Service) AgentModelProfiles() (AgentModelProfiles, error) {
	value, _, err := s.repo.GetAgentModelProfiles()
	if err != nil {
		return AgentModelProfiles{}, err
	}
	return value.clone(), nil
}

// SetAgentModelProfiles replaces the list when the caller's revision is the
// stored one, and returns what was stored.
func (s *Service) SetAgentModelProfiles(value AgentModelProfiles) (AgentModelProfiles, error) {
	modelProfilesMu.Lock()
	defer modelProfilesMu.Unlock()
	current, _, err := s.repo.GetAgentModelProfiles()
	if err != nil {
		return AgentModelProfiles{}, err
	}
	if current.Revision != value.Revision {
		return AgentModelProfiles{}, ErrModelProfilesConflict
	}
	profiles, err := NormalizeModelProfiles(value.Profiles)
	if err != nil {
		return AgentModelProfiles{}, err
	}
	next := AgentModelProfiles{Revision: current.Revision + 1, Profiles: profiles}
	if err := s.repo.SetAgentModelProfiles(next); err != nil {
		return AgentModelProfiles{}, err
	}
	return next.clone(), nil
}
