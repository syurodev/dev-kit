package forge

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---- cleaning and cutting a log ----------------------------------------------

func TestCleanLogStripsWhatIsNotText(t *testing.T) {
	raw := "2026-10-16T06:23:09.9981790Z \x1b[36;1mgo test ./...\x1b[0m\n" +
		"2026-10-16T06:23:10.1000000Z ok  \tdevkit/internal/forge\t0.5s\r\n" +
		"progress 10%\rprogress 60%\rprogress 100%\n" +
		"section_start:1700000000:build_script\r\x1b[0Kwork begins\n" +
		"\x1b]8;;https://x\x07link\x1b]8;;\x07 done   \n\n\n"
	got := CleanLog([]byte(raw))
	want := []string{"go test ./...", "ok  \tdevkit/internal/forge\t0.5s", "progress 100%", "work begins", "link done"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func numbered(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return lines
}

func TestCutLogDefaultsToTheTailAndSaysWhatItLeftOut(t *testing.T) {
	got, err := CutLog(numbered(1000), LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.ReturnedLines != DefaultLogLines || got.OmittedLines != 800 || got.TotalLines != 1000 {
		t.Fatalf("%+v", got)
	}
	if !strings.HasPrefix(got.Text, "line 801\n") || !strings.HasSuffix(got.Text, "line 1000") {
		t.Errorf("the tail must end with the last line: %q…%q", got.Text[:12], got.Text[len(got.Text)-12:])
	}
	if !strings.Contains(got.Notice, "last 200 of 1000 lines") || !strings.Contains(got.Notice, "800 earlier lines") {
		t.Errorf("notice %q", got.Notice)
	}
}

func TestCutLogRespectsTheByteBoundAndTheCaps(t *testing.T) {
	long := make([]string, 100)
	for i := range long {
		long[i] = strings.Repeat("x", 99) // 100 bytes with the newline
	}
	got, _ := CutLog(long, LogOptions{Bytes: 1000})
	if got.ReturnedLines != 10 || len(got.Text) > 1000 {
		t.Errorf("returned %d lines, %d bytes", got.ReturnedLines, len(got.Text))
	}
	big, _ := CutLog(numbered(5000), LogOptions{Lines: 99999, Bytes: 99 << 20})
	if big.ReturnedLines != MaxLogLines {
		t.Errorf("lines are capped at %d, got %d", MaxLogLines, big.ReturnedLines)
	}
	short, _ := CutLog(numbered(3), LogOptions{})
	if short.OmittedLines != 0 || short.Notice != "" || short.Text != "line 1\nline 2\nline 3" {
		t.Errorf("%+v", short)
	}
	one, _ := CutLog([]string{strings.Repeat("y", 5000)}, LogOptions{Bytes: 100})
	if one.ReturnedLines != 1 {
		t.Errorf("a single long line is kept whole, not dropped: %+v", one.ReturnedLines)
	}
	empty, _ := CutLog(nil, LogOptions{})
	if empty.Text != "" || empty.TotalLines != 0 {
		t.Errorf("%+v", empty)
	}
}

func TestCutLogFiltersAroundTheLastMatches(t *testing.T) {
	lines := numbered(300)
	lines[49] = "ERROR: first failure"
	lines[250] = "FAIL: TestRetries"
	got, err := CutLog(lines, LogOptions{Match: "error", Context: 2})
	if err != nil || got.Matches != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Text != "line 48\nline 49\nERROR: first failure\nline 51" {
		t.Errorf("%q", got.Text)
	}
	re, _ := CutLog(lines, LogOptions{Match: `^(ERROR|FAIL)`, Regex: true, Context: 1})
	if re.Matches != 2 || !strings.Contains(re.Text, "… ") || !strings.Contains(re.Text, "FAIL: TestRetries") || !strings.Contains(re.Text, "ERROR: first failure") {
		t.Errorf("%+v", re)
	}
	if re.OmittedLines != 300-re.ReturnedLines {
		t.Errorf("omitted %d returned %d", re.OmittedLines, re.ReturnedLines)
	}
	none, _ := CutLog(lines, LogOptions{Match: "nothing like this"})
	if none.Matches != 0 || none.Text != "" || !strings.Contains(none.Notice, "No line of the 300 matched") {
		t.Errorf("%+v", none)
	}
	if _, err := CutLog(lines, LogOptions{Match: "(", Regex: true}); KindOf(err) != ErrInvalid {
		t.Errorf("a bad pattern: %v", err)
	}
}

func TestCutLogKeepsOnlyTheLastMatchesWhenThereAreManyAndSaysSo(t *testing.T) {
	lines := numbered(500)
	for i := 0; i < 500; i += 50 {
		lines[i] = fmt.Sprintf("panic number %d", i/50)
	}
	got, _ := CutLog(lines, LogOptions{Match: "panic", Context: 1})
	if got.Matches != 10 || !strings.Contains(got.Notice, "5 earlier matching lines are not shown") {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Text, "panic number 9") || strings.Contains(got.Text, "panic number 0") {
		t.Errorf("the newest matches matter most: %q", got.Text)
	}
}

func TestTailBufferKeepsTheEndAndCountsAll(t *testing.T) {
	tb := &TailBuffer{Keep: 10}
	for i := 0; i < 100; i++ {
		fmt.Fprintf(tb, "%02d,", i)
	}
	if tb.Total() != 300 || string(tb.Bytes()) != ",97,98,99," {
		t.Errorf("total %d kept %q", tb.Total(), tb.Bytes())
	}
}

// ---- GitHub ----------------------------------------------------------------------

func ghRunJSON(id int, status, conclusion string) map[string]any {
	return map[string]any{
		"id": id, "name": "CI", "display_title": "Add retries", "event": "pull_request", "status": status, "conclusion": conclusion,
		"head_sha": "abc123", "head_branch": "feat/retries", "run_number": 41, "run_attempt": 1,
		"run_started_at": "2026-10-16T10:00:00Z", "created_at": "2026-10-16T09:59:50Z", "updated_at": "2026-10-16T10:03:30Z",
		"html_url": fmt.Sprintf("https://github.com/acme/widgets/actions/runs/%d", id),
	}
}

func TestGitHubRunsFilterByBranchAndShaAndMapStatus(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /repos/acme/widgets/actions/runs", map[string]any{"workflow_runs": []any{
		ghRunJSON(3, "in_progress", ""), ghRunJSON(2, "completed", "failure"), ghRunJSON(1, "completed", "success"),
		ghRunJSON(4, "completed", "cancelled"), ghRunJSON(5, "queued", ""), ghRunJSON(6, "completed", "timed_out"),
	}})
	runs, err := f.provider().Runs(bg, cred, repo, RunQuery{Branch: "feat/retries", SHA: "abc123", Limit: 99})
	if err != nil || len(runs) != 6 {
		t.Fatalf("%+v %v", runs, err)
	}
	want := []RunStatus{RunRunning, RunFailed, RunPassed, RunCancelled, RunQueued, RunFailed}
	for i, w := range want {
		if runs[i].Status != w {
			t.Errorf("run %d: %q, want %q", i, runs[i].Status, w)
		}
	}
	if runs[1].DurationSec != 210 || runs[0].DurationSec != 0 || runs[1].ID != "2" || runs[1].Event != "pull_request" || runs[1].Conclusion != "failure" || runs[1].Number != 41 {
		t.Errorf("%+v", runs[1])
	}
	q := f.requests("GET /repos/acme/widgets/actions/runs")[0].Query
	for _, w := range []string{"branch=feat%2Fretries", "head_sha=abc123", "per_page=30"} {
		if !strings.Contains(q, w) {
			t.Errorf("query %q lacks %q", q, w)
		}
	}
}

