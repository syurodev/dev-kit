package bridge

import (
	"context"
	"sync"

	"devkit/internal/apiclient"
	"devkit/internal/app"
)

// apiSends holds the cancel of every send in flight, by the id the window
// gave it, so Cancel reaches the socket rather than only hiding a spinner.
var apiSends sync.Map // sendID -> context.CancelFunc

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

// ApiSaveRequest takes a RequestInput rather than a Request: that struct
// carries CreatedAt/UpdatedAt, and a bound method with a time in its arguments
// fails in the binding layer unless the frontend invents an RFC3339 stamp.
// Timestamps are the store's to write.
func (s *AppService) ApiSaveRequest(in apiclient.RequestInput) (apiclient.Request, error) {
	scope := "request:new"
	if in.ID != "" {
		scope = "request:" + recordID(in.ID)
	}
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", scope, map[string]any{
		"action": "save",
		"method": in.Method,
	}, func(context.Context) (any, error) {
		return s.container.APIClient.SaveRequest(in.Request())
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

// ApiUpdateEnvironment renames an environment, moves it to another tag and
// replaces its variables. Secret values travel one way: the update names the
// secrets to keep, and only new values cross the bridge.
func (s *AppService) ApiUpdateEnvironment(id, name, env string, update apiclient.VarsUpdate) (apiclient.Environment, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "environment:"+recordID(id), map[string]any{
		"action": "update_environment",
		"env":    env,
	}, func(context.Context) (any, error) {
		return s.container.APIClient.UpdateEnvironment(id, name, env, update)
	})
	if err != nil {
		return apiclient.Environment{}, err
	}
	return result.(apiclient.Environment), nil
}

func (s *AppService) ApiDuplicateEnvironment(id, name string) (apiclient.Environment, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "environment:new", map[string]any{
		"action": "duplicate_environment",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.DuplicateEnvironment(id, name)
	})
	if err != nil {
		return apiclient.Environment{}, err
	}
	return result.(apiclient.Environment), nil
}

func (s *AppService) ApiDeleteEnvironment(id string) error {
	_, err := s.call(context.Background(), "builtin.apiclient", "api:write", "environment:"+recordID(id), map[string]any{
		"action": "delete_environment",
	}, func(context.Context) (any, error) {
		return nil, s.container.APIClient.DeleteEnvironment(id)
	})
	return err
}

// ApiGlobals lists the global variables, secret values withheld.
func (s *AppService) ApiGlobals() (apiclient.VarSet, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "environment:list", map[string]any{
		"action": "globals",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.Globals()
	})
	if err != nil {
		return apiclient.VarSet{}, err
	}
	return result.(apiclient.VarSet), nil
}

func (s *AppService) ApiSetGlobals(update apiclient.VarsUpdate) (apiclient.VarSet, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "environment:globals", map[string]any{
		"action": "set_globals",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.SetGlobals(update)
	})
	if err != nil {
		return apiclient.VarSet{}, err
	}
	return result.(apiclient.VarSet), nil
}

// ApiVariables is what a request in a collection would see under an
// environment, after precedence, secret values withheld — for showing where a
// {{name}} comes from and which have no value anywhere.
func (s *AppService) ApiVariables(collectionID, environmentID string) ([]apiclient.ResolvedVar, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "environment:list", map[string]any{
		"action": "variables",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.VariablesFor(collectionID, environmentID)
	})
	if err != nil {
		return nil, err
	}
	return result.([]apiclient.ResolvedVar), nil
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

// ApiSendRequest sends what the editor holds, saved or not — the same
// capability, gate and history as ApiSend. sendID names the send so
// ApiCancelSend can stop it; an empty one cannot be cancelled.
func (s *AppService) ApiSendRequest(in apiclient.RequestInput, environmentID, sendID string) (apiclient.Response, error) {
	scope := "request:new"
	if in.ID != "" {
		scope = "request:" + recordID(in.ID)
	}
	result, err := s.call(context.Background(), "builtin.apiclient", "api:send", scope, map[string]any{
		"action":         "send",
		"method":         in.Method,
		"environment_id": recordID(environmentID),
	}, func(ctx context.Context) (any, error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		if sendID != "" {
			apiSends.Store(sendID, cancel)
			defer apiSends.Delete(sendID)
		}
		return s.container.APIClient.Send(ctx, apiclient.SendInput{
			Request: in.Request(), EnvironmentID: environmentID,
		})
	})
	if err != nil {
		return apiclient.Response{}, err
	}
	return result.(apiclient.Response), nil
}

