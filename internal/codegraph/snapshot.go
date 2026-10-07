package codegraph

import (
	"context"
	"errors"
	"os"
	"strings"
)

// Snapshot states.
const (
	SnapshotIndexed    = "indexed"
	SnapshotIndexing   = "indexing"    // open, and its index is being built or refreshed
	SnapshotNotIndexed = "not-indexed" // no index of this folder on disk
)

// ServiceSnapshot is what one service offers the cross-service join: the HTTP
// routes it serves and the HTTP calls it makes. A snapshot is a read — taken
// from a resident service, or from the index on disk without opening a
// service at all — so the join never makes a service resident, which would
// start a watcher and spend one of the Registry's slots on a question.
type ServiceSnapshot struct {
	Path  string `json:"path"`
	State string `json:"state"`
	// IndexedAt is when the index was written, Unix milliseconds; 0 when unknown.
	// A service that is not resident has no watcher, so its snapshot can be as
	// old as this says without anything else saying so.
	IndexedAt int64 `json:"indexedAt,omitempty"`
	// ContextPath is server.servlet.context-path when the config sets it to a
	// literal: it prefixes every route the service serves.
	ContextPath string `json:"contextPath,omitempty"`
	// Endpoints are the HTTP entry points.
	Endpoints []SnapshotEndpoint `json:"endpoints"`
	Outbound  []SnapshotCall     `json:"outbound"`
	// Redis are the Redis keys it reads and writes.
	Redis []SnapshotRedis `json:"redis"`
	// Kafka are the topics it produces to and consumes from.
	Kafka []SnapshotKafka `json:"kafka"`
}

// SnapshotKafka is a Kafka access and the method that makes it.
type SnapshotKafka struct {
	KafkaAccess
	Label string `json:"label"`
}

// SnapshotRedis is a Redis access and the method that makes it.
type SnapshotRedis struct {
	RedisAccess
	Label string `json:"label"`
}

// SnapshotEndpoint is an HTTP entry point and the method behind it.
type SnapshotEndpoint struct {
	EntryPoint
	Label string `json:"label"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

// SnapshotCall is an outbound call and the method that makes it.
type SnapshotCall struct {
	OutboundCall
	Label string `json:"label"`
}

// Snapshot reads this service's routes and outbound calls.
func (s *Service) Snapshot(ctx context.Context, path string) (ServiceSnapshot, error) {
	store, release, err := s.activeStore()
	if errors.Is(err, ErrIndexNotReady) {
		return ServiceSnapshot{Path: path, State: SnapshotIndexing, Endpoints: []SnapshotEndpoint{}, Outbound: []SnapshotCall{}, Redis: []SnapshotRedis{}, Kafka: []SnapshotKafka{}}, nil
	}
	if err != nil {
		return ServiceSnapshot{}, err
	}
	defer release()
	return snapshotOf(ctx, store, path)
}

// ReadStoredSnapshot reads a service's snapshot from the index on disk, without
// opening a Service. A folder with no index of its own — or whose file holds
// another project's — is reported as not indexed rather than as an error.
func ReadStoredSnapshot(ctx context.Context, configDir, projectPath string) (ServiceSnapshot, error) {
	empty := ServiceSnapshot{Path: projectPath, State: SnapshotNotIndexed, Endpoints: []SnapshotEndpoint{}, Outbound: []SnapshotCall{}, Redis: []SnapshotRedis{}, Kafka: []SnapshotKafka{}}
	if configDir == "" {
		return empty, nil
	}
	dbPath := ProjectDBPath(configDir, projectPath)
	if _, err := os.Stat(dbPath); err != nil {
		return empty, nil
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return empty, nil
	}
	defer store.Close()
	if !storedIndexMatches(ctx, store, projectPath) {
		return empty, nil
	}
	return snapshotOf(ctx, store, projectPath)
}

// Snapshot is the snapshot of servicePath: a resident (or the focused) service
// answers for itself, anything else is read from disk.
func (r *Registry) Snapshot(ctx context.Context, servicePath string) (ServiceSnapshot, error) {
	if svc, ok := r.Service(servicePath); ok && svc != nil {
		return svc.Snapshot(ctx, servicePath)
	}
	configDir := ""
	if r.focus != nil {
		configDir = r.focus.Host().ConfigDir()
	}
	return ReadStoredSnapshot(ctx, configDir, servicePath)
}

func snapshotOf(ctx context.Context, store Store, path string) (ServiceSnapshot, error) {
	snap := ServiceSnapshot{Path: path, State: SnapshotIndexed, Endpoints: []SnapshotEndpoint{}, Outbound: []SnapshotCall{}, Redis: []SnapshotRedis{}, Kafka: []SnapshotKafka{}}
	snap.IndexedAt, _ = store.IndexedAt(ctx)

	entries, err := store.ListEntryPoints(ctx)
	if err != nil {
		return ServiceSnapshot{}, err
	}
	entries = completeEntryPoints(entries)
	sortEntryPoints(entries)
	calls, err := store.ListOutboundCalls(ctx)
	if err != nil {
		return ServiceSnapshot{}, err
	}

	redis, err := store.ListRedisAccesses(ctx)
	if err != nil {
		return ServiceSnapshot{}, err
	}

	kafka, err := store.ListKafkaAccesses(ctx)
	if err != nil {
		return ServiceSnapshot{}, err
	}

	var ids []string
	for _, a := range kafka {
		ids = append(ids, a.From)
	}
	for _, a := range redis {
		ids = append(ids, a.From)
	}
	for _, e := range entries {
		if e.Kind == EntryKindHTTP {
			ids = append(ids, e.EntryNode)
		}
	}
	for _, c := range calls {
		ids = append(ids, c.From)
	}
	nodes, err := store.GetNodes(ctx, ids)
	if err != nil {
		return ServiceSnapshot{}, err
	}
	for _, e := range entries {
		if e.Kind != EntryKindHTTP {
			continue
		}
		n := nodes[e.EntryNode]
		snap.Endpoints = append(snap.Endpoints, SnapshotEndpoint{EntryPoint: e, Label: n.Label, File: n.FilePath, Line: n.Line})
	}
	for _, c := range calls {
		snap.Outbound = append(snap.Outbound, SnapshotCall{OutboundCall: c, Label: nodes[c.From].Label})
	}

	for _, a := range kafka {
		snap.Kafka = append(snap.Kafka, SnapshotKafka{KafkaAccess: a, Label: nodes[a.From].Label})
	}
	for _, a := range redis {
		snap.Redis = append(snap.Redis, SnapshotRedis{RedisAccess: a, Label: nodes[a.From].Label})
	}

	// A literal context path prefixes every route; one that is a placeholder is unknown.
	keys, err := store.FindConfigKeys(ctx, CanonicalConfigKey("server.servlet.context-path"), false)
	if err == nil {
		for _, k := range keys {
			if k.Profile == "" && !k.Redacted && k.Value != "" && !strings.Contains(k.Value, "$") && strings.HasPrefix(k.Value, "/") {
				snap.ContextPath = strings.TrimRight(k.Value, "/")
				break
			}
		}
	}
	return snap, nil
}
