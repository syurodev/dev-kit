package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"devkit/internal/apperrors"
	"devkit/internal/codegraph"
	"devkit/internal/crossservice"
	"devkit/internal/workspace"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type codeGraphFindServiceCallsInput struct {
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of the service to ask about; default is the service the session's working directory is in"`
	Direction   string `json:"direction,omitempty" jsonschema:"optional; outbound (what it calls), inbound (who calls it) or both (default)"`
	EndpointID  string `json:"endpointId,omitempty" jsonschema:"optional; an id from codegraph.listEndpoints: narrows inbound to the calls that land on that route"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional; edges listed per direction (default 100, at most 500)"`
}

func (s *Server) toolCodeGraphFindServiceCalls(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphFindServiceCallsInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("find_service_calls", map[string]any{"direction": in.Direction, "endpoint_id": in.EndpointID, "limit": in.Limit}), func(ctx context.Context) (any, error) {
		return s.findServiceCalls(ctx, in)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// findServiceCalls reads every service of the asker's workspace and joins them.
func (s *Server) findServiceCalls(ctx context.Context, in codeGraphFindServiceCallsInput) (crossservice.Report, error) {
	services, subject, err := s.workspaceSnapshots(ctx, in.ServicePath)
	if err != nil {
		return crossservice.Report{}, err
	}
	return crossservice.Run(services, crossservice.Query{Service: subject, Direction: in.Direction, EndpointID: in.EndpointID, Limit: in.Limit})
}

type codeGraphFindRedisKeyUsageInput struct {
	Key         string `json:"key,omitempty" jsonschema:"optional; a key pattern such as fsap:access:* or fsap:access:{org}, at least 3 fixed characters before any wildcard; empty lists the keys two or more services touch"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of any service of the workspace, to say which workspace; default is the one the session's working directory is in"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional; keys listed (default 50, at most 500)"`
}

func (s *Server) toolCodeGraphFindRedisKeyUsage(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphFindRedisKeyUsageInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("find_redis_key_usage", map[string]any{"key": in.Key, "limit": in.Limit}), func(ctx context.Context) (any, error) {
		services, _, err := s.workspaceSnapshots(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return crossservice.RunRedis(services, crossservice.RedisQuery{Key: in.Key, Limit: in.Limit})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type codeGraphFindKafkaTopicUsageInput struct {
	Topic       string `json:"topic,omitempty" jsonschema:"optional; a topic name or a pattern with * or {hole}, at least 3 fixed characters before any wildcard; empty lists the topics two or more services touch"`
	Gaps        bool   `json:"gaps,omitempty" jsonschema:"optional; list the topics with a gap instead: produced and consumed by nothing in the workspace, or consumed and produced by nothing"`
	ServicePath string `json:"servicePath,omitempty" jsonschema:"optional; absolute path of any service of the workspace, to say which workspace; default is the one the session's working directory is in"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional; topics listed (default 50, at most 500)"`
}

func (s *Server) toolCodeGraphFindKafkaTopicUsage(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphFindKafkaTopicUsageInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("find_kafka_topic_usage", map[string]any{"topic": in.Topic, "gaps": in.Gaps, "limit": in.Limit}), func(ctx context.Context) (any, error) {
		services, _, err := s.workspaceSnapshots(ctx, in.ServicePath)
		if err != nil {
			return nil, err
		}
		return crossservice.RunKafka(services, crossservice.KafkaQuery{Topic: in.Topic, Gaps: in.Gaps, Limit: in.Limit})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// workspaceSnapshots reads every service of the asker's workspace: its Spring
// config (which folds its calls' URLs) and its snapshot (what its index offers).
// subject is the folder of the service asked about.
func (s *Server) workspaceSnapshots(ctx context.Context, servicePath string) ([]crossservice.Service, string, error) {
	if s.deps.Workspace == nil || s.deps.CodeGraphServices == nil {
		return nil, "", apperrors.New("internal_error", "Workspaces are not available.", true)
	}
	if servicePath != "" && !filepath.IsAbs(servicePath) {
		return nil, "", fmt.Errorf("codegraph: servicePath must be absolute (got %q)", servicePath)
	}
	folders, subject, err := s.serviceScope(ctx, servicePath)
	if err != nil {
		return nil, "", err
	}
	services := make([]crossservice.Service, 0, len(folders))
	for _, f := range folders {
		config, err := workspace.ReadSpringConfig(f.Path)
		if err != nil {
			config = workspace.SpringConfig{Props: map[string]string{}} // a config that does not parse folds nothing
		}
		snapshot, err := s.deps.CodeGraphServices.Snapshot(ctx, f.Path)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, "", err
			}
			snapshot = codegraph.ServiceSnapshot{Path: f.Path, State: codegraph.SnapshotNotIndexed}
		}
		services = append(services, crossservice.Service{Name: f.Name, Path: f.Path, Config: config, Snapshot: snapshot})
	}
	return services, subject, nil
}

// serviceScope is the services of the workspace the asker is in, and which of
// them is being asked about: the named one, else the one the session works in.
// An agent session sees its own workspace; a terminal sees them all and picks
// the first that holds the service.
func (s *Server) serviceScope(ctx context.Context, servicePath string) ([]WorkspaceFolder, string, error) {
	listed, err := s.workspaceList(ctx)
	if err != nil {
		return nil, "", err
	}
	want := servicePath
	if want == "" {
		want = listed.Cwd
	}
	if want == "" {
		return nil, "", errors.New("codegraph: name the service with servicePath — this caller has no working directory to take it from")
	}
	for _, ws := range listed.Workspaces {
		for _, f := range ws.Folders {
			if workspace.Within(want, f.Path) {
				return ws.Folders, f.Path, nil
			}
		}
	}
	return nil, "", fmt.Errorf("codegraph: %s is not in a workspace this caller can see — list them with workspace.list", want)
}
