package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"devkit/internal/apiclient"
	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The API client's write tools. What each one asks is set out on
// APIClientAPI; the asking itself is confirmAPIWrite, inside the capability
// check, so a caller without the grant is refused before anyone is asked.

func (s *Server) registerAPIClientWrites(ms *sdk.Server) {
	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.listCollections",
		Description: "List collections and their folders (a folder is a collection with a parentId), with " +
			"their plain variables. Secret variables come back as names only",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolAPIListCollections)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.createCollection",
		Description: "Create a collection, or a folder inside one (parentId). vars are plain variables, " +
			"secrets are sealed and never read back. The user is asked first and may allow it for the rest " +
			"of the session",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPICreateCollection)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.updateCollection",
		Description: "Rename a collection or folder, move it (parentId; \"\" for top level), and edit its " +
			"variables: setVars/removeVars, setSecrets/removeSecrets — anything not named is kept. A rename " +
			"asks once per session; a move or a variable change asks the user every time, because every " +
			"request inside reads them",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPIUpdateCollection)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.deleteCollection",
		Description: "Delete an EMPTY collection or folder; one still holding requests or folders is refused " +
			"with what is inside. Asks the user every time",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, s.toolAPIDeleteCollection)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.saveRequest",
		Description: "Save a request: a new one without request.id, or replace the saved one with that id " +
			"(read it with api.getRequest first; a masked value sent back unchanged keeps what is stored). " +
			"Write {{variables}} for anything that differs per environment, and bind a credential with " +
			"auth.accountId — never type one into a header. A request that carries a credential (an account, " +
			"a secret variable, a credential typed into a row) asks the user every time, because api.send " +
			"will hand it to this request's host; any other asks once per session",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPISaveRequest)

	sdk.AddTool(ms, &sdk.Tool{
		Name:        "api.deleteRequest",
		Description: "Delete a saved request and its send history on this machine. Asks the user every time",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, s.toolAPIDeleteRequest)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.createEnvironment",
		Description: "Create an environment: a name, an environment tag (env.listTags — the tag decides " +
			"whether every send against it asks first), plain vars and sealed secrets. Asks the user every " +
			"time: an environment decides where requests go",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPICreateEnvironment)

	sdk.AddTool(ms, &sdk.Tool{
		Name: "api.updateEnvironment",
		Description: "Rename an environment, move it to another tag, and edit its variables: " +
			"setVars/removeVars, setSecrets/removeSecrets — anything not named is kept. Asks the user every time",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: boolPtr(false)},
	}, s.toolAPIUpdateEnvironment)
}

// confirmAPIWrite asks the person before an agent's write lands. everyTime
// withholds allow_always: the answer covers this one write and no other.
// Headless (server mode, no prompter) is a refusal, like every other prompt.
func (s *Server) confirmAPIWrite(ctx context.Context, tool, env, summary string, everyTime bool) error {
	return mapInteractErr(effectiveHub(s.deps.Hub).Confirm(ctx, interaction.ConfirmRequest{
		Env:       env,
		Tool:      tool,
		Summary:   summary,
		Kind:      interaction.KindAPI,
		EnvBlocks: everyTime,
	}))
}

// ---- variables -------------------------------------------------------

// apiVarsEdit is a variable edit as an agent states it: what to set and what
// to remove, everything else kept. VarsUpdate replaces a scope's variables
// whole, which is right for an editor holding all of them and wrong for an
// agent that names one — leaving the rest out would delete them.
type apiVarsEdit struct {
	SetVars       map[string]string
	RemoveVars    []string
	SetSecrets    map[string]string
	RemoveSecrets []string
}

func (e apiVarsEdit) empty() bool {
	return len(e.SetVars) == 0 && len(e.RemoveVars) == 0 && len(e.SetSecrets) == 0 && len(e.RemoveSecrets) == 0
}

