package apperrors

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNewCreatesStructuredError(t *testing.T) {
	err := New("vault.locked", "Vault is locked", true)

	if err.Code != "vault.locked" {
		t.Fatalf("Code = %q, want %q", err.Code, "vault.locked")
	}
	if err.Message != "Vault is locked" {
		t.Fatalf("Message = %q", err.Message)
	}
	if !err.Recoverable {
		t.Fatal("Recoverable = false, want true")
	}
}

func TestWrapPreservesCauseWithoutLeakingInJSON(t *testing.T) {
	cause := errors.New("raw database password leaked")
	err := Wrap("storage.failed", "Storage operation failed", false, cause)

	if !errors.Is(err, cause) {
		t.Fatal("wrapped error should match original cause")
	}

	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("json.Marshal returned error: %v", marshalErr)
	}

	if string(payload) == "" {
		t.Fatal("payload should not be empty")
	}
	if strings.Contains(string(payload), "raw database password leaked") {
		t.Fatalf("json payload leaked cause: %s", payload)
	}
}

func TestNewWithDetailsSetsDetails(t *testing.T) {
	details := map[string]any{"path": "/vault"}
	err := NewWithDetails("vault.locked", "Vault is locked", true, details)

	if err.Details["path"] != "/vault" {
		t.Fatalf("Details[path] = %v, want %q", err.Details["path"], "/vault")
	}
}

func TestErrorFormatsCodeAndMessage(t *testing.T) {
	err := New("storage.failed", "Storage operation failed", false)

	if got := err.Error(); got != "storage.failed: Storage operation failed" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestErrorUsesMessageWhenCodeEmpty(t *testing.T) {
	err := New("", "plain message", true)

	if got := err.Error(); got != "plain message" {
		t.Fatalf("Error() = %q, want %q", got, "plain message")
	}
}

func TestUnwrapReturnsCause(t *testing.T) {
	cause := errors.New("disk full")
	err := Wrap("storage.failed", "Storage operation failed", false, cause)

	if got := err.Unwrap(); got != cause {
		t.Fatalf("Unwrap() = %v, want %v", got, cause)
	}
}

func TestNilReceiverBehavior(t *testing.T) {
	var err *AppError

	if got := err.Error(); got != "" {
		t.Fatalf("nil Error() = %q, want empty string", got)
	}
	if got := err.Unwrap(); got != nil {
		t.Fatalf("nil Unwrap() = %v, want nil", got)
	}
}
