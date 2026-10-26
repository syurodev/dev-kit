package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

// ModelProfileRef selects a model: vendor, model and effort. It says nothing
// about tools, skills, servers or budgets, which a harness snapshot owns. The
// two are separate on purpose: the same model profile can run under different
// harness snapshots, and a harness snapshot never changes the model.
type ModelProfileRef struct {
	ProfileID string
	Vendor    string
	Model     string
	Effort    string
}

// SnapshotSchema versions SnapshotRef so a stored reference from another build
// is never read as this one.
const SnapshotSchema = 1

// SnapshotRef is the small, public identity of a resolved harness configuration.
// It carries no path, command, argument, instruction or environment value, and
// no endpoint token: an opaque digest or revision is never a permission.
type SnapshotRef struct {
	Schema      int
	WorkspaceID string
	SessionID   string
	Vendor      string
	// Revisions are nil for an input that did not exist; never a synthetic 0.
	Revisions Revisions
	// Fingerprint is Effective.Fingerprint: revisions, registry and the
	// vendor-defined facts that were used.
	Fingerprint string
	// ExecutableDigest covers what could run or load (servers, skill folders and
	// vendor-defined entries that were not denied), so two snapshots with the
	// same digest launch the same executables.
	ExecutableDigest string
	// FactsComplete is false when the vendor's own servers and skills could not be
	// read in full. The snapshot is then an honest statement about DevKit's
	// configuration only; it is not a verified account of what the vendor runs.
	FactsComplete bool
	// ExecutableEntries lists each executable that could run or load as a hash of
	// its name and a hash of its definition: enough to see that an entry is new
	// or altered, without storing a name, path, command or argument.
	// WorkspaceOwned names (hashed, sorted) the entries the workspace profile
	// defines; WorkspaceSuppressed the ones only its deny lists hide.
	ExecutableEntries   map[string]string
	WorkspaceOwned      []string
	WorkspaceSuppressed []string
	// Default is true for a positively resolved configuration with no workspace
	// profile and no session override: today's unprofiled behavior.
	Default bool
}

// Snapshot is a resolved effective configuration with the identity it was
// resolved for. It is a value: Clone it before handing it to code that might
// keep or mutate it.
type Snapshot struct {
	Ref       SnapshotRef
	Effective Effective
	// Vendor is the vendor customization (V) exactly as this snapshot was resolved
	// from it. A launch uses this, not a second read of Settings, so what the
	// runtime receives is what the reference says.
	Vendor Vendor
}

func cloneVendor(v Vendor) Vendor {
	v.SkillFolders = slices.Clone(v.SkillFolders)
	v.DisabledSkills = slices.Clone(v.DisabledSkills)
	v.DisabledMCP = slices.Clone(v.DisabledMCP)
	v.Servers = cloneServers(v.Servers)
	return v
}

// ExecutableDigest identifies the set of things that could run or load.
func (e Effective) ExecutableDigest() string { return e.execDigest }

func executableDigest(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	type kv struct{ K, V string }
	facts := make([]kv, 0, len(keys))
	for _, k := range keys {
		facts = append(facts, kv{k, m[k]})
	}
	b, _ := json.Marshal(facts)
	sum := sha256.Sum256(b)
	return "sha256:v1:" + hex.EncodeToString(sum[:])
}

// ServingIsDefault reports whether the MCP server would serve exactly what it
// serves today: every group on and every non-exempt tool at the default budget
// and head cut. Only a configuration that is not default needs the server to
// enforce it.
func (e Effective) ServingIsDefault() bool {
	for _, on := range e.groupOn {
		if !on {
			return false
		}
	}
	for _, b := range e.budgets {
		if !b.Exempt && (b.Bytes != DefaultResultBudget || b.Cut != CutHead) {
			return false
		}
	}
	return true
}

// Clone returns a deep copy that shares nothing with e.
func (e Effective) Clone() Effective {
	out := e
	out.Revisions = e.Revisions.Clone()
	out.Groups = slices.Clone(e.Groups)
	out.DisabledSkills = slices.Clone(e.DisabledSkills)
	out.DisabledMCP = slices.Clone(e.DisabledMCP)
	out.SkillFolders = slices.Clone(e.SkillFolders)
	out.Servers = cloneServers(e.Servers)
	out.groupOn = cloneMap(e.groupOn)
	out.budgets = cloneMap(e.budgets)
	out.executable = cloneMap(e.executable)
	return out
}

