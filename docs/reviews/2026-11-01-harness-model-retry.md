# Model retry kernel slice review

Date: 2026-11-01

Scope: read-only review of the in-progress kernel retry slice on `codex/harness-model-retry`, against the accepted event catalog, state machine, and model adapter contracts. This review does not cover a provider runtime or persistence integration.

## Implemented contract

- `RunAccepted` pins the run retry budget and model retry policy version. A zero budget and zero policy version disable model retries.
- A failure records a typed stage, class, and disposition. Timeout and transport retries are accepted only before stream start; retryable failures must remain nonterminal so the planned retry can continue the invocation. Fatal failures are terminal. Interrupted attempts do not retry automatically; continuing from an abandoned unknown attempt requires an explicit retry policy reference.
- Scheduling commits `ModelRetryScheduled`, `ModelAttemptPlanned`, and `TimerScheduled` as one ordered batch. The retry budget is charged in that batch, before the wait, and remains charged if the host crashes. The chosen UTC millisecond deadline and policy version are durable; the timer does not create a new attempt when it elapses.
- `TimerElapsed` requires the retry deadline and current generation. It resolves the retry wait only; dispatch still requires the planned attempt and current admission/blocker gates. Attempt reservation is recorded at dispatch, as status only.
- Terminal stop cancels the planned retry attempt and timer together, including the elapsed-before-dispatch case. Suspend preserves the retry wait. Ownership recovery binds a canonical command to its event batch and places or retains recovery admission while a retry is planned.
- Canonical Evolve guards bind retry batches and terminal-stop batches that cancel retry waits to their command intent; they reject omitted, reordered, or unrelated events in those transitions.

## Limits and open work

- This is a bounded kernel slice. It adds no provider runtime, SQL/store integration, scheduler re-arm implementation, or host backoff/jitter calculation. The host supplies the chosen UTC deadline explicitly.
- There is no quantitative active-window accounting in this slice. Dispatch records a reservation status but no reservation amount.
- Zero retry budget disables the feature. Exhaustion rejects another retry schedule; the host must take the terminal budget-stop path. The kernel does not automatically emit that stop from retry exhaustion.
- Account failover is not implemented. Refusal/content-filter failure events and context-overflow compaction remain outside this slice and are rejected pending typed result/compaction handling. The adapter contract requires context overflow to compact and retry once.
- Metadata closure while retry timers remain pending still requires the existing quiescent-timer behavior.
- R1.1/R1.2 and G-02 remain partial. This implementation does not establish full transition review or activation readiness.

## Named evidence

The implementation agent reports `go test ./internal/harnessruntime/kernel -count=1` passing after the final retry guards. The focused contract tests include:

- `TestRetryContractAtomicPlanChargeAndDispatchFence`
- `TestRetryContractReducerBindsCanonicalCommandIntentToEvents`
- `TestRetryContractRejectsInvalidOrdinalLimitAndCrossInvocationAttempt`
- `TestRetryContractUnsupportedFailureClassesFailClosedOnEligibleAttempt`
- `TestRetryContractInterruptedAttemptNeedsExplicitAbandonmentAndPolicy`
- `TestRetryContractBudgetIsRunScopedAcrossInvocations`
- `TestRetryContractTerminalStopAfterElapsedCancelsBeforeDispatch`
- `TestRetryContractSuspendPreservesRetryWait`
- `TestRetryContractOwnershipRecoveryGatesElapsedRetryUntilAdmissionClear`

Final backend verification passed: `go test ./internal/... ./cmd/...` (exit 0), with output at `/tmp/devkit-model-retry-backend-final.log`. The root also reports final race, vet, and architecture checks passing. Lint ran after the final production and test changes as `GOTOOLCHAIN=go1.27.1 golangci-lint run --new-from-rev=b3c8ff5df42e6fc58c1cd8077dddd278fc7dd84e ./internal/... ./cmd/...` and reported 0 issues. No logs were provided for those additional checks. CI has not run for this slice.

This note records the bounded implementation and its limits; it is not a claim that R1.1/R1.2 or G-02 are complete.
