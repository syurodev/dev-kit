package forge

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// What CI looks like, in the vocabulary of both platforms: a Run is a GitHub
// Actions workflow run or a GitLab pipeline; a Job is a job in either; a Step
// is a GitHub step (GitLab has none). Every text a host wrote — a job's or
// step's name, an annotation's message, a log — is other people's, so callers
// present it as data.

// RunStatus is where a run stands, collapsed to what a person acts on.
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunPassed    RunStatus = "passed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	RunSkipped   RunStatus = "skipped"
)

// Run is one workflow run or pipeline.
type Run struct {
	// ID is the host's, as text; ci.run and ci.logs take it back.
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Event  string    `json:"event,omitempty"`
	Status RunStatus `json:"status"`
	// Conclusion is the host's own word ("timed_out", "failed").
	Conclusion  string    `json:"conclusion,omitempty"`
	SHA         string    `json:"sha"`
	Branch      string    `json:"branch,omitempty"`
	Number      int       `json:"number,omitempty"`
	Attempt     int       `json:"attempt,omitempty"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	DurationSec int       `json:"durationSec,omitempty"`
	URL         string    `json:"url,omitempty"`
}

// Step is one step of a job (GitHub only).
type Step struct {
	Number      int         `json:"number"`
	Name        string      `json:"name"`
	Status      CheckStatus `json:"status"`
	Conclusion  string      `json:"conclusion,omitempty"`
	DurationSec int         `json:"durationSec,omitempty"`
	// FirstFailed marks the first step of the job that failed: where to look.
	FirstFailed bool `json:"firstFailed,omitempty"`
}

// Job is one job of a run. Its ID is what ci.logs takes.
type Job struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Stage       string      `json:"stage,omitempty"`
	Status      CheckStatus `json:"status"`
	Conclusion  string      `json:"conclusion,omitempty"`
	StartedAt   time.Time   `json:"startedAt,omitempty"`
	DurationSec int         `json:"durationSec,omitempty"`
	URL         string      `json:"url,omitempty"`
	Steps       []Step      `json:"steps,omitempty"`
	// FirstFailed marks the earliest failed job of the run.
	FirstFailed bool `json:"firstFailed,omitempty"`
	// FailureReason is the host's short reason (GitLab's failure_reason).
	FailureReason string `json:"failureReason,omitempty"`
}

// Annotation is what a check reported about the code: a lint finding, a failed
// test. It is what an agent needs to fix the thing.
type Annotation struct {
	JobID     string `json:"jobId,omitempty"`
	Path      string `json:"path,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	// Level is "failure", "warning" or "notice".
	Level   string `json:"level"`
	Title   string `json:"title,omitempty"`
	Message string `json:"message"`
}

// RunDetail is a run with its jobs and the annotations of its failed jobs.
type RunDetail struct {
	Run         Run          `json:"run"`
	Jobs        []Job        `json:"jobs"`
	Annotations []Annotation `json:"annotations,omitempty"`
	// Notes say what could not be read (annotations the token may not see).
	Notes []string `json:"notes,omitempty"`
}

// RunQuery picks runs: by branch, by commit, or both; newest first.
type RunQuery struct {
	Branch string
	SHA    string
	Limit  int
}

// LogTail is the end of a job's log as the host sent it, before any cleaning.
type LogTail struct {
	JobID  string
	Name   string
	Status CheckStatus
	// Complete is whether the job has finished: a running job's log is not yet whole.
	Complete bool
	// Tail is the last bytes of the log; Total how many the host sent.
	Tail  []byte
	Total int64
}

// ---- cleaning and cutting a log ----------------------------------------------

const (
	DefaultLogLines = 200
	DefaultLogBytes = 32 << 10
	MaxLogLines     = 2000
	MaxLogBytes     = 256 << 10
	defaultContext  = 12
	maxMatches      = 5
)

