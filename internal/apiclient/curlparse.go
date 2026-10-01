package apiclient

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// ErrNotCurl is text that is not a curl command.
var ErrNotCurl = errors.New("apiclient.notCurl")

// ParseCurl reads a curl command — what a browser's "Copy as cURL", an API's
// docs or a colleague hands over — into a request an editor can open.
//
// It reads the flags that describe a request and skips the ones that describe
// curl's own behaviour (-s, -L, --compressed, -k, -o …). It runs nothing and
// expands nothing: `$TOKEN` stays the text `$TOKEN`.
//
// A -u user:password becomes an Authorization header, which is where Postman
// puts it too. The request's payload is sealed, but it is a value on the
// request rather than an account binding; binding it is one click in Auth.
func ParseCurl(command string) (RequestInput, error) {
	words, err := shellWords(command)
	if err != nil {
		return RequestInput{}, err
	}
	if len(words) == 0 || (words[0] != "curl" && !strings.HasSuffix(words[0], "/curl")) {
		return RequestInput{}, ErrNotCurl
	}

	var (
		in        RequestInput
		rawURL    string
		data      []string
		urlencode []string
		form      KVList
		getMode   bool
	)
	// Flags that take a value, long and short. Anything else starting with a
	// dash is a switch and is skipped.
	takesValue := map[string]bool{
		"-X": true, "--request": true, "-H": true, "--header": true,
		"-d": true, "--data": true, "--data-raw": true, "--data-binary": true, "--data-ascii": true,
		"--data-urlencode": true, "-F": true, "--form": true, "--form-string": true,
		"-u": true, "--user": true, "-b": true, "--cookie": true, "-A": true, "--user-agent": true,
		"-e": true, "--referer": true, "--url": true, "-o": true, "--output": true,
		"-m": true, "--max-time": true, "--connect-timeout": true, "-x": true, "--proxy": true,
		"--cacert": true, "--cert": true, "--key": true, "-w": true, "--write-out": true,
		"--retry": true, "-r": true, "--range": true, "--resolve": true,
	}
	for i := 1; i < len(words); i++ {
		word := words[i]
		flag, value, hasValue := word, "", false
		switch {
		case strings.HasPrefix(word, "--") && strings.Contains(word, "="):
			cut := strings.IndexByte(word, '=')
			flag, value, hasValue = word[:cut], word[cut+1:], true
		case len(word) > 2 && word[0] == '-' && word[1] != '-' && takesValue[word[:2]]:
			// -XPOST, -H'Accept: x' — a short flag with its value attached.
			flag, value, hasValue = word[:2], word[2:], true
		}
		if strings.HasPrefix(flag, "-") && takesValue[flag] && !hasValue {
			if i+1 >= len(words) {
				break
			}
			i++
			value = words[i]
		}

		switch flag {
		case "-X", "--request":
			in.Method = strings.ToUpper(value)
		case "-H", "--header":
			if key, val, ok := strings.Cut(value, ":"); ok {
				in.Headers = append(in.Headers, KV{Key: strings.TrimSpace(key), Value: strings.TrimSpace(val)})
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			data = append(data, value)
		case "--data-urlencode":
			urlencode = append(urlencode, value)
		case "-F", "--form", "--form-string":
			if key, val, ok := strings.Cut(value, "="); ok {
				form = append(form, KV{Key: key, Value: val})
			}
		case "-u", "--user":
			in.Headers = append(in.Headers, KV{
				Key:   "Authorization",
				Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(value)),
			})
		case "-b", "--cookie":
			in.Headers = append(in.Headers, KV{Key: "Cookie", Value: value})
		case "-A", "--user-agent":
			in.Headers = append(in.Headers, KV{Key: "User-Agent", Value: value})
		case "-e", "--referer":
			in.Headers = append(in.Headers, KV{Key: "Referer", Value: value})
		case "--url":
			rawURL = value
		case "-G", "--get":
			getMode = true
		case "-I", "--head":
			in.Method = "HEAD"
		default:
			if !strings.HasPrefix(word, "-") && rawURL == "" {
				rawURL = word
			}
		}
	}
	if rawURL == "" {
		return RequestInput{}, ErrNotCurl
	}

	base, query := splitQuery(rawURL)
	in.URL = base
	in.Query = query
	contentType, _ := in.Headers.Lookup("Content-Type", true)
	contentType = strings.ToLower(contentType)

	switch {
	case getMode:
		// -G moves the data into the query, the way curl does.
		for _, chunk := range append(data, urlencode...) {
			in.Query = append(in.Query, formRows(chunk)...)
		}
	case len(form) > 0:
		in.BodyMode, in.Form = BodyMultipart, form
		// The boundary is the sender's to choose, so a copied one is wrong.
		in.Headers = withoutHeader(in.Headers, "Content-Type")
	case len(urlencode) > 0 && len(data) == 0:
		in.BodyMode = BodyURLEncoded
		for _, chunk := range urlencode {
			in.Form = append(in.Form, formRows(chunk)...)
		}
	case len(data) > 0:
		body := strings.Join(data, "&")
		switch {
		case strings.Contains(contentType, "json") || (contentType == "" && json.Valid([]byte(body))):
			in.BodyMode, in.Body = BodyJSON, body
		case strings.Contains(contentType, "xml"):
			in.BodyMode, in.Body = BodyXML, body
		case (contentType == "" || strings.Contains(contentType, "x-www-form-urlencoded")) && looksLikeForm(body):
			in.BodyMode, in.Form = BodyURLEncoded, formRows(body)
		default:
			in.BodyMode, in.Body = BodyText, body
		}
	}
	if in.Method == "" {
		in.Method = "GET"
		if !getMode && (len(data) > 0 || len(urlencode) > 0 || len(form) > 0) {
			in.Method = "POST"
		}
	}
	in.Name = in.Method + " " + pathOf(base)
	return in, nil
}

