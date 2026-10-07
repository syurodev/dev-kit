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
	if s.deps.Workspace == nil || s.deps.CodeGraphServices == nil {
		return crossservice.Report{}, apperrors.New("internal_error", "Workspaces are not available.", true)
	}
	if in.ServicePath != "" && !filepath.IsAbs(in.ServicePath) {
		return crossservice.Report{}, fmt.Errorf("codegraph: servicePath must be absolute (got %q)", in.ServicePath)
	}
	folders, subject, err := s.serviceScope(ctx, in.ServicePath)
	if err != nil {
		return crossservice.Report{}, err
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
				return crossservice.Report{}, err
			}
			snapshot = codegraph.ServiceSnapshot{Path: f.Path, State: codegraph.SnapshotNotIndexed}
		}
		services = append(services, crossservice.Service{Name: f.Name, Path: f.Path, Config: config, Snapshot: snapshot})
	}
	return crossservice.Run(services, crossservice.Query{Service: subject, Direction: in.Direction, EndpointID: in.EndpointID, Limit: in.Limit})
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
