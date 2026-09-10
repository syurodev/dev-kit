package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxInputBytes caps what any util command will accept.
//
// tools.mdx lists this as the NFR the registry does not have yet: a command
// runs on the UI's goroutine and every one of these allocates an output the
// size of its input or larger, so a pasted 200 MB string is a local denial of
// service with no attacker involved. A megabyte is far past what anyone types
// or pastes into a text box and far short of what hurts.
const MaxInputBytes = 1 << 20

// ErrInputTooLarge is returned instead of attempting the conversion.
var ErrInputTooLarge = fmt.Errorf("input exceeds the %d byte limit", MaxInputBytes)

// ErrNotUTF8 is returned when a decode produces bytes that are not text. These
// commands are declared "text in, text out"; handing back a mangled string with
// replacement characters would look like a successful decode.
var ErrNotUTF8 = errors.New("decoded bytes are not valid UTF-8 text")

// UtilService is the pure-function half of Tools: no network, no state, no
// vault. Separate from HashService because tools.mdx gives the group its own
// capability (tools:util), and a type per capability keeps that visible.
type UtilService struct{}

func NewUtilService() *UtilService { return &UtilService{} }

// UtilResult is the shape every util command answers with. One field, because
// the UI renders one output well no matter which command produced it.
type UtilResult struct {
	Output string
}

func checkSize(input string) error {
	if len(input) > MaxInputBytes {
		return ErrInputTooLarge
	}
	return nil
}

// Base64Encode uses standard padded encoding — the one a decoder assumes when
// nobody says otherwise.
func (s *UtilService) Base64Encode(input string) (UtilResult, error) {
	if err := checkSize(input); err != nil {
		return UtilResult{}, err
	}
	return UtilResult{Output: base64.StdEncoding.EncodeToString([]byte(input))}, nil
}

// Base64Decode accepts every spelling in circulation rather than making the
// person work out which one they were handed: standard and URL-safe alphabets,
// padded or not. They are unambiguous — the alphabets differ only in two
// characters, and padding is either there or it is not.
func (s *UtilService) Base64Decode(input string) (UtilResult, error) {
	if err := checkSize(input); err != nil {
		return UtilResult{}, err
	}
	var lastErr error
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(input)
		if err != nil {
			lastErr = err
			continue
		}
		if !utf8.Valid(decoded) {
			return UtilResult{}, ErrNotUTF8
		}
		return UtilResult{Output: string(decoded)}, nil
	}
	return UtilResult{}, fmt.Errorf("not valid base64: %w", lastErr)
}

// UUIDGenerate makes one identifier. v4 is random; v7 leads with a timestamp,
// so a column of them sorts by creation order — which is why a database key
// usually wants v7 and a correlation id does not care.
func (s *UtilService) UUIDGenerate(version string) (UtilResult, error) {
	switch version {
	case "", "v4":
		value, err := uuid.NewRandom()
		if err != nil {
			return UtilResult{}, err
		}
		return UtilResult{Output: value.String()}, nil
	case "v7":
		value, err := uuid.NewV7()
		if err != nil {
			return UtilResult{}, err
		}
		return UtilResult{Output: value.String()}, nil
	default:
		return UtilResult{}, fmt.Errorf("unsupported uuid version %q", version)
	}
}

// URLEncode escapes for a query-string value, which is what "URL encode"
// means in every tool that offers it: a space becomes "+", and everything
// outside the unreserved set becomes a percent triplet.
func (s *UtilService) URLEncode(input string) (UtilResult, error) {
	if err := checkSize(input); err != nil {
		return UtilResult{}, err
	}
	return UtilResult{Output: url.QueryEscape(input)}, nil
}

// URLDecode is the inverse, so it reads "+" as a space.
func (s *UtilService) URLDecode(input string) (UtilResult, error) {
	if err := checkSize(input); err != nil {
		return UtilResult{}, err
	}
	decoded, err := url.QueryUnescape(input)
	if err != nil {
		return UtilResult{}, err
	}
	return UtilResult{Output: decoded}, nil
}

// JSONFormat indents the document without reordering it.
//
// json.Indent works on the raw bytes; unmarshalling into a map and marshalling
// back would sort every object's keys alphabetically and turn integers into
// floats, so a formatted file would no longer diff against its source.
func (s *UtilService) JSONFormat(input string) (UtilResult, error) {
	if err := checkSize(input); err != nil {
		return UtilResult{}, err
	}
	if !json.Valid([]byte(input)) {
		// Decode again purely to get the position of the fault: json.Valid
		// answers yes or no, and "invalid JSON" with no offset is a worse
		// message than the one the decoder already knows how to write.
		var probe json.RawMessage
		if err := json.Unmarshal([]byte(input), &probe); err != nil {
			return UtilResult{}, err
		}
		return UtilResult{}, errors.New("not valid JSON")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(input), "", "  "); err != nil {
		return UtilResult{}, err
	}
	return UtilResult{Output: out.String()}, nil
}
