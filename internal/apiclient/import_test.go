package apiclient

import (
	"strings"
	"testing"
)

func TestParseCurlFromABrowser(t *testing.T) {
	// What Chrome's "Copy as cURL (bash)" writes: $'…' for a body with a
	// newline, line continuations, and switches that are curl's business.
	in, err := ParseCurl(`curl 'https://api.example.com/v1/orders?page=2&q=a%20b' \
  -H 'accept: application/json' \
  -H 'content-type: application/json' \
  -b 'sid=abc' \
  --data-raw $'{"note":"line1\nline2","it\'s":1}' \
  --compressed -sSL`)
	if err != nil {
		t.Fatal(err)
	}
	if in.Method != "POST" || in.URL != "https://api.example.com/v1/orders" {
		t.Fatalf("%s %s", in.Method, in.URL)
	}
	if len(in.Query) != 2 || in.Query[1].Value != "a b" {
		t.Fatalf("query = %+v", in.Query)
	}
	if in.BodyMode != BodyJSON || in.Body != "{\"note\":\"line1\nline2\",\"it's\":1}" {
		t.Fatalf("body = %q (%s)", in.Body, in.BodyMode)
	}
	if v, _ := in.Headers.Lookup("cookie", true); v != "sid=abc" {
		t.Fatalf("headers = %+v", in.Headers)
	}
}

func TestParseCurlVariants(t *testing.T) {
	form, err := ParseCurl(`curl -XPUT "http://localhost:8080/users/7" -d "name=Ann&role=admin" -u bob:pw`)
	if err != nil {
		t.Fatal(err)
	}
	if form.Method != "PUT" || form.BodyMode != BodyURLEncoded || len(form.Form) != 2 || form.Form[0].Value != "Ann" {
		t.Fatalf("form = %+v", form)
	}
	if v, _ := form.Headers.Lookup("Authorization", true); v != "Basic Ym9iOnB3" {
		t.Fatalf("basic = %q", v)
	}

	multi, err := ParseCurl(`curl https://x.test/upload -F 'title=Hi' -F 'tag=a' -H 'Content-Type: multipart/form-data; boundary=zzz'`)
	if err != nil || multi.BodyMode != BodyMultipart || len(multi.Form) != 2 || len(multi.Headers) != 0 {
		t.Fatalf("multipart = %+v, %v", multi, err)
	}

	get, err := ParseCurl(`curl -G https://x.test/search --data-urlencode 'q=hello world'`)
	if err != nil || get.Method != "GET" || len(get.Query) != 1 || get.Query[0].Value != "hello world" {
		t.Fatalf("-G = %+v, %v", get, err)
	}

	if _, err := ParseCurl(`wget https://x.test`); err == nil {
		t.Fatal("wget read as curl")
	}
	if _, err := ParseCurl(`curl 'unterminated`); err == nil {
		t.Fatal("an unterminated quote was accepted")
	}
}

const postmanCollection = `{
  "info": {"name": "Shop API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{token}}", "type": "string"}]},
  "variable": [{"key": "baseUrl", "value": "https://shop.test"}, {"key": "off", "value": "x", "disabled": true}],
  "event": [{"listen": "prerequest"}],
  "item": [
    {"name": "Orders", "item": [
      {"name": "Get order", "request": {"method": "GET",
        "header": [{"key": "Accept", "value": "application/json"}, {"key": "X-Off", "value": "1", "disabled": true}],
        "url": {"raw": "{{baseUrl}}/orders/:id?expand=items", "query": [{"key": "expand", "value": "items"}],
                "variable": [{"key": "id", "value": "42"}]}}},
      {"name": "Create order", "request": {"method": "POST", "auth": {"type": "noauth"},
        "url": "{{baseUrl}}/orders",
        "body": {"mode": "raw", "raw": "{\"sku\":\"a\"}", "options": {"raw": {"language": "json"}}}}},
      {"name": "Upload", "request": {"method": "POST", "url": "{{baseUrl}}/files",
        "body": {"mode": "formdata", "formdata": [{"key": "title", "value": "t", "type": "text"}, {"key": "f", "src": "/tmp/x", "type": "file"}]}}}
    ]},
    {"name": "Ping", "request": "https://shop.test/ping"}
  ]
}`

