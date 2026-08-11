package mcp

import (
	"context"

	"devkit/internal/interaction"
)

// EnvConfirmer reports whether an env tag requires MCP confirmation.
type EnvConfirmer interface {
	RequiresConfirmation(name string) bool
}

// ProfileEnvReader resolves metadata.env for a connection profile id.
type ProfileEnvReader interface {
	EnvForProfile(id string) (string, error)
}

// ActiveTarget is the sticky MCP active target for SSH, database, or Kafka.
type ActiveTarget interface {
	ActiveID() (id string, ok bool)
	// EnsureActive may block on interaction.Hub.SelectProfile then set active.
	EnsureActive(ctx context.Context) (id string, err error)
}

// effectiveHub treats a nil Hub as headless (selection/confirmation required).
func effectiveHub(hub *interaction.Hub) *interaction.Hub {
	if hub == nil {
		return &interaction.Hub{Headless: true}
	}
	return hub
}

// GateActiveMCP ensures an active target exists and env confirmation policy is satisfied.
// When RequiresConfirmation(env) is true, Confirm runs every call (allow_always ignored).
// When false, Confirm may short-circuit via allow_always after the user opts in once.
// A nil hub behaves as headless.
func GateActiveMCP(
	ctx context.Context,
	hub *interaction.Hub,
	env EnvConfirmer,
	profiles ProfileEnvReader,
	active ActiveTarget,
	tool, summary string,
	kind interaction.Kind,
) error {
	id, err := active.EnsureActive(ctx)
	if err != nil {
		return err
	}
	envName, err := profiles.EnvForProfile(id)
	if err != nil {
		return err
	}
	blocks := env.RequiresConfirmation(envName)
	return effectiveHub(hub).Confirm(ctx, interaction.ConfirmRequest{
		Env:       envName,
		Tool:      tool,
		Summary:   summary,
		Kind:      kind,
		EnvBlocks: blocks,
	})
}
