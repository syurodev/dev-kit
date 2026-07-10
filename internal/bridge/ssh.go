package bridge

import (
	"context"

	sshservice "changeme/internal/services/ssh"
)

func (s *AppService) SSHListProfiles() ([]sshservice.ProfileSummary, error) {
	return s.container.SSH.ListProfiles()
}

func (s *AppService) SSHCreateProfile(input sshservice.CreateProfileInput) (sshservice.ProfileSummary, error) {
	return s.container.SSH.CreateProfile(input)
}

func (s *AppService) SSHDeleteProfile(id string) error {
	return s.container.SSH.DeleteProfile(id)
}

func (s *AppService) SSHTestConnection(ctx context.Context, id string) error {
	return s.container.SSH.TestConnection(ctx, id)
}

func (s *AppService) SSHRunCommand(ctx context.Context, id string, command string) (sshservice.CommandResult, error) {
	return s.container.SSH.RunCommand(ctx, id, command)
}
