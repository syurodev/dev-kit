package sync

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrRunnerDependencies       = errors.New("sync runner dependencies are required")
	ErrInvalidTransportResponse = errors.New("invalid sync transport response")
)

const defaultRetryDelay = time.Minute

// RunReport describes one local push attempt. It contains counts only and no
// transport error text or payload data.
type RunReport struct {
	Attempted int
	Sent      int
	Retried   int
	Conflicts int
}

// Runner coordinates local outbox state with a push-only transport. It does
// not pull or apply remote data and is safe to use with either MemoryOutbox or
// a persistent Outbox implementation.
type Runner struct {
	outbox     Outbox
	transport  Transport
	now        func() time.Time
	retryDelay time.Duration
}

// Coordinator is kept as a descriptive alias for callers that use the
// coordination terminology from the replication design.
type Coordinator = Runner

// RunnerConfig contains local scheduling hooks. RetryDelay is kept bounded to
// a positive value so MarkRetry always receives a future timestamp.
// SyncingLease documents the outbox lease used to reclaim stale syncing rows;
// the runner does not implement reclaim itself beyond passing now to MarkSyncing.
type RunnerConfig struct {
	Clock        func() time.Time
	RetryDelay   time.Duration
	SyncingLease time.Duration
}

func NewRunner(outbox Outbox, transport Transport, config ...RunnerConfig) *Runner {
	runner := &Runner{
		outbox:     outbox,
		transport:  transport,
		now:        func() time.Time { return time.Now().UTC() },
		retryDelay: defaultRetryDelay,
	}
	if len(config) > 0 {
		if config[0].Clock != nil {
			runner.now = config[0].Clock
		}
		if config[0].RetryDelay > 0 {
			runner.retryDelay = config[0].RetryDelay
		}
	}
	return runner
}

func NewCoordinator(outbox Outbox, transport Transport, config ...RunnerConfig) *Coordinator {
	return NewRunner(outbox, transport, config...)
}

// Run pushes at most limit pending changes. A limit of zero means all ready
// changes. A malformed response is rejected after safely retrying the batch;
// acknowledged entries are marked sent only when the response explicitly
// acknowledges them.
func (r *Runner) Run(ctx context.Context, limit int) (RunReport, error) {
	var report RunReport
	if r == nil || r.outbox == nil || r.transport == nil {
		return report, ErrRunnerDependencies
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}

	now := r.now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	changes, err := r.outbox.Pending(now, limit)
	if err != nil {
		return report, err
	}
	if len(changes) == 0 {
		return report, nil
	}
	selected, prepared, prepareErr := preparePushPrefix(changes)

	ready := make([]Change, 0, len(selected))
	operations := make([]PushOperation, 0, len(selected))
	for _, change := range selected {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := r.outbox.MarkSyncing(change.IdempotencyKey, now); err != nil {
			if errors.Is(err, ErrInvalidState) {
				continue
			}
			return report, err
		}
		ready = append(ready, change)
		if operation, ok := prepared[change.IdempotencyKey]; ok {
			operations = append(operations, operation)
		}
	}
	if len(ready) == 0 {
		return report, nil
	}
	report.Attempted = len(ready)
	if prepareErr != nil {
		if retryErr := r.retryAll(ready, FailureInvalidResponse, now); retryErr != nil {
			return report, retryErr
		}
		report.Retried = len(ready)
		return report, prepareErr
	}

	result, pushErr := r.transport.Push(ctx, operations)
	if pushErr != nil {
		report.Retried = len(ready)
		if retryErr := r.retryAll(ready, classifyTransportError(pushErr), now); retryErr != nil {
			// Preserve the transport classification as well as the local outbox
			// failure. Session owners must still be able to detect an authentication
			// rejection and discard runtime credentials when retry bookkeeping fails.
			return report, errors.Join(pushErr, retryErr)
		}
		return report, pushErr
	}

	acknowledged, missing, validationErr := validatePushResult(ready, result)
	if validationErr != nil {
		if retryErr := r.retryAll(ready, FailureInvalidResponse, now); retryErr != nil {
			return report, retryErr
		}
		report.Retried = len(ready)
		return report, validationErr
	}

	for _, change := range ready {
		if conflict, ok := acknowledged.conflicts[change.IdempotencyKey]; ok {
			if err := r.outbox.MarkConflict(change.IdempotencyKey, conflict); err != nil {
				return report, err
			}
			report.Conflicts++
			continue
		}
		if acknowledged.sent[change.IdempotencyKey] {
			if err := r.outbox.MarkSent(change.IdempotencyKey); err != nil {
				return report, err
			}
			report.Sent++
			continue
		}
		if containsKey(missing, change.IdempotencyKey) {
			if err := r.markRetry(change, FailureInvalidResponse, now); err != nil {
				return report, err
			}
			report.Retried++
		}
	}
	if len(missing) > 0 {
		return report, ErrInvalidTransportResponse
	}
	return report, nil
}

