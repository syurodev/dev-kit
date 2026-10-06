package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"devkit/internal/apiclient"
	"devkit/internal/apperrors"
	"devkit/internal/environments"
	"devkit/internal/interaction"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

// answeringPrompter answers every prompt at once and keeps what it was asked.
type answeringPrompter struct {
	hub    *interaction.Hub
	answer string
	asked  []interaction.Pending
}

func (a *answeringPrompter) Prompt(p interaction.Pending) error {
	a.asked = append(a.asked, p)
	return a.hub.Complete(p.ID, a.answer)
}

type apiTagLookup struct{}

func (apiTagLookup) Exists(name string) bool               { return name == "dev" || name == "prod" }
func (apiTagLookup) RequiresConfirmation(name string) bool { return name == "prod" }

type apiWithoutDrafts struct{ *apiclient.Service }

func (apiWithoutDrafts) DraftFromEndpoint(context.Context, string) (apiclient.EndpointDraft, error) {
	return apiclient.EndpointDraft{}, nil
}

func newAPIWriteServer(t *testing.T, answer string) (*Server, *apiclient.Service, *answeringPrompter) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	api := apiclient.NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), apiTagLookup{})
	prompter := &answeringPrompter{answer: answer}
	hub := interaction.NewHub(prompter)
	prompter.hub = hub
	srv := NewServer(Deps{
		Caller: &fakeCaller{},
		API:    apiWithoutDrafts{api},
		Envs: &fakeEnvs{
			tags: []environments.Tag{{Name: "dev"}, {Name: "prod", RequiresConfirmation: true}},
			req:  map[string]bool{"prod": true},
		},
		Hub: hub,
	})
	return srv, api, prompter
}

