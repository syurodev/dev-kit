package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"devkit/internal/identity"
)

const (
	// modelCatalogTTL is how long a vendor's model list is trusted. Reading it
	// starts a throw-away agent (Claude also switches to each model to read its
	// effort levels), so a caller who asks twice a minute must not pay twice.
	// Models change with a vendor release, not by the hour.
	modelCatalogTTL = 15 * time.Minute
	// modelCatalogFailureTTL is how long a failure is remembered: long enough
	// that a signed-out vendor is not respawned by every agent that asks, short
	// enough that signing in is noticed within the minute (the window's own
	// catalog read after a sign-in also replaces it at once).
	modelCatalogFailureTTL = time.Minute
	// modelCatalogFetchLimit bounds one catalog read. It runs detached from the
	// asking call so that an agent hanging up does not throw the answer away.
	modelCatalogFetchLimit = 90 * time.Second
)

// ModelChoice is one model a vendor offers, with the efforts it takes.
type ModelChoice struct {
	Value         string   `json:"value"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
}

// Model statuses of VendorModels.
const (
	ModelsOK             = "ok"
	ModelsSignInRequired = "signInRequired"
	ModelsNotInstalled   = "notInstalled"
	ModelsUnavailable    = "unavailable"
)

// VendorModels is what one vendor offers, or why it cannot say.
type VendorModels struct {
	Vendor       AgentType     `json:"vendor"`
	Status       string        `json:"status"`
	Reason       string        `json:"reason,omitempty"`
	DefaultModel string        `json:"defaultModel,omitempty"`
	Models       []ModelChoice `json:"models,omitempty"`
	// FetchedAt is when the list was read; a cached answer is that old.
	FetchedAt time.Time `json:"fetchedAt"`
	// Total, NextOffset and Hint describe a page of Models (ListModels): how
	// many models match the query, where the next page starts and how to ask
	// for the rest. Zero in the cached list itself.
	Total      int    `json:"total,omitempty"`
	NextOffset *int   `json:"nextOffset,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

// Model finds a model by its value.
func (v VendorModels) Model(value string) (ModelChoice, bool) {
	for _, m := range v.Models {
		if m.Value == value {
			return m, true
		}
	}
	return ModelChoice{}, false
}

// ModelValues lists the model values, for an error that says what would work.
func (v VendorModels) ModelValues() []string {
	out := make([]string, 0, len(v.Models))
	for _, m := range v.Models {
		out = append(out, m.Value)
	}
	return out
}

// CurrentChoice is the model and effort a session is on, from the settings
// its agent last announced. Empty when none has been announced yet.
type CurrentChoice struct {
	Vendor AgentType `json:"vendor"`
	Model  string    `json:"model,omitempty"`
	Effort string    `json:"effort,omitempty"`
}

// ModelListing is what agenthub.listModels answers.
type ModelListing struct {
	Vendors []VendorModels `json:"vendors"`
	// Current is the calling session's own choice — the default a child
	// inherits.
	Current *CurrentChoice `json:"current,omitempty"`
}

type catalogEntry struct {
	models  VendorModels
	expires time.Time
}

type modelCatalogCache struct {
	mu      sync.Mutex
	entries map[AgentType]catalogEntry
	// fetch serialises reads per vendor: two agents asking at once start one
	// throw-away agent, and the second takes the first's answer.
	fetch map[AgentType]*sync.Mutex
	now   func() time.Time
}

func (c *modelCatalogCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *modelCatalogCache) get(vendor AgentType) (VendorModels, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[vendor]
	if !ok || !c.clock().Before(entry.expires) {
		return VendorModels{}, false
	}
	return entry.models, true
}

func (c *modelCatalogCache) put(models VendorModels) {
	ttl := modelCatalogTTL
	if models.Status != ModelsOK {
		ttl = modelCatalogFailureTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[AgentType]catalogEntry{}
	}
	c.entries[models.Vendor] = catalogEntry{models: models, expires: c.clock().Add(ttl)}
}

func (c *modelCatalogCache) lock(vendor AgentType) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fetch == nil {
		c.fetch = map[AgentType]*sync.Mutex{}
	}
	if c.fetch[vendor] == nil {
		c.fetch[vendor] = &sync.Mutex{}
	}
	return c.fetch[vendor]
}

func (c *modelCatalogCache) forget(vendor AgentType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, vendor)
}