func TestGitHubRunsAreConditionalAndRefuseNothingForALongLimit(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /repos/acme/widgets/actions/runs", map[string]any{"workflow_runs": []any{ghRunJSON(1, "completed", "success")}})
	g := f.provider()
	if _, err := g.Runs(bg, cred, repo, RunQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Runs(bg, cred, repo, RunQuery{}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().NotModified != 1 {
		t.Errorf("a repeated listing must be answered 304: %+v", g.Stats())
	}
}

func serveRun(f *fakeHost) {
	f.json("GET /repos/acme/widgets/actions/runs/2", ghRunJSON(2, "completed", "failure"))
	step := func(n int, name, conclusion string) map[string]any {
		return map[string]any{"number": n, "name": name, "status": "completed", "conclusion": conclusion,
			"started_at": "2026-10-16T10:00:00Z", "completed_at": "2026-10-16T10:00:20Z"}
	}
	f.json("GET /repos/acme/widgets/actions/runs/2/jobs", map[string]any{"jobs": []any{
		map[string]any{"id": 22, "name": "lint", "status": "completed", "conclusion": "failure", "started_at": "2026-10-16T10:00:30Z", "completed_at": "2026-10-16T10:01:00Z",
			"html_url": "https://github.com/acme/widgets/actions/runs/2/job/22", "steps": []any{step(1, "Checkout", "success"), step(2, "golangci-lint", "failure"), step(3, "Post", "failure")}},
		map[string]any{"id": 21, "name": "test", "status": "completed", "conclusion": "failure", "started_at": "2026-10-16T10:00:00Z", "completed_at": "2026-10-16T10:03:00Z",
			"html_url": "https://github.com/acme/widgets/actions/runs/2/job/21", "steps": []any{step(1, "go test", "failure")}},
		map[string]any{"id": 23, "name": "build", "status": "completed", "conclusion": "success", "started_at": "2026-10-16T10:00:00Z", "completed_at": "2026-10-16T10:00:10Z"},
	}})
	f.json("GET /repos/acme/widgets/check-runs/22/annotations", []any{
		map[string]any{"path": "internal/a.go", "start_line": 12, "end_line": 12, "annotation_level": "failure", "title": "unused", "message": "func x is unused (unused)"},
		map[string]any{"path": "internal/b.go", "start_line": 3, "end_line": 4, "annotation_level": "warning", "message": "shadowed"},
	})
	f.json("GET /repos/acme/widgets/check-runs/21/annotations", []any{
		map[string]any{"path": ".github", "start_line": 1, "end_line": 1, "annotation_level": "failure", "message": "Process completed with exit code 1."},
	})
}

func TestGitHubRunHasJobsStepsFirstFailureAndAnnotations(t *testing.T) {
	f := newFakeHost(t)
	serveRun(f)
	d, err := f.provider().Run(bg, cred, repo, "2")
	if err != nil {
		t.Fatal(err)
	}
	if d.Run.Status != RunFailed || len(d.Jobs) != 3 {
		t.Fatalf("%+v", d)
	}
	byName := map[string]Job{}
	for _, j := range d.Jobs {
		byName[j.Name] = j
	}
	if !byName["test"].FirstFailed || byName["lint"].FirstFailed || byName["build"].FirstFailed {
		t.Errorf("the earliest failed job is the first failure: %+v", d.Jobs)
	}
	lint := byName["lint"]
	if lint.ID != "22" || lint.DurationSec != 30 || lint.Status != CheckFailed || lint.URL == "" {
		t.Errorf("%+v", lint)
	}
	firsts := 0
	for _, s := range lint.Steps {
		if s.FirstFailed {
			firsts++
			if s.Name != "golangci-lint" {
				t.Errorf("first failed step %q", s.Name)
			}
		}
	}
	if firsts != 1 || lint.Steps[1].DurationSec != 20 {
		t.Errorf("steps %+v", lint.Steps)
	}
	if len(d.Annotations) != 3 || d.Annotations[0].Level != "failure" || d.Annotations[2].Level != "warning" {
		t.Fatalf("failures come first: %+v", d.Annotations)
	}
	var lintFinding Annotation
	for _, a := range d.Annotations {
		if a.Path == "internal/a.go" {
			lintFinding = a
		}
	}
	if lintFinding.StartLine != 12 || lintFinding.Message != "func x is unused (unused)" || lintFinding.JobID != "22" || lintFinding.Title != "unused" {
		t.Errorf("%+v", lintFinding)
	}
	if len(f.requests("GET /repos/acme/widgets/check-runs/23")) != 0 {
		t.Error("annotations of a passed job were fetched")
	}
}

func TestGitHubRunLosesAnnotationsWithANote(t *testing.T) {
	f := newFakeHost(t)
	serveRun(f)
	f.status("GET /repos/acme/widgets/check-runs/22/annotations", 403, "Resource not accessible", nil)
	d, err := f.provider().Run(bg, cred, repo, "2")
	if err != nil || len(d.Notes) != 1 || !strings.Contains(d.Notes[0], "job 22") || len(d.Annotations) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := f.provider().Run(bg, cred, repo, "abc"); KindOf(err) != ErrInvalid {
		t.Errorf("a run id is a number: %v", err)
	}
}

func TestGitHubChecksCarryTheJobIdAndRunIdForLogs(t *testing.T) {
	run := ghCheckRun{ID: 77, Name: "unit", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/widgets/actions/runs/55/job/77"}
	ch := checkOfRun(run)
	if ch.ID != "77" || ch.RunID != "55" {
		t.Errorf("%+v", ch)
	}
	app := checkOfRun(ghCheckRun{ID: 78, Name: "codecov", Status: "completed", Conclusion: "failure", HTMLURL: "https://codecov.io/x"})
	if app.ID != "" || app.RunID != "" {
		t.Errorf("an App's check has no log to read: %+v", app)
	}
}

// ---- GitHub job logs: the redirect must not carry the token ------------------------

type storage struct {
	srv  *httptest.Server
	mu   sync.Mutex
	auth []string
	hits int
}

func newStorage(t *testing.T, body string, code int) *storage {
	s := &storage{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits++
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()
		if code != 200 {
			w.WriteHeader(code)
			w.Write([]byte("<Error><Code>BlobNotFound</Code></Error>"))
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func redirectTo(f *fakeHost, jobID, location string) {
	f.json("GET /repos/acme/widgets/actions/jobs/"+jobID, map[string]any{"id": 22, "name": "lint", "status": "completed", "conclusion": "failure"})
	f.handle("GET /repos/acme/widgets/actions/jobs/"+jobID+"/logs", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusFound)
	})
}

func TestGitHubJobLogFollowsTheRedirectWithoutTheToken(t *testing.T) {
	store := newStorage(t, "2026-10-16T06:00:00.0000000Z step one\n2026-10-16T06:00:01.0000000Z \x1b[31mboom\x1b[0m\n", 200)
	f := newFakeHost(t)
	redirectTo(f, "22", store.srv.URL+"/blob/abc?sig=SECRET")
	tail, err := f.provider().JobLog(bg, cred, repo, "22")
	if err != nil {
		t.Fatal(err)
	}
	if tail.Name != "lint" || !tail.Complete || tail.Status != CheckFailed || tail.Total == 0 {
		t.Errorf("%+v", tail)
	}
	if got := CleanLog(tail.Tail); strings.Join(got, "|") != "step one|boom" {
		t.Errorf("%q", got)
	}
	if store.hits != 1 || store.auth[0] != "" {
		t.Fatalf("the storage host got Authorization %q (hits %d): a token must never follow a redirect to another host", store.auth, store.hits)
	}
	logs := f.requests("GET /repos/acme/widgets/actions/jobs/22/logs")
	if len(logs) != 1 || logs[0].Auth != "Bearer ghp_secret_token" {
		t.Errorf("the API call carries the token: %+v", logs)
	}
}

func TestGitHubJobLogRefusesARedirectThatIsNotAnAddress(t *testing.T) {
	for _, location := range []string{"", "file:///etc/passwd", "ftp://host/x", "/relative/path", "javascript:alert(1)"} {
		f := newFakeHost(t)
		redirectTo(f, "22", location)
		_, err := f.provider().JobLog(bg, cred, repo, "22")
		if KindOf(err) != ErrUnavailable {
			t.Errorf("%q: %v", location, err)
		}
	}
	// And over a real https API an http address is refused.
	g := NewGitHub()
	if _, err := g.safeRedirect("http://storage.example/log"); err == nil {
		t.Error("http storage address accepted for an https API")
	}
	if _, err := g.safeRedirect("https://storage.example/log?sig=1"); err != nil {
		t.Errorf("https storage address refused: %v", err)
	}
}

func TestGitHubJobLogExpiredAndMissing(t *testing.T) {
	for name, code := range map[string]int{"gone": 410, "not found": 404} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHost(t)
			f.json("GET /repos/acme/widgets/actions/jobs/22", map[string]any{"id": 22, "name": "lint", "status": "completed", "conclusion": "success"})
			f.status("GET /repos/acme/widgets/actions/jobs/22/logs", code, "Gone", nil)
			_, err := f.provider().JobLog(bg, cred, repo, "22")
			if KindOf(err) != ErrNotFound || !strings.Contains(err.Error(), "expired, was deleted, or the job produced none") {
				t.Fatalf("%v", err)
			}
		})
	}
	store := newStorage(t, "", 404)
	f := newFakeHost(t)
	redirectTo(f, "22", store.srv.URL+"/blob/gone")
	if _, err := f.provider().JobLog(bg, cred, repo, "22"); KindOf(err) != ErrNotFound || !strings.Contains(err.Error(), "expired") {
		t.Errorf("storage says it is gone: %v", err)
	}
	f2 := newFakeHost(t)
	f2.status("GET /repos/acme/widgets/actions/jobs/99", 404, "Not Found", nil)
	if _, err := f2.provider().JobLog(bg, cred, repo, "99"); KindOf(err) != ErrNotFound {
		t.Errorf("no such job: %v", err)
	}
	if _, err := f2.provider().JobLog(bg, cred, repo, "../runs"); KindOf(err) != ErrInvalid {
		t.Errorf("a job id is a number: %v", err)
	}
}

func TestGitHubJobLogOfARunningJobIsNotComplete(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /repos/acme/widgets/actions/jobs/22", map[string]any{"id": 22, "name": "lint", "status": "in_progress"})
	f.handle("GET /repos/acme/widgets/actions/jobs/22/logs", func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.Write([]byte("so far\n")) })
	tail, err := f.provider().JobLog(bg, cred, repo, "22")
	if err != nil || tail.Complete || tail.Status != CheckRunning || string(tail.Tail) != "so far\n" {
		t.Fatalf("%+v %v", tail, err)
	}
}

