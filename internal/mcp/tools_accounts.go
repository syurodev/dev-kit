package mcp

import (
	"context"

	"devkit/internal/accounts"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AccountsAPI is the credential surface MCP tools invoke — and the whole
// surface is read-only.
//
// There is no Resolve here, no Reveal, and no parameter on any tool that could
// return a value. An agent selects an account by id and hands that id to the
// tool of the module doing the work (git.push, api.send); the credential is
// resolved inside Go and never enters a transcript. That is the point of the
// interface being this small: an agent cannot read a credential because the
// method does not exist, not because a check declines.
type AccountsAPI interface {
	List() ([]accounts.Account, error)
	Get(id string) (accounts.Account, error)
}

func (s *Server) registerAccounts(ms *sdk.Server) {
	if s.deps.Accounts == nil {
		return
	}

	sdk.AddTool(ms, &sdk.Tool{
		Name: "accounts.list",
		Description: "List saved accounts: who they are on which service, what scopes they have and " +
			"when they expire. Never returns a credential",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAccountsList)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "accounts.get",
		Description: "Get one account's provider, subject, scopes and expiry. Never returns a credential",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAccountsGet)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "accounts.status",
		Description: "Summarise which accounts are not usable right now — expired, revoked or never " +
			"verified. Read this when a call comes back 401 and say which credential needs attention",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAccountsStatus)
}

func (s *Server) toolAccountsList(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.accounts", "accounts:read", "account:list", meta("list", nil), func(context.Context) (any, error) {
		return s.deps.Accounts.List()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type accountIDInput struct {
	AccountID string `json:"accountId"`
}

func (s *Server) toolAccountsGet(ctx context.Context, _ *sdk.CallToolRequest, in accountIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.accounts", "accounts:read", "account:"+in.AccountID, meta("get", nil), func(context.Context) (any, error) {
		return s.deps.Accounts.Get(in.AccountID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// accountProblem is one account that will not work, and why.
type accountProblem struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Subject  string `json:"subject"`
	State    string `json:"state"`
}

func (s *Server) toolAccountsStatus(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.accounts", "accounts:read", "account:list", meta("status", nil), func(context.Context) (any, error) {
		all, err := s.deps.Accounts.List()
		if err != nil {
			return nil, err
		}
		problems := make([]accountProblem, 0)
		for _, account := range all {
			if account.State == accounts.StateValid {
				continue
			}
			problems = append(problems, accountProblem{
				ID: account.ID, Provider: account.Provider, Label: account.Label,
				Subject: account.Subject, State: string(account.State),
			})
		}
		return map[string]any{"total": len(all), "problems": problems}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
