package bridge

import (
	"context"
	"errors"

	"devkit/internal/interaction"
)

var errInteractionUnavailable = errors.New("interaction hub is not available")

func (s *AppService) interactionHub() (*interaction.Hub, error) {
	if s.container.Interaction == nil {
		return nil, errInteractionUnavailable
	}
	return s.container.Interaction, nil
}

// InteractionPending returns the current pending selection/confirmation, or
// nil when none is waiting. The frontend polls this while InteractionHost is
// mounted.
func (s *AppService) InteractionPending() (*interaction.Pending, error) {
	result, err := s.call(context.Background(), "builtin.interaction", "interaction:respond", "pending", map[string]any{
		"action": "pending",
	}, func(context.Context) (any, error) {
		hub, err := s.interactionHub()
		if err != nil {
			return nil, err
		}
		return hub.Pending(), nil
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*interaction.Pending), nil
}

// InteractionComplete resolves a pending interaction with the given value
// (profile id, "allow", "allow_always", "deny", or "" for select cancel).
func (s *AppService) InteractionComplete(id, value string) error {
	_, err := s.call(context.Background(), "builtin.interaction", "interaction:respond", "pending:complete", map[string]any{
		"action": "complete",
		"id":     recordID(id),
	}, func(context.Context) (any, error) {
		hub, err := s.interactionHub()
		if err != nil {
			return nil, err
		}
		return nil, hub.Complete(id, value)
	})
	return err
}

// InteractionDeny cancels a pending interaction: "deny" for confirm, or "" for
// select cancel.
func (s *AppService) InteractionDeny(id string) error {
	_, err := s.call(context.Background(), "builtin.interaction", "interaction:respond", "pending:complete", map[string]any{
		"action": "deny",
		"id":     recordID(id),
	}, func(context.Context) (any, error) {
		hub, err := s.interactionHub()
		if err != nil {
			return nil, err
		}
		p := hub.Pending()
		if p == nil {
			return nil, interaction.ErrNoPendingInteraction
		}
		if p.ID != id {
			return nil, interaction.ErrInvalidInteractionID
		}
		value := "deny"
		if p.Type == interaction.TypeSelect {
			value = ""
		}
		return nil, hub.Complete(id, value)
	})
	return err
}
