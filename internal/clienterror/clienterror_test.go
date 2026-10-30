package clienterror

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// capture is a Logf and a Sink that remember what they were given.
type capture struct {
	mu    sync.Mutex
	logs  []string
	lines []string
}

func (c *capture) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, fmt.Sprintf(format, args...))
}

func (c *capture) Append(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newLog(c *capture, clk *clock) *Log {
	return New(Options{Logf: c.logf, Sink: c, Now: clk.Now})
}

func startClock() *clock { return &clock{now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)} }

func TestScrubClipsByRunesAndRemovesControlCharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"empty", "", 10, ""},
		{"short is untouched", "boom", 10, "boom"},
		{"clips by runes, not bytes", "ééééé", 3, "ééé"},
		{"clips emoji whole", "😀😀😀😀", 2, "😀😀"},
		{"newline becomes a visible joint", "a\nb", 10, "a | b"},
		{"crlf is one joint", "a\r\nb", 10, "a | b"},
		{"blank lines and edge whitespace vanish", "\n  a  \n\n  b\n", 10, "a | b"},
		{"stack frames join with a bar", "Error: x\n    at f (a.js:1:2)\n    at g (b.js:3:4)", 100, "Error: x | at f (a.js:1:2) | at g (b.js:3:4)"},
		{"tab and other controls become spaces", "a\tb\x00c\x1bd\x7fe", 20, "a b c d e"},
		{"line separators become spaces", "a b c", 10, "a b c"},
		{"invalid utf-8 is replaced", "a\xffb", 10, "a�b"},
		{"clipping happens after scrubbing", "a\n\n\n\n\n\n\nb", 5, "a | b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := scrub(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("scrub(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("not valid utf-8: %q", got)
			}
		})
	}
}

func TestScrubOfAHugeInputIsBounded(t *testing.T) {
	huge := strings.Repeat("x\n", 5_000_000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := scrub(huge, 1500)
	runtime.ReadMemStats(&after)
	if n := utf8.RuneCountInString(got); n != 1500 {
		t.Fatalf("runes = %d, want 1500", n)
	}
	// 10 MB in must not cost 10 MB of copies: only a bounded head is scrubbed.
	if spent := after.TotalAlloc - before.TotalAlloc; spent > 1<<20 {
		t.Fatalf("scrubbing a 10 MB input allocated %d bytes", spent)
	}
}

func TestReportWritesOneBoundedLineWithTheDocumentedCaps(t *testing.T) {
	c, clk := &capture{}, startClock()
	l := newLog(c, clk)
	l.Report(strings.Repeat("l", 100), strings.Repeat("m", 1000), strings.Repeat("s", 5000), strings.Repeat("c", 3000))

	if len(c.logs) != 1 || len(c.lines) != 1 {
		t.Fatalf("logs=%d lines=%d, want 1 each", len(c.logs), len(c.lines))
	}
	line := c.logs[0]
	if !strings.HasPrefix(line, "webview: client error ["+strings.Repeat("l", 60)+"] ") {
		t.Fatalf("prefix/label wrong: %.120s", line)
	}
	if strings.Contains(line, strings.Repeat("l", 61)) || strings.Contains(line, strings.Repeat("m", 401)) ||
		strings.Contains(line, strings.Repeat("s", 1501)) || strings.Contains(line, strings.Repeat("c", 801)) {
		t.Fatal("a field exceeds its cap")
	}
	for _, want := range []string{strings.Repeat("m", 400), "stack: " + strings.Repeat("s", 1500), "componentStack: " + strings.Repeat("c", 800)} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %.30s... in %.200s", want, line)
		}
	}
	if strings.ContainsAny(line, "\n\r") {
		t.Fatal("the line holds a newline")
	}
	if want := "2026-10-30T12:00:00Z " + line; c.lines[0] != want {
		t.Fatalf("file line = %.200q, want the time-prefixed log line", c.lines[0])
	}
}

