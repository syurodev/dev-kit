package harness

import (
	"slices"
	"sync"
)

// ServingBasis is the frozen input of a running session's live serving policy:
// the registry, V and W exactly as the launch resolved them, and every path the
// launch canonicalized, with the answer it got. A live change to S is resolved
// against this and the latest durable S, with the ordinary Resolve merge.
//
// It is immutable and detached from its sources. It exists because neither the
// launch's SnapshotRef (digests only) nor a merged Effective can undo S against
// the W the session really started with, and because the current saved W, V or a
// retargeted symlink must never leak into a running endpoint. S changes tool
// groups only, so budgets, deny lists, servers, skill folders and the
// executable digest stay those of the launch; Resolve checks that and refuses
// otherwise.
type ServingBasis struct {
	registry  Registry
	vendor    Vendor
	env       Environment
	workspace *Profile
	wStamp    Stamp
	sStamp    Stamp // S as the launch read it
	canon     map[string]canonAnswer
	launch    Effective
	execDig   string
	factsDone bool
}

// ServingIdentity names exactly what an endpoint serves: the durable S stamp it
// was resolved from (an absent S keeps the revision of its tombstone) and the
// fingerprint of the effective configuration. It is comparable, so two
// identities are equal only when the stamp, its presence and the resolved
// configuration all are. It carries no tool, path or value.
type ServingIdentity struct {
	Session     Stamp
	Fingerprint string
}

type canonAnswer struct {
	path string
	err  bool
}

// CanonRecorder wraps the host's canonicalizer during one launch resolution and
// remembers each answer, so the basis can replay exactly what the launch saw.
type CanonRecorder struct {
	next func(string) (string, error)
	mu   sync.Mutex
	seen map[string]canonAnswer
}

// NewCanonRecorder records next, or the lexical rule when next is nil (the same
// default Resolve applies).
func NewCanonRecorder(next func(string) (string, error)) *CanonRecorder {
	if next == nil {
		next = lexical
	}
	return &CanonRecorder{next: next, seen: map[string]canonAnswer{}}
}

// Canonical is the function to put in Inputs.Canonical.
func (r *CanonRecorder) Canonical(path string) (string, error) {
	real, err := r.next(path)
	r.mu.Lock()
	r.seen[path] = canonAnswer{path: real, err: err != nil}
	r.mu.Unlock()
	return real, err
}

func (r *CanonRecorder) frozen() map[string]canonAnswer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]canonAnswer, len(r.seen))
	for k, v := range r.seen {
		out[k] = v
	}
	return out
}

// NewServingBasis freezes the inputs a launch was resolved from. in must be the
// Inputs given to Resolve (with Canonical = rec.Canonical) and launch the
// Effective that returned. wStamp and sStamp are the stores' stamps of W and S
// at that read: a nil input with a tombstone stamp is kept as such. The basis
// is verified by resolving it again: it must reproduce the launch's fingerprint
// and executable digest, or it is refused.
func NewServingBasis(in Inputs, launch Effective, rec *CanonRecorder, wStamp, sStamp Stamp) (*ServingBasis, error) {
	if rec == nil {
		return nil, fail(CodeSnapshotUnavailable, "The launch configuration could not be frozen.")
	}
	if (in.Workspace != nil) != wStamp.Present || (in.Session != nil) != sStamp.Present {
		return nil, fail(CodeSnapshotUnavailable, "The launch configuration could not be frozen.")
	}
	if in.Workspace != nil && in.Workspace.Revision != wStamp.Revision {
		return nil, fail(CodeSnapshotUnavailable, "The launch configuration could not be frozen.")
	}
	if in.Session != nil && in.Session.Revision != sStamp.Revision {
		return nil, fail(CodeSnapshotUnavailable, "The launch configuration could not be frozen.")
	}
	b := &ServingBasis{
		registry: Registry{Tools: slices.Clone(in.Registry.Tools)},
		vendor:   cloneVendor(in.Vendor),
		env: Environment{
			Servers: cloneServers(in.Environment.Servers),
			Skills:  slices.Clone(in.Environment.Skills),
		},
		wStamp:  wStamp,
		sStamp:  sStamp,
		canon:   rec.frozen(),
		launch:  launch.Clone(),
		execDig: launch.ExecutableDigest(),
	}
	if in.Workspace != nil {
		w := cloneProfile(*in.Workspace)
		b.workspace = &w
	}
	again, err := b.resolveWith(in.Session)
	if err != nil || again.Fingerprint != launch.Fingerprint || again.ExecutableDigest() != launch.ExecutableDigest() {
		return nil, fail(CodeSnapshotUnavailable, "The launch configuration could not be frozen.")
	}
	return b, nil
}

