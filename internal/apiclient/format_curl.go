package apiclient

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// curlFormat writes a collection as a shell script: one function per request,
// run by name (`./shop.sh create_order`), so nothing is sent by running the
// file itself. Variables become shell variables — the collection's plain ones
// as overridable defaults, everything else expected from the environment —
// and `set -u` names any that is missing instead of sending an empty one.
//
// It writes only. Reading curl goes through ParseCurl, which opens a request
// to edit rather than saving one: a pasted command is a draft, not an import.
type curlFormat struct{}

func (curlFormat) Info() FormatInfo {
	return FormatInfo{ID: "curl", Label: "cURL script", Extension: "sh", Exports: []ExportKind{ExportCollection}}
}

var shellUnsafe = regexp.MustCompile(`[^A-Za-z0-9_]`)

// shellName is a variable name a shell accepts.
func shellName(name string) string {
	out := shellUnsafe.ReplaceAllString(name, "_")
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "_" + out
	}
	return out
}

// shellQuoteVars single-quotes text and lets each {{name}} out of the quotes
// as "${name}", so the shell fills it and nothing else is expanded.
func shellQuoteVars(text string, used map[string]bool) string {
	var b strings.Builder
	b.WriteString("'")
	last := 0
	for _, loc := range varPattern.FindAllStringSubmatchIndex(text, -1) {
		b.WriteString(strings.ReplaceAll(text[last:loc[0]], "'", `'\''`))
		name := text[loc[2]:loc[3]]
		used[name] = true
		b.WriteString(`'"${` + shellName(name) + `}"'`)
		last = loc[1]
	}
	b.WriteString(strings.ReplaceAll(text[last:], "'", `'\''`))
	b.WriteString("'")
	return b.String()
}

