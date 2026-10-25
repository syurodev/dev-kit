package mcp

import (
	"context"
	"errors"

	"devkit/internal/interaction"
)

// ApprovalRevision digests the non-secret saved profile metadata that affects
// where a query executes. It is recomputed immediately before execution.
func (t *databaseTarget) ApprovalRevision(id string) (string, error) {
	revisioner, ok := t.api.(interface{ ApprovalRevision(string) (string, error) })
	if !ok {
		return "", errors.New("database profile revision is unavailable")
	}
	dbRevision, err := revisioner.ApprovalRevision(id)
	if err != nil {
		return "", err
	}
	profiles, err := t.api.ListProfiles()
	if err != nil {
		return "", err
	}
	for _, p := range profiles {
		if p.ID == id {
			sshRevision := ""
			if p.SSHProfileID != "" {
				revisioner, ok := t.ssh.(interface{ ApprovalRevision(string) (string, error) })
				if !ok {
					return "", errors.New("SSH tunnel revision is unavailable")
				}
				sshRevision, err = revisioner.ApprovalRevision(p.SSHProfileID)
				if err != nil {
					return "", err
				}
			}
			return interaction.Fingerprint([]byte(dbRevision + ":ssh:" + sshRevision)), nil
		}
	}
	return "", errUnknownProfile
}

// databaseTarget implements ActiveTarget and ProfileEnvReader for database MCP tools.
type databaseTarget struct {
	api DatabaseAPI
	ssh SSHAPI
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