// update applies the edit to a scope's current variables. A name set as
// plain stops being secret and the reverse, so one name is never in both
// halves (VarsUpdate.apply would silently keep the secret).
func (e apiVarsEdit) update(vars map[string]string, secretNames []string) apiclient.VarsUpdate {
	out := apiclient.VarsUpdate{Vars: map[string]string{}, Secrets: e.SetSecrets}
	for key, value := range vars {
		out.Vars[key] = value
	}
	for _, key := range e.RemoveVars {
		delete(out.Vars, key)
	}
	for key, value := range e.SetVars {
		out.Vars[key] = value
	}
	drop := map[string]bool{}
	for _, key := range e.RemoveSecrets {
		drop[key] = true
	}
	for key := range e.SetVars {
		drop[key] = true
	}
	for _, key := range secretNames {
		if !drop[key] {
			out.KeepSecrets = append(out.KeepSecrets, key)
		}
	}
	return out
}

// describe is the edit as the person reads it: plain values shown (they are
// shown in the window too), secret values never.
func (e apiVarsEdit) describe() string {
	var parts []string
	if len(e.SetVars) > 0 {
		parts = append(parts, "set "+describeVars(e.SetVars))
	}
	if len(e.SetSecrets) > 0 {
		parts = append(parts, "set secrets "+strings.Join(sortedNames(e.SetSecrets), ", "))
	}
	if len(e.RemoveVars) > 0 {
		parts = append(parts, "remove "+strings.Join(e.RemoveVars, ", "))
	}
	if len(e.RemoveSecrets) > 0 {
		parts = append(parts, "remove secrets "+strings.Join(e.RemoveSecrets, ", "))
	}
	return strings.Join(parts, "; ")
}

