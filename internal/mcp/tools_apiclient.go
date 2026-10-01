package mcp

import (
	"context"

	"devkit/internal/apiclient"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// APIClientAPI is the HTTP surface MCP tools invoke.
//
// Editing or deleting an environment is deliberately not on it: an environment
// decides which host an agent is allowed to fire at and whether doing so asks
// first, so an agent that can edit one can point itself at production.
type APIClientAPI interface {
	Requests() ([]apiclient.Request, error)
	Request(id string) (apiclient.Request, error)
	Environments() ([]apiclient.Environment, error)
	Send(ctx context.Context, in apiclient.SendInput) (apiclient.Response, error)
	DraftFromEndpoint(ctx context.Context, endpointID string) (apiclient.EndpointDraft, error)
}

func (s *Server) registerAPIClient(ms *sdk.Server) {
	if s.deps.API == nil {
		return
	}

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "api.listRequests",
		Description: "List saved HTTP requests (method, name and URL template)",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAPIListRequests)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.getRequest",
		Description: "Get one request. The auth binding comes back as an account id and a kind — " +
			"never a credential — and a header, query or form value named like a credential " +
			"(Authorization, Cookie, *token*, *key*…) comes back masked. Send a saved request " +
			"with api.send, not by copying it into api.sendAdhoc",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAPIGetRequest)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.listEnvironments",
		Description: "List environments with their environment tag. Secret variables come back as " +
			"names only",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAPIListEnvironments)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.send",
		Description: "Send a saved request against an environment. An environment is required. " +
			"A tag that requires confirmation asks every time, and the response is truncated",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPISend)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.sendAdhoc",
		Description: "Send a request that is not saved — typically one drafted by api.fromEndpoint, " +
			"with its :path variables and body filled in. Same rules as api.send: an environment " +
			"is required, its tag decides whether the person is asked first, {{variables}} come " +
			"from globals, the request's collection and the environment, and the response is truncated. " +
			"Nothing is saved. A credential is bound with auth.accountId, never typed into a header; " +
			"a request that carries an account or a secret variable asks the person every time, " +
			"whatever the environment",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPISendAdhoc)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.fromEndpoint",
		Description: "Draft a request from a Code Graph endpoint: method, path, query and header " +
			"parameters, and a body skeleton when the handler declares a type this project's " +
			"source contains. Nothing is saved, and `note` says why a body is missing",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAPIFromEndpoint)

	// Deliberately absent: creating, editing or deleting an environment. An
	// environment names the hosts and decides whether firing at them asks
	// first — an agent that can write one can aim itself at production.
}

func (s *Server) toolAPIListRequests(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:read", "request:list", meta("list", nil), func(context.Context) (any, error) {
		return s.deps.API.Requests()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiRequestIDInput struct {
	RequestID string `json:"requestId"`
}

func (s *Server) toolAPIGetRequest(ctx context.Context, _ *sdk.CallToolRequest, in apiRequestIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:read", "request:"+in.RequestID, meta("get", nil), func(context.Context) (any, error) {
		req, err := s.deps.API.Request(in.RequestID)
		if err != nil {
			return nil, err
		}
		return req.ForAgent(), nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolAPIListEnvironments(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:read", "environment:list", meta("environments", nil), func(context.Context) (any, error) {
		return s.deps.API.Environments()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiSendInput struct {
	RequestID     string `json:"requestId"`
	EnvironmentID string `json:"environmentId"`
}

func (s *Server) toolAPISend(ctx context.Context, _ *sdk.CallToolRequest, in apiSendInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:send", "request:"+in.RequestID, meta("send", map[string]any{
		"environment_id": in.EnvironmentID,
	}), func(ctx context.Context) (any, error) {
		req, err := s.deps.API.Request(in.RequestID)
		if err != nil {
			return nil, err
		}
		// ForAgent bounds the body much harder than the UI's: a response
		// handed to a model is context, and context is not free.
		return s.deps.API.Send(ctx, apiclient.SendInput{
			Request: req, EnvironmentID: in.EnvironmentID, ForAgent: true,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiSendAdhocInput struct {
	Request       apiclient.RequestInput `json:"request"`
	EnvironmentID string                 `json:"environmentId"`
}

func (s *Server) toolAPISendAdhoc(ctx context.Context, _ *sdk.CallToolRequest, in apiSendAdhocInput) (*sdk.CallToolResult, any, error) {
	// No request id: an ad-hoc send is not a saved request, and taking an id
	// here would let a masked copy of one be sent as if it were the original.
	in.Request.ID = ""
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:send", "request:new", meta("send_adhoc", map[string]any{
		"environment_id": in.EnvironmentID,
		"method":         in.Request.Method,
	}), func(ctx context.Context) (any, error) {
		return s.deps.API.Send(ctx, apiclient.SendInput{
			Request: in.Request.Request(), EnvironmentID: in.EnvironmentID, ForAgent: true, Adhoc: true,
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiEndpointInput struct {
	EndpointID string `json:"endpointId"`
}

// toolAPIFromEndpoint drafts without saving. An agent sends the draft with
// api.sendAdhoc; one that only wanted to see the shape should not leave a row
// in somebody's sidebar to find out.
func (s *Server) toolAPIFromEndpoint(ctx context.Context, _ *sdk.CallToolRequest, in apiEndpointInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:read", "request:list", meta("from_endpoint", map[string]any{
		"endpoint_id": in.EndpointID,
	}), func(ctx context.Context) (any, error) {
		return s.deps.API.DraftFromEndpoint(ctx, in.EndpointID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
