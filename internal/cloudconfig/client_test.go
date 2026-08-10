package cloudconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devkit/internal/auth"
)

func TestFetchSetsHeaders(t *testing.T) {
	var gotClient, gotUA, gotAccept string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != configPath {
			t.Fatalf("path = %q", request.URL.Path)
		}
		gotClient = request.Header.Get("X-DevKit-Client")
		gotUA = request.Header.Get("User-Agent")
		gotAccept = request.Header.Get("Accept")
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(Snapshot{
			OIDCIssuer: "https://auth.example.test/realms/devkit",
		})
	}))
	defer server.Close()

	baseURL := strings.TrimPrefix(server.URL, "https://")
	snapshot, err := Fetch(context.Background(), "https://"+baseURL, "0.1.0", server.Client())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snapshot.OIDCIssuer != "https://auth.example.test/realms/devkit" {
		t.Fatalf("issuer = %q", snapshot.OIDCIssuer)
	}
	if gotClient != "desktop/0.1.0" {
		t.Fatalf("X-DevKit-Client = %q", gotClient)
	}
	if gotUA != "DevKit-Desktop/0.1.0" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q", gotAccept)
	}
}

func TestFetchRejectsHTTPBaseURL(t *testing.T) {
	_, err := Fetch(context.Background(), "http://api.example.test", AppVersion, nil)
	if !errors.Is(err, ErrInvalidBaseURL) {
		t.Fatalf("err = %v, want ErrInvalidBaseURL", err)
	}
}

func TestFetchReturnsRateLimited(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	baseURL := strings.TrimPrefix(server.URL, "https://")
	_, err := Fetch(context.Background(), "https://"+baseURL, AppVersion, server.Client())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestFetchRejectsInvalidIssuer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(Snapshot{
			OIDCIssuer: "http://auth.example.test/realms/devkit",
		})
	}))
	defer server.Close()

	baseURL := strings.TrimPrefix(server.URL, "https://")
	_, err := Fetch(context.Background(), "https://"+baseURL, AppVersion, server.Client())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("err = %v, want ErrAuthenticationFailed", err)
	}
}