func TestGitHubJobLogOfABigLogKeepsOnlyTheTailAndCountsAll(t *testing.T) {
	big := strings.Repeat("a line of build output that goes on\n", 200_000) + "THE END\n" // ~7 MB
	store := newStorage(t, big, 200)
	f := newFakeHost(t)
	redirectTo(f, "22", store.srv.URL+"/blob/big")
	tail, err := f.provider().JobLog(bg, cred, repo, "22")
	if err != nil {
		t.Fatal(err)
	}
	if tail.Total != int64(len(big)) || len(tail.Tail) > logKeep || !strings.HasSuffix(string(tail.Tail), "THE END\n") {
		t.Errorf("total %d (want %d), kept %d", tail.Total, len(big), len(tail.Tail))
	}
	cut, _ := CutLog(CleanLog(tail.Tail), LogOptions{})
	if cut.ReturnedLines != DefaultLogLines || !strings.HasSuffix(cut.Text, "THE END") {
		t.Errorf("%+v", cut.ReturnedLines)
	}
}

// ---- GitLab ----------------------------------------------------------------------

func TestGitLabRunsAndRunDetail(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /projects/group/sub/widgets/pipelines", []any{
		map[string]any{"id": 90, "iid": 12, "status": "failed", "source": "merge_request_event", "ref": "feat/retries", "sha": "abc", "web_url": "https://gitlab.com/p/90", "created_at": "2026-10-16T10:00:00Z", "updated_at": "2026-10-16T10:05:00Z"},
		map[string]any{"id": 89, "iid": 11, "status": "running", "ref": "feat/retries", "sha": "abb", "created_at": "2026-10-16T09:00:00Z"},
		map[string]any{"id": 88, "iid": 10, "status": "canceled", "ref": "feat/retries", "sha": "aba", "created_at": "2026-10-16T08:00:00Z"},
		map[string]any{"id": 87, "iid": 9, "status": "success", "ref": "feat/retries", "sha": "ab9", "created_at": "2026-10-16T07:00:00Z"},
	})
	g := f.gitlab()
	runs, err := g.Runs(bg, cred, glRepo, RunQuery{Branch: "feat/retries", SHA: "abc"})
	if err != nil || len(runs) != 4 {
		t.Fatalf("%+v %v", runs, err)
	}
	if runs[0].Status != RunFailed || runs[1].Status != RunRunning || runs[2].Status != RunCancelled || runs[3].Status != RunPassed || runs[0].Event != "merge_request_event" || runs[0].Number != 12 {
		t.Errorf("%+v", runs)
	}
	q := f.requests("GET /projects/group/sub/widgets/pipelines")[0].Query
	if !strings.Contains(q, "ref=feat%2Fretries") || !strings.Contains(q, "sha=abc") {
		t.Errorf("query %q", q)
	}

	f.json("GET /projects/group/sub/widgets/pipelines/90", map[string]any{"id": 90, "iid": 12, "status": "failed", "ref": "feat/retries", "sha": "abc", "duration": 215.4, "started_at": "2026-10-16T10:00:00Z"})
	f.json("GET /projects/group/sub/widgets/pipelines/90/jobs", []any{
		map[string]any{"id": 501, "name": "build", "stage": "build", "status": "success", "started_at": "2026-10-16T10:00:00Z", "duration": 40.0},
		map[string]any{"id": 502, "name": "unit", "stage": "test", "status": "failed", "failure_reason": "script_failure", "started_at": "2026-10-16T10:01:00Z", "duration": 90.0, "web_url": "https://gitlab.com/j/502"},
		map[string]any{"id": 503, "name": "integration", "stage": "test", "status": "failed", "started_at": "2026-10-16T10:01:30Z", "duration": 60.0},
	})
	f.json("GET /projects/group/sub/widgets/pipelines/90/test_report", map[string]any{"test_suites": []any{map[string]any{"name": "rspec", "test_cases": []any{
		map[string]any{"status": "success", "name": "ok"},
		map[string]any{"status": "failed", "name": "retries on 503", "classname": "RetrySpec", "file": "spec/retry_spec.rb", "system_output": "expected 3 got 1"},
		map[string]any{"status": "error", "name": "boot", "stack_trace": "NoMethodError"},
	}}}})
	d, err := g.Run(bg, cred, glRepo, "90")
	if err != nil {
		t.Fatal(err)
	}
	if d.Run.DurationSec != 215 || len(d.Jobs) != 3 {
		t.Fatalf("%+v", d)
	}
	if d.Jobs[1].ID != "502" || d.Jobs[1].Stage != "test" || d.Jobs[1].FailureReason != "script failure" || !d.Jobs[1].FirstFailed || d.Jobs[2].FirstFailed || d.Jobs[0].FirstFailed {
		t.Errorf("%+v", d.Jobs)
	}
	if len(d.Annotations) != 2 || d.Annotations[0].Path != "spec/retry_spec.rb" || d.Annotations[0].Message != "expected 3 got 1" || d.Annotations[0].Title != "RetrySpec · retries on 503" || d.Annotations[1].Message != "NoMethodError" {
		t.Errorf("%+v", d.Annotations)
	}

	// A pipeline that published no test report is simply without annotations.
	f.status("GET /projects/group/sub/widgets/pipelines/90/test_report", 404, "404 Not Found", nil)
	d, err = g.Run(bg, cred, glRepo, "90")
	if err != nil || len(d.Annotations) != 0 || len(d.Notes) != 0 {
		t.Errorf("%+v %v", d, err)
	}
	if _, err := g.Run(bg, cred, glRepo, "x"); KindOf(err) != ErrInvalid {
		t.Errorf("%v", err)
	}
}

