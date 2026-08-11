package mcp

import (
	"context"

	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerSSH() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "ssh.listProfiles",
		Description: "List SSH connection profiles (metadata only)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolSSHListProfiles)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "ssh.testConnection",
		Description: "Test connectivity for an SSH profile by id",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolSSHTestConnection)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "ssh.activeProfile",
		Description: "Report the agent sticky active SSH profile",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolSSHActiveProfile)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "ssh.runCommand",
		Description: "Run a command on the active SSH profile (gated)",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(true),
		},
	}, s.toolSSHRunCommand)
}

func (s *Server) toolSSHListProfiles(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.ssh", "ssh:connect", "profile:list", meta("list_profiles", nil), func(context.Context) (any, error) {
		return s.deps.SSH.ListProfiles()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type sshTestConnectionInput struct {
	ProfileID  string `json:"profileId"`
	Passphrase string `json:"passphrase"`
}

func (s *Server) toolSSHTestConnection(ctx context.Context, _ *sdk.CallToolRequest, in sshTestConnectionInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ProfileID)
	v, err := s.deps.Caller.Call(ctx, "builtin.ssh", "ssh:connect", "profile:"+opaqueID, meta("test_connection", map[string]any{
		"profile_id": opaqueID,
	}), func(callCtx context.Context) (any, error) {
		return nil, s.deps.SSH.TestConnection(callCtx, in.ProfileID, in.Passphrase)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

func (s *Server) toolSSHActiveProfile(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.ssh", "ssh:connect", "profile:active", meta("active_profile", nil), func(context.Context) (any, error) {
		return ActiveProfileResult{ProfileID: s.deps.SSH.AgentActiveProfileID()}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type sshRunCommandInput struct {
	Command    string `json:"command"`
	Passphrase string `json:"passphrase"`
}

func (s *Server) toolSSHRunCommand(ctx context.Context, _ *sdk.CallToolRequest, in sshRunCommandInput) (*sdk.CallToolResult, any, error) {
	target := s.sshTarget()
	if err := GateActiveMCP(ctx, s.deps.Hub, s.deps.Envs, target, target, "ssh.runCommand", in.Command, interaction.KindSSH); err != nil {
		return nil, nil, mapInteractErr(err)
	}
	id := s.deps.SSH.AgentActiveProfileID()
	v, err := s.deps.Caller.Call(ctx, "builtin.ssh", "ssh:connect", "profile:active", meta("run_command", map[string]any{
		"profile_id": recordID(id),
	}), func(callCtx context.Context) (any, error) {
		return s.deps.SSH.RunCommand(callCtx, id, in.Command, in.Passphrase)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
