// Package clienterror is the one-way channel from the window to DevKit's own
// log. The production webview has no inspector, so an error that unmounts the
// whole UI vanishes when the person presses Reload; this keeps one bounded line
// per error, in the process log (journalctl on Linux) and in a small file under
// the data directory (macOS and Windows keep no stdout).
//
// It records what the window chose to send — a label, a message, a stack and a
// component stack — and nothing else. It is a standard-library-only leaf: the
// caller check and the capability grant live in the bridge.
package clienterror

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// Caps are in runes, applied after control characters are scrubbed.
	MaxLabel          = 60
	MaxMessage        = 400
	MaxStack          = 1500
	MaxComponentStack = 800

	// MaxPerMinute bounds the lines written per window of one minute, so a
	// render loop cannot flood the journal or the disk.
	MaxPerMinute = 30
	// DefaultMaxFileBytes is the size at which the file rotates once.
	DefaultMaxFileBytes = 256 * 1024

	prefix = "webview: client error"
	window = time.Minute
	// A frame joint keeps a multi-line stack on the one line the journal gets.
	joint = " | "
)

// Sink receives each accepted line, already prefixed with its UTC time. An
// implementation never fails its caller: a log that cannot be written is not
// worth losing the window over.
type Sink interface{ Append(line string) }

// Options configures a Log. The zero value logs through log.Printf with no file.
type Options struct {
	// Path is the file the lines are also appended to ("" for none). Ignored
	// when Sink is set.
	Path string
	// MaxFileBytes is the rotation cap for Path; 0 means DefaultMaxFileBytes.
	MaxFileBytes int64
	// Sink replaces the file, so a test never touches a data directory.
	Sink Sink
	// Logf and Now are injected by tests; they default to log.Printf and time.Now.
	Logf func(format string, args ...any)
	Now  func() time.Time
}

// Log writes bounded, rate-limited client error lines.
type Log struct {
	logf func(format string, args ...any)
	now  func() time.Time
	sink Sink

	mu      sync.Mutex
	started time.Time // start of the current one-minute window
	count   int       // lines accepted in it
	dropped int       // lines refused in it
}

// New returns a Log with the defaults filled in.
func New(opts Options) *Log {
	l := &Log{logf: opts.Logf, now: opts.Now, sink: opts.Sink}
	if l.logf == nil {
		l.logf = log.Printf
	}
	if l.now == nil {
		l.now = time.Now
	}
	if l.sink == nil {
		max := opts.MaxFileBytes
		if max <= 0 {
			max = DefaultMaxFileBytes
		}
		l.sink = newFileSink(opts.Path, max)
	}
	return l
}

// Report records one client error. It never fails and is safe on a nil Log.
// The dropped-lines notice is written by the first report after a window in
// which lines were dropped: nothing runs on a timer.
func (l *Log) Report(label, message, stack, componentStack string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if l.started.IsZero() || now.Sub(l.started) >= window {
		if l.dropped > 0 {
			l.emit(now, prefix+" [rate limit] "+strconv.Itoa(l.dropped)+" more client errors dropped")
		}
		l.started, l.count, l.dropped = now, 0, 0
	}
	if l.count >= MaxPerMinute {
		l.dropped++
		return
	}
	l.count++

	label = scrub(label, MaxLabel)
	if label == "" {
		label = "unknown"
	}
	body := scrub(message, MaxMessage)
	if s := scrub(stack, MaxStack); s != "" {
		body += " stack: " + s
	}
	if s := scrub(componentStack, MaxComponentStack); s != "" {
		body += " componentStack: " + s
	}
	l.emit(now, prefix+" ["+label+"] "+body)
}

func (l *Log) emit(now time.Time, line string) {
	l.logf("%s", line)
	l.sink.Append(now.UTC().Format(time.RFC3339) + " " + line)
}

// scrub makes s one line of at most max runes: line breaks become a visible
// joint (so a stack's frames stay readable), every other control character
// becomes a space, invalid UTF-8 becomes U+FFFD, and the result is clipped.
// The input is bounded first, so a window that sends megabytes costs little.
func scrub(s string, max int) string {
	if len(s) > max*8 {
		s = s[:max*8]
	}
	s = strings.ToValidUTF8(s, "�")
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", " ", " ", " ", " ").Replace(s)

	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, line))
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(joint)
		}
		b.WriteString(line)
	}
	out := b.String()
	if utf8.RuneCountInString(out) > max {
		out = string([]rune(out)[:max])
	}
	return out
}

// fileSink appends to path and rotates it once, to path+".1", when a line would
// take it past max bytes. Files are created 0600: a stack can name local paths.
type fileSink struct {
	path string
	max  int64
	mu   sync.Mutex
}

func newFileSink(path string, max int64) *fileSink { return &fileSink{path: path, max: max} }

func (s *fileSink) Append(line string) {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data := line + "\n"
	if info, err := os.Stat(s.path); err == nil && info.Size() > 0 && info.Size()+int64(len(data)) > s.max {
		_ = os.Rename(s.path, s.path+".1")
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(data)
	_ = f.Close()
}
