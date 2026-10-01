package app

import (
	"encoding/json"
	"strings"
	"testing"

	"devkit/internal/apiclient"
	"devkit/internal/codegraph"
)

func TestDraftPutsEachParameterWhereItBelongs(t *testing.T) {
	draft := draftFor(
		codegraph.EntryPoint{ID: "e1", Verb: "post", Target: "/refunds/{id}"},
		[]codegraph.Param{
			{Name: "id", Type: "String", Source: codegraph.ParamSourcePath},
			{Name: "pageSize", Type: "int", Source: codegraph.ParamSourceQuery, WireName: "page_size"},
			{Name: "tenant", Type: "String", Source: codegraph.ParamSourceHeader, WireName: "X-Tenant"},
			{Name: "request", Type: "HttpServletRequest", Source: codegraph.ParamSourceOther},
			{Name: "body", Type: "RefundRequest", Source: codegraph.ParamSourceBody, Fields: []codegraph.Field{
				{Name: "reference", Type: "String"},
				{Name: "amount", Type: "BigDecimal"},
				{Name: "lines", Type: "List<RefundLine>"},
				{Name: "urgent", Type: "boolean"},
				{Name: "customer", Type: "Customer"},
			}},
		},
	)

	if draft.Method != "POST" {
		t.Fatalf("method = %q", draft.Method)
	}
	if draft.URL != "{{baseUrl}}/refunds/:id" {
		t.Fatalf("url = %q", draft.URL)
	}
	if len(draft.PathVars) != 1 || draft.PathVars[0].Key != "id" || draft.PathVars[0].Value != "" {
		t.Fatalf("path vars = %+v", draft.PathVars)
	}
	if draft.BodyMode != apiclient.BodyJSON {
		t.Fatalf("body mode = %q", draft.BodyMode)
	}
	// The wire name, not the parameter's, and the value is the variable: an
	// empty string here would send a parameter nobody meant to send.
	if v, _ := draft.Query.Lookup("page_size", false); v != "{{page_size}}" {
		t.Fatalf("query = %v", draft.Query)
	}
	if v, _ := draft.Headers.Lookup("X-Tenant", true); v != "{{X-Tenant}}" {
		t.Fatalf("headers = %v", draft.Headers)
	}
	// An unannotated parameter is the servlet request, not a value to send.
	if len(draft.Query) != 1 || len(draft.Headers) != 1 {
		t.Fatalf("query=%v headers=%v", draft.Query, draft.Headers)
	}
	if draft.OriginRef != "e1" {
		t.Fatalf("originRef = %q", draft.OriginRef)
	}

	if !draft.BodyInferred || draft.Note != "" {
		t.Fatalf("body was read from the code: %+v", draft)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(draft.Body), &body); err != nil {
		t.Fatalf("the skeleton is not valid JSON: %v\n%s", err, draft.Body)
	}
	if body["reference"] != "" || body["amount"] != float64(0) || body["urgent"] != false {
		t.Fatalf("skeleton = %v", body)
	}
	if _, ok := body["lines"].([]any); !ok {
		t.Fatalf("a collection should be an empty array: %v", body["lines"])
	}
	// One level deep: a nested type is {} rather than a guess about what a
	// serializer this analyzer does not read would produce.
	if nested, ok := body["customer"].(map[string]any); !ok || len(nested) != 0 {
		t.Fatalf("a nested DTO should be an empty object: %v", body["customer"])
	}
}

// Three different reasons there is no body, and the draft says which. An empty
// {} for all of them would read as "the DTO is empty", which is the one thing
// none of them means.
func TestDraftSaysWhyThereIsNoBody(t *testing.T) {
	noTypes := draftFor(codegraph.EntryPoint{ID: "e", Verb: "GET", Target: "/x"},
		[]codegraph.Param{{Name: "conf"}, {Name: "ctx"}})
	if noTypes.BodyInferred || noTypes.Body != "" || noTypes.Note != apiclient.NoteNoDeclaredTypes {
		t.Fatalf("a language with no declared types: %+v", noTypes)
	}

	declaredNone := draftFor(codegraph.EntryPoint{ID: "e", Verb: "GET", Target: "/x"},
		[]codegraph.Param{{Name: "id", Source: codegraph.ParamSourcePath}})
	if declaredNone.Note != apiclient.NoteNoBodyDeclared {
		t.Fatalf("a handler with no body: %+v", declaredNone)
	}

	unreadable := draftFor(codegraph.EntryPoint{ID: "e", Verb: "POST", Target: "/x"},
		[]codegraph.Param{{Name: "charge", Type: "com.stripe.Charge", Source: codegraph.ParamSourceBody}})
	if unreadable.BodyInferred || unreadable.Body != "" || unreadable.Note != apiclient.NoteBodyNotReadable {
		t.Fatalf("a body type outside the project: %+v", unreadable)
	}
}

// A Lua endpoint reaches here with no typed parameters at all, and method and
// path are still most of what Try in API Client is for.
func TestDraftFromAnUntypedEndpointStillHasMethodAndPath(t *testing.T) {
	draft := draftFor(codegraph.EntryPoint{
		ID: "ACCESS · rate-limit", Verb: "", Target: "/consumers/:username/plugins",
	}, nil)
	if draft.Method != "GET" {
		t.Fatalf("an endpoint with no method should draft as GET: %q", draft.Method)
	}
	if draft.URL != "{{baseUrl}}/consumers/:username/plugins" {
		t.Fatalf("url = %q", draft.URL)
	}
	if !strings.Contains(draft.Note, "does not declare parameter types") {
		t.Fatalf("note = %q", draft.Note)
	}
}

func TestPathTemplateHandlesBothRouteSyntaxes(t *testing.T) {
	for path, want := range map[string]string{
		"/refunds":                     "/refunds",
		"/refunds/{id}":                "/refunds/:id",
		"/refunds/{id:[0-9]+}":         "/refunds/:id",
		"/users/:userId/posts/:postId": "/users/:userId/posts/:postId",
		// Part of a segment is not a path variable; the environment fills it.
		"/files/{name}.json": "/files/{{name}}.json",
		// A colon that is not a parameter is left alone.
		"/a:b":        "/a{{b}}",
		"/time/12:30": "/time/12:30",
		// An unclosed brace is kept as written rather than swallowed.
		"/refunds/{id": "/refunds/{id",
	} {
		if got := pathTemplate(path); got != want {
			t.Errorf("pathTemplate(%q) = %q, want %q", path, got, want)
		}
	}
}
