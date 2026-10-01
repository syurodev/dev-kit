package apiclient

import (
	"encoding/json"
	"sort"
	"strings"

	"devkit/internal/vaultitem"
)

// Variables come from three scopes, the three a Postman user already thinks
// in, and a nearer one wins:
//
//	globals < collection (root folder first, nearest folder last) < environment
//
// Each scope has plain variables and secret ones. Plain values are returned
// to the window so it can show them; a secret is named and never returned —
// it is sealed in the item's payload and opened only inside Send. The
// environment is the only scope with an env tag, and the tag is what gates a
// send, which is why a send still needs one even when every variable it uses
// comes from somewhere else.

// Scope names where a resolved variable came from.
type Scope string

const (
	ScopeGlobal      Scope = "global"
	ScopeCollection  Scope = "collection"
	ScopeEnvironment Scope = "environment"
)

// VarsUpdate is an edit to one scope's variables.
//
// Secrets are write-only, so an editor cannot send back what it was never
// shown. It says instead which secret names to keep as they are; any
// existing secret neither kept nor given a new value is removed.
type VarsUpdate struct {
	Vars        map[string]string `json:"vars"`
	Secrets     map[string]string `json:"secrets"`
	KeepSecrets []string          `json:"keepSecrets"`
}

// VarSet is one scope's variables as the window sees them.
type VarSet struct {
	Vars       map[string]string `json:"vars"`
	SecretVars []string          `json:"secretVars"`
}

// ResolvedVar is one variable as a request would see it, after precedence.
type ResolvedVar struct {
	Name string `json:"name"`
	// Value is empty for a secret.
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
	Scope  Scope  `json:"scope"`
	// ScopeName is the collection or environment it came from.
	ScopeName string `json:"scopeName"`
}

// apply merges an update into the secrets a scope has now.
func (u VarsUpdate) apply(current map[string]string) (map[string]string, map[string]string, error) {
	vars := map[string]string{}
	for key, value := range u.Vars {
		if strings.TrimSpace(key) == "" {
			continue
		}
		vars[key] = value
	}
	secrets := map[string]string{}
	for _, name := range u.KeepSecrets {
		if value, ok := current[name]; ok {
			secrets[name] = value
		}
	}
	for key, value := range u.Secrets {
		if strings.TrimSpace(key) == "" {
			continue
		}
		secrets[key] = value
	}
	// One name in both halves would make which value goes out depend on map
	// order. The secret half is the one somebody chose to hide.
	for key := range secrets {
		delete(vars, key)
	}
	return vars, secrets, nil
}

func openSecrets(items ItemStore, record vaultitem.Record, keyID string) (map[string]string, error) {
	payload, err := items.OpenPayload(record, keyID)
	if err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &secrets); err != nil {
			return nil, err
		}
	}
	return secrets, nil
}

// ---- globals ---------------------------------------------------------

// globalRecords lists every globals item, oldest first. There should be one,
// but two devices that each set a global before their first sync each made
// one, and both are somebody's work.
func (s *Service) globalRecords() ([]vaultitem.Record, error) {
	records, err := s.items.ListByType(vaultitem.ItemTypeAPIGlobals)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].CreatedAt.Before(records[j].CreatedAt)
		}
		return records[i].ID < records[j].ID
	})
	return records, nil
}

// globalValues merges every globals item, a later one winning per name.
func (s *Service) globalValues() (vars, secrets map[string]string, records []vaultitem.Record, err error) {
	records, err = s.globalRecords()
	if err != nil {
		return nil, nil, nil, err
	}
	vars, secrets = map[string]string{}, map[string]string{}
	for _, record := range records {
		var meta globalsMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		for key, value := range meta.Vars {
			vars[key] = value
		}
		if sealed, openErr := openSecrets(s.items, record, KeyIDGlobals); openErr == nil {
			for key, value := range sealed {
				secrets[key] = value
			}
		}
	}
	return vars, secrets, records, nil
}

// Globals returns the global variables, secret values withheld.
func (s *Service) Globals() (VarSet, error) {
	vars, secrets, _, err := s.globalValues()
	if err != nil {
		return VarSet{}, err
	}
	return VarSet{Vars: vars, SecretVars: sortedKeys(secrets)}, nil
}

// SetGlobals replaces the global variables. It writes to the oldest item and
// removes any other, so two devices' globals end up one set again once
// somebody edits them.
func (s *Service) SetGlobals(update VarsUpdate) (VarSet, error) {
	_, current, records, err := s.globalValues()
	if err != nil {
		return VarSet{}, err
	}
	vars, secrets, err := update.apply(current)
	if err != nil {
		return VarSet{}, err
	}
	raw, err := json.Marshal(globalsMetadata{Vars: vars})
	if err != nil {
		return VarSet{}, err
	}
	payload, err := json.Marshal(secrets)
	if err != nil {
		return VarSet{}, err
	}
	if len(records) == 0 {
		if _, err := s.items.Create(vaultitem.ItemTypeAPIGlobals, KeyIDGlobals, raw, payload); err != nil {
			return VarSet{}, err
		}
	} else {
		if _, err := s.items.Update(records[0].ID, KeyIDGlobals, raw, payload); err != nil {
			return VarSet{}, err
		}
		for _, extra := range records[1:] {
			if err := s.items.Delete(extra.ID); err != nil {
				return VarSet{}, err
			}
		}
	}
	return VarSet{Vars: vars, SecretVars: sortedKeys(secrets)}, nil
}

// ---- collection variables -------------------------------------------

