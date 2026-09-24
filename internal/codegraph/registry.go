package codegraph

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
)

// ErrNoResidentSlots is Open for a folder other than the focused one on a
// Registry built with no room for resident services.
var ErrNoResidentSlots = errors.New("codegraph: no room for another open project")

// DefaultResidentLimit is how many services the Registry keeps open besides
// the focused one (workspace.mdx § Registry, "Hai trần"). What an open
// service costs at rest is a watcher, a SQLite handle and — for TypeScript —
// a `tsgo` process; analyses themselves are bounded by the Host's job limit,
// not by this.
const DefaultResidentLimit = 8

// ProjectKey is how the Registry names a service: the same normalisation
// ProjectDBPath hashes, so one folder is one slot and one index file. It is
// deliberately no stronger than that — resolving symlinks or folding case
// here would name folders ProjectDBPath names differently, and every index
// already on disk would be orphaned.
func ProjectKey(path string) string {
	return filepath.Clean(path)
}

// Registry keeps several projects' indexes open at once, for a workspace
// whose services call each other (workspace.mdx § Registry).
//
// Two kinds of Service live here. The focused one is the app's Service as it
// has always been — the one the bridge, MCP and the UI talk to when they do
// not name a service; it switches folders in place, and the Registry never
// evicts or shuts it down (its owner, the container, closes it). The others
// are resident: one Service per folder, opened on request, kept in LRU order
// and shut down — store included (Service.Shutdown) — past the resident
// limit. All of them share the focused Service's Host, so there is one
// toolchain registry, one analyzer build and one analyze-job semaphore.
type Registry struct {
	focus *Service

	mu       sync.Mutex
	limit    int
	services map[string]*Service
	lru      []string // least recently used first

	// OnIndexUpdated fires after a resident service's re-index changed its
	// index, naming the service. The focused Service keeps its own
	// OnIndexUpdated, as today. Same contract: non-blocking only.
	OnIndexUpdated func(servicePath string, version int)
	// OnEvicted fires when a resident service is shut down to make room, so
	// the UI can say which one closed rather than have it vanish.
	OnEvicted func(servicePath string)

	// OnIndexStatus fires on every index-queue transition (Enqueue): queued,
	// running, and how it ended. The workspace registry persists the failed
	// and unsupported outcomes; codegraph does not import it. Non-blocking
	// only — it runs on the queue's worker.
	OnIndexStatus func(ServiceIndexStatus)

	indexQ *indexQueue

	// analyzer, when set, is installed on every resident Service instead of
	// the Host's per-language registry — the test seam SetAnalyzer already is
	// for a single Service.
	analyzer Analyzer
}

// NewRegistry builds a Registry around the app's Service, which becomes the
// focused one, with room for limit resident services besides it (limit < 0
// means DefaultResidentLimit; 0 means none — the app as it was).
func NewRegistry(focus *Service, limit int) *Registry {
	if limit < 0 {
		limit = DefaultResidentLimit
	}
	return &Registry{focus: focus, limit: limit, services: map[string]*Service{}}
}

// SetAnalyzer installs a on every resident Service opened from now on,
// instead of the Host's per-language registry — Service.SetAnalyzer's seam,
// for the Registry. Tests only; production resolves per folder.
func (r *Registry) SetAnalyzer(a Analyzer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.analyzer = a
}

// Focused is the Service callers get when they do not name one.
func (r *Registry) Focused() *Service { return r.focus }

