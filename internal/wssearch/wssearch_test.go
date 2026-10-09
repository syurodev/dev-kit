package wssearch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, roots []Root, o Options) Result {
	t.Helper()
	res, err := Run(context.Background(), roots, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func flat(res Result) []string {
	var out []string
	for _, f := range res.Files {
		for _, m := range f.Matches {
			out = append(out, fmt.Sprintf("%s:%s:%d:%d", f.FolderID, f.Path, m.Line, m.Column))
		}
	}
	return out
}

func TestLiteralCaseAndWholeWord(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "a.txt", "Foo foo FOO foobar _foo foo_ foo.bar\n")
	roots := []Root{{ID: "r", Path: dir}}

	cs := run(t, roots, Options{Query: "foo", CaseSensitive: true})
	if got := len(cs.Files[0].Matches); got != 5 {
		t.Fatalf("case-sensitive literal found %d, want 5 (foo, foobar, _foo, foo_, foo.bar)", got)
	}
	ci := run(t, roots, Options{Query: "foo"})
	if got := len(ci.Files[0].Matches); got != 7 {
		t.Fatalf("case-insensitive found %d, want 7", got)
	}
	ww := run(t, roots, Options{Query: "foo", WholeWord: true, CaseSensitive: true})
	// "foo" standalone: second token and "foo.bar".
	var cols []int
	for _, m := range ww.Files[0].Matches {
		cols = append(cols, m.Column)
	}
	if !reflect.DeepEqual(cols, []int{5, 30}) {
		t.Fatalf("whole-word columns = %v, want [5 30]", cols)
	}
}

func TestWholeWordIsUnicodeAware(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "u.txt", "café cafés café.\n")
	res := run(t, []Root{{"r", dir}}, Options{Query: "café", WholeWord: true})
	if got := len(res.Files[0].Matches); got != 2 {
		t.Fatalf("matches = %d, want 2 (not the one inside cafés)", got)
	}
	// An ASCII-only \b would call "caf" + "é" a boundary; it must not.
	res = run(t, []Root{{"r", dir}}, Options{Query: "caf", WholeWord: true})
	if len(res.Files) != 0 {
		t.Fatalf("caf is only ever part of a longer word: %v", flat(res))
	}
}

func TestWholeWordOnlyWhereTheQueryEdgeIsAWordCharacter(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "c.go", "call foo() and xfoo()\n")
	res := run(t, []Root{{"r", dir}}, Options{Query: "foo()", WholeWord: true, CaseSensitive: true})
	if got := flat(res); len(got) != 1 || !strings.HasSuffix(got[0], ":1:6") {
		t.Fatalf("got %v, want only the standalone foo()", got)
	}
}

func TestRegexAndInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "r.txt", "id=12 id=345\nnone\nid=6\n")
	res := run(t, []Root{{"r", dir}}, Options{Query: `id=\d+`, Regex: true, CaseSensitive: true})
	if res.Matches != 3 {
		t.Fatalf("matches = %d, want 3", res.Matches)
	}
	_, err := Run(context.Background(), []Root{{"r", dir}}, Options{Query: `(`, Regex: true})
	var invalid *InvalidQueryError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidQueryError", err)
	}
	if strings.Contains(invalid.Reason, "error parsing regexp") {
		t.Fatalf("reason leaks Go's prefix: %q", invalid.Reason)
	}
	// Empty matches are not matches.
	res = run(t, []Root{{"r", dir}}, Options{Query: `x*`, Regex: true})
	if res.Matches != 0 {
		t.Fatalf("empty regex matches reported: %v", flat(res))
	}
	if _, err := Run(context.Background(), []Root{{"r", dir}}, Options{}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("empty query err = %v", err)
	}
}