// collectionVars opens a collection's variables. An empty payload — every
// collection sealed before variables existed — is none.
func (s *Service) collectionVars(record vaultitem.Record) (collectionPayload, error) {
	raw, err := s.items.OpenPayload(record, KeyIDCollection)
	if err != nil {
		return collectionPayload{}, err
	}
	var out collectionPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return collectionPayload{}, err
		}
	}
	return out, nil
}

// SetCollectionVariables replaces one collection's (or folder's) variables.
func (s *Service) SetCollectionVariables(id string, update VarsUpdate) (Collection, error) {
	record, err := s.items.Get(id)
	if err != nil || record.Type != vaultitem.ItemTypeAPICollection {
		return Collection{}, ErrNotFound
	}
	var meta collectionMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Collection{}, err
	}
	current, err := s.collectionVars(record)
	if err != nil {
		return Collection{}, err
	}
	vars, secrets, err := update.apply(current.Secrets)
	if err != nil {
		return Collection{}, err
	}
	payload, err := json.Marshal(collectionPayload{Vars: vars, Secrets: secrets})
	if err != nil {
		return Collection{}, err
	}
	// The metadata goes back as it was read: Update replaces both halves.
	if _, err := s.items.Update(id, KeyIDCollection, record.Metadata, payload); err != nil {
		return Collection{}, err
	}
	return Collection{
		ID: id, Name: meta.Name, ParentID: meta.ParentID, Order: meta.Order,
		Vars: vars, SecretVars: sortedKeys(secrets),
	}, nil
}

// collectionChain is a collection and its ancestors, root first — the order
// their variables apply in, so the nearest folder is applied last and wins.
func (s *Service) collectionChain(id string) []vaultitem.Record {
	var chain []vaultitem.Record
	seen := map[string]bool{}
	for at, depth := id, 0; at != "" && depth < maxCollectionDepth && !seen[at]; depth++ {
		seen[at] = true
		record, err := s.items.Get(at)
		if err != nil || record.Type != vaultitem.ItemTypeAPICollection {
			break
		}
		chain = append([]vaultitem.Record{record}, chain...)
		var meta collectionMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			break
		}
		at = meta.ParentID
	}
	return chain
}

// ---- resolution ------------------------------------------------------

type resolvedScope struct {
	vars    map[string]string
	secret  map[string]bool
	sources map[string]ResolvedVar
	// envTag is the environment's tag, empty with no environment.
	envTag string
}

// resolve layers the three scopes. environmentID may be empty — the window
// asks what a request would see before an environment is picked — but Send
// never passes an empty one.
func (s *Service) resolve(collectionID, environmentID string) (resolvedScope, error) {
	out := resolvedScope{
		vars:    map[string]string{},
		secret:  map[string]bool{},
		sources: map[string]ResolvedVar{},
	}
	put := func(name, value string, secret bool, scope Scope, scopeName string) {
		out.vars[name] = value
		out.secret[name] = secret
		shown := value
		if secret {
			shown = ""
		}
		out.sources[name] = ResolvedVar{Name: name, Value: shown, Secret: secret, Scope: scope, ScopeName: scopeName}
	}

	globalVars, globalSecrets, _, err := s.globalValues()
	if err != nil {
		return out, err
	}
	for key, value := range globalVars {
		put(key, value, false, ScopeGlobal, "Globals")
	}
	for key, value := range globalSecrets {
		put(key, value, true, ScopeGlobal, "Globals")
	}

	for _, record := range s.collectionChain(collectionID) {
		var meta collectionMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		sealed, openErr := s.collectionVars(record)
		if openErr != nil {
			continue
		}
		for key, value := range sealed.Vars {
			put(key, value, false, ScopeCollection, meta.Name)
		}
		for key, value := range sealed.Secrets {
			put(key, value, true, ScopeCollection, meta.Name)
		}
	}

	if environmentID != "" {
		record, err := s.items.Get(environmentID)
		if err != nil || record.Type != vaultitem.ItemTypeAPIEnvironment {
			return out, ErrNotFound
		}
		var meta environmentMetadata
		if err := decodeInto(record.Metadata, &meta); err != nil {
			return out, err
		}
		secrets, err := openSecrets(s.items, record, KeyIDEnvironment)
		if err != nil {
			return out, err
		}
		for key, value := range meta.Vars {
			put(key, value, false, ScopeEnvironment, meta.Name)
		}
		for key, value := range secrets {
			put(key, value, true, ScopeEnvironment, meta.Name)
		}
		out.envTag = meta.Env
	}
	return out, nil
}

// plain is the resolved variables without any secret — what Copy as cURL and
// the window may see.
func (r resolvedScope) plain() map[string]string {
	out := map[string]string{}
	for key, value := range r.vars {
		if !r.secret[key] {
			out[key] = value
		}
	}
	return out
}

// VariablesFor lists what a request in collectionID would see under
// environmentID, after precedence, secret values withheld. The window reads it
// to say where each {{name}} comes from and which have no value anywhere.
func (s *Service) VariablesFor(collectionID, environmentID string) ([]ResolvedVar, error) {
	resolved, err := s.resolve(collectionID, environmentID)
	if err != nil {
		return nil, err
	}
	out := make([]ResolvedVar, 0, len(resolved.sources))
	for _, source := range resolved.sources {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// PlainVariables is VariablesFor as a map, secrets left out.
func (s *Service) PlainVariables(collectionID, environmentID string) (map[string]string, error) {
	resolved, err := s.resolve(collectionID, environmentID)
	if err != nil {
		return nil, err
	}
	return resolved.plain(), nil
}