func cloneMap[V any](m map[string]V) map[string]V {
	if m == nil {
		return nil
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneServers(in []Server) []Server {
	if in == nil {
		return nil
	}
	out := make([]Server, len(in))
	for i, s := range in {
		s.Args = slices.Clone(s.Args)
		s.EnvKeys = slices.Clone(s.EnvKeys)
		out[i] = s
	}
	return out
}

// Clone copies the revision pointers.
func (r Revisions) Clone() Revisions {
	out := r
	if r.Workspace != nil {
		v := *r.Workspace
		out.Workspace = &v
	}
	if r.Session != nil {
		v := *r.Session
		out.Session = &v
	}
	return out
}

// Clone returns a snapshot that shares nothing with s.
func (s Snapshot) Clone() Snapshot {
	out := s
	out.Ref = s.Ref.Clone()
	out.Effective = s.Effective.Clone()
	out.Vendor = cloneVendor(s.Vendor)
	return out
}

// Equal reports whether two references name the same configuration: same
// identity, revisions, fingerprint and executables.
func (r SnapshotRef) Equal(o SnapshotRef) bool {
	return r.Schema == o.Schema && r.WorkspaceID == o.WorkspaceID && r.SessionID == o.SessionID && r.Vendor == o.Vendor &&
		sameRev(r.Revisions.Workspace, o.Revisions.Workspace) && sameRev(r.Revisions.Session, o.Revisions.Session) &&
		r.Revisions.Vendor == o.Revisions.Vendor && r.Fingerprint == o.Fingerprint &&
		r.ExecutableDigest == o.ExecutableDigest && r.FactsComplete == o.FactsComplete && r.Default == o.Default &&
		slices.Equal(r.WorkspaceOwned, o.WorkspaceOwned) && slices.Equal(r.WorkspaceSuppressed, o.WorkspaceSuppressed)
}

// AdoptsNewExecutables reports whether reopening with r instead of what was
// applied (previous) would bring in an executable that is new or altered because
// of the workspace profile: an entry the profile defines (now, or before and
// since replaced), or one its deny lists stopped hiding. Removals and narrowing
// never count. A change that comes from the vendor customization or the vendor's
// own files is not this rule's business, even in the same reopen as a workspace
// edit that only narrows: a reload has always adopted those.
func (r SnapshotRef) AdoptsNewExecutables(previous SnapshotRef) bool {
	owned := hashSet(r.WorkspaceOwned)
	prevOwned, prevSuppressed := hashSet(previous.WorkspaceOwned), hashSet(previous.WorkspaceSuppressed)
	for key, shape := range r.ExecutableEntries {
		if old, ok := previous.ExecutableEntries[key]; ok && old == shape {
			continue
		}
		if owned[key] || prevOwned[key] || prevSuppressed[key] {
			return true
		}
	}
	return false
}

func hashSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

// ExecutableEntries is the hashed entry table a SnapshotRef carries.
func (e Effective) ExecutableEntries() map[string]string {
	out := make(map[string]string, len(e.executable))
	for k, v := range e.executable {
		out[shortHash(k)] = shortHash(v)
	}
	return out
}

// WorkspaceOwned and WorkspaceSuppressed are the hashed, sorted ownership tables
// a SnapshotRef carries (see AdoptsNewExecutables).
func (e Effective) WorkspaceOwned() []string      { return hashedKeys(e.owned) }
func (e Effective) WorkspaceSuppressed() []string { return hashedKeys(e.suppressed) }

func hashedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, shortHash(k))
	}
	slices.Sort(out)
	return out
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func sameRev(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Clone returns a copy of the reference that shares no pointer or map with r.
func (r SnapshotRef) Clone() SnapshotRef {
	out := r
	out.Revisions = r.Revisions.Clone()
	out.ExecutableEntries = cloneMap(r.ExecutableEntries)
	out.WorkspaceOwned = slices.Clone(r.WorkspaceOwned)
	out.WorkspaceSuppressed = slices.Clone(r.WorkspaceSuppressed)
	return out
}