func TestColumnsAreUTF16AndLinesHandleCRLF(t *testing.T) {
	dir := t.TempDir()
	// 😀 is a surrogate pair in UTF-16 (two units, four bytes in UTF-8).
	put(t, dir, "e.txt", "😀 needle\r\nline2 😀😀needle\r\nplain needle\n")
	res := run(t, []Root{{"r", dir}}, Options{Query: "needle", CaseSensitive: true})
	var got [][2]int
	for _, m := range res.Files[0].Matches {
		got = append(got, [2]int{m.Line, m.Column})
		if m.Length != 6 {
			t.Fatalf("length = %d, want 6", m.Length)
		}
		if strings.ContainsRune(m.Preview, '\r') {
			t.Fatalf("preview kept a carriage return: %q", m.Preview)
		}
	}
	want := [][2]int{{1, 4}, {2, 11}, {3, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("positions = %v, want %v", got, want)
	}
}

func TestLongLinesAreSearchedNotDropped(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("x", 300_000) + "NEEDLE" + strings.Repeat("y", 300_000)
	put(t, dir, "min.js", line+"\n")
	res := run(t, []Root{{"r", dir}}, Options{Query: "NEEDLE", CaseSensitive: true})
	if res.Matches != 1 {
		t.Fatalf("a long line was lost: %+v", res)
	}
	m := res.Files[0].Matches[0]
	if m.Column != 300_001 {
		t.Fatalf("column = %d", m.Column)
	}
	if len(m.Preview) > previewBefore+previewAfter+len("NEEDLE")+4 {
		t.Fatalf("preview of %d bytes is not clipped", len(m.Preview))
	}
	// The match sits at Column-1-PreviewStart inside the preview.
	at := m.Column - 1 - m.PreviewStart
	if !strings.HasPrefix(m.Preview[at:], "NEEDLE") {
		t.Fatalf("preview offset wrong: at=%d preview=%q", at, m.Preview)
	}
}

func TestBinarySymlinkAndOversizedFilesAreSkipped(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	put(t, dir, "ok.txt", "needle\n")
	put(t, dir, "bin.dat", "needle\x00\x01\x02")
	put(t, dir, "big.txt", strings.Repeat("needle\n", 200))
	put(t, outside, "secret.txt", "needle\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skip("no symlinks")
	}
	res := run(t, []Root{{"r", dir}}, Options{Query: "needle", MaxFileBytes: 1000})
	if got := flat(res); len(got) != 1 || !strings.HasPrefix(got[0], "r:ok.txt:") {
		t.Fatalf("hits = %v, want only ok.txt", got)
	}
	if res.FilesSkipped != 3 {
		t.Fatalf("skipped = %d, want 3 (binary, oversized, symlink)", res.FilesSkipped)
	}
}

func TestIncludeExcludeSemantics(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"src/a.ts", "src/gen/b.ts", "src/c.go", "docs/d.ts", "node_modules/e.ts", "top.ts"} {
		put(t, dir, f, "needle\n")
	}
	paths := func(o Options) []string {
		o.Query = "needle"
		var out []string
		for _, f := range run(t, []Root{{"r", dir}}, o).Files {
			out = append(out, f.Path)
		}
		return out
	}
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"include ext any depth", Options{Include: []string{"*.ts"}}, []string{"docs/d.ts", "node_modules/e.ts", "src/a.ts", "src/gen/b.ts", "top.ts"}},
		{"braces", Options{Include: []string{"*.{go,ts}"}, Exclude: []string{"node_modules"}}, []string{"docs/d.ts", "src/a.ts", "src/c.go", "src/gen/b.ts", "top.ts"}},
		{"anchored dir", Options{Include: []string{"src/"}}, []string{"src/a.ts", "src/c.go", "src/gen/b.ts"}},
		{"exclude wins", Options{Include: []string{"src/**"}, Exclude: []string{"gen"}}, []string{"src/a.ts", "src/c.go"}},
		{"anchored glob", Options{Include: []string{"src/**/*.ts"}}, []string{"src/a.ts", "src/gen/b.ts"}},
		{"top only", Options{Include: []string{"/top.ts"}}, []string{"top.ts"}},
	}
	for _, c := range cases {
		// No .git here: the walk skips node_modules by name itself, so it only
		// matters for the cases that include it by pattern.
		got := paths(c.opts)
		if c.name == "include ext any depth" {
			got = filterOut(got, "node_modules/e.ts")
			c.want = filterOut(c.want, "node_modules/e.ts")
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func filterOut(list []string, drop string) []string {
	var out []string
	for _, s := range list {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

func TestResultOrderIsFolderThenPathThenPositionWhateverTheScheduling(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for i := 0; i < 60; i++ {
		put(t, a, fmt.Sprintf("d%02d/f%02d.txt", i%7, i), "needle one\nneedle two needle\n")
		put(t, b, fmt.Sprintf("d%02d/f%02d.txt", i%5, i), "needle\n")
	}
	roots := []Root{{"B", b}, {"A", a}} // the caller's order, not alphabetical
	first := flat(run(t, roots, Options{Query: "needle", Workers: 8}))
	for i := 0; i < 20; i++ {
		got := flat(run(t, roots, Options{Query: "needle", Workers: 1 + i%8}))
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs from the first", i)
		}
	}
	if !strings.HasPrefix(first[0], "B:") || !strings.HasPrefix(first[len(first)-1], "A:") {
		t.Fatalf("folders are not grouped in the caller's order: %v ... %v", first[0], first[len(first)-1])
	}
	for i := 1; i < len(first); i++ {
		pa, pb := strings.SplitN(first[i-1], ":", 3), strings.SplitN(first[i], ":", 3)
		if pa[0] == pb[0] && pa[1] > pb[1] {
			t.Fatalf("paths out of order within a folder: %s then %s", first[i-1], first[i])
		}
	}
}

func TestTruncationKeepsTheFirstMatchesDeterministically(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		put(t, dir, fmt.Sprintf("f%02d.txt", i), "needle\nneedle\nneedle\n")
	}
	var first []string
	for i := 0; i < 10; i++ {
		res := run(t, []Root{{"r", dir}}, Options{Query: "needle", MaxMatches: 10, Workers: 1 + i%8})
		if res.Truncated != TruncatedMatches || res.Matches != 10 {
			t.Fatalf("res = truncated %q matches %d", res.Truncated, res.Matches)
		}
		got := flat(res)
		if first == nil {
			first = got
		} else if !reflect.DeepEqual(first, got) {
			t.Fatalf("truncated output is not deterministic:\n%v\n%v", first, got)
		}
	}
	if !strings.HasPrefix(first[0], "r:f00.txt:1:") {
		t.Fatalf("first = %v", first[0])
	}
}

func TestNestedRootsAreSearchedOnceAndAttributedToTheInnerFolder(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "packages", "svc")
	put(t, outer, "top.txt", "needle\n")
	put(t, outer, "packages/svc/inner.txt", "needle\n")
	put(t, outer, "packages/other/o.txt", "needle\n")

	both := run(t, []Root{{"outer", outer}, {"inner", inner}}, Options{Query: "needle"})
	got := flat(both)
	want := []string{"outer:packages/other/o.txt:1:1", "outer:top.txt:1:1", "inner:inner.txt:1:1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Scoped to the outer folder alone, the inner subtree is the outer's.
	alone := run(t, []Root{{"outer", outer}}, Options{Query: "needle"})
	if len(alone.Files) != 3 {
		t.Fatalf("outer alone found %v", flat(alone))
	}

	// The same folder under two spellings is not searched twice.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(outer, link); err == nil {
		res := run(t, []Root{{"a", outer}, {"b", link}}, Options{Query: "needle"})
		if res.Folders[1].State != FolderDuplicate || res.Folders[1].DuplicateOf != "a" {
			t.Fatalf("folders = %+v", res.Folders)
		}
		if res.Matches != 3 {
			t.Fatalf("duplicate folder searched twice: %d matches", res.Matches)
		}
	}
}

func TestUnavailableFolderDoesNotStopTheOthers(t *testing.T) {
	ok := t.TempDir()
	put(t, ok, "a.txt", "needle\n")
	res := run(t, []Root{{"gone", filepath.Join(ok, "missing")}, {"ok", ok}}, Options{Query: "needle"})
	if res.Folders[0].State != FolderUnavailable || res.Folders[0].Error == "" {
		t.Fatalf("folders = %+v", res.Folders)
	}
	if res.Folders[1].State != FolderOK || res.Matches != 1 {
		t.Fatalf("the healthy folder was not searched: %+v", res)
	}
}

func TestGitFolderHonoursGitignoreAndSearchesTrackedBuildDirectories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	put(t, dir, ".gitignore", "secret.txt\nlogs/\n")
	put(t, dir, "build/tracked.go", "needle\n") // a source directory that merely has a build-like name
	put(t, dir, "bin/run.sh", "needle\n")
	put(t, dir, "secret.txt", "needle\n")
	put(t, dir, "logs/x.log", "needle\n")
	put(t, dir, "src/new.go", "needle\n") // untracked, not ignored
	git("add", ".gitignore", "build", "bin")
	git("commit", "-q", "-m", "x")

	res := run(t, []Root{{"r", dir}}, Options{Query: "needle"})
	var paths []string
	for _, f := range res.Files {
		paths = append(paths, f.Path)
	}
	want := []string{"bin/run.sh", "build/tracked.go", "src/new.go"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if res.Folders[0].Source != SourceGit {
		t.Fatalf("source = %q", res.Folders[0].Source)
	}
}

func TestCancellationStopsTheSearchAndReturnsNoPartialResults(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 400; i++ {
		put(t, dir, fmt.Sprintf("d%02d/f%03d.txt", i%20, i), strings.Repeat("hay hay hay\n", 2000)+"needle\n")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(2 * time.Millisecond)
		cancel()
	}()
	res, err := Run(ctx, []Root{{"r", dir}}, Options{Query: "needle", Workers: 2})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (result %d matches)", err, res.Matches)
	}
	if len(res.Files) != 0 {
		t.Fatal("a cancelled search returned results")
	}
}

func TestGlobMatching(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*.go", "a/b/c.go", true},
		{"*.go", "c.go.txt", false},
		{"vendor", "a/vendor/x.go", true},
		{"vendor", "a/vendors/x.go", false},
		{"a/b", "a/b/c/d.txt", true},
		{"a/b", "x/a/b/c.txt", false},
		{"**/gen/**", "x/y/gen/z.go", true},
		{"src/**/test", "src/test", true},
		{"src/**/test", "src/a/b/test/f.go", true},
		{"/top.ts", "top.ts", true},
		{"/top.ts", "sub/top.ts", false},
		{"[ab].txt", "a.txt", true},
		{"[ab].txt", "c.txt", false},
	}
	for _, c := range cases {
		f := newPathFilter([]string{c.pattern}, nil)
		if got := f.allows(c.path); got != c.want {
			t.Errorf("include %q on %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}