var (
	// ANSI: CSI sequences (colours, cursor), OSC (titles, hyperlinks), and the
	// stray ESC-letter pair.
	ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
	// GitHub prefixes every line with an RFC 3339 timestamp.
	timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z `)
	// GitLab's fold markers, on a line of their own or glued to its start.
	sectionPattern = regexp.MustCompile(`section_(?:start|end):\d+:[A-Za-z0-9_.\-]+(?:\[[^\]]*\])?\r?`)
)

// LogOptions say how much of a cleaned log to return.
type LogOptions struct {
	// Lines and Bytes bound the answer (the tail); 0 means the default.
	Lines int
	Bytes int
	// Match, when set, returns only the neighbourhoods of the lines containing
	// it (a regular expression when Regex, else a case-insensitive substring),
	// Context lines around each, instead of the tail.
	Match   string
	Regex   bool
	Context int
}

// LogText is a cleaned, bounded log and what was left out, said plainly.
type LogText struct {
	Text string `json:"text"`
	// TotalLines is the log's length after cleaning; ReturnedLines what is in Text.
	TotalLines    int `json:"totalLines"`
	ReturnedLines int `json:"returnedLines"`
	// OmittedLines were cut from the start (a tail) or lie between the matches.
	OmittedLines int `json:"omittedLines"`
	// Matches counts the lines matching, when Match was given.
	Matches int `json:"matches,omitempty"`
	// Notice says in words how the result was cut, for the reader of the text.
	Notice string `json:"notice,omitempty"`
}

// CleanLog strips ANSI, the timestamp prefix, fold markers and carriage-return
// overwrites (a progress bar keeps its last state), and splits into lines.
func CleanLog(raw []byte) []string {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if i := strings.LastIndex(line, "\r"); i >= 0 && i < len(line)-1 {
			line = line[i+1:]
		} else {
			line = strings.TrimSuffix(line, "\r")
		}
		line = ansiPattern.ReplaceAllString(line, "")
		line = timestampPattern.ReplaceAllString(line, "")
		line = sectionPattern.ReplaceAllString(line, "")
		out = append(out, strings.TrimRight(line, " \t"))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// CutLog returns the tail of a cleaned log, or the neighbourhoods of a match.
func CutLog(lines []string, opts LogOptions) (LogText, error) {
	maxLines, maxBytes := opts.Lines, opts.Bytes
	if maxLines <= 0 {
		maxLines = DefaultLogLines
	}
	if maxLines > MaxLogLines {
		maxLines = MaxLogLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultLogBytes
	}
	if maxBytes > MaxLogBytes {
		maxBytes = MaxLogBytes
	}
	result := LogText{TotalLines: len(lines)}
	if opts.Match == "" {
		start := tailStart(lines, maxLines, maxBytes)
		result.Text = strings.Join(lines[start:], "\n")
		result.ReturnedLines = len(lines) - start
		result.OmittedLines = start
		if start > 0 {
			result.Notice = fmt.Sprintf("Showing the last %d of %d lines; %d earlier lines were left out. Ask for more with lines/maxBytes, or filter with match.", result.ReturnedLines, len(lines), start)
		}
		return result, nil
	}

	matches, err := matcher(opts)
	if err != nil {
		return LogText{}, &Error{Kind: ErrInvalid, Message: "not a valid pattern: " + err.Error()}
	}
	context := opts.Context
	if context <= 0 {
		context = defaultContext
	}
	var hits []int
	for i, line := range lines {
		if matches(line) {
			hits = append(hits, i)
		}
	}
	result.Matches = len(hits)
	if len(hits) == 0 {
		result.Notice = fmt.Sprintf("No line of the %d matched %q.", len(lines), opts.Match)
		return result, nil
	}
	// The last matches matter most: an error is at the end. Keep up to
	// maxMatches neighbourhoods, newest, merged where they touch.
	if len(hits) > maxMatches {
		hits = hits[len(hits)-maxMatches:]
	}
	type span struct{ from, to int }
	var spans []span
	for _, hit := range hits {
		from, to := max(0, hit-context), min(len(lines)-1, hit+context/2)
		if n := len(spans); n > 0 && from <= spans[n-1].to+1 {
			spans[n-1].to = max(spans[n-1].to, to)
			continue
		}
		spans = append(spans, span{from, to})
	}
	var b strings.Builder
	returned := 0
	for i, s := range spans {
		if i > 0 {
			fmt.Fprintf(&b, "\n… %d lines …\n", s.from-spans[i-1].to-1)
		}
		chunk := strings.Join(lines[s.from:s.to+1], "\n")
		b.WriteString(chunk)
		returned += s.to - s.from + 1
	}
	text := b.String()
	if len(text) > maxBytes {
		text = text[len(text)-maxBytes:]
		result.Notice = fmt.Sprintf("The matching text was longer than %d bytes; its start was cut.", maxBytes)
	}
	result.Text, result.ReturnedLines, result.OmittedLines = text, returned, len(lines)-returned
	if skipped := result.Matches - len(hits); skipped > 0 {
		note := fmt.Sprintf("%d earlier matching lines are not shown; the last %d are.", skipped, len(hits))
		if result.Notice != "" {
			result.Notice += " " + note
		} else {
			result.Notice = note
		}
	}
	return result, nil
}

// tailStart is the index of the first line of the longest tail that fits both
// bounds. A single line longer than the byte bound is kept alone (cut inside
// by the caller's text limit is not done: a line is never split here).
func tailStart(lines []string, maxLines, maxBytes int) int {
	start := len(lines)
	bytes := 0
	for start > 0 && len(lines)-start < maxLines {
		size := len(lines[start-1]) + 1
		if bytes+size > maxBytes && start < len(lines) {
			break
		}
		bytes += size
		start--
	}
	return start
}

func matcher(opts LogOptions) (func(string) bool, error) {
	if opts.Regex {
		re, err := regexp.Compile(opts.Match)
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	}
	needle := strings.ToLower(opts.Match)
	return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
}

// TailBuffer keeps the last Keep bytes written to it and counts everything: a
// log is read to its end, but only its end is held.
type TailBuffer struct {
	Keep  int
	buf   []byte
	total int64
}

func (t *TailBuffer) Write(p []byte) (int, error) {
	t.total += int64(len(p))
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.Keep {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.Keep:]...)
	}
	return len(p), nil
}

// Bytes is the retained tail, at most Keep bytes.
func (t *TailBuffer) Bytes() []byte {
	if len(t.buf) > t.Keep {
		return t.buf[len(t.buf)-t.Keep:]
	}
	return t.buf
}

// Total is every byte written.
func (t *TailBuffer) Total() int64 { return t.total }
