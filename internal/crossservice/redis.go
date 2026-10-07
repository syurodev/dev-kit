package crossservice

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"devkit/internal/codegraph"
)

// Redis is a resource two services can share without either naming the other:
// one writes fsap:access:{org}:{api}, the other reads fsap:access:*. The
// connection is the key, so it is made from the keys: two patterns are the same
// resource when some string matches both.

// minLiteralPrefix is how many fixed characters a key pattern must start with to
// identify anything. "*" and "{key}:cache" overlap every other key in the
// workspace and so mean nothing; "fsap:" is a namespace.
const minLiteralPrefix = 3

var holeRE = regexp.MustCompile(`\$?\{[^{}]*}`)

// normalizeKey makes a key comparable: every hole (a part only the runtime
// knows) and every glob star becomes one "*".
func normalizeKey(key string) string {
	k := holeRE.ReplaceAllString(key, "*")
	for strings.Contains(k, "**") {
		k = strings.ReplaceAll(k, "**", "*")
	}
	return k
}

// specific reports whether a normalized pattern starts with enough fixed text
// to say which resource it is.
func specific(pattern string) bool {
	prefix := pattern
	if i := strings.IndexByte(pattern, '*'); i >= 0 {
		prefix = pattern[:i]
	}
	return len(prefix) >= minLiteralPrefix
}

// overlaps reports whether any string matches both patterns, "*" standing for
// any run of characters in either.
func overlaps(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	memo := map[[2]int]bool{}
	seen := map[[2]int]bool{}
	var f func(i, j int) bool
	f = func(i, j int) bool {
		if i == len(ra) && j == len(rb) {
			return true
		}
		k := [2]int{i, j}
		if seen[k] {
			return memo[k]
		}
		seen[k] = true
		res := false
		starA := i < len(ra) && ra[i] == '*'
		starB := j < len(rb) && rb[j] == '*'
		switch {
		case starA && f(i+1, j): // a's star matches nothing more
			res = true
		case starB && f(i, j+1): // b's star matches nothing more
			res = true
		case starA && j < len(rb) && f(i, j+1): // a's star takes one more of b
			res = true
		case starB && i < len(ra) && f(i+1, j): // b's star takes one more of a
			res = true
		case i < len(ra) && j < len(rb) && !starA && !starB && ra[i] == rb[j] && f(i+1, j+1):
			res = true
		}
		memo[k] = res
		return res
	}
	return f(0, 0)
}

// RedisUse is one access to a key, by one method of one service.
type RedisUse struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	NodeID  string `json:"nodeId"`
	Label   string `json:"label"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	// Key is the key as this use wrote it, holes named.
	Key     string `json:"key"`
	Command string `json:"command,omitempty"`
}

// RedisKey is one key pattern and everything that touches it.
type RedisKey struct {
	// Pattern has every hole as "*".
	Pattern string     `json:"pattern"`
	Writers []RedisUse `json:"writers"`
	Readers []RedisUse `json:"readers"`
	// Services are the services touching this key or one that overlaps it.
	Services []string `json:"services"`
	// CrossService is true when one service writes it and a different one reads it:
	// changing the key's shape or its writer is a change another service feels.
	CrossService bool `json:"crossService"`
}

// RedisQuery asks who shares a key. Key is a pattern (fsap:access:* or
// fsap:access:{org}); empty lists every key two or more services touch.
type RedisQuery struct {
	Key   string
	Limit int
}

// RedisReport is the keys, and what was read to find them.
type RedisReport struct {
	Query    string        `json:"query"`
	Keys     []RedisKey    `json:"keys"`
	KeyTotal int           `json:"keyTotal"`
	Services []ServiceInfo `json:"services"`
	Notes    []string      `json:"notes,omitempty"`
}

// RunRedis answers who reads and writes a Redis key across the workspace's services.
func RunRedis(services []Service, q RedisQuery) (RedisReport, error) {
	if q.Limit < 0 {
		return RedisReport{}, fmt.Errorf("limit must be positive (got %d)", q.Limit)
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultLimit / 2
	}
	limit = min(limit, MaxLimit)

	wanted := normalizeKey(strings.TrimSpace(q.Key))
	if wanted != "" && !specific(wanted) {
		return RedisReport{}, errors.New("key is too generic: it needs at least 3 fixed characters before the first wildcard, or it matches every key")
	}

	report := RedisReport{Query: q.Key, Keys: []RedisKey{}}
	byPattern := map[string]*RedisKey{}
	for i := range services {
		s := &services[i]
		report.Services = append(report.Services, infoOf(s))
		for _, a := range s.Snapshot.Redis {
			pattern := normalizeKey(a.Key)
			if !specific(pattern) {
				continue
			}
			k := byPattern[pattern]
			if k == nil {
				k = &RedisKey{Pattern: pattern, Writers: []RedisUse{}, Readers: []RedisUse{}}
				byPattern[pattern] = k
			}
			use := RedisUse{Service: s.Name, Path: s.Path, NodeID: a.From, Label: a.Label, File: a.File, Line: a.Line, Key: a.Key, Command: a.Command}
			if a.Access == "write" {
				k.Writers = append(k.Writers, use)
			} else {
				k.Readers = append(k.Readers, use)
			}
		}
	}

	patterns := make([]string, 0, len(byPattern))
	for p := range byPattern {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)

	// For each pattern, who touches it or anything that overlaps it, and whether
	// one of them writes what a different one reads.
	for _, p := range patterns {
		k := byPattern[p]
		services, writers, readers := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, other := range patterns {
			if other != p && !overlaps(p, other) {
				continue
			}
			for _, u := range byPattern[other].Writers {
				services[u.Service], writers[u.Service] = true, true
			}
			for _, u := range byPattern[other].Readers {
				services[u.Service], readers[u.Service] = true, true
			}
		}
		for s := range services {
			k.Services = append(k.Services, s)
		}
		sort.Strings(k.Services)
		for w := range writers {
			for r := range readers {
				if w != r {
					k.CrossService = true
				}
			}
		}
	}

	var out []RedisKey
	for _, p := range patterns {
		k := byPattern[p]
		if wanted != "" {
			if !overlaps(wanted, p) {
				continue
			}
		} else if len(k.Services) < 2 {
			continue
		}
		out = append(out, *k)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CrossService != out[j].CrossService {
			return out[i].CrossService
		}
		return out[i].Pattern < out[j].Pattern
	})
	report.KeyTotal = len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	if out != nil {
		report.Keys = out
	}

	for _, s := range report.Services {
		if s.State != codegraph.SnapshotIndexed {
			report.Notes = append(report.Notes, fmt.Sprintf("%s is %s: its Redis keys are not in this answer", s.Name, s.State))
		}
	}
	report.Notes = append(report.Notes,
		"read from each service's index: only keys given to a Redis template, Jedis/Lettuce command object or resty.redis client by a literal or a constant are seen — a key built elsewhere, Spring cache annotations, Lua scripts and pub/sub channels are not",
		"two patterns are one resource when some string matches both (a hole is any run of characters); a key with fewer than 3 fixed leading characters is ignored as too generic to identify anything")
	return report, nil
}
