package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBase64RoundTrip(t *testing.T) {
	svc := NewUtilService()
	for _, input := range []string{"", "hello", "xin chào", "a\nb\tc", "🔑 emoji"} {
		encoded, err := svc.Base64Encode(input)
		if err != nil {
			t.Fatalf("Base64Encode(%q): %v", input, err)
		}
		decoded, err := svc.Base64Decode(encoded.Output)
		if err != nil {
			t.Fatalf("Base64Decode(%q): %v", encoded.Output, err)
		}
		if decoded.Output != input {
			t.Errorf("round trip of %q gave %q", input, decoded.Output)
		}
	}
}

// Every spelling in circulation decodes, so nobody has to work out which
// variant they were handed.
func TestBase64DecodeAcceptsEveryAlphabet(t *testing.T) {
	svc := NewUtilService()
	// "ab~cd?" encodes differently under the standard and URL-safe alphabets.
	for _, encoded := range []string{
		"aGVsbG8=", // standard, padded
		"aGVsbG8",  // standard, unpadded
		"8J-Ukg==", // URL-safe, padded
		"8J-Ukg",   // URL-safe, unpadded
	} {
		if _, err := svc.Base64Decode(encoded); err != nil {
			t.Errorf("Base64Decode(%q): %v", encoded, err)
		}
	}
}

func TestBase64DecodeRejectsJunkAndBinary(t *testing.T) {
	svc := NewUtilService()
	if _, err := svc.Base64Decode("not base64 at all!!"); err == nil {
		t.Error("Base64Decode accepted junk")
	}
	// Valid base64 whose bytes are not text: reported rather than returned as
	// a string full of replacement characters.
	if _, err := svc.Base64Decode("/w=="); !errors.Is(err, ErrNotUTF8) {
		t.Errorf("Base64Decode of binary = %v, want ErrNotUTF8", err)
	}
}

func TestUUIDGenerate(t *testing.T) {
	svc := NewUtilService()
	tests := []struct {
		version string
		want    uuid.Version
	}{
		{version: "", want: 4}, // the default
		{version: "v4", want: 4},
		{version: "v7", want: 7},
	}
	for _, tc := range tests {
		result, err := svc.UUIDGenerate(tc.version)
		if err != nil {
			t.Fatalf("UUIDGenerate(%q): %v", tc.version, err)
		}
		parsed, err := uuid.Parse(result.Output)
		if err != nil {
			t.Fatalf("UUIDGenerate(%q) produced %q, which does not parse: %v", tc.version, result.Output, err)
		}
		if parsed.Version() != tc.want {
			t.Errorf("UUIDGenerate(%q) version = %v, want %v", tc.version, parsed.Version(), tc.want)
		}
	}
	if _, err := svc.UUIDGenerate("v9"); err == nil {
		t.Error("UUIDGenerate accepted an unsupported version")
	}
}

// Two calls must not return the same value; a generator that repeats is worse
// than one that errors.
func TestUUIDGenerateIsUnique(t *testing.T) {
	svc := NewUtilService()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		result, err := svc.UUIDGenerate("v7")
		if err != nil {
			t.Fatalf("UUIDGenerate: %v", err)
		}
		if seen[result.Output] {
			t.Fatalf("UUIDGenerate repeated %q", result.Output)
		}
		seen[result.Output] = true
	}
}

func TestURLRoundTrip(t *testing.T) {
	svc := NewUtilService()
	for _, input := range []string{"a b&c=d", "xin chào?x=1", "100%", "/path/to/thing"} {
		encoded, err := svc.URLEncode(input)
		if err != nil {
			t.Fatalf("URLEncode(%q): %v", input, err)
		}
		decoded, err := svc.URLDecode(encoded.Output)
		if err != nil {
			t.Fatalf("URLDecode(%q): %v", encoded.Output, err)
		}
		if decoded.Output != input {
			t.Errorf("round trip of %q gave %q", input, decoded.Output)
		}
	}
	if _, err := svc.URLDecode("%zz"); err == nil {
		t.Error("URLDecode accepted an invalid escape")
	}
}

func TestJSONFormat(t *testing.T) {
	svc := NewUtilService()
	result, err := svc.JSONFormat(`{"b":1,"a":[2,3]}`)
	if err != nil {
		t.Fatalf("JSONFormat: %v", err)
	}
	want := "{\n  \"b\": 1,\n  \"a\": [\n    2,\n    3\n  ]\n}"
	if result.Output != want {
		t.Errorf("JSONFormat gave\n%s\nwant\n%s", result.Output, want)
	}
}

// The reason json.Indent is used instead of unmarshal/marshal: those would
// sort keys and turn a large integer into a float in exponent notation, so the
// formatted document would no longer say what the source said.
func TestJSONFormatPreservesOrderAndNumbers(t *testing.T) {
	svc := NewUtilService()
	result, err := svc.JSONFormat(`{"zebra":1,"apple":2,"big":12345678901234567890}`)
	if err != nil {
		t.Fatalf("JSONFormat: %v", err)
	}
	if strings.Index(result.Output, "zebra") > strings.Index(result.Output, "apple") {
		t.Errorf("JSONFormat reordered keys:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "12345678901234567890") {
		t.Errorf("JSONFormat rewrote a large integer:\n%s", result.Output)
	}
}

func TestJSONFormatRejectsInvalid(t *testing.T) {
	svc := NewUtilService()
	if _, err := svc.JSONFormat(`{"a":}`); err == nil {
		t.Error("JSONFormat accepted invalid JSON")
	}
}

// The NFR tools.mdx asks for: every command that takes input refuses an
// oversized one rather than allocating for it.
func TestInputSizeLimit(t *testing.T) {
	svc := NewUtilService()
	huge := strings.Repeat("a", MaxInputBytes+1)
	calls := map[string]func(string) (UtilResult, error){
		"Base64Encode": svc.Base64Encode,
		"Base64Decode": svc.Base64Decode,
		"URLEncode":    svc.URLEncode,
		"URLDecode":    svc.URLDecode,
		"JSONFormat":   svc.JSONFormat,
	}
	for name, call := range calls {
		if _, err := call(huge); !errors.Is(err, ErrInputTooLarge) {
			t.Errorf("%s on an oversized input = %v, want ErrInputTooLarge", name, err)
		}
	}
	// Exactly at the limit is allowed: the cap is a ceiling, not a fence.
	if _, err := svc.Base64Encode(strings.Repeat("a", MaxInputBytes)); err != nil {
		t.Errorf("Base64Encode at exactly the limit: %v", err)
	}
}