// ApiCancelSend stops a send in flight. Stopping one that already finished is
// not an error: the person pressed Cancel as the answer arrived.
func (s *AppService) ApiCancelSend(sendID string) {
	if cancel, ok := apiSends.LoadAndDelete(sendID); ok {
		cancel.(context.CancelFunc)()
	}
}

// ApiCurl renders what the editor holds as cURL, with the plain variables of
// its collection and environment filled in and the credential left as a shell
// variable — safe to paste into a ticket. A secret variable stays {{name}}.
func (s *AppService) ApiCurl(in apiclient.RequestInput, environmentID string) (string, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:list", map[string]any{
		"action": "curl",
	}, func(context.Context) (any, error) {
		vars, err := s.container.APIClient.PlainVariables(in.CollectionID, environmentID)
		if err != nil {
			return nil, err
		}
		return apiclient.CurlFor(in.Request(), vars), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// ApiParseCurl reads a pasted curl command into a request the editor opens.
// Nothing is saved and nothing is run.
func (s *AppService) ApiParseCurl(command string) (apiclient.RequestInput, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:read", "request:list", map[string]any{
		"action": "parse_curl",
	}, func(context.Context) (any, error) {
		return apiclient.ParseCurl(command)
	})
	if err != nil {
		return apiclient.RequestInput{}, err
	}
	return result.(apiclient.RequestInput), nil
}

// ApiImportPostmanCollection reads a Postman v2 collection export into a new
// collection. The audit line says how big it was, never what was in it.
func (s *AppService) ApiImportPostmanCollection(data string) (apiclient.ImportResult, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:new", map[string]any{
		"action": "import_postman_collection",
		"bytes":  len(data),
	}, func(context.Context) (any, error) {
		return s.container.APIClient.ImportPostmanCollection(data)
	})
	if err != nil {
		return apiclient.ImportResult{}, err
	}
	return result.(apiclient.ImportResult), nil
}

// ApiImportPostmanEnvironment reads a Postman environment (or globals) export.
// Postman has no env tags, so the window asks for one.
func (s *AppService) ApiImportPostmanEnvironment(data, envTag string) (apiclient.ImportResult, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "environment:new", map[string]any{
		"action": "import_postman_environment",
		"env":    envTag,
		"bytes":  len(data),
	}, func(context.Context) (any, error) {
		return s.container.APIClient.ImportPostmanEnvironment(data, envTag)
	})
	if err != nil {
		return apiclient.ImportResult{}, err
	}
	return result.(apiclient.ImportResult), nil
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

// ApiSetCollectionVariables replaces a collection's (or folder's) variables.
func (s *AppService) ApiSetCollectionVariables(id string, update apiclient.VarsUpdate) (apiclient.Collection, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:*", map[string]any{
		"action": "collection_variables",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.SetCollectionVariables(id, update)
	})
	if err != nil {
		return apiclient.Collection{}, err
	}
	return result.(apiclient.Collection), nil
}

// ApiDeleteCollectionTree removes a folder with everything inside. The window
// only offers it after showing what that is; it returns how many requests
// went with it.
func (s *AppService) ApiDeleteCollectionTree(id string) (int, error) {
	result, err := s.call(context.Background(), "builtin.apiclient", "api:write", "request:*", map[string]any{
		"action": "delete_collection_tree",
	}, func(context.Context) (any, error) {
		return s.container.APIClient.DeleteCollectionTree(id)
	})
	if err != nil {
		return 0, err
	}
	return result.(int), nil
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
