package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"devkit/internal/vaultitem"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// A request saved when query and headers were maps opens as the same request.
func TestKVListReadsTheMapItReplaced(t *testing.T) {
	var payload requestPayload
	if err := json.Unmarshal([]byte(`{"query":{"b":"2","a":"1"},"headers":{"X-Tenant":"t"},"body":"{\"x\":1}","auth":{"kind":"","accountId":""}}`), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Query) != 2 || payload.Query[0].Key != "a" || payload.Query[1].Value != "2" || payload.Query[0].Disabled {
		t.Fatalf("query = %+v", payload.Query)
	}
	req := Request{Method: "post", Body: payload.Body}.normalized()
	if req.BodyMode != BodyJSON || req.Method != "POST" {
		t.Fatalf("legacy body read as %q, method %q", req.BodyMode, req.Method)
	}
}

type captured struct {
	method, path, rawQuery, contentType, body string
	headers                                   http.Header
}

func captureServer(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = captured{
			method: r.Method, path: r.URL.EscapedPath(), rawQuery: r.URL.RawQuery,
			contentType: r.Header.Get("Content-Type"), body: string(body), headers: r.Header.Clone(),
		}
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(200)
	}))
	t.Cleanup(server.Close)
	return server, got
}

// What a person types in the editor is what reaches the server: rows in their
// order, duplicates kept, disabled rows left out, :path variables filled, and
// a JSON body labelled as JSON.
func TestSendBuildsWhatTheEditorHolds(t *testing.T) {
	server, got := captureServer(t)
	svc := newService(t)
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"baseUrl": server.URL, "id": "env-id"}, nil)

	resp, err := svc.Send(context.Background(), SendInput{EnvironmentID: env.ID, Request: Request{
		Method: "post", URL: "{{baseUrl}}/refunds/:refundId/items?fixed=1",
		PathVars: KVList{{Key: "refundId", Value: "r 7"}},
		Query: KVList{
			{Key: "ids", Value: "1"}, {Key: "ids", Value: "2"},
			{Key: "off", Value: "x", Disabled: true}, {Key: "", Value: "blank row"},
		},
		Headers:  KVList{{Key: "X-Trace", Value: "a"}, {Key: "X-Trace", Value: "b"}},
		BodyMode: BodyJSON, Body: `{"id":"{{id}}"}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/refunds/r%207/items" {
		t.Fatalf("reached %s %s", got.method, got.path)
	}
	if got.rawQuery != "fixed=1&ids=1&ids=2" {
		t.Fatalf("query = %q", got.rawQuery)
	}
	if got.contentType != "application/json" || got.body != `{"id":"env-id"}` {
		t.Fatalf("body = %q as %q", got.body, got.contentType)
	}
	if v := got.headers.Values("X-Trace"); len(v) != 2 {
		t.Fatalf("X-Trace = %v", v)
	}
	if got.headers.Get("User-Agent") != userAgent {
		t.Fatalf("user agent = %q", got.headers.Get("User-Agent"))
	}
	cookies := 0
	for _, row := range resp.HeaderList {
		if row.Key == "Set-Cookie" {
			cookies++
		}
	}
	if cookies != 2 {
		t.Fatalf("header list kept %d Set-Cookie lines, want 2", cookies)
	}
}

func TestFormBodies(t *testing.T) {
	server, got := captureServer(t)
	svc := newService(t)
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"baseUrl": server.URL}, nil)

	form := KVList{{Key: "name", Value: "a b"}, {Key: "skip", Value: "x", Disabled: true}, {Key: "tag", Value: "1"}}
	if _, err := svc.Send(context.Background(), SendInput{EnvironmentID: env.ID, Request: Request{
		Method: "POST", URL: "{{baseUrl}}/f", BodyMode: BodyURLEncoded, Form: form,
	}}); err != nil {
		t.Fatal(err)
	}
	if got.contentType != "application/x-www-form-urlencoded" || got.body != "name=a+b&tag=1" {
		t.Fatalf("urlencoded = %q as %q", got.body, got.contentType)
	}

	if _, err := svc.Send(context.Background(), SendInput{EnvironmentID: env.ID, Request: Request{
		Method: "POST", URL: "{{baseUrl}}/f", BodyMode: BodyMultipart, Form: form,
		// A Content-Type typed by hand cannot know the boundary, so it loses.
		Headers: KVList{{Key: "Content-Type", Value: "multipart/form-data"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.contentType, "multipart/form-data; boundary=") ||
		!strings.Contains(got.body, `name="name"`) || strings.Contains(got.body, `name="skip"`) {
		t.Fatalf("multipart = %q as %q", got.body, got.contentType)
	}
}

// An address typed without a scheme gets one: http for this machine.
func TestAnAddressWithoutAScheme(t *testing.T) {
	cases := map[string]string{
		"localhost:8080/users": "http://localhost:8080/users",
		"127.0.0.1/x":          "http://127.0.0.1/x",
		"api.example.com/v1":   "https://api.example.com/v1",
		"{{baseUrl}}/v1":       "{{baseUrl}}/v1",
		"https://a.example":    "https://a.example",
	}
	for in, want := range cases {
		if got := withScheme(in); got != want {
			t.Errorf("withScheme(%q) = %q, want %q", in, got, want)
		}
	}
	if names := PathVarNames("{{baseUrl}}/users/:id/repos/:repo?x=:no"); strings.Join(names, ",") != "id,repo" {
		t.Fatalf("PathVarNames = %v", names)
	}
}

// Copy as cURL is pasted into a shell: single quotes keep a newline a newline
// and a `$` a dollar, and the query is part of the address.
func TestCurlIsAValidShellCommand(t *testing.T) {
	got := CurlFor(Request{
		Method: "post", URL: "https://x.dev/a/:id",
		PathVars: KVList{{Key: "id", Value: "7"}},
		Query:    KVList{{Key: "page", Value: "2"}},
		Body:     "{\n  \"price\": \"$5\", \"it's\": 1\n}",
		BodyMode: BodyJSON,
	}, nil)
	for _, want := range []string{
		`'https://x.dev/a/7?page=2'`,
		`-H 'Content-Type: application/json'`,
		"--data-raw '{\n  \"price\": \"$5\", \"it'\\''s\": 1\n}'",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("curl is missing %s:\n%s", want, got)
		}
	}
}

// Variables layer the way Postman's do — globals, then the collection from its
// root down, then the environment — and a secret is named but never shown.
func TestVariableScopesAndPrecedence(t *testing.T) {
	server, got := captureServer(t)
	svc := newService(t)
	if _, err := svc.SetGlobals(VarsUpdate{
		Vars:    map[string]string{"baseUrl": "https://wrong.example", "who": "global", "only": "g"},
		Secrets: map[string]string{"token": "global-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	root, _ := svc.SaveCollection(Collection{Name: "Shop"})
	if _, err := svc.SetCollectionVariables(root.ID, VarsUpdate{Vars: map[string]string{"who": "root", "baseUrl": server.URL}}); err != nil {
		t.Fatal(err)
	}
	leaf, _ := svc.SaveCollection(Collection{Name: "Orders", ParentID: root.ID})
	if _, err := svc.SetCollectionVariables(leaf.ID, VarsUpdate{Vars: map[string]string{"who": "leaf"}}); err != nil {
		t.Fatal(err)
	}
	// A rename keeps the folder's variables.
	if _, err := svc.SaveCollection(Collection{ID: leaf.ID, Name: "Orders v2", ParentID: root.ID}); err != nil {
		t.Fatal(err)
	}
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"env": "dev"}, nil)

	if _, err := svc.Send(context.Background(), SendInput{EnvironmentID: env.ID, Request: Request{
		Method: "GET", URL: "{{baseUrl}}/x", CollectionID: leaf.ID,
		Query: KVList{{Key: "who", Value: "{{who}}"}, {Key: "only", Value: "{{only}}"}, {Key: "env", Value: "{{env}}"}, {Key: "t", Value: "{{token}}"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if got.rawQuery != "who=leaf&only=g&env=dev&t=global-secret" {
		t.Fatalf("query = %q", got.rawQuery)
	}

	resolved, err := svc.VariablesFor(leaf.ID, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range resolved {
		if v.Name == "token" && (v.Value != "" || !v.Secret || v.Scope != ScopeGlobal) {
			t.Fatalf("token resolved as %+v", v)
		}
		if v.Name == "who" && (v.Value != "leaf" || v.ScopeName != "Orders v2") {
			t.Fatalf("who resolved as %+v", v)
		}
	}

	// Keeping a secret by name leaves its value; dropping it removes it.
	if _, err := svc.SetGlobals(VarsUpdate{KeepSecrets: []string{"token"}}); err != nil {
		t.Fatal(err)
	}
	if g, _ := svc.Globals(); len(g.SecretVars) != 1 || len(g.Vars) != 0 {
		t.Fatalf("globals after keep = %+v", g)
	}
	if _, err := svc.SetGlobals(VarsUpdate{}); err != nil {
		t.Fatal(err)
	}
	if g, _ := svc.Globals(); len(g.SecretVars) != 0 {
		t.Fatalf("globals after drop = %+v", g)
	}
}

func TestEnvironmentEditKeepsSecretsItWasToldTo(t *testing.T) {
	svc := newService(t)
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"a": "1"}, map[string]string{"keep": "k", "drop": "d"})
	updated, err := svc.UpdateEnvironment(env.ID, "Staging", "prod", VarsUpdate{
		Vars: map[string]string{"b": "2"}, KeepSecrets: []string{"keep"}, Secrets: map[string]string{"new": "n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Env != "prod" || strings.Join(updated.SecretVars, ",") != "keep,new" || updated.Vars["b"] != "2" || updated.Vars["a"] != "" {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := svc.UpdateEnvironment(env.ID, "x", "nope", VarsUpdate{}); err == nil {
		t.Fatal("an unknown tag was accepted on edit")
	}
	copyEnv, err := svc.DuplicateEnvironment(env.ID, "")
	if err != nil || copyEnv.Name != "Staging copy" || len(copyEnv.SecretVars) != 2 {
		t.Fatalf("duplicate = %+v, %v", copyEnv, err)
	}
	if err := svc.DeleteEnvironment(env.ID); err != nil {
		t.Fatal(err)
	}
	if envs, _ := svc.Environments(); len(envs) != 1 {
		t.Fatalf("environments after delete = %d", len(envs))
	}
}

func TestDeleteCollectionTreeTakesEverythingInside(t *testing.T) {
	svc := newService(t)
	root, _ := svc.SaveCollection(Collection{Name: "Root"})
	child, _ := svc.SaveCollection(Collection{Name: "Child", ParentID: root.ID})
	other, _ := svc.SaveCollection(Collection{Name: "Other"})
	for _, collection := range []string{root.ID, child.ID, other.ID} {
		if _, err := svc.SaveRequest(Request{Name: "r", Method: "GET", URL: "https://x.test", CollectionID: collection}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := svc.DeleteCollectionTree(root.ID)
	if err != nil || removed != 2 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	collections, _ := svc.Collections()
	requests, _ := svc.Requests()
	if len(collections) != 1 || collections[0].ID != other.ID || len(requests) != 1 {
		t.Fatalf("left %d collections, %d requests", len(collections), len(requests))
	}
}

// What an agent reads of a request names where a credential lives and never
// holds one somebody typed in.
func TestAgentsSeeCredentialRowsMasked(t *testing.T) {
	req := Request{
		Headers: KVList{
			{Key: "Authorization", Value: "Bearer ghp_live"},
			{Key: "X-Api-Key", Value: "{{apiKey}}"},
			{Key: "Accept", Value: "application/json"},
		},
		Query: KVList{{Key: "access_token", Value: "abc"}, {Key: "page", Value: "2"}},
	}.ForAgent()
	if req.Headers[0].Value != Masked || req.Headers[1].Value != "{{apiKey}}" || req.Headers[2].Value != "application/json" {
		t.Fatalf("headers = %+v", req.Headers)
	}
	if req.Query[0].Value != Masked || req.Query[1].Value != "2" {
		t.Fatalf("query = %+v", req.Query)
	}
	resp := Response{Headers: map[string]string{"Set-Cookie": "sid=1"}, HeaderList: KVList{{Key: "Set-Cookie", Value: "sid=1"}}}.ForAgent()
	if resp.Headers["Set-Cookie"] != Masked || resp.HeaderList[0].Value != Masked {
		t.Fatalf("response = %+v", resp)
	}
}

// A collection keeps the metadata shape every DevKit already validates — its
// variables live in the payload — and one sealed before variables existed
// reads as having none.
func TestCollectionVariablesStayOutOfMetadata(t *testing.T) {
	svc := newService(t)
	legacy, err := svc.items.Create("api-collection", KeyIDCollection, []byte(`{"name":"Old","order":0}`), []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCollectionVariables(legacy.ID, VarsUpdate{Vars: map[string]string{"a": "1"}, Secrets: map[string]string{"s": "2"}}); err != nil {
		t.Fatal(err)
	}
	record, err := svc.items.Get(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(record.Metadata), "vars") || strings.Contains(string(record.Metadata), `"a"`) {
		t.Fatalf("variables leaked into metadata: %s", record.Metadata)
	}
	if err := vaultitem.ValidateRecordSchema(record.Type, record.Metadata, record.Payload); err != nil {
		t.Fatalf("a collection with variables no longer validates: %v", err)
	}
	collections, _ := svc.Collections()
	if len(collections) != 1 || collections[0].Vars["a"] != "1" || strings.Join(collections[0].SecretVars, ",") != "s" {
		t.Fatalf("collections = %+v", collections)
	}
}

// An agent picks the address of an ad-hoc request, so carrying a secret or an
// account there is asked about even on an environment that never asks.
func TestAgentAdhocWithASecretAlwaysAsks(t *testing.T) {
	server, _ := captureServer(t)
	svc := newService(t)
	svc.SetCredentials(&stubCreds{secret: "tok"})
	confirm := &stubConfirm{allow: false}
	svc.SetConfirmer(confirm)
	env, _ := svc.CreateEnvironment("Dev", "dev", map[string]string{"baseUrl": server.URL}, map[string]string{"token": "s3cr3t"})

	plain := Request{Method: "GET", URL: "{{baseUrl}}/x"}
	if _, err := svc.Send(context.Background(), SendInput{Request: plain, EnvironmentID: env.ID, Adhoc: true, ForAgent: true}); err != nil {
		t.Fatalf("a plain ad-hoc send on dev = %v", err)
	}
	if confirm.asked != 0 {
		t.Fatal("a plain ad-hoc send on dev asked")
	}
	for _, req := range []Request{
		{Method: "GET", URL: "{{baseUrl}}/x?t={{token}}"},
		{Method: "GET", URL: "{{baseUrl}}/x", Auth: Auth{Kind: AuthBearer, AccountID: "acct"}},
	} {
		if _, err := svc.Send(context.Background(), SendInput{Request: req, EnvironmentID: env.ID, Adhoc: true}); !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("a denied ad-hoc send carrying a secret = %v", err)
		}
	}
	if confirm.asked != 2 {
		t.Fatalf("asked %d times, want 2", confirm.asked)
	}
	// The same request saved by a person keeps the tag's rule.
	if _, err := svc.Send(context.Background(), SendInput{Request: Request{Method: "GET", URL: "{{baseUrl}}/x?t={{token}}"}, EnvironmentID: env.ID}); err != nil {
		t.Fatal(err)
	}
	if confirm.asked != 2 {
		t.Fatal("a person's own send on dev asked")
	}
}
