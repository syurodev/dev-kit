// internal/bridge/codegraph.go
package bridge

import (
	"context"

	"devkit/internal/codegraph"
)

// CodeGraphOpenProject indexes a folder and reports what it turned out to hold.
//
// Returning the detection rather than just an error is what lets the UI say
// how many modules were found. A repository of microservices is the case that
// needs it: it indexes as one graph, and without a count it is indistinguishable
// on screen from a single project that happens to have a lot of endpoints.
//
// ctx is the call's own context: cancelling the frontend's promise cancels it
// (the same mechanism DBQuery uses), and with it the analysis — an open of a
// large project is minutes, and "never mind" has to stop the JVM, not just the
// spinner. A newer open or the app quitting cancels it too, reported as
// codegraph.ErrProjectChanged.
func (s *AppService) CodeGraphOpenProject(ctx context.Context, path string) (codegraph.DetectedProject, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:index", "project:new", map[string]any{
		"action": "open_project",
	}, func(ctx context.Context) (any, error) {
		dbPath := codegraph.ProjectDBPath(s.container.ConfigDir, path)
		if err := s.container.CodeGraph.OpenProjectAt(ctx, dbPath, path); err != nil {
			return nil, err
		}
		return s.container.CodeGraph.ActiveDetection(), nil
	})
	if err != nil {
		return codegraph.DetectedProject{}, err
	}
	return result.(codegraph.DetectedProject), nil
}

func (s *AppService) CodeGraphListEndpoints() ([]codegraph.Endpoint, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "list_endpoints",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListEndpoints(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.Endpoint), nil
}

func (s *AppService) CodeGraphGetApiFlow(endpointID string) (codegraph.ApiFlow, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":      "get_api_flow",
		"endpoint_id": endpointID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetApiFlow(ctx, endpointID)
	})
	if err != nil {
		return codegraph.ApiFlow{}, err
	}
	return result.(codegraph.ApiFlow), nil
}

func (s *AppService) CodeGraphSearchSymbol(query string) ([]codegraph.SymbolMatch, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "search_symbol",
		"query":  query,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.SearchSymbol(ctx, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.SymbolMatch), nil
}

// CodeGraphFindDefinitions answers "go to definition" for the identifier
// under the cursor at filePath:line. Not found is a normal, non-error result
// (an empty slice) — the editor tries other things (e.g. leaving the cursor
// alone) rather than surfacing a failure to the user for a symbol the index
// simply has no location for.
func (s *AppService) CodeGraphFindDefinitions(filePath string, line int, identifier string) ([]codegraph.Definition, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":     "find_definitions",
		"file_path":  filePath,
		"line":       line,
		"identifier": identifier,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.FindDefinitions(ctx, filePath, line, identifier)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.Definition), nil
}

func (s *AppService) CodeGraphGetBlastRadius(filePath string) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":    "get_blast_radius",
		"file_path": filePath,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetBlastRadius(ctx, filePath)
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}

func (s *AppService) CodeGraphWaitForIndexReady() (int, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "wait_for_index_ready",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.WaitForIndexReady(ctx)
	})
	if err != nil {
		return 0, err
	}
	return result.(int), nil
}

func (s *AppService) CodeGraphGetNeighbors(rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":  "get_neighbors",
		"root_id": rootID,
		"depth":   depth,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetNeighbors(ctx, rootID, depth, filter)
	})
	if err != nil {
		return codegraph.Subgraph{}, err
	}
	return result.(codegraph.Subgraph), nil
}

func (s *AppService) CodeGraphListFiles() ([]codegraph.FileEntry, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"action": "list_files",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListFiles(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.FileEntry), nil
}

func (s *AppService) CodeGraphReadFile(relativePath string) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"action":        "read_file",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ReadFile(ctx, relativePath)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// CodeGraphWriteFile saves an editor buffer. expectedRoot is the project root
// the buffer was opened under: if the active project has changed since — an
// agent can switch it over MCP while the buffer is open — the save is refused
// with project_changed instead of landing, as a new or overwritten file, in a
// project the user was not editing. Empty skips the check.
func (s *AppService) CodeGraphWriteFile(relativePath, content, expectedRoot string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+relativePath, map[string]any{
		"action":        "write_file",
		"relative_path": relativePath,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.WriteFile(ctx, relativePath, content, expectedRoot)
	})
	return err
}

func (s *AppService) CodeGraphGetCodeSnippet(filePath string, line, endLine int) (codegraph.CodeSnippet, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:fs", "file:"+filePath, map[string]any{
		"action":    "get_code_snippet",
		"file_path": filePath,
		"line":      line,
		"end_line":  endLine,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetCodeSnippet(ctx, filePath, line, endLine)
	})
	if err != nil {
		return codegraph.CodeSnippet{}, err
	}
	return result.(codegraph.CodeSnippet), nil
}