// Resolve is the effective configuration under the pinned V and W and the given
// S (nil: absent or revoked). It never reads anything but the frozen inputs. It
// refuses a result whose executables differ from the launch's.
func (b *ServingBasis) Resolve(session *Override) (Effective, error) {
	if b == nil {
		return Effective{}, fail(CodeSnapshotUnavailable, "This session has no pinned configuration.")
	}
	eff, err := b.resolveWith(session)
	if err != nil {
		return Effective{}, err
	}
	if eff.ExecutableDigest() != b.execDig {
		return Effective{}, fail(CodeSnapshotUnavailable, "A session override cannot change what a session can run.")
	}
	return eff, nil
}

func (b *ServingBasis) resolveWith(session *Override) (Effective, error) {
	in := Inputs{
		Registry:    Registry{Tools: slices.Clone(b.registry.Tools)},
		Vendor:      cloneVendor(b.vendor),
		Environment: Environment{Servers: cloneServers(b.env.Servers), Skills: slices.Clone(b.env.Skills)},
		Canonical:   b.replay,
	}
	if b.workspace != nil {
		w := cloneProfile(*b.workspace)
		in.Workspace = &w
	}
	if session != nil {
		s := *session
		s.Groups = slices.Clone(session.Groups)
		in.Session = &s
	}
	return Resolve(in)
}

// replay answers a path the way the launch did. A path the launch never asked
// about cannot be resolved honestly now and is refused rather than looked up.
func (b *ServingBasis) replay(path string) (string, error) {
	a, ok := b.canon[path]
	if !ok || a.err {
		return "", fail(CodeProfileInvalid, "A path cannot be resolved.")
	}
	return a.path, nil
}

// Identity is what an endpoint serving eff from durable S stamp s serves.
func (b *ServingBasis) Identity(s Stamp, eff Effective) ServingIdentity {
	return ServingIdentity{Session: s, Fingerprint: eff.Fingerprint}
}

// Launch is the effective configuration the launch itself was given.
func (b *ServingBasis) Launch() Effective { return b.launch.Clone() }

// WorkspaceStamp is the stamp of W as the launch read it.
func (b *ServingBasis) WorkspaceStamp() Stamp { return b.wStamp }

// LaunchSession is the stamp of S as the launch read it.
func (b *ServingBasis) LaunchSession() Stamp { return b.sStamp }

// VendorRevision is V's revision at launch.
func (b *ServingBasis) VendorRevision() int64 { return b.vendor.Revision }

// Registry is a copy of the tool registry the launch was resolved against.
func (b *ServingBasis) Registry() Registry { return Registry{Tools: slices.Clone(b.registry.Tools)} }

// ExecutableDigest is the launch's executable digest, the one live serving keeps.
func (b *ServingBasis) ExecutableDigest() string { return b.execDig }

func cloneProfile(p Profile) Profile {
	out := p
	out.ToolGroups = cloneMap(p.ToolGroups)
	if p.ResultBudget != nil {
		rb := &BudgetProfile{Tools: cloneMap(p.ResultBudget.Tools), Groups: cloneMap(p.ResultBudget.Groups)}
		for k, r := range rb.Tools {
			rb.Tools[k] = cloneRule(r)
		}
		for k, r := range rb.Groups {
			rb.Groups[k] = cloneRule(r)
		}
		out.ResultBudget = rb
	}
	out.DisabledSkills = slices.Clone(p.DisabledSkills)
	out.DisabledMCP = slices.Clone(p.DisabledMCP)
	out.Servers = cloneServers(p.Servers)
	out.SkillFolders = slices.Clone(p.SkillFolders)
	return out
}

func cloneRule(r BudgetRule) BudgetRule {
	if r.Bytes != nil {
		v := *r.Bytes
		r.Bytes = &v
	}
	return r
}