// preparePushPrefix constructs the largest ready prefix that fits the exact
// HTTP JSON request budget before any row is marked syncing. Other transports
// inherit the conservative bound, avoiding a retry loop on aggregate size.
func preparePushPrefix(changes []Change) ([]Change, map[string]PushOperation, error) {
	selected := make([]Change, 0, len(changes))
	operations := make([]PushOperation, 0, len(changes))
	prepared := make(map[string]PushOperation, len(changes))
	for _, change := range changes {
		operation, err := pushOperationFromChange(change)
		if err != nil {
			if len(selected) == 0 {
				return []Change{change}, prepared, err
			}
			break
		}
		candidate := append(operations, operation)
		body, err := json.Marshal(pushRequest{Operations: candidate})
		if err != nil {
			return selected, prepared, ErrInvalidTransportResponse
		}
		if len(body) > maxHTTPRequestBytes {
			if len(selected) == 0 {
				return []Change{change}, prepared, ErrInvalidTransportResponse
			}
			break
		}
		selected = append(selected, change)
		operations = candidate
		prepared[change.IdempotencyKey] = operation
	}
	return selected, prepared, nil
}

type acknowledgedResult struct {
	sent      map[string]bool
	conflicts map[string]Conflict
}

func validatePushResult(changes []Change, result PushResult) (acknowledgedResult, []string, error) {
	known := make(map[string]Change, len(changes))
	for _, change := range changes {
		known[change.IdempotencyKey] = change
	}
	ack := acknowledgedResult{
		sent:      make(map[string]bool, len(result.SentIdempotencyKeys)),
		conflicts: make(map[string]Conflict, len(result.Conflicts)),
	}
	for _, key := range result.SentIdempotencyKeys {
		if _, ok := known[key]; !ok || key == "" || ack.sent[key] || ack.conflicts[key].ID != "" {
			return acknowledgedResult{}, nil, ErrInvalidTransportResponse
		}
		ack.sent[key] = true
	}
	for _, conflict := range result.Conflicts {
		change, ok := known[conflict.LocalChangeKey]
		if !ok || ack.sent[conflict.LocalChangeKey] {
			return acknowledgedResult{}, nil, ErrInvalidTransportResponse
		}
		if _, duplicate := ack.conflicts[conflict.LocalChangeKey]; duplicate {
			return acknowledgedResult{}, nil, ErrInvalidTransportResponse
		}
		if err := conflict.Validate(); err != nil || conflict.RecordID != change.RecordID || conflict.RecordType != change.RecordType {
			return acknowledgedResult{}, nil, ErrInvalidTransportResponse
		}
		ack.conflicts[conflict.LocalChangeKey] = cloneConflict(conflict)
	}

	missing := make([]string, 0, len(changes))
	for _, change := range changes {
		if !ack.sent[change.IdempotencyKey] {
			if _, conflict := ack.conflicts[change.IdempotencyKey]; !conflict {
				missing = append(missing, change.IdempotencyKey)
			}
		}
	}
	return ack, missing, nil
}

func (r *Runner) retryAll(changes []Change, reason FailureReason, now time.Time) error {
	for _, change := range changes {
		if err := r.markRetry(change, reason, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) markRetry(change Change, reason FailureReason, now time.Time) error {
	return r.outbox.MarkRetry(change.IdempotencyKey, now.Add(r.retryDelay), reason)
}

func classifyTransportError(err error) FailureReason {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return FailureTimeout
	}
	var reasonProvider interface{ FailureReason() FailureReason }
	if errors.As(err, &reasonProvider) {
		reason := reasonProvider.FailureReason()
		if validFailureReason(reason) {
			return reason
		}
		return FailureUnknown
	}
	return FailureNetwork
}

func containsKey(keys []string, target string) bool {
	for _, key := range keys {
		if key == target {
			return true
		}
	}
	return false
}
