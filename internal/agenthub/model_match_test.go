package agenthub

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// openCodeLikeModels is a long list in opencode's shape: `provider/model` ids
// and names that differ from them.
func openCodeLikeModels() []ModelChoice {
	models := []ModelChoice{
		{Value: "opencode/glm-5.3-flash-free", Name: "GLM-5.3 Flash Free", Description: "Free tier, fast"},
		{Value: "opencode/glm-5.3", Name: "GLM-5.3"},
		{Value: "opencode/big-pickle", Name: "Big Pickle"},
	}
	for _, provider := range []string{"anthropic", "openai", "openrouter", "google", "groq", "mistral"} {
		for i := 0; i < 60; i++ {
			models = append(models, ModelChoice{
				Value:       fmt.Sprintf("%s/model-%d-preview-2026", provider, i),
				Name:        fmt.Sprintf("%s Model %d Preview (2026 build)", strings.ToUpper(provider[:1])+provider[1:], i),
				Description: "A general purpose model offered through " + provider + " with a long description that opencode reports for every single entry of its catalog, repeated at length",
				Efforts:     []string{"low", "medium", "high"},
			})
		}
	}
	return models
}

func TestResolveFindsTheOneModelAnAgentMeant(t *testing.T) {
	v := VendorModels{Vendor: "opencode", Status: ModelsOK, Models: openCodeLikeModels()}
	for ref, want := range map[string]string{
		"opencode/glm-5.3-flash-free":         "opencode/glm-5.3-flash-free", // the id
		"OPENCODE/GLM-5.3-FLASH-FREE":         "opencode/glm-5.3-flash-free", // another case
		"GLM-5.3 Flash Free":                  "opencode/glm-5.3-flash-free", // the name
		"opencode/GLM-5.3 Flash Free":         "opencode/glm-5.3-flash-free", // provider + name
		"glm 5.3 flash-free":                  "opencode/glm-5.3-flash-free", // punctuation
		"flash free":                          "opencode/glm-5.3-flash-free", // a fragment of one
		"  big pickle  ":                      "opencode/big-pickle",
		"OpenAI Model 7 Preview (2026 build)": "openai/model-7-preview-2026",
	} {
		got := v.Resolve(ref)
		if !got.Found || got.Model.Value != want {
			t.Errorf("Resolve(%q) = %+v, want %s", ref, got, want)
		}
	}
}

func TestResolveRefusesAGuess(t *testing.T) {
	v := VendorModels{Vendor: "opencode", Status: ModelsOK, Models: openCodeLikeModels()}

	// The agent's typo: "Plash" for "Flash". Not taken — suggested.
	got := v.Resolve("opencode/GLM-5.3-Plash Free")
	if got.Found || got.Ambiguous || len(got.Candidates) == 0 || got.Candidates[0].Value != "opencode/glm-5.3-flash-free" {
		t.Fatalf("typo = %+v, want the flash model as the closest, not taken", got)
	}

	// "glm-5.3" is an id and a prefix of another: the exact id wins.
	if got := v.Resolve("glm-5.3"); !got.Found || got.Model.Value != "opencode/glm-5.3" {
		t.Errorf("Resolve(glm-5.3) = %+v", got)
	}
	// A fragment of several models is ambiguous, never the first.
	got = v.Resolve("model 7 preview")
	if got.Found || !got.Ambiguous || len(got.Candidates) == 0 {
		t.Errorf("Resolve(model 7 preview) = %+v, want ambiguous", got)
	}
	// Too short to mean anything.
	if got := v.Resolve("glm"); got.Found {
		t.Errorf("Resolve(glm) = %+v, want ambiguous or not found", got)
	}
	if got := v.Resolve("gpt-9-turbo-xl-nothing-like-it"); got.Found || len(got.Candidates) != 0 {
		t.Errorf("a name near nothing = %+v", got)
	}
}

func TestAnUnknownModelErrorIsShortAndSaysWhatWouldWork(t *testing.T) {
	v := VendorModels{Vendor: "opencode", Status: ModelsOK, Models: openCodeLikeModels()}
	err := unknownModelError("opencode", v, "opencode/GLM-5.3-Plash Free", v.Resolve("opencode/GLM-5.3-Plash Free"))
	var mc *ModelChoiceError
	if !errors.Is(err, ErrInvalidConfigOption) || !errors.As(err, &mc) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(mc.Detail, "opencode/glm-5.3-flash-free") || !strings.Contains(mc.Detail, "agenthub.listModels") {
		t.Errorf("detail = %q, want the close model and the way to search", mc.Detail)
	}
	if len(mc.Detail) > 600 || strings.Contains(mc.Detail, "\n") || strings.Contains(mc.Detail, "anthropic/model-0") {
		t.Errorf("detail is not short and on one line (%d bytes): %q", len(mc.Detail), mc.Detail)
	}
	detail, ok := ModelChoiceDetail(fmt.Errorf("wrapped: %w", err))
	if !ok || detail != mc.Detail {
		t.Errorf("ModelChoiceDetail through a wrap = %q, %v", detail, ok)
	}
}