// ForgetModelCatalog drops a vendor's cached list.
func (s *Service) ForgetModelCatalog(vendor AgentType) { s.catalog.forget(vendor) }

// ModelCatalog is a vendor's model list for any caller, an agent included,
// from a cache (15 minutes; a failure for one). It never fails: a vendor that
// cannot answer comes back with a status and the reason, so one signed-out
// vendor does not hide the other.
//
// The UI's ConfigCatalog stays uncached — the person pressing Refresh wants
// the vendor's answer now — but what it reads refreshes this cache.
func (s *Service) ModelCatalog(ctx context.Context, vendor AgentType) VendorModels {
	if cached, ok := s.catalog.get(vendor); ok {
		return cached
	}
	gate := s.catalog.lock(vendor)
	gate.Lock()
	defer gate.Unlock()
	if cached, ok := s.catalog.get(vendor); ok {
		return cached
	}
	// Detached: an agent that hangs up must not discard a read that other
	// callers are about to use. Bounded, so it cannot outlive its usefulness.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelCatalogFetchLimit)
	defer cancel()
	options, err := s.readCatalog(fetchCtx, vendor)
	models := vendorModelsOf(vendor, options, err, s.catalog.clock())
	s.catalog.put(models)
	return models
}

func vendorModelsOf(vendor AgentType, options []ConfigOption, err error, at time.Time) VendorModels {
	out := VendorModels{Vendor: vendor, Status: ModelsOK, FetchedAt: at.UTC()}
	switch {
	case err == nil:
	case errors.Is(err, ErrAgentAuthRequired):
		out.Status, out.Reason = ModelsSignInRequired, "not signed in — the person signs in under Settings → Agents"
		return out
	case errors.Is(err, ErrAgentNotInstalled):
		out.Status, out.Reason = ModelsNotInstalled, "not installed — the person installs it under Settings → Agents"
		return out
	default:
		log.Printf("agenthub: model catalog for %s: %v", vendor, err)
		out.Status, out.Reason = ModelsUnavailable, "its model list could not be read"
		return out
	}
	model := findConfigOption(options, "model")
	if model == nil || len(model.Options) == 0 {
		out.Status, out.Reason = ModelsUnavailable, "it reported no models"
		return out
	}
	out.DefaultModel = model.DefaultValue
	if out.DefaultModel == "" {
		_ = json.Unmarshal(model.CurrentValue, &out.DefaultModel)
	}
	for _, option := range model.Options {
		choice := ModelChoice{Value: option.Value, Name: option.Name, Description: option.Description, DefaultEffort: option.DefaultEffort}
		for _, effort := range option.SupportedEfforts {
			choice.Efforts = append(choice.Efforts, effort.Value)
		}
		out.Models = append(out.Models, choice)
	}
	return out
}

// readCatalog asks the vendor's adapter. The same read ConfigCatalog does,
// minus the caller check: what it returns is a list of names.
func (s *Service) readCatalog(ctx context.Context, vendor AgentType) ([]ConfigOption, error) {
	adapter, ok := s.reg.Get(vendor)
	if !ok {
		return nil, ErrVendorUnknown
	}
	catalog, ok := adapter.(VendorConfigCatalog)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no model catalog", ErrUnsupported, vendor)
	}
	spec := OpenSpec{}
	s.applySandboxExtras(&spec)
	return catalog.ConfigCatalog(ctx, spec)
}

