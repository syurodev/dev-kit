package forge

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRuns       = 10
	maxRuns           = 30
	maxAnnotatedJobs  = 5
	maxAnnotations    = 50
	annotationMsgClip = 600
	maxJobsPerRun     = 200
)

type ghRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Title      string    `json:"display_title"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HeadSHA    string    `json:"head_sha"`
	HeadBranch string    `json:"head_branch"`
	RunNumber  int       `json:"run_number"`
	Attempt    int       `json:"run_attempt"`
	StartedAt  time.Time `json:"run_started_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
}

func runOfGitHub(r ghRun) Run {
	started := r.StartedAt
	if started.IsZero() {
		started = r.CreatedAt
	}
	run := Run{
		ID: strconv.FormatInt(r.ID, 10), Name: r.Name, Event: r.Event, Conclusion: r.Conclusion,
		SHA: r.HeadSHA, Branch: r.HeadBranch, Number: r.RunNumber, Attempt: r.Attempt, StartedAt: started, URL: r.HTMLURL,
	}
	if run.Name == "" {
		run.Name = r.Title
	}
	switch {
	case r.Status == "queued" || r.Status == "waiting" || r.Status == "requested" || r.Status == "pending":
		run.Status = RunQueued
	case r.Status != "completed":
		run.Status = RunRunning
	default:
		switch r.Conclusion {
		case "success", "neutral":
			run.Status = RunPassed
		case "cancelled":
			run.Status = RunCancelled
		case "skipped":
			run.Status = RunSkipped
		default:
			run.Status = RunFailed
		}
		if !started.IsZero() && r.UpdatedAt.After(started) {
			run.DurationSec = int(r.UpdatedAt.Sub(started).Seconds())
		}
	}
	return run
}

func (g *GitHub) Runs(ctx context.Context, c Credential, repo Repo, q RunQuery) ([]Run, error) {
	if err := validRepo(repo); err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultRuns
	}
	if limit > maxRuns {
		limit = maxRuns
	}
	query := url.Values{"per_page": {strconv.Itoa(limit)}}
	if q.Branch != "" {
		query.Set("branch", q.Branch)
	}
	if q.SHA != "" {
		query.Set("head_sha", q.SHA)
	}
	var out struct {
		Runs []ghRun `json:"workflow_runs"`
	}
	if _, err := g.get(ctx, c, repoPath(repo)+"/actions/runs", query, &out); err != nil {
		return nil, err
	}
	runs := make([]Run, 0, len(out.Runs))
	for _, r := range out.Runs {
		runs = append(runs, runOfGitHub(r))
	}
	return runs, nil
}

type ghJob struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url"`
	Steps       []struct {
		Name        string    `json:"name"`
		Number      int       `json:"number"`
		Status      string    `json:"status"`
		Conclusion  string    `json:"conclusion"`
		StartedAt   time.Time `json:"started_at"`
		CompletedAt time.Time `json:"completed_at"`
	} `json:"steps"`
}

func seconds(from, to time.Time) int {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return 0
	}
	return int(to.Sub(from).Seconds())
}

func jobOfGitHub(j ghJob) Job {
	job := Job{
		ID: strconv.FormatInt(j.ID, 10), Name: j.Name, Status: checkStatusOf(j.Status, j.Conclusion), Conclusion: j.Conclusion,
		StartedAt: j.StartedAt, DurationSec: seconds(j.StartedAt, j.CompletedAt), URL: j.HTMLURL,
	}
	firstFailed := true
	for _, s := range j.Steps {
		step := Step{Number: s.Number, Name: s.Name, Status: checkStatusOf(s.Status, s.Conclusion), Conclusion: s.Conclusion, DurationSec: seconds(s.StartedAt, s.CompletedAt)}
		if step.Status == CheckFailed && firstFailed {
			step.FirstFailed, firstFailed = true, false
		}
		job.Steps = append(job.Steps, step)
	}
	return job
}

