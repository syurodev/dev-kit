package workspace

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SpringConfig is what one service's own Spring Boot config says about it,
// read from the handful of files at fixed paths — auto-discovery step 2 in
// workspace.mdx, and the property source the cross-service resolver folds
// "${billing.url}" against. Default profile only: which profile runs where
// is a deployment fact this machine does not know, and guessing one would
// produce an edge that looks certain and is not.
type SpringConfig struct {
	// Props is every key the files set, flattened to Spring's dotted form
	// (spring.application.name, clients.billing.url, list[0]).
	Props map[string]string
	// Files lists the files the props came from, in the order applied.
	Files []string
}

// springConfigFiles are read in this order, a later file overriding an
// earlier one — the same order Spring gives them: bootstrap (Spring Cloud's
// parent context, where spring.application.name often lives) under
// application, and .properties over .yml at the same location.
var springConfigFiles = []string{
	"bootstrap.yml", "bootstrap.yaml", "bootstrap.properties",
	"application.yml", "application.yaml", "application.properties",
}

// ReadSpringConfig reads servicePath/src/main/resources. A service without
// any of the files is not an error: it returns an empty config, which
// resolves nothing and names nothing — the folder name stays.
//
// A file that exists but does not parse IS an error, reported with its path,
// so the caller can decide to keep the folder name rather than half a config.
func ReadSpringConfig(servicePath string) (SpringConfig, error) {
	cfg := SpringConfig{Props: map[string]string{}}
	dir := filepath.Join(servicePath, "src", "main", "resources")
	for _, name := range springConfigFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return SpringConfig{}, err
		}
		var props map[string]string
		if strings.HasSuffix(name, ".properties") {
			props, err = parseProperties(data)
		} else {
			props, err = parseSpringYAML(data)
		}
		if err != nil {
			return SpringConfig{}, fmt.Errorf("%s: %w", path, err)
		}
		for k, v := range props {
			cfg.Props[k] = v
		}
		cfg.Files = append(cfg.Files, path)
	}
	return cfg, nil
}

// ApplicationName is spring.application.name with its placeholders folded,
// or "" when it is unset or cannot be folded to a literal.
func (c SpringConfig) ApplicationName() string {
	raw, ok := c.Props["spring.application.name"]
	if !ok {
		return ""
	}
	name, complete := c.Resolve(raw)
	if !complete {
		return ""
	}
	return strings.TrimSpace(name)
}

// ServerPort is server.port folded, or "8080" — Spring Boot's default — when
// the config does not set it. "" when it is set to something that does not
// fold to a number (an unresolved placeholder, "0" for a random port).
func (c SpringConfig) ServerPort() string {
	raw, ok := c.Props["server.port"]
	if !ok {
		return "8080"
	}
	port, complete := c.Resolve(raw)
	if !complete {
		return ""
	}
	port = strings.TrimSpace(port)
	if n, err := strconv.Atoi(port); err != nil || n <= 0 {
		return ""
	}
	return port
}

// maxPlaceholderDepth bounds nested and chained placeholders: a key whose
// value refers back to itself would otherwise never finish.
const maxPlaceholderDepth = 8

// Resolve folds every ${key} and ${key:default} in s against the config.
// complete is false when any placeholder was left as written — a key the
// config does not set and that carries no default, typically an environment
// variable only the deployment provides. What was folded is still folded.
func (c SpringConfig) Resolve(s string) (string, bool) {
	return c.resolve(s, 0)
}

func (c SpringConfig) resolve(s string, depth int) (string, bool) {
	if depth > maxPlaceholderDepth {
		return s, false
	}
	var b strings.Builder
	complete := true
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			return b.String(), complete
		}
		end := matchingBrace(s, start+2)
		if end < 0 {
			b.WriteString(s)
			return b.String(), false
		}
		b.WriteString(s[:start])
		inner := s[start+2 : end]
		key, def, hasDefault := splitPlaceholder(inner)
		if v, ok := c.Props[key]; ok {
			folded, ok := c.resolve(v, depth+1)
			complete = complete && ok
			b.WriteString(folded)
		} else if hasDefault {
			folded, ok := c.resolve(def, depth+1)
			complete = complete && ok
			b.WriteString(folded)
		} else {
			complete = false
			b.WriteString(s[start : end+1])
		}
		s = s[end+1:]
	}
}

// matchingBrace finds the "}" closing a placeholder whose body starts at
// from, skipping nested "${...}" in a default.
func matchingBrace(s string, from int) int {
	depth := 1
	for i := from; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "${"):
			depth++
			i++
		case s[i] == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitPlaceholder splits "key:default" at the first top-level colon.
func splitPlaceholder(inner string) (key, def string, hasDefault bool) {
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch {
		case strings.HasPrefix(inner[i:], "${"):
			depth++
			i++
		case inner[i] == '}':
			depth--
		case inner[i] == ':' && depth == 0:
			return strings.TrimSpace(inner[:i]), inner[i+1:], true
		}
	}
	return strings.TrimSpace(inner), "", false
}

// parseSpringYAML flattens every default-profile document of a (possibly
// multi-document) application.yml. A document activated by a profile —
// spring.config.activate.on-profile, or the older spring.profiles — is
// skipped whole.
func parseSpringYAML(data []byte) (map[string]string, error) {
	out := map[string]string{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		props := map[string]string{}
		flattenYAML("", doc, props)
		if props["spring.config.activate.on-profile"] != "" || props["spring.profiles"] != "" {
			continue
		}
		for k, v := range props {
			out[k] = v
		}
	}
}

func flattenYAML(prefix string, node any, out map[string]string) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			flattenYAML(joinKey(prefix, k), child, out)
		}
	case map[any]any:
		for k, child := range v {
			flattenYAML(joinKey(prefix, fmt.Sprint(k)), child, out)
		}
	case []any:
		for i, child := range v {
			flattenYAML(fmt.Sprintf("%s[%d]", prefix, i), child, out)
		}
	case nil:
		if prefix != "" {
			out[prefix] = ""
		}
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprint(v)
		}
	}
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// parseProperties reads the java.util.Properties format closely enough for
// config lookups: "key=value", "key: value" and "key value", comment lines
// starting with # or !, and a trailing backslash continuing a line.
// Unicode escapes are left as written.
func parseProperties(data []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var pending strings.Builder
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), " \t\f")
		if pending.Len() == 0 && (line == "" || line[0] == '#' || line[0] == '!') {
			continue
		}
		if strings.HasSuffix(line, `\`) && !strings.HasSuffix(line, `\\`) {
			pending.WriteString(strings.TrimSuffix(line, `\`))
			continue
		}
		pending.WriteString(line)
		key, value := splitPropertyLine(pending.String())
		pending.Reset()
		if key != "" {
			out[key] = value
		}
	}
	if pending.Len() > 0 {
		key, value := splitPropertyLine(pending.String())
		if key != "" {
			out[key] = value
		}
	}
	return out, sc.Err()
}

func splitPropertyLine(line string) (string, string) {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++ // an escaped separator is part of the key
		case '=', ':', ' ', '\t':
			key := line[:i]
			rest := strings.TrimLeft(line[i:], " \t")
			if rest != "" && (rest[0] == '=' || rest[0] == ':') {
				rest = strings.TrimLeft(rest[1:], " \t")
			}
			return strings.ReplaceAll(key, `\`, ""), rest
		}
	}
	return strings.ReplaceAll(line, `\`, ""), ""
}
