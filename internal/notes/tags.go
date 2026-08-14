package notes

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxTags   = 32
	maxTagLen = 40
)

var (
	ErrTagTooLong  = errors.New("tag too long")
	ErrTooManyTags = errors.New("too many tags")
)

func NormalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > maxTagLen {
			return nil, ErrTagTooLong
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, ErrTooManyTags
	}
	return out, nil
}

func UnionTags(own, inherited []string) []string {
	out := make([]string, 0, len(own)+len(inherited))
	seen := make(map[string]struct{}, len(own)+len(inherited))
	for _, group := range [][]string{own, inherited} {
		for _, tag := range group {
			key := strings.ToLower(tag)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, tag)
		}
	}
	return out
}
