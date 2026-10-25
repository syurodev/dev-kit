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

// GateActiveMCP ensures an active target exists and env confirmation policy is
// satisfied, returning the profile id the confirmation covered. The caller MUST
// execute against that exact id: re-reading the sticky active target after the
// gate would let a concurrent SetActiveProfile swap the target between the
// user's confirmation and the execution. When RequiresConfirmation(env) is
// true, Confirm runs every call (allow_always ignored). When false, Confirm may
// short-circuit via allow_always after the user opts in once. A nil hub behaves
// as headless.
func GateActiveMCP(
	ctx context.Context,
	hub *interaction.Hub,
	env EnvConfirmer,
	profiles ProfileEnvReader,
	active ActiveTarget,
	tool, summary string,
	kind interaction.Kind,
) (string, error) {
	id, err := active.EnsureActive(ctx)
	if err != nil {
		return "", err
	}
	envName, err := profiles.EnvForProfile(id)
	if err != nil {
		return "", err
	}
	err = effectiveHub(hub).Confirm(ctx, interaction.ConfirmRequest{Env: envName, Tool: tool, ProfileID: id, Summary: summary, Kind: kind, EnvBlocks: env.RequiresConfirmation(envName)})
	return id, err
}

type ActiveMCPDecision struct {
	ProfileID, Env, TargetRevision, EnvRevision, ApprovalFingerprint, ReservationToken string
	ApprovedByScope                                                                    bool
}

// GateActiveMCPWithApproval is used by a caller that can revalidate the saved
// target immediately before execution. Other gate callers keep the historical
// manual-confirmation behavior.
func GateActiveMCPWithApproval(ctx context.Context, hub *interaction.Hub, env EnvConfirmer, profiles ProfileEnvReader, active ActiveTarget, tool, summary string, kind interaction.Kind, fingerprint string) (ActiveMCPDecision, error) {
	id, err := active.EnsureActive(ctx)
	if err != nil {
		return ActiveMCPDecision{}, err
	}
	envName, err := profiles.EnvForProfile(id)
	if err != nil {
		return ActiveMCPDecision{}, err
	}
	blocks := env.RequiresConfirmation(envName)
	envRevision := ""
	if revisioner, ok := env.(interface{ PolicyRevision(string) (string, error) }); ok {
		envRevision, _ = revisioner.PolicyRevision(envName) // empty disables this approval path fail-closed
	}
	targetRevision := ""
	if tool == "db.query" && kind == interaction.KindDatabase {
		if revisioner, ok := profiles.(interface{ ApprovalRevision(string) (string, error) }); ok {
			targetRevision, _ = revisioner.ApprovalRevision(id) // unavailable means no scoped match; ordinary confirmation still works
		}
	}
	if fingerprint != "" {
		effectiveHub(hub).InvalidateChangedScopedApprovals(id, envName, targetRevision, envRevision)
	}
	confirmReq := interaction.ConfirmRequest{
		Env:                 envName,
		Tool:                tool,
		ProfileID:           id,
		Summary:             summary,
		Kind:                kind,
		EnvBlocks:           blocks,
		ApprovalFingerprint: fingerprint,
		TargetRevision:      targetRevision,
		EnvRevision:         envRevision,
	}
	approvedByScope, reservationToken, err := effectiveHub(hub).ConfirmDecision(ctx, confirmReq)
	if err != nil {
		return ActiveMCPDecision{}, err
	}
	if approvedByScope {
		// A scoped approval is invalidated by policy or target changes. A human
		// confirmation remains authoritative for this captured profile as before.
		currentEnv, err := profiles.EnvForProfile(id)
		currentRevision := ""
		if revisioner, ok := env.(interface{ PolicyRevision(string) (string, error) }); ok {
			currentRevision, _ = revisioner.PolicyRevision(currentEnv)
		}
		if err != nil || currentEnv != envName || env.RequiresConfirmation(currentEnv) || envRevision == "" || currentRevision != envRevision {
			effectiveHub(hub).RevokeMatchingScopedApproval(ctx, confirmReq)
			return ActiveMCPDecision{}, interaction.ErrConfirmationDenied
		}
		if targetRevision != "" {
			revision, err := profiles.(interface{ ApprovalRevision(string) (string, error) }).ApprovalRevision(id)
			if err != nil || revision != targetRevision {
				effectiveHub(hub).RevokeMatchingScopedApproval(ctx, confirmReq)
				return ActiveMCPDecision{}, interaction.ErrConfirmationDenied
			}
		}
	}
	return ActiveMCPDecision{ProfileID: id, Env: envName, TargetRevision: targetRevision, EnvRevision: envRevision, ApprovalFingerprint: fingerprint, ReservationToken: reservationToken, ApprovedByScope: approvedByScope}, nil
}

// GateProfileMCP is GateActiveMCP for a tool that names its profile in its own
// arguments instead of using the sticky active target: db.explain,
// db.listForeignKeys, db.schema.get (which indexes the live database on a
// cache miss) and the two testConnection tools. They only read, so a tag that
// does not require confirmation lets them through without a prompt, as before.
// A tag that does — prod, a custom tag that opted in, or a name that no
// longer resolves (fail closed) — prompts on every call and never honours
// allow_always, because these tools still open a connection to that server,
// and "an agent may reach prod without asking" is exactly the sentence the env
// tag exists to make false. A nil hub behaves as headless.
func GateProfileMCP(
	ctx context.Context,
	hub *interaction.Hub,
	env EnvConfirmer,
	profiles ProfileEnvReader,
	profileID, tool, summary string,
	kind interaction.Kind,
) error {
	envName, err := profiles.EnvForProfile(profileID)
	if err != nil {
		return err
	}
	if !env.RequiresConfirmation(envName) {
		return nil
	}
	return effectiveHub(hub).Confirm(ctx, interaction.ConfirmRequest{
		Env:       envName,
		Tool:      tool,
		ProfileID: profileID,
		Summary:   summary,
		Kind:      kind,
		EnvBlocks: true,
	})
}