// splitQuery separates an address from its query, the query as rows.
func splitQuery(raw string) (string, KVList) {
	base, query, found := strings.Cut(raw, "?")
	if !found {
		return raw, nil
	}
	if fragment := strings.IndexByte(query, '#'); fragment >= 0 {
		query = query[:fragment]
	}
	return base, formRows(query)
}

// formRows reads k=v&k=v into rows, unescaping each side.
func formRows(encoded string) KVList {
	var rows KVList
	for _, pair := range strings.Split(encoded, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		if k, err := url.QueryUnescape(key); err == nil {
			key = k
		}
		if v, err := url.QueryUnescape(value); err == nil {
			value = v
		}
		rows = append(rows, KV{Key: key, Value: value})
	}
	return rows
}

func looksLikeForm(body string) bool {
	if body == "" || strings.ContainsAny(body, " \n\t{}[]<>\"") {
		return false
	}
	for _, pair := range strings.Split(body, "&") {
		if !strings.Contains(pair, "=") {
			return false
		}
	}
	return true
}

func withoutHeader(rows KVList, name string) KVList {
	out := rows[:0:0]
	for _, row := range rows {
		if !strings.EqualFold(row.Key, name) {
			out = append(out, row)
		}
	}
	return out
}

func pathOf(raw string) string {
	rest := raw
	if at := strings.Index(rest, "://"); at >= 0 {
		rest = rest[at+3:]
	}
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return rest[slash:]
	}
	return "/"
}

// shellWords splits a command the way a POSIX shell would, without expanding
// anything: single quotes, double quotes with their four escapes, bash's
// $'…' (what Chrome's "Copy as cURL" writes for anything with a newline),
// backslash escapes and line continuations. Windows cmd's ^ is not read.
func shellWords(s string) ([]string, error) {
	var (
		words   []string
		current strings.Builder
		inWord  bool
	)
	flush := func() {
		if inWord {
			words = append(words, current.String())
			current.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && (s[i+1] == '\n' || s[i+1] == '\r'):
			// A line continuation joins lines; it is not a character.
			i++
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		case c == '\\' && i+1 < len(s):
			current.WriteByte(s[i+1])
			inWord = true
			i++
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, ErrNotCurl
			}
			current.WriteString(s[i+1 : i+1+end])
			inWord = true
			i += end + 1
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			j := i + 2
			for ; j < len(s) && s[j] != '\''; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
					switch s[j] {
					case 'n':
						current.WriteByte('\n')
					case 't':
						current.WriteByte('\t')
					case 'r':
						current.WriteByte('\r')
					default:
						current.WriteByte(s[j])
					}
					continue
				}
				current.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, ErrNotCurl
			}
			inWord = true
			i = j
		case c == '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) && strings.IndexByte("\"\\$`\n", s[j+1]) >= 0 {
					j++
					if s[j] == '\n' {
						continue
					}
				}
				current.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, ErrNotCurl
			}
			inWord = true
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			current.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return words, nil
}
