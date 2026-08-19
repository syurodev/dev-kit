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

const (
	defaultRetryDelay            = time.Minute
	defaultMaxInlinePushAttempts = 3
	defaultInlinePushBackoff     = 100 * time.Millisecond
	// defaultDeadLetterAttempts is the number of consecutive preparation
	// failures after which a poison outbox row is marked failed instead of
	// retried. Preparation failures are permanent (the stored row cannot encode
	// a valid push operation), so retrying forever only hammers every SyncNow.
	defaultDeadLetterAttempts = 5
	// maxPushBatchOperations matches the wire limit the transport enforces;
	// "all ready changes" (limit 0) must still respect it or every push would
	// fail on an oversized backlog and retry forever.
	maxPushBatchOperations = 1000
)

// RunReport describes one local push attempt. It contains counts only and no
// transport error text or payload data.
type RunReport struct {
	Attempted int
	Sent      int
	Retried   int
	Conflicts int
	Failed    int
}

// Runner coordinates local outbox state with a push-only transport. It does
// not pull or apply remote data and is safe to use with either MemoryOutbox or
// a persistent Outbox implementation.
type Runner struct {
	outbox                Outbox
	transport             Transport
	now                   func() time.Time
	retryDelay            time.Duration
	maxInlinePushAttempts int
	inlinePushBackoff     time.Duration
	deadLetterAttempts    int
}

// Coordinator is kept as a descriptive alias for callers that use the
// coordination terminology from the replication design.
type Coordinator = Runner

// RunnerConfig contains local scheduling hooks. RetryDelay is kept bounded to
// a positive value so MarkRetry always receives a future timestamp.
// SyncingLease documents the outbox lease used to reclaim stale syncing rows;
// the runner does not implement reclaim itself beyond passing now to MarkSyncing.
type RunnerConfig struct {
	Clock                 func() time.Time
	RetryDelay            time.Duration
	SyncingLease          time.Duration
	MaxInlinePushAttempts int
	InlinePushBackoff     time.Duration
	// DeadLetterAttempts bounds consecutive preparation failures before a
	// poison row is marked failed (0 keeps the default).
	DeadLetterAttempts int
}

func NewRunner(outbox Outbox, transport Transport, config ...RunnerConfig) *Runner {
	runner := &Runner{
		outbox:                outbox,
		transport:             transport,
		now:                   func() time.Time { return time.Now().UTC() },
		retryDelay:            defaultRetryDelay,
		maxInlinePushAttempts: defaultMaxInlinePushAttempts,
		inlinePushBackoff:     defaultInlinePushBackoff,
		deadLetterAttempts:    defaultDeadLetterAttempts,
	}
	if len(config) > 0 {
		if config[0].Clock != nil {
			runner.now = config[0].Clock
		}
		if config[0].RetryDelay > 0 {
			runner.retryDelay = config[0].RetryDelay
		}
		if config[0].MaxInlinePushAttempts > 0 {
			runner.maxInlinePushAttempts = config[0].MaxInlinePushAttempts
		}
		if config[0].InlinePushBackoff > 0 {
			runner.inlinePushBackoff = config[0].InlinePushBackoff
		}
		if config[0].DeadLetterAttempts > 0 {
			runner.deadLetterAttempts = config[0].DeadLetterAttempts
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
	selected, prepared, poison := preparePushPrefix(changes)
	if len(selected) > maxPushBatchOperations {
		// Keep the oldest prefix; Pending orders by creation time, so the
		// remaining rows ship on the next Run.
		selected = selected[:maxPushBatchOperations]
	}

	// A row that cannot be prepared at all (stored data cannot encode a valid
	// push operation) is excluded from every batch. Preparation failures are
	// permanent, so after the dead-letter threshold it is marked failed instead
	// of retried — it stays visible (and still causally blocks its successors,
	// which the server would reject as gaps) rather than hammering each SyncNow.
	for _, change := range poison {
		if change.Attempts+1 >= r.deadLetterAttempts {
			if err := r.outbox.MarkFailed(change.IdempotencyKey, FailureInvalidResponse); err != nil {
				return report, err
			}
			report.Failed++
			continue
		}
		if err := r.markRetry(change, FailureInvalidResponse, now); err != nil {
			return report, err
		}
		report.Retried++
	}

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

	result, pushErr := r.pushWithInlineRetry(ctx, operations)
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
// HTTP JSON request budget before any row is marked syncing. Changes that
// cannot be prepared are returned as poison (never part of the batch) so the
// runner can dead-letter them separately. Other transports inherit the
// conservative bound, avoiding a retry loop on aggregate size.
func preparePushPrefix(changes []Change) (selected []Change, prepared map[string]PushOperation, poison []Change) {
	selected = make([]Change, 0, len(changes))
	operations := make([]PushOperation, 0, len(changes))
	prepared = make(map[string]PushOperation, len(changes))
	for _, change := range changes {
		operation, err := pushOperationFromChange(change)
		if err != nil {
			poison = append(poison, change)
			continue
		}
		candidate := append(operations, operation)
		body, err := json.Marshal(pushRequest{Operations: candidate})
		if err != nil {
			// A PushOperation of validated scalar fields cannot fail to marshal;
			// treat it as a wire-budget failure and keep the prefix so far.
			return selected, prepared, poison
		}
		if len(body) > maxHTTPRequestBytes {
			// The shared snapshot size cap keeps a single change well under the
			// wire budget, so this is unreachable in production; leave the
			// unselected rows pending rather than dead-lettering valid data.
			return selected, prepared, poison
		}
		selected = append(selected, change)
		operations = candidate
		prepared[change.IdempotencyKey] = operation
	}
	return selected, prepared, poison
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

func (r *Runner) pushWithInlineRetry(ctx context.Context, operations []PushOperation) (PushResult, error) {
	attempts := r.maxInlinePushAttempts
	if attempts < 1 {
		attempts = 1
	}
	var (
		result  PushResult
		pushErr error
	)
	for attempt := 1; attempt <= attempts; attempt++ {
		result, pushErr = r.transport.Push(ctx, operations)
		if pushErr == nil {
			return result, nil
		}
		reason := classifyTransportError(pushErr)
		if reason != FailureNetwork && reason != FailureTimeout {
			return result, pushErr
		}
		if attempt == attempts {
			break
		}
		if err := sleepWithContext(ctx, r.inlineBackoff(attempt)); err != nil {
			return PushResult{}, err
		}
	}
	return result, pushErr
}

func (r *Runner) inlineBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := r.inlinePushBackoff
	if backoff <= 0 {
		backoff = defaultInlinePushBackoff
	}
	for i := 1; i < attempt; i++ {
		backoff *= 2
	}
	return backoff
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