func (g *GitHub) Run(ctx context.Context, c Credential, repo Repo, runID string) (RunDetail, error) {
	if err := validRepo(repo); err != nil {
		return RunDetail{}, err
	}
	if _, err := strconv.ParseInt(runID, 10, 64); err != nil {
		return RunDetail{}, &Error{Kind: ErrInvalid, Message: "not a run id: " + runID}
	}
	var run ghRun
	if _, err := g.get(ctx, c, repoPath(repo)+"/actions/runs/"+runID, nil, &run); err != nil {
		return RunDetail{}, err
	}
	detail := RunDetail{Run: runOfGitHub(run)}

	var ghJobs []ghJob
	next := ""
	for page := 0; page < maxPages && len(ghJobs) < maxJobsPerRun; page++ {
		var out struct {
			Jobs []ghJob `json:"jobs"`
		}
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, repoPath(repo)+"/actions/runs/"+runID+"/jobs", url.Values{"per_page": {"100"}, "filter": {"latest"}}, &out)
		} else {
			next, err = g.getURL(ctx, c, next, &out)
		}
		if err != nil {
			return RunDetail{}, err
		}
		ghJobs = append(ghJobs, out.Jobs...)
		if next == "" {
			break
		}
	}
	for _, j := range ghJobs {
		detail.Jobs = append(detail.Jobs, jobOfGitHub(j))
	}
	markFirstFailedJob(detail.Jobs)

	// Annotations are what a lint or test run says about the code. They are
	// read for the failed jobs only, a handful of them, and a token that may
	// not read them costs a note, not the run.
	annotated := 0
	for _, job := range detail.Jobs {
		if job.Status != CheckFailed || annotated >= maxAnnotatedJobs || len(detail.Annotations) >= maxAnnotations {
			continue
		}
		annotated++
		var anns []struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
			Level     string `json:"annotation_level"`
			Title     string `json:"title"`
			Message   string `json:"message"`
		}
		if _, err := g.get(ctx, c, repoPath(repo)+"/check-runs/"+job.ID+"/annotations", url.Values{"per_page": {"50"}}, &anns); err != nil {
			detail.Notes = append(detail.Notes, "annotations of job "+job.ID+" unavailable: "+err.Error())
			continue
		}
		for _, a := range anns {
			detail.Annotations = append(detail.Annotations, Annotation{
				JobID: job.ID, Path: a.Path, StartLine: a.StartLine, EndLine: a.EndLine,
				Level: a.Level, Title: clip(a.Title, summaryClip), Message: clip(a.Message, annotationMsgClip),
			})
		}
	}
	sort.SliceStable(detail.Annotations, func(i, j int) bool {
		return levelRank(detail.Annotations[i].Level) < levelRank(detail.Annotations[j].Level)
	})
	if len(detail.Annotations) > maxAnnotations {
		detail.Annotations = detail.Annotations[:maxAnnotations]
		detail.Notes = append(detail.Notes, "annotations were cut to the first "+strconv.Itoa(maxAnnotations))
	}
	return detail, nil
}

func levelRank(level string) int {
	switch level {
	case "failure":
		return 0
	case "warning":
		return 1
	}
	return 2
}

// markFirstFailedJob flags the earliest failed job: in a pipeline whose later
// jobs failed because an earlier one did, that is the one to read.
func markFirstFailedJob(jobs []Job) {
	best := -1
	for i, j := range jobs {
		if j.Status != CheckFailed {
			continue
		}
		if best < 0 || (!j.StartedAt.IsZero() && (jobs[best].StartedAt.IsZero() || j.StartedAt.Before(jobs[best].StartedAt))) {
			best = i
		}
	}
	if best >= 0 {
		jobs[best].FirstFailed = true
	}
}

// JobLog reads a job's log. GitHub answers the logs endpoint with a redirect to
// a short-lived storage address; the credential goes to the API, and the
// redirect is followed by a request that carries none — an address named by a
// response is never handed a token.
func (g *GitHub) JobLog(ctx context.Context, c Credential, repo Repo, jobID string) (LogTail, error) {
	if err := validRepo(repo); err != nil {
		return LogTail{}, err
	}
	if _, err := strconv.ParseInt(jobID, 10, 64); err != nil {
		return LogTail{}, &Error{Kind: ErrInvalid, Message: "not a job id: " + jobID}
	}
	var job ghJob
	if _, err := g.get(ctx, c, repoPath(repo)+"/actions/jobs/"+jobID, nil, &job); err != nil {
		return LogTail{}, err
	}
	tail := LogTail{JobID: jobID, Name: job.Name, Status: checkStatusOf(job.Status, job.Conclusion), Complete: job.Status == "completed"}

	req, err := g.newRequest(ctx, c, http.MethodGet, g.base+repoPath(repo)+"/actions/jobs/"+jobID+"/logs", nil)
	if err != nil {
		return LogTail{}, err
	}
	first, err := g.fetchTail(ctx, req, false)
	if err != nil {
		return LogTail{}, err
	}
	switch {
	case first.Status == http.StatusFound || first.Status == http.StatusMovedPermanently || first.Status == http.StatusTemporaryRedirect || first.Status == http.StatusSeeOther:
		location, err := g.safeRedirect(first.Header.Get("Location"))
		if err != nil {
			return LogTail{}, err
		}
		// A new request, built here with no Authorization header at all.
		follow, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return LogTail{}, &Error{Kind: ErrInvalid, Message: err.Error()}
		}
		follow.Header.Set("User-Agent", "DevKit")
		second, err := g.fetchTail(ctx, follow, true)
		if err != nil {
			return LogTail{}, err
		}
		if second.Tail == nil {
			return LogTail{}, logGone(second)
		}
		tail.Tail, tail.Total = second.Tail.Bytes(), second.Tail.Total()
	case first.Tail != nil:
		tail.Tail, tail.Total = first.Tail.Bytes(), first.Tail.Total()
	default:
		return LogTail{}, logGone(first)
	}
	return tail, nil
}

// safeRedirect accepts an https address (http only when the API itself is
// plain http, which is a test double).
func (g *GitHub) safeRedirect(location string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(location))
	if err != nil || u.Host == "" {
		return "", &Error{Kind: ErrUnavailable, Message: "the host sent a log address that was not understood"}
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && strings.HasPrefix(g.base, "http://")) {
		return "", &Error{Kind: ErrUnavailable, Message: "the host sent a log address that is not https"}
	}
	return u.String(), nil
}

// logGone words a log that cannot be had: expired, deleted, or never kept.
func logGone(r tailResult) error {
	err := r.failure()
	if fe, ok := err.(*Error); ok && (r.Status == http.StatusNotFound || r.Status == http.StatusGone) {
		fe.Kind = ErrNotFound
		fe.Message = "the log is not available: it expired, was deleted, or the job produced none (" + fe.Message + ")"
	}
	return err
}
