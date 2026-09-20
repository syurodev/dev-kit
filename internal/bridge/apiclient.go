package bridge

import (
	"context"

	"devkit/internal/apiclient"
	"devkit/internal/app"
)

func (s *AppService) ApiRequests() ([]apiclient.Request, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:list", map[string]any{
		"action": "list",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.Requests()
	})
	if err != nil {
		return nil, err
	}
	return result.([]apiclient.Request), nil
}

func (s *AppService) ApiRequest(id string) (apiclient.Request, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:"+opaqueID, map[string]any{
		"action":     "get",
		"request_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.APIClient.Request(id)
	})
	if err != nil {
		return apiclient.Request{}, err
	}
	return result.(apiclient.Request), nil
}

// ApiSaveRequest takes the fields the window actually has rather than a whole
// Request: that struct carries CreatedAt/UpdatedAt, and a bound method with a
// time in its arguments fails in the binding layer unless the frontend invents
// an RFC3339 stamp. Timestamps are the store's to write.
func (s *AppService) ApiSaveRequest(
	id, collectionID, name, method, url string,
	query, headers map[string]string,
	body, authKind, accountID, authHeader, originRef string,
) (apiclient.Request, error) {
	scope := "request:new"
	if id != "" {
		scope = "request:" + recordID(id)
	}
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", scope, map[string]any{
		"action": "save",
		"method": method,
	}, func(context.Context) (any, error) {
		return s.container.APIClient.SaveRequest(apiclient.Request{
			ID: id, CollectionID: collectionID, Name: name, Method: method,
			URL: url, Query: query, Headers: headers, Body: body,
			Auth: apiclient.Auth{
				Kind:      apiclient.AuthKind(authKind),
				AccountID: accountID,
				Header:    authHeader,
			},
			OriginRef: originRef,
		})
	})
	if err != nil {
		return apiclient.Request{}, err
	}
	return result.(apiclient.Request), nil
}

func (s *AppService) ApiDeleteRequest(id string) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:"+opaqueID, map[string]any{
		"action":     "delete",
		"request_id": opaqueID,
	}, func(context.Context) (any, error) {
		return nil, s.container.APIClient.DeleteRequest(id)
	})
	return err
}

func (s *AppService) ApiEnvironments() ([]apiclient.Environment, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "environment:list", map[string]any{
		"action": "environments",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.Environments()
	})
	if err != nil {
		return nil, err
	}
	return result.([]apiclient.Environment), nil
}

// ApiCreateEnvironment takes the secret variables once. They are sealed and
// never returned by any method here — Environments lists their names only.
func (s *AppService) ApiCreateEnvironment(name, env string, vars map[string]string, secrets map[string]string) (apiclient.Environment, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "environment:new", map[string]any{
		"action": "create_environment",
		"env":    env,
	}, func(context.Context) (any, error) {
		return s.container.APIClient.CreateEnvironment(name, env, vars, secrets)
	})
	if err != nil {
		return apiclient.Environment{}, err
	}
	return result.(apiclient.Environment), nil
}

// ApiSend is its own capability: reading a request and firing it at a
// production host are not the same permission.
//
// The audit metadata carries method and environment, never headers or body —
// the credential is resolved inside the service and does not pass here.
func (s *AppService) ApiSend(id, environmentID string) (apiclient.Response, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.apiclient", "api:send", "request:"+opaqueID, map[string]any{
		"action":         "send",
		"request_id":     opaqueID,
		"environment_id": recordID(environmentID),
	}, func(ctx context.Context) (any, error) {
		req, err := s.container.APIClient.Request(id)
		if err != nil {
			return nil, err
		}
		return s.container.APIClient.Send(ctx, apiclient.SendInput{
			Request: req, EnvironmentID: environmentID,
		})
	})
	if err != nil {
		return apiclient.Response{}, err
	}
	return result.(apiclient.Response), nil
}

// ApiCurl renders a request as cURL with the credential left as a shell
// variable, so the result is safe to paste into a ticket.
func (s *AppService) ApiCurl(id string, vars map[string]string) (string, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:"+opaqueID, map[string]any{
		"action":     "curl",
		"request_id": opaqueID,
	}, func(context.Context) (any, error) {
		req, err := s.container.APIClient.Request(id)
		if err != nil {
			return nil, err
		}
		return apiclient.CurlFor(req, vars), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// ---- collections -----------------------------------------------------

func (s *AppService) ApiCollections() ([]apiclient.Collection, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:list", map[string]any{
		"action": "collections",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.Collections()
	})
	if err != nil {
		return nil, err
	}
	return result.([]apiclient.Collection), nil
}

// ApiSaveCollection creates or renames a folder. It takes the fields the
// window has rather than the struct, for the same reason ApiSaveRequest does.
func (s *AppService) ApiSaveCollection(id, name, parentID string, order int) (apiclient.Collection, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:*", map[string]any{
		"action": "save_collection",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.SaveCollection(apiclient.Collection{
			ID: id, Name: name, ParentID: parentID, Order: order,
		})
	})
	if err != nil {
		return apiclient.Collection{}, err
	}
	return result.(apiclient.Collection), nil
}

// ApiDeleteCollection removes an empty folder and refuses a full one, naming
// what is inside.
func (s *AppService) ApiDeleteCollection(id string) error {
	_, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:*", map[string]any{
		"action": "delete_collection",
	}, func(context.Context) (any, error) {
		return nil, s.container.APIClient.DeleteCollection(id)
	})
	return err
}

// ---- history ---------------------------------------------------------

// ApiHistory lists what this machine sent for one request. The URL is sealed
// at rest and opened here; a locked vault gives the row back without it.
func (s *AppService) ApiHistory(requestID string, limit int) ([]apiclient.HistoryEntry, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:list", map[string]any{
		"action": "history",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.History(requestID, limit)
	})
	if err != nil {
		return nil, err
	}
	return result.([]apiclient.HistoryEntry), nil
}

// ApiClearHistory forgets one request's sends, or every request's when the id
// is empty.
func (s *AppService) ApiClearHistory(requestID string) error {
	_, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:*", map[string]any{
		"action": "clear_history",
	}, func(context.Context) (any, error) {
		return nil, s.container.APIClient.ClearHistory(requestID)
	})
	return err
}

// ApiDraftFromEndpoint builds a request from a Code Graph endpoint without
// saving it. FR-7 is explicit that a draft becomes a request only when the
// person saves it, so this returns one and stores nothing.
func (s *AppService) ApiDraftFromEndpoint(endpointID string) (apiclient.EndpointDraft, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:list", map[string]any{
		"action":      "draft_from_endpoint",
		"endpoint_id": recordID(endpointID),
	}, func(ctx context.Context) (any, error) {
		return app.DraftFromEndpoint(ctx, s.container.CodeGraph, endpointID)
	})
	if err != nil {
		return apiclient.EndpointDraft{}, err
	}
	return result.(apiclient.EndpointDraft), nil
}
