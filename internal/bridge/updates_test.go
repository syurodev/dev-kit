package bridge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/updater"
)

func updateService(t *testing.T, version string, status int, body string) (*AppService, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	container := app.NewContainer()
	container.Version = version
	container.Updates = &updater.Checker{APIBase: srv.URL, Client: srv.Client()}
	return NewAppService(container), &hits
}

const newerRelease = `[{"tag_name":"v2.0.0","html_url":"https://github.com/syurodev/dev-kit/releases/tag/v2.0.0"}]`

func TestUpdatesCheckReportsNewerRelease(t *testing.T) {
	service, _ := updateService(t, "1.0.0", 200, newerRelease)
	got, err := service.UpdatesCheck(true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Latest != "2.0.0" || got.Current != "1.0.0" || got.Skipped != "" ||
		got.URL != "https://github.com/syurodev/dev-kit/releases/tag/v2.0.0" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdatesCheckDevBuildNeverReachesTheNetwork(t *testing.T) {
	service, hits := updateService(t, "dev", 200, newerRelease)
	got, err := service.UpdatesCheck(false)
	if err != nil || got.Skipped != "dev" || got.Available {
		t.Fatalf("got %+v, %v", got, err)
	}
	if hits.Load() != 0 {
		t.Fatalf("a dev build made %d requests", hits.Load())
	}
}

func TestUpdatesCheckAutomaticHonoursThePreferenceAndManualDoesNot(t *testing.T) {
	service, hits := updateService(t, "1.0.0", 200, newerRelease)

	if on, err := service.PreferencesGetUpdateChecks(); err != nil || !on {
		t.Fatalf("default = %v, %v; want on", on, err)
	}
	if err := service.PreferencesSetUpdateChecks(false); err != nil {
		t.Fatal(err)
	}
	if on, err := service.PreferencesGetUpdateChecks(); err != nil || on {
		t.Fatalf("after turning off = %v, %v", on, err)
	}

	got, err := service.UpdatesCheck(true)
	if err != nil || got.Skipped != "disabled" || got.Available {
		t.Fatalf("automatic with checks off: %+v, %v", got, err)
	}
	if hits.Load() != 0 {
		t.Fatalf("automatic check made %d requests while off", hits.Load())
	}

	got, err = service.UpdatesCheck(false)
	if err != nil || !got.Available {
		t.Fatalf("manual check with checks off: %+v, %v", got, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("manual check made %d requests, want 1", hits.Load())
	}
}

func TestUpdatesCheckNoReleaseIsAStateNotAnError(t *testing.T) {
	service, _ := updateService(t, "1.0.0", 404, `{}`)
	got, err := service.UpdatesCheck(false)
	if err != nil || got.Skipped != "no_release" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestUpdatesCheckFailureIsPublicAndRedacted(t *testing.T) {
	service, _ := updateService(t, "1.0.0", 500, `secret upstream body`)
	_, err := service.UpdatesCheck(false)
	var public *apperrors.AppError
	if !errors.As(err, &public) {
		t.Fatalf("err = %v, want *apperrors.AppError", err)
	}
	if public.Code != codeConnectionFailed {
		t.Fatalf("code = %q", public.Code)
	}
	if public.Message == "" || len(public.Message) > 120 {
		t.Fatalf("message = %q", public.Message)
	}
}