func TestGitLabJobLogTrace(t *testing.T) {
	f := newFakeHost(t)
	f.json("GET /projects/group/sub/widgets/jobs/502", map[string]any{"id": 502, "name": "unit", "status": "failed"})
	f.handle("GET /projects/group/sub/widgets/jobs/502/trace", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Write([]byte("Running with gitlab-runner\r\nsection_start:1:step_script\r\x1b[0K\x1b[32;1m$ rspec\x1b[0;m\nFailures: 1\n"))
	})
	tail, err := f.gitlab().JobLog(bg, cred, glRepo, "502")
	if err != nil || tail.Name != "unit" || !tail.Complete || tail.Status != CheckFailed {
		t.Fatalf("%+v %v", tail, err)
	}
	if got := strings.Join(CleanLog(tail.Tail), "|"); got != "Running with gitlab-runner|$ rspec|Failures: 1" {
		t.Errorf("%q", got)
	}
	if logs := f.requests("GET /projects/group/sub/widgets/jobs/502/trace"); logs[0].Auth != "Bearer ghp_secret_token" {
		t.Errorf("%+v", logs)
	}

	f.json("GET /projects/group/sub/widgets/jobs/503", map[string]any{"id": 503, "name": "slow", "status": "running"})
	f.handle("GET /projects/group/sub/widgets/jobs/503/trace", func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.Write([]byte("working\n")) })
	if tail, err := f.gitlab().JobLog(bg, cred, glRepo, "503"); err != nil || tail.Complete {
		t.Errorf("a running job's trace is not whole: %+v %v", tail, err)
	}

	f.json("GET /projects/group/sub/widgets/jobs/504", map[string]any{"id": 504, "name": "old", "status": "success"})
	f.status("GET /projects/group/sub/widgets/jobs/504/trace", 404, "404 Not Found", nil)
	if _, err := f.gitlab().JobLog(bg, cred, glRepo, "504"); KindOf(err) != ErrNotFound || !strings.Contains(err.Error(), "not available") {
		t.Errorf("%v", err)
	}

	f.json("GET /projects/group/sub/widgets/jobs/505", map[string]any{"id": 505, "name": "x", "status": "success"})
	other := newStorage(t, "secret log", 200)
	f.handle("GET /projects/group/sub/widgets/jobs/505/trace", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.Header().Set("Location", other.srv.URL+"/x")
		w.WriteHeader(302)
	})
	if _, err := f.gitlab().JobLog(bg, cred, glRepo, "505"); KindOf(err) != ErrUnavailable || other.hits != 0 {
		t.Errorf("a redirect is not followed: %v hits=%d", err, other.hits)
	}
}

func TestGitLabChecksCarryJobAndPipelineIds(t *testing.T) {
	ch := checkOfJob(glJob{ID: 502, Name: "unit", Status: "failed"})
	if ch.ID != "502" {
		t.Errorf("%+v", ch)
	}
	f := newFakeHost(t)
	serveGitLab(f, sampleMR(nil), []map[string]any{job("unit", "failed", map[string]any{"id": 502})})
	checks, err := f.gitlab().Checks(bg, cred, glRepo, 7)
	if err != nil || len(checks) != 1 || checks[0].ID != "502" || checks[0].RunID != "99" {
		t.Fatalf("%+v %v", checks, err)
	}
}
