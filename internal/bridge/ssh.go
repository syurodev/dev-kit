package bridge

import (
	"context"

	sshservice "changeme/internal/services/ssh"
)

func (s *AppService) SSHListProfiles() ([]sshservice.ProfileSummary, error) {
	result, err := s.call(context.Background(), "builtin.ssh", "ssh:connect", "profile:list", map[string]any{
		"action": "list_profiles",
	}, func(context.Context) (any, error) {
		return s.container.SSH.ListProfiles()
	})
	if err != nil {
		return nil, err
	}
	return result.([]sshservice.ProfileSummary), nil
}

func (s *AppService) SSHCreateProfile(input sshservice.CreateProfileInput) (sshservice.ProfileSummary, error) {
	result, err := s.call(context.Background(), "builtin.ssh", "ssh:connect", "profile:new", map[string]any{
		"action": "create_profile",
	}, func(context.Context) (any, error) {
		return s.container.SSH.CreateProfile(input)
	})
	if err != nil {
		return sshservice.ProfileSummary{}, err
	}
	return result.(sshservice.ProfileSummary), nil
}

func (s *AppService) SSHDeleteProfile(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.ssh", "ssh:connect", "profile:"+opaqueID, map[string]any{
		"action":     "delete_profile",
		"profile_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.SSH.DeleteProfile(id)
	})
	return err
}

func (s *AppService) SSHObserveHostKey(ctx context.Context, id string) (string, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.ssh", "ssh:connect", "profile:"+opaqueID, map[string]any{
		"action":     "observe_host_key",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.SSH.ObserveHostKey(callCtx, id)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (s *AppService) SSHTrustHostKey(ctx context.Context, id string, fingerprint string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.ssh", "ssh:connect", "profile:"+opaqueID, map[string]any{
		"action":     "trust_host_key",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.SSH.TrustHostKey(callCtx, id, fingerprint)
	})
	return err
}

func (s *AppService) SSHTestConnection(ctx context.Context, id string, passphrase string) error {
	opaqueID := recordID(id)
	_, err := s.call(ctx, "builtin.ssh", "ssh:connect", "profile:"+opaqueID, map[string]any{
		"action":     "test_connection",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return nil, s.container.SSH.TestConnection(callCtx, id, passphrase)
	})
	return err
}

func (s *AppService) SSHRunCommand(ctx context.Context, id string, command string, passphrase string) (sshservice.CommandResult, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.ssh", "ssh:connect", "profile:"+opaqueID, map[string]any{
		"action":     "run_command",
		"profile_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.SSH.RunCommand(callCtx, id, command, passphrase)
	})
	if err != nil {
		return sshservice.CommandResult{}, err
	}
	return result.(sshservice.CommandResult), nil
}

// SSHStartForward starts a runtime-only local (-L) or remote (-R) TCP
// forward. The forwarding capability is separate from ordinary SSH command
// execution because it creates a long-lived network path.
func (s *AppService) SSHStartForward(ctx context.Context, id string, spec sshservice.ForwardSpec, passphrase string) (sshservice.ForwardInfo, error) {
	opaqueID := recordID(id)
	result, err := s.call(ctx, "builtin.ssh", "ssh:forward", "profile:"+opaqueID, map[string]any{
		"action":     "start_forward",
		"profile_id": opaqueID,
		"direction":  spec.Direction,
	}, func(callCtx context.Context) (any, error) {
		return s.container.SSH.StartForward(callCtx, id, spec, passphrase)
	})
	if err != nil {
		return sshservice.ForwardInfo{}, err
	}
	return result.(sshservice.ForwardInfo), nil
}

func (s *AppService) SSHListForwards() ([]sshservice.ForwardInfo, error) {
	result, err := s.call(context.Background(), "builtin.ssh", "ssh:forward", "forward:list", map[string]any{
		"action": "list_forwards",
	}, func(context.Context) (any, error) {
		return s.container.SSH.ListForwards(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]sshservice.ForwardInfo), nil
}

func (s *AppService) SSHStopForward(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.ssh", "ssh:forward", "forward:"+opaqueID, map[string]any{
		"action":     "stop_forward",
		"forward_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.SSH.StopForward(id)
	})
	return err
}