// escapeOutsideVars applies escape to the literal parts of text, leaving each
// {{name}} as written for the shell to fill.
func escapeOutsideVars(text string, escape func(string) string) string {
	var b strings.Builder
	last := 0
	for _, loc := range varPattern.FindAllStringIndex(text, -1) {
		b.WriteString(escape(text[last:loc[0]]))
		b.WriteString(text[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(escape(text[last:]))
	return b.String()
}

// scriptURL is the address with :path variables and the query filled in, the
// literal parts encoded and every {{name}} left for the shell.
func scriptURL(req Request) string {
	address := req.URL
	values := map[string]string{}
	for _, row := range req.PathVars.Active() {
		values[row.Key] = row.Value
	}
	if len(values) > 0 {
		scheme, rest := "", address
		if at := strings.Index(rest, "://"); at >= 0 {
			scheme, rest = rest[:at+3], rest[at+3:]
		}
		segments := strings.Split(rest, "/")
		for i := 1; i < len(segments); i++ {
			if len(segments[i]) > 1 && segments[i][0] == ':' {
				if value := values[segments[i][1:]]; value != "" {
					segments[i] = escapeOutsideVars(value, url.PathEscape)
				}
			}
		}
		address = scheme + strings.Join(segments, "/")
	}
	var query []string
	for _, row := range req.Query.Active() {
		query = append(query, escapeOutsideVars(row.Key, url.QueryEscape)+"="+escapeOutsideVars(row.Value, url.QueryEscape))
	}
	if len(query) > 0 {
		sep := "?"
		if strings.Contains(address, "?") {
			sep = "&"
		}
		address += sep + strings.Join(query, "&")
	}
	return address
}

func scriptCommand(br BundleRequest, used map[string]bool) string {
	req := br.Request.Request().normalized()
	var b strings.Builder
	fmt.Fprintf(&b, "curl -sS -X %s %s", req.Method, shellQuoteVars(scriptURL(req), used))
	hasContentType := false
	for _, row := range req.Headers.Active() {
		if strings.EqualFold(row.Key, "Content-Type") {
			hasContentType = true
		}
		fmt.Fprintf(&b, " \\\n    -H %s", shellQuoteVars(row.Key+": "+row.Value, used))
	}
	if ct := contentTypeFor(req.BodyMode); ct != "" && !hasContentType && req.BodyMode != BodyURLEncoded {
		fmt.Fprintf(&b, " \\\n    -H %s", shellQuote("Content-Type: "+ct))
	}
	if c := br.Credential; c != nil {
		switch c.Kind {
		case AuthBasic:
			fmt.Fprintf(&b, " \\\n    -u %s", shellQuoteVars("{{"+c.UserVar+"}}:{{"+c.PasswordVar+"}}", used))
		case AuthHeader:
			fmt.Fprintf(&b, " \\\n    -H %s", shellQuoteVars(nonEmpty(c.Header, "X-API-Key")+": {{"+c.TokenVar+"}}", used))
		default:
			fmt.Fprintf(&b, " \\\n    -H %s", shellQuoteVars("Authorization: Bearer {{"+c.TokenVar+"}}", used))
		}
	}
	switch req.BodyMode {
	case BodyURLEncoded:
		for _, row := range req.Form.Active() {
			fmt.Fprintf(&b, " \\\n    --data-urlencode %s", shellQuoteVars(row.Key+"="+row.Value, used))
		}
	case BodyMultipart:
		for _, row := range req.Form.Active() {
			fmt.Fprintf(&b, " \\\n    --form-string %s", shellQuoteVars(row.Key+"="+row.Value, used))
		}
	case BodyNone:
	default:
		if req.Body != "" {
			fmt.Fprintf(&b, " \\\n    --data-raw %s", shellQuoteVars(req.Body, used))
		}
	}
	return b.String()
}

func (curlFormat) Encode(kind ExportKind, bundle Bundle, report *Report) ([]byte, error) {
	if kind != ExportCollection || len(bundle.Collections) != 1 {
		return nil, ErrFormatCannot
	}
	root := bundle.Collections[0]

	// Defaults are the collection's plain variables, root first so a folder's
	// own value wins — the same order a send applies them in. A folder that
	// overrides its parent flattens to one value for the whole script, which
	// is said.
	defaults := map[string]string{}
	secrets := map[string]bool{}
	overridden := 0
	var collect func(c BundleCollection)
	collect = func(c BundleCollection) {
		for _, v := range c.Vars {
			if v.Disabled {
				continue
			}
			if v.Secret {
				secrets[v.Key] = true
				continue
			}
			if old, ok := defaults[v.Key]; ok && old != v.Value {
				overridden++
			}
			defaults[v.Key] = v.Value
		}
		for _, folder := range c.Folders {
			collect(folder)
		}
	}
	collect(root)
	if overridden > 0 {
		report.Note(fmt.Sprintf("%d folder variable(s) override a parent's value; a script has one value per name, so the deepest folder's is used for every request.", overridden))
	}

	used := map[string]bool{}
	var functions strings.Builder
	var names []string
	taken := map[string]bool{}
	var walk func(c BundleCollection, path []string)
	walk = func(c BundleCollection, path []string) {
		for _, br := range c.Requests {
			fn := slug(br.Request.Name)
			if fn == "" || (fn[0] >= '0' && fn[0] <= '9') {
				fn = "request_" + fn
			}
			for i := 2; taken[fn]; i++ {
				fn = fmt.Sprintf("%s_%d", strings.TrimRight(fn, "_0123456789"), i)
			}
			taken[fn] = true
			names = append(names, fn)
			fmt.Fprintf(&functions, "\n# %s\n%s() {\n  %s\n}\n",
				strings.Join(append(append([]string{}, path...), br.Request.Name), " / "),
				fn, scriptCommand(br, used))
		}
		for _, folder := range c.Folders {
			walk(folder, append(path, folder.Name))
		}
	}
	walk(root, []string{root.Name})

	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	fmt.Fprintf(&b, "# %s — exported from DevKit.\n", root.Name)
	b.WriteString("# Run one request by name: ./this-file <request>. Running the file alone sends nothing.\n")
	b.WriteString("set -euo pipefail\n")

	var plainUsed, required []string
	for name := range used {
		if _, ok := defaults[name]; ok && !secrets[name] {
			plainUsed = append(plainUsed, name)
		} else {
			required = append(required, name)
		}
	}
	sort.Strings(plainUsed)
	sort.Strings(required)
	if len(plainUsed) > 0 {
		b.WriteString("\n# Collection variables. Override any by exporting it first.\n")
		for _, name := range plainUsed {
			fmt.Fprintf(&b, "[ -n \"${%s+x}\" ] || %s=%s\n", shellName(name), shellName(name), shellQuote(defaults[name]))
		}
	}
	if len(required) > 0 {
		b.WriteString("\n# Set these before running — secrets, accounts and environment variables are not written here:\n")
		for _, name := range required {
			fmt.Fprintf(&b, "#   export %s=...\n", shellName(name))
		}
	}
	b.WriteString(functions.String())
	b.WriteString("\nif [ $# -eq 0 ] || ! declare -F \"$1\" >/dev/null; then\n")
	b.WriteString("  echo \"usage: $0 <request>\" >&2\n")
	fmt.Fprintf(&b, "  printf '  %%s\\n' %s >&2\n", strings.Join(names, " "))
	b.WriteString("  exit 2\nfi\n\"$@\"\n")
	return []byte(b.String()), nil
}
