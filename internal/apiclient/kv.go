package apiclient

import (
	"bytes"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

// KV is one row of a key/value table: a query parameter, a header, a form
// field, a path variable.
//
// Disabled rather than Enabled so the zero value is a row that counts: an
// agent, an import or a request saved before rows could be switched off
// writes no flag at all, and every one of those means "send it".
type KV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
}

// KVList is an ordered table. A map was the first shape, and it could not say
// three things a person types every day: `ids=1&ids=2`, the order headers go
// out in, and "keep this row but do not send it for now".
type KVList []KV

// UnmarshalJSON reads both the table and the map it replaced, so a request
// saved before the change opens as the same request — every row enabled, in
// key order, which is the order the map was shown in.
func (l *KVList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*l = nil
		return nil
	}
	if trimmed[0] == '{' {
		var legacy map[string]string
		if err := json.Unmarshal(trimmed, &legacy); err != nil {
			return err
		}
		*l = KVFromMap(legacy)
		return nil
	}
	var rows []KV
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return err
	}
	*l = rows
	return nil
}

// KVFromMap turns a map into rows in key order.
func KVFromMap(m map[string]string) KVList {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(KVList, 0, len(keys))
	for _, key := range keys {
		out = append(out, KV{Key: key, Value: m[key]})
	}
	return out
}

// Active is the rows that go out: enabled and named. A row with no key is the
// empty line an editor keeps at the bottom of every table, not a parameter.
func (l KVList) Active() KVList {
	out := make(KVList, 0, len(l))
	for _, row := range l {
		if row.Disabled || strings.TrimSpace(row.Key) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

// Lookup finds the first active row named key. Header names are compared
// without case, the way HTTP compares them; everything else exactly.
func (l KVList) Lookup(key string, foldCase bool) (string, bool) {
	for _, row := range l.Active() {
		if row.Key == key || (foldCase && strings.EqualFold(row.Key, key)) {
			return row.Value, true
		}
	}
	return "", false
}

// encodeQuery renders rows as a query string in their own order, which
// url.Values cannot do: it is a map and Encode sorts it.
func encodeQuery(rows KVList, vars map[string]string) string {
	var parts []string
	for _, row := range rows.Active() {
		key := url.QueryEscape(Interpolate(row.Key, vars))
		value := url.QueryEscape(Interpolate(row.Value, vars))
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "&")
}

// Masked is what a redacted value reads as.
const Masked = "••••••"

// sensitiveName says whether a header, query parameter or form field is named
// like something that carries a credential.
func sensitiveName(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	}
	for _, part := range []string{"token", "secret", "password", "passwd", "api-key", "apikey", "api_key", "signature", "session"} {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// redacted masks the value of every row named like a credential, unless the
// value is only a {{variable}} — that names where the value lives, which is
// not the value.
func (l KVList) redacted() KVList {
	if l == nil {
		return nil
	}
	out := make(KVList, len(l))
	for i, row := range l {
		out[i] = row
		if sensitiveName(row.Key) && row.Value != "" && !onlyVariables(row.Value) {
			out[i].Value = Masked
		}
	}
	return out
}

func onlyVariables(value string) bool {
	rest := strings.TrimSpace(varPattern.ReplaceAllString(value, ""))
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rest, "Bearer"), "Basic"))
	return rest == ""
}

// ForAgent is a request as an agent may read it. The account binding is an id
// and was always safe; what is not is a token somebody typed into a header
// row, which is the one credential the binding cannot keep out of a request.
func (r Request) ForAgent() Request {
	r.Headers = r.Headers.redacted()
	r.Query = r.Query.redacted()
	r.Form = r.Form.redacted()
	return r
}

// ForAgent is a response as an agent may read it: a session cookie the server
// set is a credential, whoever asked.
func (r Response) ForAgent() Response {
	r.HeaderList = r.HeaderList.redacted()
	if r.Headers != nil {
		headers := make(map[string]string, len(r.Headers))
		for name, value := range r.Headers {
			if sensitiveName(name) {
				value = Masked
			}
			headers[name] = value
		}
		r.Headers = headers
	}
	return r
}