func (s *AppService) CodeGraphTraceDataFlow(symbolID, fieldName string) (codegraph.DataFlowTrace, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":     "trace_data_flow",
		"symbol_id":  symbolID,
		"field_name": fieldName,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.TraceDataFlow(ctx, symbolID, fieldName)
	})
	if err != nil {
		return codegraph.DataFlowTrace{}, err
	}
	return result.(codegraph.DataFlowTrace), nil
}

func (s *AppService) CodeGraphListFields(nodeID string) ([]codegraph.FieldRef, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":  "list_fields",
		"node_id": nodeID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListFields(ctx, nodeID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.FieldRef), nil
}

func (s *AppService) CodeGraphStartTerminal(rows, cols int) (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:terminal", "project:active", map[string]any{
		"action": "start_terminal",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.StartTerminal(rows, cols)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// CodeGraphProjectRoot returns the directory the active project was opened
// from, or "" when none is open. It exists so other modules can offer that
// directory as a default — the Agent Hub's new-session dialog uses it, on the
// grounds that the folder you are reading is usually the folder you want an
// agent to work in.
func (s *AppService) CodeGraphProjectRoot() (string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "project_root",
	}, func(context.Context) (any, error) {
		if s.container.CodeGraph == nil {
			return "", nil
		}
		return s.container.CodeGraph.ProjectRoot(), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// CodeGraphAnalyzerStatus reports, per language, whether that analyzer is
// built and — when it is not — exactly which programs are missing and what to
// install. The UI renders this instead of the bare "analyzer not configured"
// error a failed index used to produce, which told the user nothing they could
// act on.
//
// Read-only and side-effect free apart from running each program's version
// flag, so it takes codegraph:read rather than a capability of its own.
func (s *AppService) CodeGraphAnalyzerStatus() ([]codegraph.ToolchainStatus, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "analyzer_status",
	}, func(ctx context.Context) (any, error) {
		if s.container.CodeGraph == nil {
			return []codegraph.ToolchainStatus{}, nil
		}
		return s.container.CodeGraph.AnalyzerStatus(ctx), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.ToolchainStatus), nil
}

// CodeGraphBuildAnalyzer builds one language's analyzer and installs it, with
// no restart: on success the next CodeGraphOpenProject indexes for real.
//
// The build's output reaches the frontend as codegraph:analyzerBuildOutput
// events (wired in main.go, same shape as codegraph:indexUpdated) rather than
// as a returned transcript, because a cold Maven cache takes minutes and a call
// that returns only at the end is indistinguishable from a hang. The return
// value is the outcome, not the log.
//
// Its own capability: this is the one CodeGraph call that runs a build tool
// against the user's machine, and a grant that covers reading an index should
// not silently also cover that.
//
// ctx cancels the build (promise cancel), as does CodeGraphCancelAnalyzerBuild
// for a caller that no longer holds the promise.
func (s *AppService) CodeGraphBuildAnalyzer(ctx context.Context, language string) error {
	_, err := s.call(ctx, "builtin.codegraph", "codegraph:toolchain", "analyzer:build", map[string]any{
		"action":   "build_analyzer",
		"language": language,
	}, func(ctx context.Context) (any, error) {
		if s.container.CodeGraph == nil {
			return nil, codegraph.ErrUnknownToolchain
		}
		return nil, s.container.CodeGraph.BuildAnalyzer(ctx, language)
	})
	return err
}

// CodeGraphCancelAnalyzerBuild stops the analyzer build in flight — Maven and
// every process it started — so the setup panel's Cancel works even after the
// view that started the build has gone. The build's own call then fails with
// operation_cancelled. A no-op when nothing is building.
//
// Same capability as the build: stopping one is part of running one.
func (s *AppService) CodeGraphCancelAnalyzerBuild() error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:toolchain", "analyzer:build", map[string]any{
		"action": "cancel_analyzer_build",
	}, func(context.Context) (any, error) {
		if s.container.CodeGraph != nil {
			s.container.CodeGraph.CancelAnalyzerBuild()
		}
		return nil, nil
	})
	return err
}

// CodeGraphIndexInfo is what the sidebar says under the project name: when the
// index was built, whether a run is in flight, why the last one failed, and the
// modules the project was detected with (a restored session knows them too).
func (s *AppService) CodeGraphIndexInfo(ctx context.Context) (codegraph.IndexInfo, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "index_info",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.IndexInfo(ctx)
	})
	if err != nil {
		return codegraph.IndexInfo{}, err
	}
	return result.(codegraph.IndexInfo), nil
}

// CodeGraphGitStatus maps changed files in the active project to M/A/D/R, for
// the Files tab. Empty outside a git work tree.
func (s *AppService) CodeGraphGitStatus(ctx context.Context) (map[string]string, error) {
	result, err := s.call(ctx, "builtin.codegraph", "codegraph:fs", "project:active", map[string]any{
		"action": "git_status",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GitStatus(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]string), nil
}