func TestReportScrubsEveryField(t *testing.T) {
	c := &capture{}
	newLog(c, startClock()).Report("la\nbel", "m1\nm2", "Error\n  at a\n  at b", "\n  in A\n  in B")
	if got, want := c.logs[0], "webview: client error [la | bel] m1 | m2 stack: Error | at a | at b componentStack: in A | in B"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestReportOfEmptyInputDoesNotPanicAndStaysReadable(t *testing.T) {
	c := &capture{}
	l := newLog(c, startClock())
	l.Report("", "", "", "")
	if got, want := c.logs[0], "webview: client error [unknown] "; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	var nilLog *Log
	nilLog.Report("a", "b", "c", "d") // a window with no log wired is not an error
}

func TestReportIsRateLimitedAndTheDroppedLinesAreCountedOnce(t *testing.T) {
	c, clk := &capture{}, startClock()
	l := newLog(c, clk)

	for i := 0; i < 50; i++ {
		l.Report("root", fmt.Sprintf("boom %d", i), "", "")
	}
	if len(c.logs) != MaxPerMinute {
		t.Fatalf("logged %d lines in one minute, want %d", len(c.logs), MaxPerMinute)
	}

	clk.now = clk.now.Add(59 * time.Second)
	l.Report("root", "still the same window", "", "")
	if len(c.logs) != MaxPerMinute {
		t.Fatal("a line got through inside the window")
	}

	clk.now = clk.now.Add(2 * time.Second) // the window has passed
	l.Report("root", "fresh", "", "")
	if len(c.logs) != MaxPerMinute+2 {
		t.Fatalf("logs = %d, want the cap plus the dropped line plus the fresh one", len(c.logs))
	}
	if got, want := c.logs[MaxPerMinute], "webview: client error [rate limit] 21 more client errors dropped"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.Contains(c.logs[MaxPerMinute+1], "fresh") {
		t.Fatalf("the new window's first line is %q", c.logs[MaxPerMinute+1])
	}
	if len(c.lines) != len(c.logs) {
		t.Fatalf("the file got %d lines, the log %d", len(c.lines), len(c.logs))
	}

	clk.now = clk.now.Add(10 * time.Minute)
	l.Report("root", "quiet again", "", "")
	for _, line := range c.logs[MaxPerMinute+2:] {
		if strings.Contains(line, "dropped") {
			t.Fatalf("a second dropped line: %q", line)
		}
	}
}

func TestReportBeyondTheCapInAWindowThatNeverEndsWritesNothingMore(t *testing.T) {
	c := &capture{}
	l := newLog(c, startClock())
	for i := 0; i < 1000; i++ {
		l.Report("a", "b", "", "")
	}
	if len(c.logs) != MaxPerMinute || len(c.lines) != MaxPerMinute {
		t.Fatalf("logs=%d lines=%d, want %d", len(c.logs), len(c.lines), MaxPerMinute)
	}
}

func TestFileSinkAppendsWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-errors.log")
	s := newFileSink(path, 1024)
	s.Append("one")
	s.Append("two")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "one\ntwo\n" {
		t.Fatalf("file = %q", raw)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}

func TestFileSinkRotatesOnceAtTheCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client-errors.log")
	s := newFileSink(path, 100)
	a, b, c := strings.Repeat("a", 59), strings.Repeat("b", 59), strings.Repeat("c", 59) // 60 bytes each with the newline
	s.Append(a)
	s.Append(b) // 120 > 100: a rotates out
	s.Append(c) // 120 > 100: b rotates out, the old .1 (a) is gone

	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".1")
	if string(cur) != c+"\n" || string(old) != b+"\n" {
		t.Fatalf("current=%q old=%q", cur, old)
	}
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Fatal("a second rotation file exists")
	}
	info, _ := os.Stat(path + ".1")
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("rotated mode = %o, want 600", perm)
	}
}

func TestFileSinkStaysUnderTheCapAcrossManyLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client-errors.log")
	s := newFileSink(path, 4096)
	for i := 0; i < 500; i++ {
		s.Append(strings.Repeat("x", 99))
	}
	for _, p := range []string{path, path + ".1"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 4096 {
			t.Fatalf("%s is %d bytes", p, info.Size())
		}
	}
}

func TestFileSinkNeverFailsTheCaller(t *testing.T) {
	s := newFileSink(filepath.Join(t.TempDir(), "missing", "dir", "x.log"), 100)
	s.Append("goes nowhere") // the directory does not exist: swallowed
	newFileSink("", 100).Append("no path")
}

func TestNewFillsTheDefaults(t *testing.T) {
	l := New(Options{})
	if l.logf == nil || l.now == nil || l.sink == nil {
		t.Fatal("defaults missing")
	}
	if got := New(Options{Path: filepath.Join(t.TempDir(), "x.log")}); got.sink == nil {
		t.Fatal("no file sink")
	}
	if DefaultMaxFileBytes != 256*1024 {
		t.Fatalf("file cap = %d", DefaultMaxFileBytes)
	}
}