func TestAListingOfHundredsOfModelsFitsAPageAndCanBeSearched(t *testing.T) {
	v := VendorModels{Vendor: "opencode", Status: ModelsOK, DefaultModel: "opencode/glm-5.3", Models: openCodeLikeModels()}
	whole, _ := json.Marshal(v)
	if len(whole) < 24*1024 {
		t.Fatalf("fixture is %d bytes: it must be over the 24 KB budget to prove anything", len(whole))
	}

	page := pageModels(v, ModelQuery{})
	raw, _ := json.Marshal(page)
	if len(raw) > 16*1024 {
		t.Errorf("a default page is %d bytes", len(raw))
	}
	if len(page.Models) != DefaultModelPage || page.Total != len(v.Models) || page.NextOffset == nil || *page.NextOffset != DefaultModelPage || !strings.Contains(page.Hint, "offset 40") {
		t.Errorf("page = %d models, total %d, next %v, hint %q", len(page.Models), page.Total, page.NextOffset, page.Hint)
	}
	if len(v.Models[5].Description) < 120 || len(page.Models[5].Description) > 124 {
		t.Errorf("description not clipped in the page: %d bytes", len(page.Models[5].Description))
	}
	if len(v.Models) != len(openCodeLikeModels()) {
		t.Error("the cached list was changed")
	}

	// A query finds the model by words from its id or name, whatever the case.
	found := pageModels(v, ModelQuery{Query: "GLM flash"})
	if len(found.Models) != 1 || found.Models[0].Value != "opencode/glm-5.3-flash-free" || found.NextOffset != nil || found.Hint != "" {
		t.Errorf("query = %+v", found)
	}
	// The last page says nothing more is left.
	last := pageModels(v, ModelQuery{Query: "groq", Limit: 100, Offset: 50})
	if len(last.Models) != 10 || last.Total != 60 || last.NextOffset != nil {
		t.Errorf("last page = %d models of %d, next %v", len(last.Models), last.Total, last.NextOffset)
	}
	if none := pageModels(v, ModelQuery{Query: "zzzz"}); len(none.Models) != 0 || !strings.Contains(none.Hint, "No model") {
		t.Errorf("no match = %+v", none)
	}
	if capped := pageModels(v, ModelQuery{Limit: 10000}); len(capped.Models) != MaxModelPage {
		t.Errorf("limit not capped: %d", len(capped.Models))
	}
	// A vendor that cannot answer is passed through whole.
	if down := pageModels(VendorModels{Vendor: "x", Status: ModelsSignInRequired, Reason: "r"}, ModelQuery{Query: "a"}); down.Status != ModelsSignInRequired {
		t.Errorf("down = %+v", down)
	}
}

func TestAnAgentMayNameAModelByItsDisplayNameAndGetsTheVendorsId(t *testing.T) {
	f := newChoiceFixture(t)
	f.adapter.extra = []ConfigSelectOption{
		{Value: "oc/glm-5.3-flash-free", Name: "GLM-5.3 Flash Free"},
		{Value: "oc/glm-5.3-pro", Name: "GLM-5.3 Pro"},
	}
	for i := 0; i < 20; i++ {
		f.adapter.extra = append(f.adapter.extra, ConfigSelectOption{Value: fmt.Sprintf("oc/filler-%d", i), Name: fmt.Sprintf("Filler %d", i)})
	}
	f.svc.ForgetModelCatalog(AgentTypeMock)

	plan, err := f.plan(t, map[string]string{"model": "GLM-5.3 Flash Free"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Config["model"] != "oc/glm-5.3-flash-free" || !plan.NeedsApproval || !strings.Contains(plan.Why[0], "oc/glm-5.3-flash-free") {
		t.Fatalf("plan = %+v, want the id, and the person asked about the id", plan)
	}

	// The parent's own model, spelled as its name, is not "another model".
	plan, err = f.plan(t, map[string]string{"model": "mid", "reasoning_effort": "MEDIUM"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.NeedsApproval || plan.Config["model"] != "m-mid" || plan.Config["reasoning_effort"] != "medium" {
		t.Fatalf("plan = %+v, want m-mid / medium with no question", plan)
	}

	// The typo is refused with the close model, through the whole stack.
	_, err = f.plan(t, map[string]string{"model": "GLM-5.3-Plash Free"}, "")
	detail, ok := ModelChoiceDetail(err)
	if !errors.Is(err, ErrInvalidConfigOption) || !ok || !strings.Contains(detail, "oc/glm-5.3-flash-free") {
		t.Fatalf("err = %v", err)
	}
	if _, err = f.svc.CreateSession(f.ctx, CreateInput{AgentType: AgentTypeMock, Cwd: f.parent.ProjectPath, InitialConfig: map[string]string{"model": "GLM-5.3-Plash Free"}}); !errors.Is(err, ErrInvalidConfigOption) {
		t.Fatalf("CreateSession err = %v", err)
	}
}
