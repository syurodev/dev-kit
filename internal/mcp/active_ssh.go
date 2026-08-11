package mcp

import (
	"context"
	"errors"

	"devkit/internal/interaction"
)

var errUnknownProfile = errors.New("profile not found")

// sshTarget implements ActiveTarget and ProfileEnvReader for SSH MCP tools.
type sshTarget struct {
	api SSHAPI
	hub *interaction.Hub
}

func (t *sshTarget) ActiveID() (string, bool) {
	if t == nil || t.api == nil {
		return "", false
	}
	id := t.api.AgentActiveProfileID()
	return id, id != ""
}

func (t *sshTarget) EnsureActive(ctx context.Context) (string, error) {
	if id, ok := t.ActiveID(); ok {
		return id, nil
	}
	id, err := effectiveHub(t.hub).SelectProfile(ctx, interaction.SelectRequest{Kind: interaction.KindSSH})
	if err != nil {
		return "", err
	}
	if err := t.api.SetAgentActiveProfile(id); err != nil {
		return "", err
	}
	return id, nil
}

func (t *sshTarget) EnvForProfile(id string) (string, error) {
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
