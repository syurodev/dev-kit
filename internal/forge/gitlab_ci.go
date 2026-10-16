package forge

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type glPipeline struct {
	ID        int       `json:"id"`
	IID       int       `json:"iid"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	Ref       string    `json:"ref"`
	SHA       string    `json:"sha"`
	Name      string    `json:"name"`
	WebURL    string    `json:"web_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	StartedAt time.Time `json:"started_at"`
	Finished  time.Time `json:"finished_at"`
	Duration  float64   `json:"duration"`
}

func runOfPipeline(p glPipeline) Run {
	started := p.StartedAt
	if started.IsZero() {
		started = p.CreatedAt
	}
	run := Run{
		ID: strconv.Itoa(p.ID), Name: p.Name, Event: p.Source, Conclusion: p.Status, SHA: p.SHA, Branch: p.Ref,
		Number: p.IID, StartedAt: started, URL: p.WebURL,
	}
	if run.Name == "" {
		run.Name = "pipeline #" + strconv.Itoa(p.ID)
	}
	switch p.Status {
	case "created", "pending", "waiting_for_resource", "preparing", "scheduled", "manual":
		run.Status = RunQueued
	case "running":
		run.Status = RunRunning
	case "success":
		run.Status = RunPassed
	case "canceled", "canceling":
		run.Status = RunCancelled
	case "skipped":
		run.Status = RunSkipped
	default: // failed
		run.Status = RunFailed
	}
	if p.Duration > 0 {
		run.DurationSec = int(p.Duration)
	} else if !p.Finished.IsZero() {
		run.DurationSec = seconds(started, p.Finished)
	}
	return run
}

func (g *GitLab) Runs(ctx context.Context, c Credential, repo Repo, q RunQuery) ([]Run, error) {
	if err := validProject(repo); err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultRuns
	}
	if limit > maxRuns {
		limit = maxRuns
	}
	query := url.Values{"per_page": {strconv.Itoa(limit)}, "order_by": {"id"}, "sort": {"desc"}}
	if q.Branch != "" {
		query.Set("ref", q.Branch)
	}
	if q.SHA != "" {
		query.Set("sha", q.SHA)
	}
	var pipelines []glPipeline
	if _, err := g.get(ctx, c, projectPath(repo)+"/pipelines", query, &pipelines); err != nil {
		return nil, err
	}
	runs := make([]Run, 0, len(pipelines))
	for _, p := range pipelines {
		runs = append(runs, runOfPipeline(p))
	}
	return runs, nil
}