// catalogVendors are the vendors that can list their models, in a stable order.
func (s *Service) catalogVendors() []AgentType {
	var out []AgentType
	for _, t := range s.reg.Types() {
		if adapter, ok := s.reg.Get(t); ok {
			if _, has := adapter.(VendorConfigCatalog); has {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

const (
	// DefaultModelPage and MaxModelPage bound how many models one vendor puts
	// in a listModels answer. opencode offers hundreds across its providers; the
	// whole list is tens of KB, which the MCP result budget would cut blindly.
	DefaultModelPage = 40
	MaxModelPage     = 100
	// modelDescriptionClip bounds a description in a listing.
	modelDescriptionClip = 120
)

// ModelQuery narrows agenthub.listModels.
type ModelQuery struct {
	// Vendor limits the answer to one vendor; empty asks all.
	Vendor AgentType
	// Query keeps the models whose id or name holds every word (case, spaces
	// and punctuation ignored): "glm flash" finds "opencode/glm-5.3-flash-free".
	Query string
	// Limit is models per vendor (DefaultModelPage when 0, at most MaxModelPage);
	// Offset skips that many of the matches, for the next page.
	Limit, Offset int
}

// ListModels answers agenthub.listModels: every vendor's models (or why it
// cannot say), narrowed and paged by q, and what the calling session is on.
func (s *Service) ListModels(ctx context.Context, q ModelQuery) (ModelListing, error) {
	vendors := s.catalogVendors()
	if q.Vendor != "" {
		if _, ok := s.reg.Get(q.Vendor); !ok {
			return ModelListing{}, ErrVendorUnknown
		}
		vendors = []AgentType{q.Vendor}
	}
	var listing ModelListing
	for _, v := range vendors {
		listing.Vendors = append(listing.Vendors, pageModels(s.ModelCatalog(ctx, v), q))
	}
	if caller, ok := identity.FromContext(ctx); ok && caller.IsAgent() {
		if current, found := s.currentChoice(ctx, caller.SessionID); found {
			listing.Current = &current
		}
	}
	return listing, nil
}

// pageModels is a copy of one vendor's list narrowed to the query and cut to a
// page. The cached list is never touched.
func pageModels(all VendorModels, q ModelQuery) VendorModels {
	out := all
	if all.Status != ModelsOK {
		return out
	}
	words := strings.Fields(q.Query)
	for i := range words {
		words[i] = normalizeModelText(words[i])
	}
	var matched []ModelChoice
	for _, m := range all.Models {
		if modelHasWords(m, words) {
			matched = append(matched, m)
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultModelPage
	}
	limit = min(limit, MaxModelPage)
	offset := min(max(q.Offset, 0), len(matched))
	end := min(offset+limit, len(matched))
	out.Total = len(matched)
	out.Models = make([]ModelChoice, 0, end-offset)
	for _, m := range matched[offset:end] {
		out.Models = append(out.Models, compactModel(m))
	}
	switch {
	case len(matched) == 0 && len(words) > 0:
		out.Hint = fmt.Sprintf("No model of %d matches %q. Try fewer or shorter words.", len(all.Models), strings.TrimSpace(q.Query))
	case end < len(matched):
		next := end
		out.NextOffset = &next
		out.Hint = fmt.Sprintf("Showing %d-%d of %d. Pass offset %d for the next page, or `query` (words from the id or name) to narrow. Use the `value` of a model as initialConfig.model.", offset+1, end, len(matched), next)
	}
	return out
}

// modelHasWords reports whether every word (already normalized) is in the
// model's id or name.
func modelHasWords(m ModelChoice, words []string) bool {
	if len(words) == 0 {
		return true
	}
	text := normalizeModelText(m.Value) + " " + normalizeModelText(m.Name)
	for _, w := range words {
		if w != "" && !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// compactModel is a model as a listing carries it: a name only when it says
// more than the id, a short one-line description.
func compactModel(m ModelChoice) ModelChoice {
	if m.Name == m.Value {
		m.Name = ""
	}
	m.Name = clipRunes(oneLine(m.Name), modelSuggestionClip)
	m.Description = clipRunes(oneLine(m.Description), modelDescriptionClip)
	return m
}

// currentChoice reads a session's model and effort from the settings its
// agent last announced (the stored config_option_update).
func (s *Service) currentChoice(ctx context.Context, sessionID string) (CurrentChoice, bool) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return CurrentChoice{}, false
	}
	// The newest announcement may still be in the recorder's buffer.
	_ = s.recorder.Flush(ctx, sessionID)
	entry, found, err := s.store.FindTranscriptKind(ctx, sessionID, UpdateConfigOptions)
	if err != nil || !found {
		return CurrentChoice{Vendor: sess.AgentType}, false
	}
	var saved SessionUpdate
	if json.Unmarshal(entry.Payload, &saved) != nil {
		return CurrentChoice{Vendor: sess.AgentType}, false
	}
	out := CurrentChoice{Vendor: sess.AgentType}
	for _, option := range saved.ConfigOptions {
		var value string
		if json.Unmarshal(option.CurrentValue, &value) != nil {
			continue
		}
		switch option.ID {
		case "model":
			out.Model = value
		case "reasoning_effort", "effort":
			out.Effort = value
		}
	}
	return out, out.Model != "" || out.Effort != ""
}

// valuesText renders a list of values for an error an agent will read.
func valuesText(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}
