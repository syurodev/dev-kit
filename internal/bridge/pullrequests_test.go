package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/forge"
	"devkit/internal/prwatch"
)

type prSource struct{}

func (prSource) Checkout(context.Context, string) (prwatch.Checkout, error) {
	ref := prwatch.RemoteRef{Name: "origin", Host: "github.com", Path: "acme/widgets", Owner: "acme"}
	return prwatch.Checkout{Branch: "feat/x", Origin: ref, Push: ref, DefaultBranch: "main"}, nil
}

type prToken struct{}

func (prToken) Token(context.Context, prwatch.TokenRequest) (string, string, error) {
	return "ghp_test", "work", nil
}

func pullRequestService(t *testing.T) *AppService {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/widgets/pulls":
			w.Write([]byte(`[{"number":4,"title":"Add x","html_url":"https://github.com/acme/widgets/pull/4","state":"open","head":{"ref":"feat/x","sha":"abc"},"base":{"ref":"main"},"updated_at":"2026-10-16T09:00:00Z","user":{"login":"me"}}]`))
		case r.URL.Path == "/repos/acme/widgets/pulls/4":
			w.Write([]byte(`{"number":4,"title":"Add x","html_url":"https://github.com/acme/widgets/pull/4","state":"open","additions":3,"deletions":1,"mergeable":true,"mergeable_state":"clean","head":{"ref":"feat/x","sha":"abc"},"base":{"ref":"main"},"updated_at":"2026-10-16T09:00:00Z","user":{"login":"me"}}`))
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			w.Write([]byte(`{"check_runs":[{"name":"ci","status":"completed","conclusion":"success"}]}`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.Write([]byte(`{"statuses":[]}`))
		case strings.HasSuffix(r.URL.Path, "/reviews"):
			w.Write([]byte(`[]`))
		case r.URL.Path == "/graphql":
			w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	container := app.NewContainer()
	container.PullRequests = prwatch.New(forge.NewRegistry(forge.NewGitHub(forge.WithAPIBase(srv.URL))), prSource{}, prToken{}, nil)
	return NewAppService(container)
}

// Every bridge method of this feature is called with the UI's real default
// grants: a (capability, scope) the policy does not cover would be refused here
// and work in a test that skips the policy.
func TestPullRequestBridgeMethodsAreGrantedToTheUI(t *testing.T) {
	svc := pullRequestService(t)

	state, err := svc.PullRequestState("s1", []string{"/w/widgets"})
	if err != nil {
		t.Fatalf("PullRequestState: %v", err)
	}
	if len(state.Items) != 1 || state.Items[0].ChangeRequest == nil || state.Items[0].ChangeRequest.Number != 4 {
		t.Fatalf("state %+v", state)
	}
	if state.Items[0].ChangeRequest.Checks.Passed != 1 {
		t.Errorf("checks %+v", state.Items[0].ChangeRequest.Checks)
	}

	item, err := svc.PullRequestAttach("s1", "/w/widgets", "https://github.com/acme/widgets/pull/4")
	if err != nil || item.ChangeRequest == nil {
		t.Fatalf("PullRequestAttach: %v %+v", err, item)
	}
	if err := svc.PullRequestDetach("s1", "github.com", "acme/widgets", 4); err != nil {
		t.Fatalf("PullRequestDetach: %v", err)
	}
}

func TestPullRequestStateRefusesRelativeFoldersAndAnUnwiredContainer(t *testing.T) {
	svc := pullRequestService(t)
	if _, err := svc.PullRequestState("s1", []string{"relative/dir"}); err == nil {
		t.Error("a relative folder was accepted")
	}
	bare := NewAppService(app.NewContainer())
	state, err := bare.PullRequestState("s1", []string{"/w"})
	if err != nil || state.NextPollMs != -1 || len(state.Items) != 0 {
		t.Errorf("%+v %v", state, err)
	}
	if _, err := bare.PullRequestAttach("s1", "/w", "https://github.com/a/b/pull/1"); err == nil {
		t.Error("attach without a tracker must say so")
	}
}

func TestPullRequestAttachExplainsWhyItFailed(t *testing.T) {
	svc := pullRequestService(t)
	_, err := svc.PullRequestAttach("s1", "/w/widgets", "https://example.com/nothing")
	if err == nil || publicCode(err) != "pullrequests.attachFailed" {
		t.Fatalf("%v", err)
	}
}
