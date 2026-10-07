package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"devkit/internal/codexapp"
)

func codexConfigOptions(models []codexapp.Model, model, effort string) []ConfigOption {
	choices := make([]ConfigSelectOption, 0, len(models))
	defaultModel := ""
	var selected codexapp.Model
	for _, m := range models {
		name := m.DisplayName
		if name == "" {
			name = codexModelSlug(m)
		}
		choice := ConfigSelectOption{Value: codexModelSlug(m), Name: name, Description: m.Description, DefaultEffort: m.DefaultReasoningEffort}
		for _, e := range m.SupportedReasoningEfforts {
			choice.SupportedEfforts = append(choice.SupportedEfforts, ConfigSelectOption{Value: e.ReasoningEffort, Name: e.ReasoningEffort, Description: e.Description})
		}
		choices = append(choices, choice)
		if m.IsDefault {
			defaultModel = choice.Value
		}
		if choice.Value == model {
			selected = m
		}
	}
	if defaultModel == "" && len(choices) > 0 {
		defaultModel = choices[0].Value
	}
	if model == "" {
		model = defaultModel
		for _, m := range models {
			if codexModelSlug(m) == model {
				selected = m
			}
		}
	}
	current, _ := json.Marshal(model)
	out := []ConfigOption{{ID: codexModelOption, Name: "Model", Category: "model", Type: "select", CurrentValue: current, DefaultValue: defaultModel, Options: choices}}
	if len(selected.SupportedReasoningEfforts) > 0 {
		if effort == "" {
			effort = selected.DefaultReasoningEffort
		}
		current, _ := json.Marshal(effort)
		options := make([]ConfigSelectOption, 0, len(selected.SupportedReasoningEfforts))
		for _, e := range selected.SupportedReasoningEfforts {
			options = append(options, ConfigSelectOption{Value: e.ReasoningEffort, Name: e.ReasoningEffort, Description: e.Description})
		}
		out = append(out, ConfigOption{ID: codexEffortOption, Name: "Reasoning effort", Category: "thought_level", Type: "select", CurrentValue: current, DefaultValue: selected.DefaultReasoningEffort, Options: options})
	}
	return out
}

func (r *codexRuntime) normalizeEffortLocked() {
	for _, m := range r.models {
		if codexModelSlug(m) != r.model {
			continue
		}
		for _, e := range m.SupportedReasoningEfforts {
			if e.ReasoningEffort == r.effort {
				return
			}
		}
		r.effort = m.DefaultReasoningEffort
		if len(m.SupportedReasoningEfforts) > 0 {
			for _, e := range m.SupportedReasoningEfforts {
				if e.ReasoningEffort == r.effort {
					return
				}
			}
			r.effort = m.SupportedReasoningEfforts[0].ReasoningEffort
		}
		return
	}
	r.effort = ""
}

func (r *codexRuntime) RefreshConfigOptions(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	models := codexListModels(ctx, r.conn)
	if len(models) == 0 {
		return fmt.Errorf("%w: Codex returned no models", ErrProcessFailed)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	r.models = models
	known := false
	for _, m := range models {
		known = known || codexModelSlug(m) == r.model
	}
	if !known {
		defaults := codexConfigOptions(models, "", "")
		_ = json.Unmarshal(defaults[0].CurrentValue, &r.model)
	}
	r.normalizeEffortLocked()
	update := r.configOptionsLocked()
	r.mu.Unlock()
	r.emit([]codexUpdate{{Update: update}})
	return nil
}

// ConfigCatalog uses a short-lived app-server, without thread/start or inference.
// It uses the same account and jail as sessions and closes its process/lease.
func (a *CodexAdapter) ConfigCatalog(ctx context.Context, spec OpenSpec) ([]ConfigOption, error) {
	work, err := os.MkdirTemp("", "devkit-codex-catalog-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	l, err := a.resolveCommand()
	if err != nil {
		return nil, err
	}
	key, err := a.apiKey(ctx)
	if err != nil {
		l.releaseLease()
		return nil, err
	}
	store := codexStoreFile
	if key != "" {
		store = codexStoreEphemeral
	}
	p, err := a.start(work, l, spec, store, codexNoHandler{})
	if err != nil {
		return nil, err
	}
	defer p.stop()
	callCtx, cancel := p.bounded(ctx, handshakeTimeout)
	defer cancel()
	if err := a.initialize(callCtx, p); err != nil {
		return nil, err
	}
	if key != "" {
		var login codexapp.LoginStartResponse
		if err := p.conn.Call(callCtx, codexapp.MethodAccountLoginStart, codexapp.LoginStartParams{Type: codexapp.LoginAPIKey, APIKey: key}, &login); err != nil {
			return nil, vendorError(AgentTypeCodex, fmt.Errorf("%w: %w: Codex refused the API key", ErrAgentAuthRequired, ErrAgentAPIKeyUnusable))
		}
	}
	models := codexListModels(callCtx, p.conn)
	if len(models) == 0 {
		return nil, fmt.Errorf("%w: Codex returned no models", ErrProcessFailed)
	}
	return codexConfigOptions(models, "", ""), nil
}
