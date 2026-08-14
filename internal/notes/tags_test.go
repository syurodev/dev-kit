package notes

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{" Work ", "work", "Q3", ""})
	if err != nil {
		t.Fatalf("NormalizeTags: %v", err)
	}
	if len(got) != 2 || got[0] != "Work" || got[1] != "Q3" {
		t.Fatalf("got %#v", got)
	}
}

func TestNormalizeTagsRejectsTooLong(t *testing.T) {
	_, err := NormalizeTags([]string{strings.Repeat("a", 41)})
	if !errors.Is(err, ErrTagTooLong) {
		t.Fatalf("err = %v, want ErrTagTooLong", err)
	}
}

func TestNormalizeTagsRejectsTooMany(t *testing.T) {
	in := make([]string, 33)
	for i := range in {
		in[i] = string(rune('a' + i))
	}
	_, err := NormalizeTags(in)
	if !errors.Is(err, ErrTooManyTags) {
		t.Fatalf("err = %v, want ErrTooManyTags", err)
	}
}

func TestUnionTagsOwnFirst(t *testing.T) {
	got := UnionTags([]string{"standup"}, []string{"Work", "standup"})
	if len(got) != 2 || got[0] != "standup" || got[1] != "Work" {
		t.Fatalf("got %#v", got)
	}
}
