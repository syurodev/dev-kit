package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type namedCreds struct{ stubCreds }

func (namedCreds) AccountLabel(string) string { return "GitHub Work" }

// a collection worth exporting: nesting, both kinds of variable, a body, a
// path variable, a disabled row and a request bound to an account.
func seedShop(t *testing.T, svc *Service, baseURL string) (root, orders Collection) {
	t.Helper()
	root, _ = svc.SaveCollection(Collection{Name: "Shop"})
	if _, err := svc.SetCollectionVariables(root.ID, VarsUpdate{
		Vars:    map[string]string{"baseUrl": baseURL},
		Secrets: map[string]string{"apiKey": "never-leaves"},
	}); err != nil {
		t.Fatal(err)
	}
	orders, _ = svc.SaveCollection(Collection{Name: "Orders", ParentID: root.ID})
	if _, err := svc.SaveRequest(Request{
		CollectionID: orders.ID, Name: "Create order", Method: "POST", URL: "{{baseUrl}}/orders/:id",
		PathVars: KVList{{Key: "id", Value: "42"}},
		Query:    KVList{{Key: "expand", Value: "items"}, {Key: "debug", Value: "1", Disabled: true}},
		Headers:  KVList{{Key: "X-Key", Value: "{{apiKey}}"}},
		BodyMode: BodyJSON, Body: `{"note":"it's $5"}`,
		Auth: Auth{Kind: AuthBearer, AccountID: "acct-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRequest(Request{CollectionID: root.ID, Name: "Health", Method: "GET", URL: "{{baseUrl}}/health"}); err != nil {
		t.Fatal(err)
	}
	return root, orders
}

func TestFormatsAreListedWithWhatTheyCanDo(t *testing.T) {
	byID := map[string]FormatInfo{}
	for _, f := range Formats() {
		byID[f.ID] = f
	}
	if pm := byID["postman"]; !pm.CanImport || len(pm.Exports) != 3 {
		t.Fatalf("postman = %+v", pm)
	}
	if c := byID["curl"]; c.CanImport || len(c.Exports) != 1 || c.Exports[0] != ExportCollection {
		t.Fatalf("curl = %+v", c)
	}
	svc := newService(t)
	if _, err := svc.Export(ExportScope{Kind: ExportEnvironment, ID: "x"}, "curl"); err != ErrFormatCannot {
		t.Fatalf("curl environment export = %v", err)
	}
	if _, err := svc.Export(ExportScope{Kind: ExportGlobals}, "nope"); err != ErrUnknownFormat {
		t.Fatalf("unknown format = %v", err)
	}
	if _, err := svc.Import("curl", "curl https://x", ImportOptions{}); err != ErrFormatCannot {
		t.Fatalf("curl import through the file path = %v", err)
	}
}

// What leaves DevKit, whatever the format: no secret value, no credential.
// What comes back in: the same tree, requests that still work.
func TestPostmanExportRoundTrips(t *testing.T) {
	svc := newService(t)
	svc.SetCredentials(&namedCreds{stubCreds{secret: "ghp_live"}})
	root, _ := seedShop(t, svc, "https://shop.test")

	file, err := svc.Export(ExportScope{Kind: ExportCollection, ID: root.ID}, "postman")
	if err != nil {
		t.Fatal(err)
	}
	if file.FileName != "Shop.json" {
		t.Fatalf("file name = %q", file.FileName)
	}
	for _, leaked := range []string{"never-leaves", "ghp_live", "acct-1"} {
		if strings.Contains(file.Content, leaked) {
			t.Fatalf("export leaked %q", leaked)
		}
	}
	if len(file.Warnings) != 2 || !strings.Contains(strings.Join(file.Warnings, " "), "{{github_work_token}}") {
		t.Fatalf("warnings = %v", file.Warnings)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(file.Content), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["info"].(map[string]any)["schema"] != postmanSchema {
		t.Fatalf("schema = %v", doc["info"])
	}
	var shape struct {
		Item []struct {
			Name string `json:"name"`
			Item []struct {
				Request struct {
					URL pmURL `json:"url"`
				} `json:"request"`
			} `json:"item"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(file.Content), &shape); err != nil {
		t.Fatal(err)
	}
	u := shape.Item[0].Item[0].Request.URL
	if shape.Item[0].Name != "Orders" || strings.Join(u.Host, "|") != "{{baseUrl}}" ||
		strings.Join(u.Path, "|") != "orders|:id" || u.Raw != "{{baseUrl}}/orders/:id?expand=items" {
		t.Fatalf("a {{variable}} host should stay one piece: %+v", u)
	}

	// Back in, as a second copy.
	other := newService(t)
	result, err := other.Import("", file.Content, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "postman" || result.Folders != 1 || result.Requests != 2 || result.Variables != 2 {
		t.Fatalf("result = %+v", result)
	}
	requests, _ := other.Requests()
	var create Request
	for _, r := range requests {
		if r.Name == "Create order" {
			create, _ = other.Request(r.ID)
		}
	}
	if create.URL != "{{baseUrl}}/orders/:id" || create.PathVars[0].Value != "42" || create.BodyMode != BodyJSON ||
		len(create.Query) != 2 || !create.Query[1].Disabled || create.Body != `{"note":"it's $5"}` {
		t.Fatalf("create order came back as %+v", create)
	}
	if v, _ := create.Headers.Lookup("Authorization", true); v != "Bearer {{github_work_token}}" {
		t.Fatalf("the account came back as %q", v)
	}
	collections, _ := other.Collections()
	for _, c := range collections {
		if c.Name == "Shop" && (c.Vars["baseUrl"] != "https://shop.test" || strings.Join(c.SecretVars, ",") != "apiKey") {
			t.Fatalf("shop came back as %+v", c)
		}
	}
}

func TestPostmanEnvironmentExportKeepsTheTagInTheName(t *testing.T) {
	svc := newService(t)
	env, _ := svc.CreateEnvironment("Staging", "prod", map[string]string{"baseUrl": "https://stg"}, map[string]string{"token": "s3cr3t"})
	file, err := svc.Export(ExportScope{Kind: ExportEnvironment, ID: env.ID}, "postman")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(file.Content, "s3cr3t") || !strings.Contains(file.Content, `"Staging [prod]"`) ||
		!strings.Contains(file.Content, `"type": "secret"`) || file.FileName != "Staging.environment.json" {
		t.Fatalf("environment export:\n%s", file.Content)
	}
	// With no tag chosen, the tag in the name is used — when this machine has it.
	other := newService(t)
	result, err := other.Import("", file.Content, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	envs, _ := other.Environments()
	if len(envs) != 1 || envs[0].ID != result.EnvironmentID || envs[0].Name != "Staging" || envs[0].Env != "prod" {
		t.Fatalf("environments = %+v", envs)
	}
	// A file with no tag and none chosen is refused before anything is written.
	plain := strings.Replace(file.Content, " [prod]", "", 1)
	if _, err := other.Import("", plain, ImportOptions{}); err != ErrTagRequired {
		t.Fatalf("untagged import = %v", err)
	}
	if envs, _ := other.Environments(); len(envs) != 1 {
		t.Fatal("a refused import still wrote an environment")
	}

	globals, err := svc.Export(ExportScope{Kind: ExportGlobals}, "postman")
	if err != nil || !strings.Contains(globals.Content, `"_postman_variable_scope": "globals"`) || globals.FileName != "globals.json" {
		t.Fatalf("globals export = %+v, %v", globals, err)
	}
}

// The script is run for real: bash, curl, a server. A request reaches it with
// the collection's defaults, the environment's secret and the placeholder for
// the account filled from the shell — and running the file alone sends
// nothing.
func TestCurlScriptExportRuns(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	var got *http.Request
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got, body = r.Clone(context.Background()), string(raw)
		w.WriteHeader(204)
	}))
	defer server.Close()

	svc := newService(t)
	svc.SetCredentials(&namedCreds{})
	root, _ := seedShop(t, svc, server.URL)
	file, err := svc.Export(ExportScope{Kind: ExportCollection, ID: root.ID}, "curl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(file.Content, "never-leaves") || file.FileName != "Shop.sh" {
		t.Fatalf("script:\n%s", file.Content)
	}
	path := filepath.Join(t.TempDir(), file.FileName)
	if err := os.WriteFile(path, []byte(file.Content), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("not valid bash: %v\n%s\n%s", err, out, file.Content)
	}

	if out, err := exec.Command("bash", path).CombinedOutput(); err == nil || got != nil || !strings.Contains(string(out), "create_order") {
		t.Fatalf("running the file alone: err=%v sent=%v\n%s", err, got != nil, out)
	}

	missing := exec.Command("bash", path, "create_order")
	missing.Env = []string{"PATH=" + os.Getenv("PATH")}
	if out, err := missing.CombinedOutput(); err == nil || got != nil {
		t.Fatalf("a missing secret was sent empty: %s", out)
	}

	run := exec.Command("bash", path, "create_order")
	run.Env = []string{"PATH=" + os.Getenv("PATH"), "apiKey=k-1", "github_work_token=tok"}
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s\n%s", err, out, file.Content)
	}
	if got == nil || got.Method != "POST" || got.URL.Path != "/orders/42" || got.URL.RawQuery != "expand=items" {
		t.Fatalf("server saw %+v", got)
	}
	if got.Header.Get("X-Key") != "k-1" || got.Header.Get("Authorization") != "Bearer tok" ||
		got.Header.Get("Content-Type") != "application/json" || body != `{"note":"it's $5"}` {
		t.Fatalf("headers %v body %q", got.Header, body)
	}
}
