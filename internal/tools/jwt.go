package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTokenEncrypted means the token is a JWE — five parts, with the payload
// encrypted to a key. Reported separately from a malformed token because the
// answer is different: this one is well-formed and simply cannot be read
// without the key.
var ErrTokenEncrypted = errors.New("this is an encrypted token (JWE); its payload cannot be read without the decryption key")

// ErrNotAToken means the input is not three dot-separated parts.
var ErrNotAToken = errors.New("not a JWT: expected three dot-separated parts (header.payload.signature)")

// jwtClock is swapped in tests. Relative times ("expired 2 hours ago") are the
// most useful line in the output and the only one that depends on now, so the
// dependency is injected rather than reached for.
type jwtClock func() time.Time

// registered claims that carry a NumericDate, in the order RFC 7519 lists them.
var timeClaims = []struct {
	name string
	what string
}{
	{name: "iat", what: "issued at"},
	{name: "nbf", what: "not before"},
	{name: "exp", what: "expires"},
}

// JWTDecode reads a token and reports what is inside it.
//
// It decodes; it does NOT verify. Verifying needs the signing key, which this
// tool never asks for and never has — so the output says so in as many words.
// A decoder that prints claims without that line invites the reader to trust
// a token that anyone could have written: the header and payload are base64,
// not encryption, and rewriting them takes a text editor.
func (s *UtilService) JWTDecode(token string) (UtilResult, error) {
	return s.jwtDecode(token, time.Now)
}

func (s *UtilService) jwtDecode(token string, now jwtClock) (UtilResult, error) {
	if err := checkSize(token); err != nil {
		return UtilResult{}, err
	}
	token = strings.TrimSpace(token)
	// A pasted Authorization header is the common case and costs one line to
	// accept, rather than making someone trim it and try again.
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimSpace(token)

	parts := strings.Split(token, ".")
	if len(parts) == 5 {
		return UtilResult{}, ErrTokenEncrypted
	}
	if len(parts) != 3 {
		return UtilResult{}, ErrNotAToken
	}

	header, err := decodeSegment(parts[0], "header")
	if err != nil {
		return UtilResult{}, err
	}
	payload, err := decodeSegment(parts[1], "payload")
	if err != nil {
		return UtilResult{}, err
	}

	var out strings.Builder
	out.WriteString("HEADER\n")
	out.WriteString(indentOrRaw(header))
	out.WriteString("\n\nPAYLOAD\n")
	out.WriteString(indentOrRaw(payload))

	if times := describeTimeClaims(payload, now()); times != "" {
		out.WriteString("\n\nTIMES\n")
		out.WriteString(times)
	}

	out.WriteString("\n\nSIGNATURE — NOT VERIFIED\n")
	out.WriteString(parts[2])
	out.WriteString("\n")
	out.WriteString("Everything above is base64, not encryption: anyone can read it, and\n")
	out.WriteString("anyone can write it. Checking it needs the signing key, which this tool\n")
	out.WriteString("does not have. Do not trust these claims on the strength of this output.")

	if alg := headerAlg(header); strings.EqualFold(alg, "none") {
		out.WriteString("\n\nThe header says alg \"none\", which means the token carries no signature\nat all. A server that accepts it accepts anything.")
	}

	return UtilResult{Output: out.String()}, nil
}

// decodeSegment reads one base64url segment. JWT segments are unpadded by spec,
// but padded ones turn up in the wild often enough that refusing them would
// only make the person strip the "=" themselves.
func decodeSegment(segment, what string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding} {
		if decoded, err := encoding.DecodeString(segment); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("the %s is not valid base64url", what)
}

// indentOrRaw pretty-prints JSON, or hands back the text when it is not JSON.
// A JWS payload is JSON by spec, but a nested or detached token is not, and
// showing what is actually there beats refusing to show anything.
func indentOrRaw(segment []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, segment, "", "  "); err != nil {
		return string(segment)
	}
	return buf.String()
}

func headerAlg(header []byte) string {
	var parsed struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(header, &parsed); err != nil {
		return ""
	}
	return parsed.Alg
}

// describeTimeClaims renders the NumericDate claims as dates, with how far from
// now they are — which is the question anyone opening a token is asking.
func describeTimeClaims(payload []byte, now time.Time) string {
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	var lines []string
	for _, claim := range timeClaims {
		raw, ok := claims[claim.name]
		if !ok {
			continue
		}
		// JSON numbers decode to float64; a NumericDate is seconds since the
		// epoch and fits exactly, so the conversion is lossless in this range.
		seconds, ok := raw.(float64)
		if !ok {
			continue
		}
		at := time.Unix(int64(seconds), 0).UTC()
		lines = append(lines, fmt.Sprintf("%-4s %-9s %s  (%s)",
			claim.name, claim.what, at.Format(time.RFC3339), relative(at, now, claim.name)))
	}
	return strings.Join(lines, "\n")
}

// relative words the distance from now, and for exp says whether it has passed
// rather than leaving the reader to compare two timestamps.
func relative(at, now time.Time, claim string) string {
	delta := at.Sub(now)
	past := delta < 0
	if past {
		delta = -delta
	}
	magnitude := humanDuration(delta)
	switch {
	case claim == "exp" && past:
		return "EXPIRED " + magnitude + " ago"
	case claim == "exp":
		return "valid for another " + magnitude
	case claim == "nbf" && !past:
		return "NOT VALID YET, for another " + magnitude
	case past:
		return magnitude + " ago"
	default:
		return "in " + magnitude
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return plural(int(d.Seconds()), "second")
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	default:
		// Past a year, days stop being readable: "3157 days ago" makes the
		// reader divide, and the question was only ever how stale it is.
		years := int(d.Hours() / 24 / 365)
		remainder := int(d.Hours()/24) - years*365
		if remainder < 30 {
			return plural(years, "year")
		}
		return plural(years, "year") + " " + plural(remainder/30, "month")
	}
}
