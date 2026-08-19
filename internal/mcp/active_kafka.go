package mcp

import (
	"context"

	"devkit/internal/interaction"
)

// kafkaTarget implements ActiveTarget and ProfileEnvReader for Kafka MCP tools.
// Active target is the live connected profile (Status), not a sticky agent id.
type kafkaTarget struct {
	api KafkaAPI
	hub *interaction.Hub
}

func (t *kafkaTarget) ActiveID() (string, bool) {
	if t == nil || t.api == nil {
		return "", false
	}
	st := t.api.Status()
	if st.Connected && st.ProfileID != "" {
		return st.ProfileID, true
	}
	return "", false
}

func (t *kafkaTarget) EnsureActive(ctx context.Context) (string, error) {
	if id, ok := t.ActiveID(); ok {
		return id, nil
	}
	// Selection only: the caller establishes the connection after the env
	// confirmation, so sealed SASL credentials never travel to the broker
	// before the user has approved the target.
	return effectiveHub(t.hub).SelectProfile(ctx, interaction.SelectRequest{Kind: interaction.KindKafka})
}

func (t *kafkaTarget) EnvForProfile(id string) (string, error) {
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