// Service returns the Service for servicePath without opening anything: the
// focused one for "" or for the folder it has open, else a resident one.
func (r *Registry) Service(servicePath string) (*Service, bool) {
	if servicePath == "" {
		return r.focus, r.focus != nil
	}
	key := ProjectKey(servicePath)
	if r.focus != nil && r.focusRoot() == key {
		return r.focus, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	svc, ok := r.services[key]
	if ok {
		r.touchLocked(key)
	}
	return svc, ok
}

// Open returns the Service for servicePath, opening its index if it is not
// already open. The focused Service answers for its own folder; any other
// folder gets a resident Service, which may shut down the least recently
// used one to stay within the limit.
//
// A second Open for a folder whose open is still running returns the same
// Service at once; its queries report the index as not ready until the
// first open finishes, as a single Service's always have. An open that fails
// leaves nothing behind: the slot is freed and the Service shut down.
func (r *Registry) Open(ctx context.Context, servicePath string) (*Service, error) {
	if servicePath == "" {
		return r.focus, nil
	}
	key := ProjectKey(servicePath)
	if r.focus != nil && r.focusRoot() == key {
		return r.focus, nil
	}

	r.mu.Lock()
	if svc, ok := r.services[key]; ok {
		r.touchLocked(key)
		r.mu.Unlock()
		return svc, nil
	}
	if r.limit == 0 {
		r.mu.Unlock()
		return nil, ErrNoResidentSlots
	}
	svc := r.newResident(key)
	r.services[key] = svc
	r.lru = append(r.lru, key)
	evicted := r.evictLocked()
	r.mu.Unlock()
	r.shutdown(evicted)

	if err := svc.OpenProjectDir(ctx, key); err != nil {
		r.mu.Lock()
		if r.services[key] == svc {
			delete(r.services, key)
			r.removeLocked(key)
		}
		r.mu.Unlock()
		_ = svc.Shutdown()
		return nil, err
	}
	return svc, nil
}

// Focus makes servicePath the focused service's folder — what the UI shows
// and what callers that name no service get — and returns the focused
// Service once its index is ready.
//
// The focused Service switches folders in place, as it always has; it is not
// swapped for the resident one. What Focus adds is handing the folder over:
// a resident service for it is shut down first, so the folder is never open
// twice (two watchers re-indexing one tree, two handles on one index file).
// Nothing is lost by that: the index is on disk, and a reopen with the same
// analyzer serves it at once while it refreshes (Service's stale index). The
// folder focus leaves is not made resident — it opens again lazily, like any
// other, the next time something asks for it.
func (r *Registry) Focus(ctx context.Context, servicePath string) (*Service, error) {
	if servicePath == "" {
		return nil, errors.New("codegraph: focus needs a folder")
	}
	key := ProjectKey(servicePath)
	if err := r.Close(key); err != nil {
		return nil, err
	}
	if err := r.focus.OpenProjectDir(ctx, key); err != nil {
		return nil, err
	}
	return r.focus, nil
}

// Close shuts down one resident service. The focused Service is not the
// Registry's to close; asking to close it is a no-op.
func (r *Registry) Close(servicePath string) error {
	key := ProjectKey(servicePath)
	r.mu.Lock()
	svc, ok := r.services[key]
	if ok {
		delete(r.services, key)
		r.removeLocked(key)
	}
	r.mu.Unlock()
	if !ok {
		return nil
	}
	return svc.Shutdown()
}

// Resident lists the resident services' folders, least recently used first.
// The focused Service is not among them.
func (r *Registry) Resident() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lru...)
}

// Shutdown shuts down every resident service and stops the index queue (its
// run in flight is cancelled; nothing queued is persisted — queued and
// running are states of this process, and the next start queues again what
// has no index). The focused one is left to its owner, which closes it on
// its own schedule (main.go's deferred Close).
func (r *Registry) Shutdown() error {
	r.mu.Lock()
	if r.indexQ != nil {
		r.indexQ.cancel()
	}
	services := make([]*Service, 0, len(r.services))
	for _, svc := range r.services {
		services = append(services, svc)
	}
	r.services = map[string]*Service{}
	r.lru = nil
	r.mu.Unlock()
	for _, svc := range services {
		_ = svc.Shutdown()
	}
	return nil
}

func (r *Registry) focusRoot() string {
	root := r.focus.ProjectRoot()
	if root == "" {
		return ""
	}
	return ProjectKey(root)
}

// newService builds a Service on the focused one's Host — a resident, or the
// index queue's throwaway. Caller holds mu.
func (r *Registry) newService() *Service {
	var host *Host
	if r.focus != nil {
		host = r.focus.Host()
	}
	analyzer := r.analyzer
	if analyzer == nil {
		analyzer = NewSubprocessAnalyzer()
	}
	return NewServiceWithHost(host, NewNilStore(), analyzer)
}

// newResident builds a resident Service, reporting its re-indexes under its
// own folder. Caller holds mu.
func (r *Registry) newResident(key string) *Service {
	svc := r.newService()
	svc.OnIndexUpdated = func(version int) {
		if notify := r.OnIndexUpdated; notify != nil {
			notify(key, version)
		}
	}
	return svc
}

// evictLocked takes resident services out past the limit, oldest first, and
// returns them for the caller to shut down once mu is released — Shutdown
// takes the Service's own locks and may wait on a reader.
func (r *Registry) evictLocked() []evictedService {
	var out []evictedService
	for len(r.lru) > r.limit {
		key := r.lru[0]
		r.lru = r.lru[1:]
		if svc, ok := r.services[key]; ok {
			delete(r.services, key)
			out = append(out, evictedService{key: key, svc: svc})
		}
	}
	return out
}

type evictedService struct {
	key string
	svc *Service
}

func (r *Registry) shutdown(evicted []evictedService) {
	for _, e := range evicted {
		_ = e.svc.Shutdown()
		if notify := r.OnEvicted; notify != nil {
			notify(e.key)
		}
	}
}

func (r *Registry) touchLocked(key string) {
	r.removeLocked(key)
	r.lru = append(r.lru, key)
}

func (r *Registry) removeLocked(key string) {
	for i, k := range r.lru {
		if k == key {
			r.lru = append(r.lru[:i], r.lru[i+1:]...)
			return
		}
	}
}
