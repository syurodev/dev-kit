package prwatch

import (
	"context"
	"fmt"

	"devkit/internal/forge"
)

// JobLog is one job's log, cleaned and cut, with how it was cut said plainly.
type JobLog struct {
	JobID  string            `json:"jobId"`
	Name   string            `json:"name"`
	Status forge.CheckStatus `json:"status"`
	// Complete is false while the job is still running: the log is not whole yet.
	Complete bool `json:"complete"`
	// TotalBytes is what the host sent; only the last part of a very large log is read.
	TotalBytes int64 `json:"totalBytes"`
	forge.LogText
}

// Runs lists recent workflow runs or pipelines. With no query it is the
// folder's branch.
func (h *Handle) Runs(ctx context.Context, q forge.RunQuery) ([]forge.Run, error) {
	if q.Branch == "" && q.SHA == "" {
		if h.checkout.Detached || h.checkout.Branch == "" {
			return nil, ErrDetached
		}
		q.Branch = h.checkout.Branch
	}
	return h.provider.Runs(ctx, h.cred, h.repo, q)
}

// Run is one run with its jobs and the annotations of those that failed.
func (h *Handle) Run(ctx context.Context, runID string) (forge.RunDetail, error) {
	return h.provider.Run(ctx, h.cred, h.repo, runID)
}

// JobLog reads the end of a job's log and cuts it as asked.
func (h *Handle) JobLog(ctx context.Context, jobID string, opts forge.LogOptions) (JobLog, error) {
	tail, err := h.provider.JobLog(ctx, h.cred, h.repo, jobID)
	if err != nil {
		return JobLog{}, err
	}
	cut, err := forge.CutLog(forge.CleanLog(tail.Tail), opts)
	if err != nil {
		return JobLog{}, err
	}
	out := JobLog{JobID: tail.JobID, Name: tail.Name, Status: tail.Status, Complete: tail.Complete, TotalBytes: tail.Total, LogText: cut}
	var notes []string
	if cut.Notice != "" {
		notes = append(notes, cut.Notice)
	}
	if tail.Total > int64(len(tail.Tail)) {
		notes = append(notes, fmt.Sprintf("The log is %d bytes; only its last %d were read.", tail.Total, len(tail.Tail)))
	}
	if !tail.Complete {
		notes = append(notes, "The job is still running: this is the log so far, not the whole of it.")
	}
	if len(notes) > 0 {
		out.Notice = ""
		for i, n := range notes {
			if i > 0 {
				out.Notice += " "
			}
			out.Notice += n
		}
	}
	return out, nil
}