func TestAPICreateCollectionAsksOnceWhenAllowedAlways(t *testing.T) {
	srv, api, prompter := newAPIWriteServer(t, "allow_always")
	ctx := context.Background()
	if _, _, err := srv.toolAPICreateCollection(ctx, nil, apiCreateCollectionInput{
		Name: "Payments", Vars: map[string]string{"baseUrl": "https://pay.example"}, Secrets: map[string]string{"key": "s3cret"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(prompter.asked) != 1 || !prompter.asked[0].AllowAlwaysOffered || prompter.asked[0].Kind != interaction.KindAPI {
		t.Fatalf("asked = %+v, want one api prompt offering allow_always", prompter.asked)
	}
	if strings.Contains(prompter.asked[0].Summary, "s3cret") {
		t.Fatalf("a secret value reached the prompt: %q", prompter.asked[0].Summary)
	}
	if _, _, err := srv.toolAPICreateCollection(ctx, nil, apiCreateCollectionInput{Name: "Refunds"}); err != nil {
		t.Fatal(err)
	}
	if len(prompter.asked) != 1 {
		t.Fatalf("allow_always did not hold: asked %d times", len(prompter.asked))
	}
	collections, _ := api.Collections()
	if len(collections) != 2 {
		t.Fatalf("collections = %+v", collections)
	}
	for _, c := range collections {
		if c.Name == "Payments" && (c.Vars["baseUrl"] != "https://pay.example" || len(c.SecretVars) != 1) {
			t.Fatalf("Payments = %+v", c)
		}
	}
}

func TestAPIWritesRefusedWhenDeniedOrHeadless(t *testing.T) {
	srv, api, _ := newAPIWriteServer(t, "deny")
	_, _, err := srv.toolAPICreateCollection(context.Background(), nil, apiCreateCollectionInput{Name: "Payments"})
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "permission_denied" {
		t.Fatalf("err = %v, want permission_denied", err)
	}

	srv.deps.Hub = &interaction.Hub{Headless: true}
	_, _, err = srv.toolAPICreateEnvironment(context.Background(), nil, apiCreateEnvironmentInput{Name: "Dev", Env: "dev"})
	if !errors.As(err, &app) || app.Message != "Confirmation required." {
		t.Fatalf("headless err = %v, want Confirmation required", err)
	}
	if collections, _ := api.Collections(); len(collections) != 0 {
		t.Fatalf("written after a refusal: %+v", collections)
	}
	if envs, _ := api.Environments(); len(envs) != 0 {
		t.Fatalf("written after a refusal: %+v", envs)
	}
}

func TestAPIEnvironmentWritesAskEveryTime(t *testing.T) {
	srv, api, prompter := newAPIWriteServer(t, "allow_always")
	ctx := context.Background()
	if _, _, err := srv.toolAPICreateEnvironment(ctx, nil, apiCreateEnvironmentInput{Name: "Local", Env: "nope"}); err == nil ||
		!strings.Contains(err.Error(), "dev, prod") {
		t.Fatalf("unknown tag: err = %v, want the known tags named", err)
	}
	for _, name := range []string{"Local", "Staging"} {
		if _, _, err := srv.toolAPICreateEnvironment(ctx, nil, apiCreateEnvironmentInput{
			Name: name, Env: "dev", Vars: map[string]string{"baseUrl": "http://localhost:8080"}, Secrets: map[string]string{"token": "t0k"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	envs, _ := api.Environments()
	if len(envs) != 2 {
		t.Fatalf("environments = %+v", envs)
	}
	if _, _, err := srv.toolAPIUpdateEnvironment(ctx, nil, apiUpdateEnvironmentInput{
		EnvironmentID: envs[0].ID, Env: ptr("prod"), SetVars: map[string]string{"region": "eu"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(prompter.asked) != 3 {
		t.Fatalf("asked %d times, want every environment write asked", len(prompter.asked))
	}
	for _, p := range prompter.asked {
		if p.AllowAlwaysOffered {
			t.Fatalf("environment write offered allow_always: %+v", p)
		}
	}
	last := prompter.asked[2]
	if last.Env != "prod" || !strings.Contains(last.Summary, "asks before every send") {
		t.Fatalf("move prompt = %+v", last)
	}
	envs, _ = api.Environments()
	for _, env := range envs {
		if env.ID == envs[0].ID {
			if env.Env != "prod" || env.Vars["baseUrl"] == "" || env.Vars["region"] != "eu" || len(env.SecretVars) != 1 {
				t.Fatalf("update lost what it did not name: %+v", env)
			}
		}
	}
}

func TestAPISaveRequestCarryingACredentialAsksEveryTime(t *testing.T) {
	srv, api, prompter := newAPIWriteServer(t, "allow_always")
	ctx := context.Background()
	if _, err := api.CreateEnvironment("Dev", "dev", nil, map[string]string{"token": "t0k"}); err != nil {
		t.Fatal(err)
	}

	plain := apiclient.RequestInput{Name: "Health", Method: "GET", URL: "https://api.example/health"}
	for range 2 {
		if _, _, err := srv.toolAPISaveRequest(ctx, nil, apiSaveRequestInput{Request: plain}); err != nil {
			t.Fatal(err)
		}
	}
	if len(prompter.asked) != 1 {
		t.Fatalf("a plain request asked %d times, want once then allow_always", len(prompter.asked))
	}

	for _, carrying := range []apiclient.RequestInput{
		{Name: "Me", URL: "https://evil.example/me", Auth: apiclient.Auth{Kind: apiclient.AuthBearer, AccountID: "acct-1"}},
		{Name: "Me", URL: "https://evil.example/me", Headers: apiclient.KVList{{Key: "Authorization", Value: "Bearer {{token}}"}}},
		{Name: "Me", URL: "https://evil.example/me", Headers: apiclient.KVList{{Key: "X-Api-Key", Value: "literal"}}},
	} {
		before := len(prompter.asked)
		if _, _, err := srv.toolAPISaveRequest(ctx, nil, apiSaveRequestInput{Request: carrying}); err != nil {
			t.Fatal(err)
		}
		if len(prompter.asked) != before+1 {
			t.Fatalf("%+v: not asked although allow_always was given for plain saves", carrying)
		}
		p := prompter.asked[len(prompter.asked)-1]
		if p.AllowAlwaysOffered || !strings.Contains(p.Summary, "carries") || !strings.Contains(p.Summary, "evil.example") {
			t.Fatalf("%+v: prompt = %+v", carrying, p)
		}
	}
}

func TestAPISaveRequestKeepsAMaskedValue(t *testing.T) {
	srv, api, prompter := newAPIWriteServer(t, "allow")
	ctx := context.Background()
	saved, err := api.SaveRequest(apiclient.Request{
		Name: "Me", Method: "GET", URL: "https://api.example/me",
		Headers: apiclient.KVList{{Key: "Authorization", Value: "Bearer real-token"}, {Key: "Accept", Value: "application/json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	shown := saved.ForAgent()
	if shown.Headers[0].Value != apiclient.Masked {
		t.Fatalf("not masked for the agent: %+v", shown.Headers)
	}
	edit := apiclient.RequestInput{
		ID: saved.ID, Name: "Me (renamed)", Method: "GET", URL: shown.URL,
		Headers: append(shown.Headers, apiclient.KV{Key: "X-Trace", Value: "1"}),
	}
	_, out, err := srv.toolAPISaveRequest(ctx, nil, apiSaveRequestInput{Request: edit})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompter.asked[0].Summary, "real-token") {
		t.Fatalf("the stored value reached the prompt: %q", prompter.asked[0].Summary)
	}
	if returned := out.(apiclient.Request); returned.Headers[0].Value != apiclient.Masked {
		t.Fatalf("the tool returned the stored value: %+v", returned.Headers)
	}
	stored, _ := api.Request(saved.ID)
	if stored.Name != "Me (renamed)" || stored.Headers[0].Value != "Bearer real-token" || len(stored.Headers) != 3 {
		t.Fatalf("stored = %+v", stored)
	}

	_, _, err = srv.toolAPISaveRequest(ctx, nil, apiSaveRequestInput{Request: apiclient.RequestInput{
		Name: "New", URL: "https://api.example/x", Headers: apiclient.KVList{{Key: "Cookie", Value: apiclient.Masked}},
	}})
	if !errors.Is(err, apiclient.ErrInvalid) {
		t.Fatalf("a masked value with nothing to keep: err = %v", err)
	}
}

func TestAPISaveRequestRefusesAnIDThatIsNotARequest(t *testing.T) {
	srv, api, _ := newAPIWriteServer(t, "allow")
	env, err := api.CreateEnvironment("Prod", "prod", map[string]string{"baseUrl": "https://prod.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = srv.toolAPISaveRequest(context.Background(), nil, apiSaveRequestInput{Request: apiclient.RequestInput{
		ID: env.ID, Name: "x", URL: "https://evil.example",
	}})
	if !errors.Is(err, apiclient.ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	envs, _ := api.Environments()
	if len(envs) != 1 || envs[0].Vars["baseUrl"] != "https://prod.example" {
		t.Fatalf("environment overwritten: %+v", envs)
	}
}

func TestAPIUpdateCollectionAsksEveryTimeOnlyForWhatRequestsRead(t *testing.T) {
	srv, api, prompter := newAPIWriteServer(t, "allow_always")
	ctx := context.Background()
	c, err := api.SaveCollection(apiclient.Collection{Name: "Payments", Vars: map[string]string{"baseUrl": "https://pay.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.SetCollectionVariables(c.ID, apiclient.VarsUpdate{Vars: c.Vars, Secrets: map[string]string{"key": "k"}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Pay", "Pay v2"} {
		if _, _, err := srv.toolAPIUpdateCollection(ctx, nil, apiUpdateCollectionInput{CollectionID: c.ID, Name: ptr(name)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(prompter.asked) != 1 {
		t.Fatalf("renames asked %d times, want once then allow_always", len(prompter.asked))
	}
	if _, _, err := srv.toolAPIUpdateCollection(ctx, nil, apiUpdateCollectionInput{
		CollectionID: c.ID, SetVars: map[string]string{"version": "2"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(prompter.asked) != 2 || prompter.asked[1].AllowAlwaysOffered {
		t.Fatalf("a variable change must ask every time: %+v", prompter.asked)
	}
	collections, _ := api.Collections()
	got := collections[0]
	if got.Name != "Pay v2" || got.Vars["baseUrl"] != "https://pay.example" || got.Vars["version"] != "2" || len(got.SecretVars) != 1 {
		t.Fatalf("collection = %+v", got)
	}
}

func TestAPIDeletesAskEveryTime(t *testing.T) {
	srv, api, prompter := newAPIWriteServer(t, "allow_always")
	ctx := context.Background()
	c, _ := api.SaveCollection(apiclient.Collection{Name: "Old"})
	req, _ := api.SaveRequest(apiclient.Request{Name: "Ping", URL: "https://api.example/ping", CollectionID: c.ID})

	if _, _, err := srv.toolAPIDeleteCollection(ctx, nil, apiCollectionIDInput{CollectionID: c.ID}); !errors.Is(err, apiclient.ErrCollectionNotEmpty) {
		t.Fatalf("a full collection: err = %v", err)
	}
	if _, _, err := srv.toolAPIDeleteRequest(ctx, nil, apiRequestIDInput{RequestID: req.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.toolAPIDeleteCollection(ctx, nil, apiCollectionIDInput{CollectionID: c.ID}); err != nil {
		t.Fatal(err)
	}
	if len(prompter.asked) != 3 {
		t.Fatalf("asked %d times, want each delete asked", len(prompter.asked))
	}
	for _, p := range prompter.asked {
		if p.AllowAlwaysOffered {
			t.Fatalf("a delete offered allow_always: %+v", p)
		}
	}
}

func TestAPIWriteNotifiesTheWindow(t *testing.T) {
	srv, api, _ := newAPIWriteServer(t, "allow")
	changed := 0
	api.OnChanged = func() { changed++ }
	if _, _, err := srv.toolAPICreateCollection(context.Background(), nil, apiCreateCollectionInput{Name: "Payments"}); err != nil {
		t.Fatal(err)
	}
	if changed == 0 {
		t.Fatal("OnChanged not called")
	}
}

func ptr[T any](v T) *T { return &v }