func (g *GitLab) Run(ctx context.Context, c Credential, repo Repo, runID string) (RunDetail, error) {
	if err := validProject(repo); err != nil {
		return RunDetail{}, err
	}
	if _, err := strconv.Atoi(runID); err != nil {
		return RunDetail{}, &Error{Kind: ErrInvalid, Message: "not a pipeline id: " + runID}
	}
	var p glPipeline
	if _, err := g.get(ctx, c, projectPath(repo)+"/pipelines/"+runID, nil, &p); err != nil {
		return RunDetail{}, err
	}
	detail := RunDetail{Run: runOfPipeline(p)}

	var jobs []struct {
		glJob
		ID         int       `json:"id"`
		StartedAt  time.Time `json:"started_at"`
		FinishedAt time.Time `json:"finished_at"`
		Duration   float64   `json:"duration"`
	}
	next := ""
	for page := 0; page < maxPages && len(jobs) < maxJobsPerRun; page++ {
		var batch []struct {
			glJob
			ID         int       `json:"id"`
			StartedAt  time.Time `json:"started_at"`
			FinishedAt time.Time `json:"finished_at"`
			Duration   float64   `json:"duration"`
		}
		var err error
		if page == 0 {
			next, err = g.get(ctx, c, projectPath(repo)+"/pipelines/"+runID+"/jobs", url.Values{"per_page": {"100"}}, &batch)
		} else {
			next, err = g.getURL(ctx, c, next, &batch)
		}
		if err != nil {
			return RunDetail{}, err
		}
		jobs = append(jobs, batch...)
		if next == "" {
			break
		}
	}
	for _, j := range jobs {
		check := checkOfJob(j.glJob)
		job := Job{
			ID: strconv.Itoa(j.ID), Name: j.Name, Stage: j.Stage, Status: check.Status, Conclusion: check.Conclusion,
			StartedAt: j.StartedAt, DurationSec: int(j.Duration), URL: j.WebURL, FailureReason: strings.ReplaceAll(j.FailureReason, "_", " "),
		}
		if job.DurationSec == 0 {
			job.DurationSec = seconds(j.StartedAt, j.FinishedAt)
		}
		detail.Jobs = append(detail.Jobs, job)
	}
	markFirstFailedJob(detail.Jobs)

	// A pipeline's test report is the closest GitLab has to annotations: the
	// failed cases, with the file when the runner recorded one. Not every
	// pipeline publishes one; 404 is simply "none".
	if detail.Run.Status == RunFailed {
		var report struct {
			Suites []struct {
				Name  string `json:"name"`
				Cases []struct {
					Status       string `json:"status"`
					Name         string `json:"name"`
					Classname    string `json:"classname"`
					File         string `json:"file"`
					SystemOutput any    `json:"system_output"`
					StackTrace   string `json:"stack_trace"`
				} `json:"test_cases"`
			} `json:"test_suites"`
		}
		if _, err := g.get(ctx, c, projectPath(repo)+"/pipelines/"+runID+"/test_report", nil, &report); err != nil {
			if KindOf(err) != ErrNotFound {
				detail.Notes = append(detail.Notes, "test report unavailable: "+err.Error())
			}
		} else {
			for _, suite := range report.Suites {
				for _, tc := range suite.Cases {
					if tc.Status != "failed" && tc.Status != "error" {
						continue
					}
					if len(detail.Annotations) >= maxAnnotations {
						detail.Notes = append(detail.Notes, "failed tests were cut to the first "+strconv.Itoa(maxAnnotations))
						break
					}
					message := tc.StackTrace
					if s, ok := tc.SystemOutput.(string); ok && strings.TrimSpace(s) != "" {
						message = s
					}
					title := tc.Name
					if tc.Classname != "" {
						title = tc.Classname + " · " + tc.Name
					}
					detail.Annotations = append(detail.Annotations, Annotation{
						Path: tc.File, Level: "failure", Title: clip(title, summaryClip), Message: clip(message, annotationMsgClip),
					})
				}
			}
		}
	}
	return detail, nil
}

// JobLog reads a job's trace. It is on the API host and the credential goes to
// it alone; a redirect is not followed with the token, and is not expected.
func (g *GitLab) JobLog(ctx context.Context, c Credential, repo Repo, jobID string) (LogTail, error) {
	if err := validProject(repo); err != nil {
		return LogTail{}, err
	}
	if _, err := strconv.Atoi(jobID); err != nil {
		return LogTail{}, &Error{Kind: ErrInvalid, Message: "not a job id: " + jobID}
	}
	var job struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if _, err := g.get(ctx, c, projectPath(repo)+"/jobs/"+jobID, nil, &job); err != nil {
		return LogTail{}, err
	}
	check := checkOfJob(glJob{Name: job.Name, Status: job.Status})
	tail := LogTail{JobID: jobID, Name: job.Name, Status: check.Status, Complete: check.Status != CheckRunning}

	req, err := g.newRequest(ctx, c, http.MethodGet, g.base+projectPath(repo)+"/jobs/"+jobID+"/trace", nil)
	if err != nil {
		return LogTail{}, err
	}
	res, err := g.fetchTail(ctx, req, false)
	if err != nil {
		return LogTail{}, err
	}
	if res.Tail == nil {
		if res.Status >= 300 && res.Status < 400 {
			return LogTail{}, &Error{Kind: ErrUnavailable, Message: "the host answered the log request with a redirect, which is not followed"}
		}
		return LogTail{}, logGone(res)
	}
	tail.Tail, tail.Total = res.Tail.Bytes(), res.Tail.Total()
	return tail, nil
}
