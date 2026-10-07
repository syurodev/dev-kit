package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

const (
	// claudeModelOption and claudeEffortOption are the ids claude-agent-acp
	// publishes in configOptions and takes back in session/set_config_option.
	claudeModelOption  = "model"
	claudeEffortOption = "effort"
)

// ConfigCatalog reads the model and effort choices before there is a
// conversation, the way Codex's does: a short-lived agent in a scratch folder
// that is asked for its settings and never sent a prompt, so nothing is
// inferred and nothing is billed.
//
// The agent only lists the effort levels of the model it is on, so each model
// is switched to in turn and the list read back from the answer. A model whose
// switch fails simply offers no effort choice.
func (a *ClaudeAdapter) ConfigCatalog(ctx context.Context, spec OpenSpec) ([]ConfigOption, error) {
	work, err := os.MkdirTemp("", "devkit-claude-catalog-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	spec.Cwd = work
	opened, err := a.open(ctx, spec, openPlan{})
	if err != nil {
		return nil, err
	}
	rt, ok := opened.(*claudeRuntime)
	if !ok {
		_ = opened.Close(ctx)
		return nil, fmt.Errorf("%w: unexpected runtime %T", ErrProcessFailed, opened)
	}
	defer rt.Close(ctx)

	callCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	return rt.configCatalog(callCtx)
}

func (r *claudeRuntime) configCatalog(ctx context.Context) ([]ConfigOption, error) {
	initial := parseConfigOptions(r.configOptions)
	model, effort := findConfigOption(initial, claudeModelOption), findConfigOption(initial, claudeEffortOption)
	if model == nil || len(model.Options) == 0 {
		return nil, fmt.Errorf("%w: Claude returned no models", ErrProcessFailed)
	}
	var current string
	_ = json.Unmarshal(model.CurrentValue, &current)
	model.DefaultValue = current

	for i := range model.Options {
		choice := &model.Options[i]
		levels := effort
		if choice.Value != current {
			if ctx.Err() != nil {
				break
			}
			switched, err := r.setConfigOption(ctx, claudeModelOption, choice.Value)
			if err != nil {
				continue
			}
			levels = findConfigOption(switched, claudeEffortOption)
		}
		if levels == nil {
			continue
		}
		var def string
		_ = json.Unmarshal(levels.CurrentValue, &def)
		choice.DefaultEffort = def
		for _, level := range levels.Options {
			// Named by value, like Codex's: the picker labels a level itself.
			choice.SupportedEfforts = append(choice.SupportedEfforts, ConfigSelectOption{Value: level.Value, Name: level.Value, Description: level.Description})
		}
	}
	// Only the model rides the catalog: the picker derives the effort control
	// from the chosen model's levels, and mode and the rest are not chosen
	// before a session exists.
	model.Category = "model"
	return []ConfigOption{*model}, nil
}

func findConfigOption(options []ConfigOption, id string) *ConfigOption {
	for i := range options {
		if options[i].ID == id {
			return &options[i]
		}
	}
	return nil
}
