package harnessconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// Stale-profile repair (F-C of the AP1 review).
//
// A stored workspace profile (W) can name a tool group, or a budgeted tool or
// group, that the server no longer registers (an optional module that is not wired
// in this build, a renamed group). The resolver rightly refuses such a profile, so
// no session of the workspace can start and no ordinary edit validates. This is the
// one narrow way out: the window can ask for the stale references to be removed, sees
// every one of them, and approves through the same shared confirmation, once.
//
// What it is not: the normal resolver is untouched and stays strict; there is no
// silent ignore, no reset of the profile and no edit of anything but those keys.
// Removing an "off" entry loses a remembered denial, so the operation is judged as
// potentially widening and always asks, whatever the auto-narrowing setting.

// TargetRepair is the Target of a prepared stale-profile repair. It exists for the
// confirmation and for the outcome's wording; no Request can carry it.
const TargetRepair Target = "repair"

// StaleKind says which map of W an entry sits in.
type StaleKind string

const (
	// StaleGroup is a W.ToolGroups entry for a group the registry does not have.
	StaleGroup StaleKind = "group"
	// StaleToolBudget is a W.ResultBudget.Tools rule for a tool the registry does not have.
	StaleToolBudget StaleKind = "toolBudget"
	// StaleGroupBudget is a W.ResultBudget.Groups rule for a group the registry does not have.
	StaleGroupBudget StaleKind = "groupBudget"
)

// StaleEntry is one stale reference with the saved value that removing it loses.
// Name is untrusted stored text, shown bounded and without control characters.
type StaleEntry struct {
	Kind StaleKind `json:"kind"`
	Name string    `json:"name"`
	// On is the saved setting of a stale group (true: on, false: off).
	On *bool `json:"on,omitempty"`
	// Bytes and Cut are the saved limit of a stale budget rule.
	Bytes *int   `json:"bytes,omitempty"`
	Cut   string `json:"cut,omitempty"`
}

// StaleState is the diagnosis of a workspace's stored profile.
type StaleState string

const (
	// StaleNone: no profile, or one with no stale reference that resolves. Nothing to repair.
	StaleNone StaleState = "none"
	// StaleRepairable: stale references exist and removing exactly them leaves a valid profile.
	StaleRepairable StaleState = "stale"
	// StaleUnrepairable: the profile is unreadable or invalid for a reason this repair does not cover.
	StaleUnrepairable StaleState = "unrepairable"
)

// MaxStaleEntries bounds the diagnosis and a repair. A profile with more stale
// references than this is shown in part and cannot be prepared: an approval must
// cover every removal.
const MaxStaleEntries = 64

const maxStaleNameRunes = 128

// StaleDiagnosis is what the window shows for a workspace's stored profile. It
// carries no server, command, path, skill or raw JSON.
type StaleDiagnosis struct {
	WorkspaceID     string       `json:"workspaceId"`
	ProfileRevision int64        `json:"profileRevision"`
	ProfilePresent  bool         `json:"profilePresent"`
	State           StaleState   `json:"state"`
	Entries         []StaleEntry `json:"entries"`
	// Total counts every stale reference; Entries holds at most MaxStaleEntries.
	Total int `json:"total"`
	// Truncated: Total is larger than the listed entries, so a repair cannot be prepared.
	Truncated bool `json:"truncated,omitempty"`
	// Message is fixed host text explaining an unrepairable state.
	Message string `json:"message,omitempty"`
}

// Fixed refusals of the repair.
var (
	ErrRepairUnavailable = &harness.Error{Code: harness.CodeProfileInvalid, Message: "This workspace profile has a problem that stale-setting repair does not cover, so it cannot be repaired here."}
	ErrRepairTooLarge    = &harness.Error{Code: harness.CodeNotAvailable, Message: "Too many saved settings are stale to review in one step, so nothing can be repaired from here."}
)

const (
	messageUnreadable = "The stored profile cannot be read, so it cannot be repaired here."
	messageInvalid    = "The profile has other invalid settings besides stale ones, so it cannot be repaired here."
	messageNoRegistry = "The tool registry is not available, so stale settings cannot be told apart."
)

