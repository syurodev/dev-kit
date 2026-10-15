package bridge

import (
	"context"
	"log"

	"devkit/internal/codegraph"
)

// CodeGraphGetSteps returns the steps of the given methods of a flow — the
// Outline mode's rows and the Graph mode's labels — scored with the person's
// call-visibility overrides. Read-only, like CodeGraphGetNeighbors, and asked
// of the same service.
func (s *AppService) CodeGraphGetSteps(nodeIDs []string, servicePath string) (codegraph.StepsResult, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"service_path": servicePath,
		"action":       "get_steps",
		"node_count":   len(nodeIDs),
	}, func(ctx context.Context) (any, error) {
		// Overrides that cannot be read are not worth losing the outline
		// over: the defaults are a complete answer, and the next read may work.
		overrides := map[string]string{}
		if s.container.Preferences != nil {
			got, err := s.container.Preferences.CallVisibility()
			if err != nil {
				log.Printf("codegraph: steps: call-visibility overrides unreadable, using defaults: %v", err)
			} else {
				overrides = got
			}
		}
		svc, err := s.codeGraphService(ctx, servicePath)
		if err != nil {
			return nil, err
		}
		return svc.GetSteps(ctx, nodeIDs, overrides)
	})
	if err != nil {
		return codegraph.StepsResult{}, err
	}
	return result.(codegraph.StepsResult), nil
}

// PreferencesGetCallVisibility returns the "always show / hide" choices.
func (s *AppService) PreferencesGetCallVisibility() (map[string]string, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "codegraph_call_visibility", map[string]any{
		"action": "get_call_visibility",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.CallVisibility()
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]string), nil
}

// PreferencesSetCallVisibility sets one callee key to "show" or "hide", or
// clears it with "". UI only: no MCP tool changes what a person sees.
func (s *AppService) PreferencesSetCallVisibility(calleeKey, mode string) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "codegraph_call_visibility", map[string]any{
		"action": "set_call_visibility",
		"mode":   mode,
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return nil, s.container.Preferences.SetCallVisibility(calleeKey, mode)
	})
	return err
}
