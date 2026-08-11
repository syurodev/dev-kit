package mcp

import (
	"context"

	"devkit/internal/interaction"
)

// databaseTarget implements ActiveTarget and ProfileEnvReader for database MCP tools.
type databaseTarget struct {
	api DatabaseAPI
	hub *interaction.Hub
}

func (t *databaseTarget) ActiveID() (string, bool) {
	if t == nil || t.api == nil {
		return "", false
	}
	id := t.api.AgentActiveProfileID()
	return id, id != ""
}

func (t *databaseTarget) EnsureActive(ctx context.Context) (string, error) {
	if id, ok := t.ActiveID(); ok {
		return id, nil
	}
	id, err := effectiveHub(t.hub).SelectProfile(ctx, interaction.SelectRequest{Kind: interaction.KindDatabase})
	if err != nil {
		return "", err
	}
	if err := t.api.SetAgentActiveProfile(id); err != nil {
		return "", err
	}
	return id, nil
}

func (t *databaseTarget) EnvForProfile(id string) (string, error) {
	profiles, err := t.api.ListProfiles()
	if err != nil {
		return "", err
	}
	for _, p := range profiles {
		if p.ID == id {
			return p.Env, nil
		}
	}
	return "", errUnknownProfile
}