func TestImportPostmanCollection(t *testing.T) {
	svc := newService(t)
	result, err := svc.Import("", postmanCollection, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Folders != 1 || result.Requests != 4 || result.Variables != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %v", result.Warnings)
	}

	collections, _ := svc.Collections()
	var root Collection
	for _, c := range collections {
		if c.ID == result.CollectionID {
			root = c
		}
	}
	if root.Vars["baseUrl"] != "https://shop.test" || root.Vars["off"] != "" {
		t.Fatalf("collection vars = %+v", root.Vars)
	}

	requests, _ := svc.Requests()
	byName := map[string]Request{}
	for _, summary := range requests {
		full, err := svc.Request(summary.ID)
		if err != nil {
			t.Fatal(err)
		}
		byName[full.Name] = full
	}
	get := byName["Get order"]
	if get.URL != "{{baseUrl}}/orders/:id" || len(get.Query) != 1 || get.PathVars[0].Value != "42" {
		t.Fatalf("get = %+v", get)
	}
	if v, _ := get.Headers.Lookup("Authorization", true); v != "Bearer {{token}}" {
		t.Fatalf("inherited bearer = %q", v)
	}
	if !get.Headers[1].Disabled {
		t.Fatalf("a disabled header came back enabled: %+v", get.Headers)
	}
	create := byName["Create order"]
	if create.BodyMode != BodyJSON || create.Body != `{"sku":"a"}` {
		t.Fatalf("create = %+v", create)
	}
	if _, ok := create.Headers.Lookup("Authorization", true); ok {
		t.Fatal("noauth still carried the inherited token")
	}
	if upload := byName["Upload"]; upload.BodyMode != BodyMultipart || len(upload.Form) != 1 {
		t.Fatalf("upload = %+v", upload)
	}
	if ping := byName["Ping"]; ping.URL != "https://shop.test/ping" || ping.CollectionID != result.CollectionID {
		t.Fatalf("ping = %+v", ping)
	}

	if _, err := svc.Import("", `{"hello": "world"}`, ImportOptions{}); err == nil {
		t.Fatal("a non-collection was imported")
	}
}

func TestImportPostmanEnvironmentAndGlobals(t *testing.T) {
	svc := newService(t)
	env := `{"name": "Shop dev", "values": [
		{"key": "baseUrl", "value": "https://dev.shop.test", "type": "default", "enabled": true},
		{"key": "token", "value": "s3cret", "type": "secret", "enabled": true},
		{"key": "old", "value": "x", "enabled": false}]}`
	result, err := svc.Import("postman", env, ImportOptions{EnvTag: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	envs, _ := svc.Environments()
	if len(envs) != 1 || envs[0].ID != result.EnvironmentID || envs[0].Vars["baseUrl"] == "" ||
		strings.Join(envs[0].SecretVars, ",") != "token" || envs[0].Vars["old"] != "" {
		t.Fatalf("environments = %+v", envs)
	}
	if _, err := svc.Import("postman", env, ImportOptions{EnvTag: "nope"}); err == nil {
		t.Fatal("an unknown tag was accepted")
	}

	if _, err := svc.SetGlobals(VarsUpdate{Vars: map[string]string{"keep": "1"}, Secrets: map[string]string{"s": "v"}}); err != nil {
		t.Fatal(err)
	}
	globals := `{"name": "Globals", "_postman_variable_scope": "globals", "values": [{"key": "region", "value": "eu", "enabled": true}]}`
	if _, err := svc.Import("", globals, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	g, _ := svc.Globals()
	if g.Vars["keep"] != "1" || g.Vars["region"] != "eu" || len(g.SecretVars) != 1 {
		t.Fatalf("globals = %+v", g)
	}
}
