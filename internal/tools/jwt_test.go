package tools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// makeToken builds a token with a signature that is deliberately not real: the
// decoder never checks one, and a test that supplied a valid signature would
// imply it does.
func makeToken(t *testing.T, header, payload map[string]any) string {
	t.Helper()
	segment := func(value map[string]any) string {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(encoded)
	}
	return segment(header) + "." + segment(payload) + ".c2lnbmF0dXJl"
}

var fixedNow = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func decodeAt(t *testing.T, token string) string {
	t.Helper()
	result, err := NewUtilService().jwtDecode(token, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("jwtDecode: %v", err)
	}
	return result.Output
}

func TestJWTDecodeShowsHeaderAndPayload(t *testing.T) {
	token := makeToken(t,
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{"sub": "1234567890", "name": "Phạm Vũ"},
	)
	output := decodeAt(t, token)

	for _, want := range []string{"HEADER", "PAYLOAD", "HS256", "1234567890", "Phạm Vũ"} {
		if !strings.Contains(output, want) {
			t.Errorf("output is missing %q:\n%s", want, output)
		}
	}
	// Pretty-printed, not the raw one-line JSON it arrived as.
	if !strings.Contains(output, "\n  \"alg\"") {
		t.Errorf("header was not indented:\n%s", output)
	}
}

// The single most important line in the whole output.
func TestJWTDecodeAlwaysSaysTheSignatureIsUnverified(t *testing.T) {
	output := decodeAt(t, makeToken(t,
		map[string]any{"alg": "HS256"},
		map[string]any{"sub": "x"},
	))
	if !strings.Contains(output, "NOT VERIFIED") {
		t.Errorf("output does not say the signature is unverified:\n%s", output)
	}
	if !strings.Contains(output, "does not have") {
		t.Errorf("output does not explain why it cannot verify:\n%s", output)
	}
}

// alg "none" means no signature at all — worth calling out, since a server
// that accepts it accepts a token anyone can write.
func TestJWTDecodeFlagsAlgNone(t *testing.T) {
	output := decodeAt(t, makeToken(t, map[string]any{"alg": "none"}, map[string]any{"sub": "x"}))
	if !strings.Contains(output, `alg "none"`) {
		t.Errorf("alg none was not flagged:\n%s", output)
	}
	// And not flagged when there is a real algorithm.
	normal := decodeAt(t, makeToken(t, map[string]any{"alg": "RS256"}, map[string]any{"sub": "x"}))
	if strings.Contains(normal, `alg "none"`) {
		t.Errorf("alg none was flagged for RS256:\n%s", normal)
	}
}

func TestJWTDecodeReadsTimeClaims(t *testing.T) {
	token := makeToken(t, map[string]any{"alg": "HS256"}, map[string]any{
		"iat": fixedNow.Add(-2 * time.Hour).Unix(),
		"nbf": fixedNow.Add(-2 * time.Hour).Unix(),
		"exp": fixedNow.Add(3 * time.Hour).Unix(),
	})
	output := decodeAt(t, token)

	for _, want := range []string{
		"TIMES",
		"iat  issued at",
		"2 hours ago",
		"valid for another 3 hours",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output is missing %q:\n%s", want, output)
		}
	}
}

// "Is this expired?" is the question people open a decoder to answer.
func TestJWTDecodeSaysWhenExpired(t *testing.T) {
	token := makeToken(t, map[string]any{"alg": "HS256"}, map[string]any{
		"exp": fixedNow.Add(-25 * time.Hour).Unix(),
	})
	output := decodeAt(t, token)
	if !strings.Contains(output, "EXPIRED 1 day ago") {
		t.Errorf("output does not report expiry:\n%s", output)
	}
}

func TestHumanDurationReadsAtEveryScale(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: 30 * time.Second, want: "30 seconds"},
		{d: time.Minute, want: "1 minute"},
		{d: 90 * time.Minute, want: "1 hour"},
		{d: 26 * time.Hour, want: "1 day"},
		{d: 400 * 24 * time.Hour, want: "1 year 1 month"},
		// The jwt.io sample token: 3157 days, which as days makes the reader
		// divide and as years does not.
		{d: 3157 * 24 * time.Hour, want: "8 years 7 months"},
	}
	for _, tc := range tests {
		if got := humanDuration(tc.d); got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestJWTDecodeSaysWhenNotYetValid(t *testing.T) {
	token := makeToken(t, map[string]any{"alg": "HS256"}, map[string]any{
		"nbf": fixedNow.Add(30 * time.Minute).Unix(),
	})
	if output := decodeAt(t, token); !strings.Contains(output, "NOT VALID YET") {
		t.Errorf("output does not report a future nbf:\n%s", output)
	}
}

// A token with no time claims must not grow an empty TIMES heading.
func TestJWTDecodeOmitsTimesWhenThereAreNone(t *testing.T) {
	output := decodeAt(t, makeToken(t, map[string]any{"alg": "HS256"}, map[string]any{"sub": "x"}))
	if strings.Contains(output, "TIMES") {
		t.Errorf("output has an empty TIMES section:\n%s", output)
	}
}

func TestJWTDecodeAcceptsBearerPrefixAndPadding(t *testing.T) {
	token := makeToken(t, map[string]any{"alg": "HS256"}, map[string]any{"sub": "x"})
	for _, input := range []string{token, "Bearer " + token, "  " + token + "  ", "Bearer   " + token} {
		if _, err := NewUtilService().JWTDecode(input); err != nil {
			t.Errorf("JWTDecode(%q…) rejected a token it should accept: %v", input[:12], err)
		}
	}
}

func TestJWTDecodeRejects(t *testing.T) {
	svc := NewUtilService()
	tests := []struct {
		name  string
		token string
		want  error
	}{
		{name: "empty", token: "", want: ErrNotAToken},
		{name: "one part", token: "abc", want: ErrNotAToken},
		{name: "two parts", token: "abc.def", want: ErrNotAToken},
		{name: "four parts", token: "a.b.c.d", want: ErrNotAToken},
		// Five parts is a JWE, which is well-formed and simply unreadable
		// without the key — a different answer from "malformed".
		{name: "encrypted", token: "a.b.c.d.e", want: ErrTokenEncrypted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.JWTDecode(tc.token); !errors.Is(err, tc.want) {
				t.Errorf("JWTDecode = %v, want %v", err, tc.want)
			}
		})
	}

	if _, err := svc.JWTDecode("!!!.!!!.sig"); err == nil {
		t.Error("JWTDecode accepted segments that are not base64url")
	}
}

// A payload that is not JSON is shown rather than refused: a nested or detached
// token is legal and still worth reading.
func TestJWTDecodeShowsNonJSONPayload(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte("plain text payload"))
	result, err := NewUtilService().JWTDecode(header + "." + payload + ".sig")
	if err != nil {
		t.Fatalf("JWTDecode: %v", err)
	}
	if !strings.Contains(result.Output, "plain text payload") {
		t.Errorf("a non-JSON payload was not shown:\n%s", result.Output)
	}
}

func TestJWTDecodeRespectsTheInputLimit(t *testing.T) {
	if _, err := NewUtilService().JWTDecode(strings.Repeat("a", MaxInputBytes+1)); !errors.Is(err, ErrInputTooLarge) {
		t.Error("JWTDecode ignored the input size limit")
	}
}
