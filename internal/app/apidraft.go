package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"devkit/internal/apiclient"
	"devkit/internal/codegraph"
)

// DraftFromEndpoint builds a request from an endpoint in the open index.
//
// It lives here rather than in apiclient because apiclient must not import
// codegraph — it declares what it needs from the world as interfaces, and
// "read the index" is not one of them. The composition root is where the two
// are allowed to meet.
func DraftFromEndpoint(ctx context.Context, graph *codegraph.Service, endpointID string) (apiclient.EndpointDraft, error) {
	if graph == nil || endpointID == "" {
		return apiclient.EndpointDraft{}, apiclient.ErrNotFound
	}
	entries, err := graph.ListEntryPoints(ctx)
	if err != nil {
		return apiclient.EndpointDraft{}, err
	}
	var endpoint codegraph.EntryPoint
	for _, candidate := range entries {
		if candidate.ID == endpointID {
			endpoint = candidate
			break
		}
	}
	// Only an HTTP route is a request the API Client can send. A Kong phase,
	// a message pattern or a cron job has no URL, and a draft for one would
	// be "ACCESS {{baseUrl}}rate-limiting".
	if endpoint.ID == "" || endpoint.Kind != codegraph.EntryKindHTTP {
		return apiclient.EndpointDraft{}, apiclient.ErrNotFound
	}

	// The flow's first step is the handler, which is where the parameters are.
	// A flow that cannot be walked still leaves method and path, and those are
	// most of what Try in API Client is for.
	var params []codegraph.Param
	if flow, flowErr := graph.GetApiFlow(ctx, endpointID); flowErr == nil && len(flow.Steps) > 0 {
		params = flow.Steps[0].Params
	}
	return draftFor(endpoint, params), nil
}

func draftFor(endpoint codegraph.EntryPoint, params []codegraph.Param) apiclient.EndpointDraft {
	method := strings.ToUpper(endpoint.Verb)
	if method == "" {
		method = "GET"
	}
	draft := apiclient.EndpointDraft{Request: apiclient.Request{
		Name:      method + " " + endpoint.Target,
		Method:    method,
		URL:       "{{baseUrl}}" + pathTemplate(endpoint.Target),
		OriginRef: endpoint.ID,
	}}

	var body *codegraph.Param
	typed := false
	for i := range params {
		param := params[i]
		if param.Source != "" {
			typed = true
		}
		switch param.Source {
		case codegraph.ParamSourceQuery:
			if draft.Query == nil {
				draft.Query = map[string]string{}
			}
			// The value is the variable, not a guess at what belongs there:
			// an environment fills it, and an empty string in the URL would
			// send a parameter the caller never meant to send.
			draft.Query[param.Wire()] = "{{" + param.Wire() + "}}"
		case codegraph.ParamSourceHeader:
			if draft.Headers == nil {
				draft.Headers = map[string]string{}
			}
			draft.Headers[param.Wire()] = "{{" + param.Wire() + "}}"
		case codegraph.ParamSourceBody:
			if body == nil {
				body = &params[i]
			}
		}
	}

	switch {
	case body == nil && !typed:
		draft.Note = apiclient.NoteNoDeclaredTypes
	case body == nil:
		draft.Note = apiclient.NoteNoBodyDeclared
	case len(body.Fields) == 0:
		draft.Note = apiclient.NoteBodyNotReadable
	default:
		draft.Body = bodySkeleton(*body)
		draft.BodyInferred = true
	}
	return draft
}

// pathTemplate turns a route's own placeholders into the API client's, so an
// environment fills them: /refunds/{id} and /refunds/:id both become
// /refunds/{{id}}.
func pathTemplate(path string) string {
	var out strings.Builder
	for i := 0; i < len(path); {
		switch {
		case path[i] == '{':
			end := strings.IndexByte(path[i:], '}')
			if end < 0 {
				out.WriteString(path[i:])
				return out.String()
			}
			name := strings.TrimSpace(path[i+1 : i+end])
			// Spring allows a regex after a colon: {id:[0-9]+}.
			if colon := strings.IndexByte(name, ':'); colon >= 0 {
				name = strings.TrimSpace(name[:colon])
			}
			out.WriteString("{{" + name + "}}")
			i += end + 1
		case path[i] == ':' && i+1 < len(path) && isNameStart(path[i+1]):
			// A parameter name has to START with a letter or an underscore,
			// in Express and in Lapis alike. Without that rule a timestamp in
			// a literal path — /time/12:30 — reads as a parameter called 30.
			end := i + 1
			for end < len(path) && (isNameStart(path[end]) || path[end] == '-' ||
				(path[end] >= '0' && path[end] <= '9')) {
				end++
			}
			out.WriteString("{{" + path[i+1:end] + "}}")
			i = end
		default:
			out.WriteByte(path[i])
			i++
		}
	}
	return out.String()
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// bodySkeleton is the DTO's fields with an empty value of the right shape.
//
// One level deep: a nested type is {} rather than a guess, because what it
// serializes to depends on annotations and a serializer configuration this
// analyzer does not read. The known ceiling says so, and showing {} is how it
// says so to the person.
func bodySkeleton(param codegraph.Param) string {
	fields := make([]string, 0, len(param.Fields))
	for _, field := range param.Fields {
		name, _ := json.Marshal(field.Name)
		fields = append(fields, fmt.Sprintf("  %s: %s", name, zeroFor(field.Type)))
	}
	// Declaration order, which is the order a reader of the DTO expects.
	return "{\n" + strings.Join(fields, ",\n") + "\n}"
}

func zeroFor(typeName string) string {
	name := strings.TrimSpace(typeName)
	name = strings.TrimSuffix(name, "?")
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(name, "[]"), strings.HasPrefix(lower, "list<"),
		strings.HasPrefix(lower, "set<"), strings.HasPrefix(lower, "collection<"),
		strings.HasPrefix(lower, "array<"), strings.HasPrefix(lower, "iterable<"):
		return "[]"
	}
	switch lower {
	case "string", "char", "character", "uuid", "localdate", "localdatetime",
		"instant", "offsetdatetime", "zoneddatetime", "date":
		return `""`
	case "int", "integer", "long", "short", "byte", "float", "double",
		"bigdecimal", "biginteger", "number":
		return "0"
	case "bool", "boolean":
		return "false"
	default:
		// A nested DTO, an enum, a map — anything whose shape depends on a
		// serializer this does not read.
		return "{}"
	}
}

// APIWithDrafts is the API client plus "draft one from a Code Graph endpoint".
//
// It exists so internal/mcp can offer api.fromEndpoint without reaching for
// the index itself, and without internal/apiclient importing codegraph. The
// composition root is where the two are allowed to meet, and this is the
// smallest shape that says so.
type APIWithDrafts struct {
	*apiclient.Service
	graph *codegraph.Service
}

func NewAPIWithDrafts(api *apiclient.Service, graph *codegraph.Service) *APIWithDrafts {
	return &APIWithDrafts{Service: api, graph: graph}
}

func (a *APIWithDrafts) DraftFromEndpoint(ctx context.Context, endpointID string) (apiclient.EndpointDraft, error) {
	return DraftFromEndpoint(ctx, a.graph, endpointID)
}
