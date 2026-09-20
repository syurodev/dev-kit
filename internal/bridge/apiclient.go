package bridge

import (
	"context"

	"devkit/internal/apiclient"
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