// InspectStale lists the references in w that the registry does not know, in a
// canonical order (kind, then name). It is pure: it reads only w and the registry.
// A tool that exists but is exempt from cutting is NOT stale (a budget on it is a
// different problem the resolver reports).
func InspectStale(w *harness.Profile, reg harness.Registry) []StaleEntry {
	if w == nil {
		return nil
	}
	tools := make(map[string]bool, len(reg.Tools))
	groups := map[string]bool{}
	for _, t := range reg.Tools {
		tools[t.Name] = true
		groups[harness.Group(t.Name)] = true
	}
	var out []StaleEntry
	for name, on := range w.ToolGroups {
		if !groups[name] {
			v := on
			out = append(out, StaleEntry{Kind: StaleGroup, Name: name, On: &v})
		}
	}
	if w.ResultBudget != nil {
		for name, rule := range w.ResultBudget.Tools {
			if !tools[name] {
				out = append(out, ruleEntry(StaleToolBudget, name, rule))
			}
		}
		for name, rule := range w.ResultBudget.Groups {
			if !groups[name] {
				out = append(out, ruleEntry(StaleGroupBudget, name, rule))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func ruleEntry(kind StaleKind, name string, rule harness.BudgetRule) StaleEntry {
	e := StaleEntry{Kind: kind, Name: name, Cut: string(rule.Cut)}
	if rule.Bytes != nil {
		v := *rule.Bytes
		e.Bytes = &v
	}
	return e
}

// RepairedProfile is a copy of w without exactly the given entries. Every other
// field is kept; a map left empty becomes absent, as the budget editor leaves it.
func RepairedProfile(w *harness.Profile, entries []StaleEntry) *harness.Profile {
	out := clone(w)
	if out == nil {
		return nil
	}
	for _, e := range entries {
		switch e.Kind {
		case StaleGroup:
			delete(out.ToolGroups, e.Name)
		case StaleToolBudget:
			if out.ResultBudget != nil {
				delete(out.ResultBudget.Tools, e.Name)
			}
		case StaleGroupBudget:
			if out.ResultBudget != nil {
				delete(out.ResultBudget.Groups, e.Name)
			}
		}
	}
	if len(out.ToolGroups) == 0 {
		out.ToolGroups = nil
	}
	if rb := out.ResultBudget; rb != nil {
		if len(rb.Tools) == 0 {
			rb.Tools = nil
		}
		if len(rb.Groups) == 0 {
			rb.Groups = nil
		}
		if rb.Tools == nil && rb.Groups == nil {
			out.ResultBudget = nil
		}
	}
	return out
}

// displayName is a stored name made safe to show: printable runes only, bounded.
func displayName(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			r = '?'
		}
		if n == maxStaleNameRunes {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// shown is the entry as the window sees it: the same values, a safe name.
func (e StaleEntry) shown() StaleEntry {
	e.Name = displayName(e.Name)
	return e
}

// repairScan is one reading of everything a repair is bound to.
type repairScan struct {
	state   StaleState
	message string
	w       *harness.Profile
	stamp   harness.Stamp
	entries []StaleEntry // all of them, real names, canonical order
	// repaired is w without entries; nil unless state is StaleRepairable.
	repaired *harness.Profile
	binding  repairBinding

	// The session repair's own readings (scanStaleSession): S and its stamp, the
	// groups of S the registry lacks, S without them (nil: delete S) and whether the
	// workspace profile, not S, is what stops the result from resolving.
	o            *harness.Override
	oStamp       harness.Stamp
	staleGroups  []string
	repairedS    *harness.Override
	needsProfile bool
}

// repairBinding is everything an approval of a repair is bound to. A second scan
// of the same workspace must produce an equal digest or the repair is stale.
type repairBinding struct {
	WorkspaceID string
	W           harness.Stamp
	Removed     []StaleEntry
	Registry    string
	Vendors     []string
	Policy      string
	Repaired    string
	// A session repair also binds the session, its S stamp and the groups to remove.
	SessionID     string
	S             harness.Stamp
	RemovedGroups []string
}

func (b repairBinding) digest() string {
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// registryIdentity is a digest of the tool names and cut exemptions: the facts that
// decide what is stale and which budgets are valid.
func registryIdentity(reg harness.Registry) string {
	lines := make([]string, 0, len(reg.Tools))
	for _, t := range reg.Tools {
		lines = append(lines, fmt.Sprintf("%s\x00%t", t.Name, t.BudgetExempt))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func profileDigest(p *harness.Profile) string {
	c := clone(p)
	c.Revision = 0
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// scanStale reads the stored profile, the registry, V and the policy and decides
// what a repair would be. It writes nothing. A store failure other than a corrupt
// record is returned as is.
func (s *Service) scanStale(ctx context.Context, workspaceID string) (repairScan, error) {
	var sc repairScan
	w, stamp, err := s.cfg.Store.GetProfile(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, harness.ErrStoreCorrupt) {
			sc.state, sc.message = StaleUnrepairable, messageUnreadable
			return sc, nil
		}
		return sc, storeErr(err)
	}
	sc.w, sc.stamp = w, stamp
	if w == nil {
		sc.state = StaleNone
		return sc, nil
	}
	reg := s.cfg.Registry()
	if len(reg.Tools) == 0 {
		sc.state, sc.message = StaleUnrepairable, messageNoRegistry
		return sc, nil
	}
	if w.Schema != harness.SchemaVersion {
		sc.state, sc.message = StaleUnrepairable, messageInvalid
		return sc, nil
	}
	sc.entries = InspectStale(w, reg)
	candidate := w
	if len(sc.entries) > 0 {
		candidate = RepairedProfile(w, sc.entries)
	}
	if err := s.validateWhole(ctx, candidate, stamp); err != nil {
		if errors.Is(err, errVendorsUnavailable) {
			return sc, ErrFactsUnavailable
		}
		sc.state, sc.message = StaleUnrepairable, messageInvalid
		return sc, nil
	}
	if len(sc.entries) == 0 {
		sc.state = StaleNone
		return sc, nil
	}
	policy, err := s.cfg.Policy.Policy(ctx)
	if err != nil {
		return sc, ErrDenied
	}
	vendors, err := s.vendorRevisions(ctx)
	if err != nil {
		return sc, ErrFactsUnavailable
	}
	sc.state, sc.repaired = StaleRepairable, candidate
	sc.binding = repairBinding{WorkspaceID: workspaceID, W: stamp, Removed: sc.entries, Registry: registryIdentity(reg),
		Vendors: vendors, Policy: policy.Revision, Repaired: profileDigest(candidate)}
	return sc, nil
}

var errVendorsUnavailable = errors.New("vendors unavailable")

// validateWhole runs the profile through the existing validator and through the
// resolver for every vendor a workspace profile applies to, exactly as an ordinary
// workspace edit would be. A failure of the profile itself is returned as is.
func (s *Service) validateWhole(ctx context.Context, w *harness.Profile, stamp harness.Stamp) error {
	if err := s.cfg.Validator.Validate(ctx, *w); err != nil {
		return err
	}
	registry := s.cfg.Registry()
	for _, name := range nonEmpty(s.cfg.Vendors.Names()) {
		vs, err := s.cfg.Vendors.State(ctx, name)
		if err != nil {
			return errVendorsUnavailable
		}
		env := vs.Environment
		if !vs.FactsComplete {
			env = harness.Environment{}
		}
		if _, err := harness.Resolve(harness.Inputs{Registry: registry, Vendor: vs.Vendor, Environment: env,
			Workspace: withRevision(w, stamp.Revision), Canonical: s.cfg.Canonical}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) vendorRevisions(ctx context.Context) ([]string, error) {
	var out []string
	for _, name := range nonEmpty(s.cfg.Vendors.Names()) {
		vs, err := s.cfg.Vendors.State(ctx, name)
		if err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s:%d", name, vs.Vendor.Revision))
	}
	sort.Strings(out)
	return out, nil
}

// requireUI is the Go-side half of "the window only".
func requireUICaller(ctx context.Context) (identity.Caller, error) {
	caller, ok := identity.FromContext(ctx)
	if !ok || caller.Kind != identity.KindUI {
		return caller, ErrDenied
	}
	return caller, nil
}

// DiagnoseStale reports whether a workspace's stored profile has stale references,
// with each one. It needs no session and does not depend on the profile resolving.
// UI callers only; an agent, a terminal or an unknown origin is refused.
func (s *Service) DiagnoseStale(ctx context.Context, workspaceID string) (StaleDiagnosis, error) {
	if _, err := requireUICaller(ctx); err != nil {
		return StaleDiagnosis{}, err
	}
	if workspaceID == "" {
		return StaleDiagnosis{}, ErrInvalid
	}
	if exists, err := s.cfg.Directory.WorkspaceExists(ctx, workspaceID); err != nil || !exists {
		return StaleDiagnosis{}, ErrScopeGone
	}
	sc, err := s.scanStale(ctx, workspaceID)
	if err != nil {
		return StaleDiagnosis{}, err
	}
	d := StaleDiagnosis{WorkspaceID: workspaceID, ProfileRevision: sc.stamp.Revision, ProfilePresent: sc.stamp.Present,
		State: sc.state, Entries: []StaleEntry{}, Total: len(sc.entries), Message: sc.message}
	for i, e := range sc.entries {
		if i == MaxStaleEntries {
			d.Truncated = true
			break
		}
		d.Entries = append(d.Entries, e.shown())
	}
	if sc.state != StaleRepairable {
		// Nothing is offered for removal in a state that cannot be repaired.
		d.Entries, d.Total, d.Truncated = []StaleEntry{}, 0, false
	}
	return d, nil
}

// PrepareRepair derives the complete removal set from the stored profile and the
// registry and keeps it pending for the UI caller that asked. The caller supplies
// only the workspace: never a profile, a key, a target or a flag. It writes nothing
// and asks nobody; Apply asks.
func (s *Service) PrepareRepair(ctx context.Context, workspaceID string) (Prepared, error) {
	caller, err := requireUICaller(ctx)
	if err != nil {
		return Prepared{}, err
	}
	if workspaceID == "" {
		return Prepared{}, ErrInvalid
	}
	if exists, err := s.cfg.Directory.WorkspaceExists(ctx, workspaceID); err != nil || !exists {
		return Prepared{}, ErrScopeGone
	}
	sc, err := s.scanStale(ctx, workspaceID)
	if err != nil {
		return Prepared{}, err
	}
	switch sc.state {
	case StaleNone:
		return Prepared{}, ErrNoChange
	case StaleUnrepairable:
		return Prepared{}, ErrRepairUnavailable
	}
	if len(sc.entries) > MaxStaleEntries {
		return Prepared{}, ErrRepairTooLarge
	}
	view, err := buildRepairView(sc.entries)
	if err != nil {
		return Prepared{}, err
	}
	id, err := newID()
	if err != nil {
		return Prepared{}, err
	}
	b := sc.binding
	p := &pendingProposal{id: id, caller: caller.String(), req: Request{WorkspaceID: workspaceID, Target: TargetRepair}, view: view, repair: &b}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) >= maxPending {
		return Prepared{}, ErrBusy
	}
	s.pending[id] = p
	return Prepared{ID: id, View: view}, nil
}

// buildRepairView writes the confirmation for a repair. Every removal is listed;
// the consequences of removing an off entry and a limit are stated.
func buildRepairView(entries []StaleEntry) (View, error) {
	v := View{Target: TargetRepair, Tier: harness.TierWidening.String(), NeedsConfirmation: true}
	var b strings.Builder
	fmt.Fprintf(&b, "Repair this workspace's saved harness profile. %d saved setting(s) refer to tool groups or tools this build does not have, which stops new sessions from starting. Approving removes exactly these and changes nothing else:", len(entries))
	for _, e := range entries {
		line := repairLine(e.shown())
		v.Changes = append(v.Changes, ViewChange{Field: staleField(e.Kind), Key: displayName(e.Name), Effect: line, Scope: ScopeStored})
		fmt.Fprintf(&b, "\n- %s", line)
	}
	b.WriteString("\nEffect: widening. A removed \"off\" setting is forgotten: if that group returns later (a module or build adds it back) it uses the default, which is available, instead of what was saved. Removed result limits are not restored either.")
	b.WriteString("\nSessions already running keep what they opened with. The repaired profile applies when a session is next opened or reloaded, and the usual checks on servers and skills still apply then. No session is reloaded now.")
	b.WriteString("\nIf you would rather restore the missing module or build, cancel and do that instead.")
	if len(v.Changes) > maxViewChanges || b.Len() > maxSummaryBytes {
		return View{}, ErrTooLargeToShow
	}
	v.Summary = b.String()
	return v, nil
}

func staleField(k StaleKind) string {
	switch k {
	case StaleGroup:
		return "toolGroups"
	case StaleToolBudget:
		return "resultBudget.tools"
	}
	return "resultBudget.groups"
}

// repairLine is one removal as a fixed sentence around the (untrusted) name and the
// saved value that is lost.
func repairLine(e StaleEntry) string {
	switch e.Kind {
	case StaleGroup:
		state := "on"
		if e.On != nil && !*e.On {
			state = "off"
		}
		return fmt.Sprintf("remove the saved %q setting for tool group %q", state, e.Name)
	case StaleToolBudget:
		return fmt.Sprintf("remove the saved result limit for tool %q (%s)", e.Name, ruleText(e))
	}
	return fmt.Sprintf("remove the saved result limit for tool group %q (%s)", e.Name, ruleText(e))
}

func ruleText(e StaleEntry) string {
	var parts []string
	if e.Bytes != nil {
		parts = append(parts, fmt.Sprintf("size %d bytes", *e.Bytes))
	}
	if e.Cut != "" {
		parts = append(parts, "cut "+e.Cut)
	}
	if len(parts) == 0 {
		return "no value"
	}
	return strings.Join(parts, ", ")
}

// applyRepair is Apply for a prepared repair: it always asks (the auto-narrowing
// setting is never consulted), then under the same validation boundary and with a
// fresh scan requires the very same binding before one compare-and-swap write of
// the repaired profile. Nothing about a session, S or a running endpoint is touched.
func (s *Service) applyRepair(ctx context.Context, p *pendingProposal) (Result, error) {
	if p.repair.SessionID != "" {
		return s.applySessionRepair(ctx, p)
	}
	fail := func(err error) (Result, error) {
		s.auditRepair(p, harness.Applied{}, outcomeOf(err))
		return Result{}, err
	}
	if s.cfg.Confirmer == nil || !s.cfg.Confirmer.Available() || s.cfg.Boundary == nil {
		return fail(ErrConfirmUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return fail(ErrNotConfirmed)
	}
	if err := s.confirm(ctx, p, Confirmation{Target: TargetRepair, Tier: p.view.Tier, Summary: p.view.Summary}); err != nil {
		return fail(err)
	}
	var (
		applied harness.Applied
		commit  CommitState
	)
	err := s.cfg.Boundary.Hold(ctx, func() error {
		sc, err := s.scanStale(ctx, p.repair.WorkspaceID)
		if err != nil {
			return refusalFor(err)
		}
		if sc.stamp != p.repair.W {
			return ErrConflict
		}
		if sc.state != StaleRepairable || len(sc.entries) > MaxStaleEntries || sc.binding.digest() != p.repair.digest() {
			return ErrStale
		}
		if !s.beginCommit(p) {
			return ErrCancelled
		}
		write := harness.StoreWrite{WorkspaceID: p.repair.WorkspaceID,
			Profile: &harness.ProfileWrite{Expect: sc.stamp, Value: sc.repaired}}
		a, err := s.cfg.Store.Apply(ctx, write)
		switch {
		case err == nil:
			applied, commit = a, CommitCommitted
		case storeCommitUnknown(err):
			rctx, cancel := hostContext(ctx)
			defer cancel()
			_, st, rerr := s.cfg.Store.GetProfile(rctx, p.repair.WorkspaceID)
			switch {
			case rerr != nil:
				commit = CommitUnknown
			case st.Revision == sc.stamp.Revision+1:
				applied, commit = harness.Applied{Profile: st}, CommitCommitted
			case st.Revision == sc.stamp.Revision:
				return ErrStale // proven: nothing was written
			default:
				commit = CommitUnknown
			}
		default:
			return mapApplyErr(err)
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	res := Result{Profile: applied.Profile, Commit: commit, ReloadRequired: true}
	if commit == CommitUnknown {
		s.auditRepair(p, harness.Applied{}, "unknown")
		return res, &PublicationError{Result: res, Cause: harness.ErrStoreCommitUnknown}
	}
	s.auditRepair(p, applied, "applied")
	return res, nil
}

func (s *Service) auditRepair(p *pendingProposal, applied harness.Applied, outcome string) {
	if s.cfg.Audit == nil {
		return
	}
	s.cfg.Audit(Event{Outcome: outcome, Tier: p.view.Tier, Target: p.view.Target, Caller: p.caller,
		WorkspaceID: p.repair.WorkspaceID, SessionID: p.repair.SessionID, Profile: applied.Profile.Revision, Override: applied.Override.Revision})
}