func describeVars(vars map[string]string) string {
	parts := make([]string, 0, len(vars))
	for _, key := range sortedNames(vars) {
		value := vars[key]
		if r := []rune(value); len(r) > 80 {
			value = string(r[:80]) + "…"
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ", ")
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// ---- collections -----------------------------------------------------

func (s *Server) toolAPIListCollections(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:read", "request:list", meta("collections", nil), func(context.Context) (any, error) {
		return s.deps.API.Collections()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// collectionLabel names a collection in a prompt: its path from the top, so
// two folders called "Auth" in different collections read differently.
func collectionLabel(collections []apiclient.Collection, id string) string {
	if id == "" {
		return "the top level"
	}
	byID := map[string]apiclient.Collection{}
	for _, c := range collections {
		byID[c.ID] = c
	}
	var path []string
	for at, depth := id, 0; at != "" && depth < 32; depth++ {
		c, ok := byID[at]
		if !ok {
			break
		}
		path = append([]string{c.Name}, path...)
		at = c.ParentID
	}
	if len(path) == 0 {
		return id
	}
	return "\"" + strings.Join(path, " / ") + "\""
}

func findCollection(collections []apiclient.Collection, id string) (apiclient.Collection, bool) {
	for _, c := range collections {
		if c.ID == id {
			return c, true
		}
	}
	return apiclient.Collection{}, false
}

type apiCreateCollectionInput struct {
	Name     string            `json:"name"`
	ParentID string            `json:"parentId,omitempty" jsonschema:"a collection id to create a folder inside; empty for a top-level collection"`
	Vars     map[string]string `json:"vars,omitempty" jsonschema:"plain variables, e.g. baseUrl"`
	Secrets  map[string]string `json:"secrets,omitempty" jsonschema:"sealed variables; never returned by any tool"`
}

func (s *Server) toolAPICreateCollection(ctx context.Context, _ *sdk.CallToolRequest, in apiCreateCollectionInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", "request:*", meta("create_collection", nil), func(ctx context.Context) (any, error) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, fmt.Errorf("%w: a collection needs a name", apiclient.ErrInvalid)
		}
		collections, err := s.deps.API.Collections()
		if err != nil {
			return nil, err
		}
		if in.ParentID != "" {
			if _, ok := findCollection(collections, in.ParentID); !ok {
				return nil, fmt.Errorf("%w: no collection %q (api.listCollections)", apiclient.ErrNotFound, in.ParentID)
			}
		}
		summary := "Create collection \"" + in.Name + "\" in " + collectionLabel(collections, in.ParentID)
		if edit := (apiVarsEdit{SetVars: in.Vars, SetSecrets: in.Secrets}); !edit.empty() {
			summary += " — " + edit.describe()
		}
		// A new collection holds nothing yet, so its variables reach no
		// request the person made: once per session is enough.
		if err := s.confirmAPIWrite(ctx, "api.createCollection", "", summary, false); err != nil {
			return nil, err
		}
		created, err := s.deps.API.SaveCollection(apiclient.Collection{Name: in.Name, ParentID: in.ParentID, Vars: in.Vars})
		if err != nil {
			return nil, err
		}
		if len(in.Secrets) > 0 {
			return s.deps.API.SetCollectionVariables(created.ID, apiclient.VarsUpdate{Vars: in.Vars, Secrets: in.Secrets})
		}
		return created, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiUpdateCollectionInput struct {
	CollectionID  string            `json:"collectionId"`
	Name          *string           `json:"name,omitempty" jsonschema:"new name; omit to keep"`
	ParentID      *string           `json:"parentId,omitempty" jsonschema:"collection to move it into, \"\" for top level; omit to keep"`
	SetVars       map[string]string `json:"setVars,omitempty"`
	RemoveVars    []string          `json:"removeVars,omitempty"`
	SetSecrets    map[string]string `json:"setSecrets,omitempty"`
	RemoveSecrets []string          `json:"removeSecrets,omitempty"`
}

func (s *Server) toolAPIUpdateCollection(ctx context.Context, _ *sdk.CallToolRequest, in apiUpdateCollectionInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", "request:*", meta("update_collection", nil), func(ctx context.Context) (any, error) {
		collections, err := s.deps.API.Collections()
		if err != nil {
			return nil, err
		}
		current, ok := findCollection(collections, in.CollectionID)
		if !ok {
			return nil, fmt.Errorf("%w: no collection %q (api.listCollections)", apiclient.ErrNotFound, in.CollectionID)
		}
		next := current
		if in.Name != nil {
			next.Name = *in.Name
		}
		if in.ParentID != nil {
			next.ParentID = *in.ParentID
		}
		edit := apiVarsEdit{SetVars: in.SetVars, RemoveVars: in.RemoveVars, SetSecrets: in.SetSecrets, RemoveSecrets: in.RemoveSecrets}
		renamed := next.Name != current.Name
		moved := next.ParentID != current.ParentID
		if !renamed && !moved && edit.empty() {
			return current, nil
		}

		var changes []string
		if renamed {
			changes = append(changes, "rename to \""+next.Name+"\"")
		}
		if moved {
			changes = append(changes, "move to "+collectionLabel(collections, next.ParentID))
		}
		if !edit.empty() {
			changes = append(changes, edit.describe())
		}
		summary := "Change collection " + collectionLabel(collections, current.ID) + ": " + strings.Join(changes, "; ")
		// A rename changes nothing a request sends; a move or a variable
		// changes what every request inside resolves {{name}} to.
		if err := s.confirmAPIWrite(ctx, "api.updateCollection", "", summary, moved || !edit.empty()); err != nil {
			return nil, err
		}

		result := current
		if renamed || moved {
			if result, err = s.deps.API.SaveCollection(next); err != nil {
				return nil, err
			}
		}
		if !edit.empty() {
			return s.deps.API.SetCollectionVariables(current.ID, edit.update(current.Vars, current.SecretVars))
		}
		return result, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiCollectionIDInput struct {
	CollectionID string `json:"collectionId"`
}

func (s *Server) toolAPIDeleteCollection(ctx context.Context, _ *sdk.CallToolRequest, in apiCollectionIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", "request:*", meta("delete_collection", nil), func(ctx context.Context) (any, error) {
		collections, err := s.deps.API.Collections()
		if err != nil {
			return nil, err
		}
		if _, ok := findCollection(collections, in.CollectionID); !ok {
			return nil, fmt.Errorf("%w: no collection %q (api.listCollections)", apiclient.ErrNotFound, in.CollectionID)
		}
		summary := "Delete collection " + collectionLabel(collections, in.CollectionID)
		if err := s.confirmAPIWrite(ctx, "api.deleteCollection", "", summary, true); err != nil {
			return nil, err
		}
		if err := s.deps.API.DeleteCollection(in.CollectionID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": in.CollectionID}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// ---- requests --------------------------------------------------------

type apiSaveRequestInput struct {
	Request apiclient.RequestInput `json:"request"`
}

func (s *Server) toolAPISaveRequest(ctx context.Context, _ *sdk.CallToolRequest, in apiSaveRequestInput) (*sdk.CallToolResult, any, error) {
	scope := "request:new"
	if in.Request.ID != "" {
		scope = "request:" + in.Request.ID
	}
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", scope, meta("save", map[string]any{
		"method": in.Request.Method,
	}), func(ctx context.Context) (any, error) {
		req := in.Request.Request()
		if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.URL) == "" {
			return nil, fmt.Errorf("%w: a request needs a name and a url", apiclient.ErrInvalid)
		}
		collections, err := s.deps.API.Collections()
		if err != nil {
			return nil, err
		}
		if req.CollectionID != "" {
			if _, ok := findCollection(collections, req.CollectionID); !ok {
				return nil, fmt.Errorf("%w: no collection %q (api.listCollections)", apiclient.ErrNotFound, req.CollectionID)
			}
		}
		// Also what proves an id names a saved request and not some other
		// vault item an agent hopes to overwrite.
		req, err = s.deps.API.RestoreMasked(req)
		if err != nil {
			return nil, err
		}
		carries, err := s.deps.API.Carries(req)
		if err != nil {
			return nil, err
		}

		verb := "Save new request"
		if req.ID != "" {
			verb = "Replace saved request"
		}
		method := strings.ToUpper(strings.TrimSpace(req.Method))
		if method == "" {
			method = "GET"
		}
		summary := fmt.Sprintf("%s \"%s\" — %s %s, in %s", verb, req.Name, method, req.URL, collectionLabel(collections, req.CollectionID))
		if len(carries) > 0 {
			summary += ". It carries " + strings.Join(carries, ", ") +
				", which api.send will hand to this host without asking on a tag that does not ask first"
		}
		if err := s.confirmAPIWrite(ctx, "api.saveRequest", "", summary, len(carries) > 0); err != nil {
			return nil, err
		}
		saved, err := s.deps.API.SaveRequest(req)
		if err != nil {
			return nil, err
		}
		return saved.ForAgent(), nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

func (s *Server) toolAPIDeleteRequest(ctx context.Context, _ *sdk.CallToolRequest, in apiRequestIDInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", "request:"+in.RequestID, meta("delete", nil), func(ctx context.Context) (any, error) {
		req, err := s.deps.API.Request(in.RequestID)
		if err != nil {
			return nil, err
		}
		summary := fmt.Sprintf("Delete saved request \"%s\" — %s %s", req.Name, req.Method, req.URL)
		if err := s.confirmAPIWrite(ctx, "api.deleteRequest", "", summary, true); err != nil {
			return nil, err
		}
		if err := s.deps.API.DeleteRequest(in.RequestID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": in.RequestID}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

// ---- environments ----------------------------------------------------

// envTagSummary checks a tag exists and says in the prompt what it means for
// sending, which is the one thing about it the person is being asked.
func (s *Server) envTagSummary(tag string) (string, error) {
	if strings.TrimSpace(tag) == "" {
		return "", fmt.Errorf("%w: an environment needs a tag (env.listTags)", apiclient.ErrInvalid)
	}
	if s.deps.Envs == nil {
		return "tag " + tag, nil
	}
	tags, err := s.deps.Envs.ListTags()
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		if t.Name == tag {
			if s.deps.Envs.RequiresConfirmation(tag) {
				return "tag " + tag + " (asks before every send)", nil
			}
			return "tag " + tag + " (sends without asking)", nil
		}
		names = append(names, t.Name)
	}
	return "", fmt.Errorf("%w: no environment tag %q; tags: %s", apiclient.ErrInvalid, tag, strings.Join(names, ", "))
}

type apiCreateEnvironmentInput struct {
	Name    string            `json:"name"`
	Env     string            `json:"env" jsonschema:"environment tag from env.listTags, e.g. dev"`
	Vars    map[string]string `json:"vars,omitempty" jsonschema:"plain variables, e.g. baseUrl"`
	Secrets map[string]string `json:"secrets,omitempty" jsonschema:"sealed variables; never returned by any tool"`
}

func (s *Server) toolAPICreateEnvironment(ctx context.Context, _ *sdk.CallToolRequest, in apiCreateEnvironmentInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", "environment:new", meta("create_environment", map[string]any{
		"env": in.Env,
	}), func(ctx context.Context) (any, error) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, fmt.Errorf("%w: an environment needs a name", apiclient.ErrInvalid)
		}
		tag, err := s.envTagSummary(in.Env)
		if err != nil {
			return nil, err
		}
		summary := "Create environment \"" + in.Name + "\" on " + tag
		if edit := (apiVarsEdit{SetVars: in.Vars, SetSecrets: in.Secrets}); !edit.empty() {
			summary += " — " + edit.describe()
		}
		if err := s.confirmAPIWrite(ctx, "api.createEnvironment", in.Env, summary, true); err != nil {
			return nil, err
		}
		return s.deps.API.CreateEnvironment(in.Name, in.Env, in.Vars, in.Secrets)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}

type apiUpdateEnvironmentInput struct {
	EnvironmentID string            `json:"environmentId"`
	Name          *string           `json:"name,omitempty" jsonschema:"new name; omit to keep"`
	Env           *string           `json:"env,omitempty" jsonschema:"new environment tag; omit to keep"`
	SetVars       map[string]string `json:"setVars,omitempty"`
	RemoveVars    []string          `json:"removeVars,omitempty"`
	SetSecrets    map[string]string `json:"setSecrets,omitempty"`
	RemoveSecrets []string          `json:"removeSecrets,omitempty"`
}

func (s *Server) toolAPIUpdateEnvironment(ctx context.Context, _ *sdk.CallToolRequest, in apiUpdateEnvironmentInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.apiclient", "api:write", "environment:"+in.EnvironmentID, meta("update_environment", nil), func(ctx context.Context) (any, error) {
		environments, err := s.deps.API.Environments()
		if err != nil {
			return nil, err
		}
		var current apiclient.Environment
		found := false
		for _, env := range environments {
			if env.ID == in.EnvironmentID {
				current, found = env, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: no environment %q (api.listEnvironments)", apiclient.ErrNotFound, in.EnvironmentID)
		}
		name, tag := current.Name, current.Env
		if in.Name != nil {
			name = *in.Name
		}
		if in.Env != nil {
			tag = *in.Env
		}
		edit := apiVarsEdit{SetVars: in.SetVars, RemoveVars: in.RemoveVars, SetSecrets: in.SetSecrets, RemoveSecrets: in.RemoveSecrets}
		if name == current.Name && tag == current.Env && edit.empty() {
			return current, nil
		}
		tagText, err := s.envTagSummary(tag)
		if err != nil {
			return nil, err
		}

		var changes []string
		if name != current.Name {
			changes = append(changes, "rename to \""+name+"\"")
		}
		if tag != current.Env {
			changes = append(changes, "move from tag "+current.Env+" to "+tagText)
		}
		if !edit.empty() {
			changes = append(changes, edit.describe())
		}
		summary := "Change environment \"" + current.Name + "\" (tag " + current.Env + "): " + strings.Join(changes, "; ")
		if err := s.confirmAPIWrite(ctx, "api.updateEnvironment", tag, summary, true); err != nil {
			return nil, err
		}
		return s.deps.API.UpdateEnvironment(current.ID, name, tag, edit.update(current.Vars, current.SecretVars))
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, structured(v), nil
}
