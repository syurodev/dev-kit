package kernel

// ID is an opaque, caller-supplied identifier. It is never user content.
type ID string

type Lifecycle uint8

const (
	LifecycleUnknown Lifecycle = iota
	Queued
	Running
	Waiting
	Cancelling
	Interrupted
	Finished
)

type Phase uint8

const (
	PhasePreparing Phase = iota + 1
	PhaseCallingModel
	PhaseProcessingTools
	PhaseFinalizing
)

type StopMode uint8

const (
	StopNone StopMode = iota
	StopTerminal
	StopSuspend
)

type StopReason uint8

const (
	StopReasonNone StopReason = iota
	StopUserCancel
	StopParentCancel
	StopBudget
	StopFatalError
	StopShutdown
)

type RunOrigin uint8

const (
	OriginUser RunOrigin = iota + 1
	OriginPeer
	OriginAgent
	OriginGoal
	OriginSystem
)

type ContinuationReason uint8

const (
	ContinuationUnknown ContinuationReason = iota
	ContinuationToolCalls
	ContinuationTruncatedArgs
	ContinuationSteerPending
	ContinuationNaturalEnd
	ContinuationTruncatedContinue
	ContinuationTruncatedEnd
	ContinuationToolTerminate
	ContinuationRefused
	ContinuationStopIntent
	ContinuationBudget
	ContinuationStepBudget
)

type BudgetKind uint8

const (
	BudgetSteps BudgetKind = iota + 1
	BudgetModelTokens
	BudgetStorage
	BudgetCost
	BudgetToolCalls
	BudgetRetries
	BudgetTime
	BudgetConcurrency
)

type ActorKind uint8

const (
	ActorUnknown ActorKind = iota
	ActorUser
	ActorParent
	ActorModel
	ActorWorker
	ActorTimer
	ActorRecovery
	ActorSystem
)

type Outcome uint8

const (
	OutcomeNone Outcome = iota
	OutcomeCompleted
	OutcomeCancelled
	OutcomeBudgetExceeded
	OutcomeFailed
	OutcomeAbandoned
	OutcomeAbandonedUnknown
)

type ClosurePreviewMode uint8

const (
	ClosurePreviewUnspecified ClosurePreviewMode = iota
	ClosurePreviewMetadataOnly
	ClosurePreviewExact
)

type UnavailableReason uint8

const (
	UnavailableNone UnavailableReason = iota
	UnavailableVaultLocked
	UnavailablePayloadMissing
	UnavailablePayloadCorrupt
)

type ContextRequirement uint8

const (
	ContextRequirementNone ContextRequirement = iota
	ContextRequirementRevalidationRequired
	ContextRequirementFreshRequired
)

type RecoveryDisposition uint8

const (
	RecoveryDispositionUnspecified RecoveryDisposition = iota
	RecoveryDispositionReconciled
	RecoveryDispositionAcceptedUnknown
	RecoveryDispositionClosedUnavailable
)

type UsageStatus uint8

const (
	UsageUnknown UsageStatus = iota
	UsageKnown
	UsageReserved
)

// ModelFailureStage records where a terminal model failure was observed.
type ModelFailureStage uint8

const (
	ModelFailureStageUnknown ModelFailureStage = iota
	ModelFailurePreDispatch
	ModelFailureBeforeStart
	ModelFailureStream
	ModelFailureProtocol
)

// ModelFailureClass is provider-neutral; provider codes remain protected refs.
type ModelFailureClass uint8

const (
	ModelFailureClassUnknown ModelFailureClass = iota
	ModelFailureRateLimit
	ModelFailureOverloaded
	ModelFailureServer
	ModelFailureTimeout
	ModelFailureTransport
	ModelFailureEmptyResponse
	ModelFailureContextOverflow
	ModelFailureAuth
	ModelFailurePermission
	ModelFailureQuota
	ModelFailureInvalidRequest
	ModelFailureModelNotFound
	ModelFailureRefusal
	ModelFailureContentFilter
)

// RetryDisposition is a policy outcome supplied by the host adapter/policy.
type RetryDisposition uint8

const (
	RetryDispositionUnknown RetryDisposition = iota
	RetryDispositionRetry
	RetryDispositionFatal
	RetryDispositionResult
	RetryDispositionInterrupted
)

type Exposure uint8

const (
	ExposureNone Exposure = iota
	ExposurePossible
	ExposureUnknown
	ExposureKnown
)

type ResultDisposition uint8

const (
	ResultNone ResultDisposition = iota
	ResultSucceeded
	ResultFailed
	ResultDenied
	ResultRefused
	ResultCancelled
	ResultAbandonedUnknown
)

type InvocationState uint8

const (
	InvocationPlanned InvocationState = iota + 1
	InvocationActive
	InvocationCompleted
	InvocationFailed
	InvocationCancelled
	InvocationAbandoned
)

type AttemptState uint8

const (
	AttemptPlanned AttemptState = iota + 1
	AttemptRequesting
	AttemptStreaming
	AttemptCompleted
	AttemptFailed
	AttemptInterrupted
	AttemptCancelled
	AttemptAbandoned
)

type ToolState uint8

const (
	ToolPlanned ToolState = iota + 1
	ToolChecking
	ToolAwaitingApproval
	ToolReady
	ToolDispatching
	ToolSucceeded
	ToolFailed
	ToolDenied
	ToolRefused
	ToolCancelled
	ToolUnknown
	ToolAbandoned
)

type EffectClass uint8

const (
	EffectUnspecified EffectClass = iota
	EffectPureRead
	EffectIdempotent
	EffectSideEffecting
)

type BlockKind uint8

const (
	BlockApproval BlockKind = iota + 1
	BlockUserInput
	BlockDependency
	BlockRetryTimer
	BlockWorkerCapacity
	BlockVaultLocked
	BlockHostPaused
	BlockStepBudget
	BlockRecoveryAdmission
)

type InterruptionKind uint8

const (
	InterruptEffectUnknown InterruptionKind = iota + 1
	InterruptPayloadUnavailable
	InterruptUsageUnknown
)

type TimerPurpose uint8

const (
	TimerModelRetry TimerPurpose = iota + 1
	TimerToolRetry
	TimerRunDeadline
	TimerDependencyExpiry
)

type NextAction uint8

const (
	NextActionUnknown NextAction = iota
	NextContinue
	NextEnd
	NextWait
)

type EventKind uint8

const (
	EventRunAccepted EventKind = iota + 1
	EventRunAdmitted
	EventRunOwnershipAcquired
	EventRunFinished
	EventRunStopRequested
	EventRunBlockAdded
	EventRunBlockCleared
	EventRunInterrupted
	EventRunRecoveryCleared
	EventRunClosureConfirmed
	EventUnknownAccepted
	EventBudgetExtended
	EventStepStarted
	EventStepCompleted
	EventStepAborted
	EventModelRequestPlanned
	EventModelDispatchStarted
	EventModelStreamStarted
	EventModelResponseRecorded
	EventModelRequestFailed
	EventModelRequestInterrupted
	EventModelRetryScheduled
	EventModelAttemptPlanned
	EventModelAttemptCancelled
	EventModelAttemptAbandoned
	EventModelInvocationFailed
	EventModelUsageReconciled
	EventToolCallPlanned
	EventToolCheckStarted
	EventToolCheckReset
	EventToolApprovalRequested
	EventToolApprovalResolved
	EventToolReadyRecorded
	EventToolDispatchStarted
	EventToolResultRecorded
	EventToolOutcomeUnknown
	EventToolReconciled
	EventToolAttemptCancelled
	EventToolAttemptAbandoned
	EventToolAttemptPlanned
	EventTimerScheduled
	EventTimerElapsed
	EventTimerCancelled
	EventTimeBudgetWindowOpened
	EventTimeBudgetWindowClosed
	EventBudgetObserved
)

type CommandKind uint8

const (
	CommandAcceptRun CommandKind = iota + 1
	CommandAdmitRun
	CommandStartStep
	CommandPlanModel
	CommandDispatchModel
	CommandObserveModelStream
	CommandObserveModelResponse
	CommandPrepareTool
	CommandReadyTool
	CommandDispatchTool
	CommandObserveToolResult
	CommandFinishStep
	CommandFinishRun
	CommandRequestStop
	CommandExtendBudget
	CommandAbortStep
	CommandObserveModelFailure
	CommandCancelModel
	CommandAbandonModel
	CommandRequestToolApproval
	CommandResolveToolApproval
	CommandResetToolCheck
	CommandCancelTool
	CommandObserveUnknownTool
	CommandAbandonTool
	CommandReconcileTool
	CommandPlanToolRetry
	CommandScheduleTimer
	CommandObserveTimerElapsed
	CommandAcquireOwnership
	CommandConfirmRunClosure
	CommandAcceptUnknownOutcome
	CommandReconcileModelUsage
	CommandOpenTimeWindow
	CommandCloseTimeWindow
	CommandObserveBudget
	CommandClearRunBlock
	CommandClearRecovery
	CommandScheduleModelRetry
	CommandPlanModelRetry
	CommandFailModelInvocation
	CommandExpireTimeWindow
	CommandAcceptUnknownModelOutcome
)

type Command struct {
	ID                ID
	Kind              CommandKind
	ExpectedRevision  uint64
	WriterToken       uint64
	WriterIncarnation ID
	RunID             ID
	SessionID         ID
	AcceptRun         AcceptRunInput
	Admission         AdmissionInput
	Step              StepInput
	Model             ModelInput
	ModelResponse     ModelResponseInput
	Tool              ToolInput
	ToolResult        ToolResultInput
	FinishStep        FinishStepInput
	FinishRun         FinishRunInput
	Stop              StopInput
	BudgetExtension   BudgetExtensionInput
	Transition        TransitionInput
	Closure           ClosureInput
}

// TransitionInput carries allowlisted facts for non-happy-path commands. It is
// interpreted only by the CommandKind selected above; no arbitrary event list
// or effect callback is accepted.
type TransitionInput struct {
	TargetID                ID
	RelatedID               ID
	AttemptID               ID
	PreviousAttemptID       ID
	TimerID                 ID
	WindowID                ID
	BlockerID               ID
	Ref                     ID
	CauseRef                ID
	RequestRef              ID
	ReceiptRef              ID
	ResultRef               ID
	Fence                   uint64
	Generation              uint64
	Revision                uint64
	ExecutorToken           uint64
	ExecutorIncarnation     ID
	Disposition             ResultDisposition
	Exposure                Exposure
	Usage                   UsageStatus
	Next                    NextAction
	Continuation            ContinuationReason
	Outcome                 Outcome
	PreviewMode             ClosurePreviewMode
	PreviewVersion          uint32
	UnavailableReason       UnavailableReason
	PreviousContextRevision uint64
	ContextRevision         uint64
	ContextRequirement      ContextRequirement
	CauseEventRef           ID
	RecoveryDisposition     RecoveryDisposition
	Interrupt               InterruptionKind
	Purpose                 TimerPurpose
	BudgetKind              BudgetKind
	BudgetBy                uint64
	Actor                   ActorKind
	EffectClass             EffectClass
	Covered                 bool
	WindowOpen              bool
	Terminal                bool
	Truncated               bool
	BudgetExhausted         bool
	FailureStage            ModelFailureStage
	FailureClass            ModelFailureClass
	RetryDisposition        RetryDisposition
	RetryOrdinal            uint32
	RetryLimit              uint32
	PolicyVersion           uint32
	DeadlineUnixMilli       int64
	ObservedUnixMilli       int64
	Recoveries              []RecoveryFact
	UnknownOutcomes         []UnknownOutcomeFact
}

type RecoveryFact struct {
	TargetID            ID
	AttemptID           ID
	Tool                bool
	CauseRef            ID
	Interrupt           InterruptionKind
	Exposure            Exposure
	PartialRef          ID
	Usage               UsageStatus
	ResultRef           ID
	BlockerID           ID
	PreviousToken       uint64
	PreviousIncarnation ID
	Fence               uint64
}

type UnknownOutcomeFact struct {
	ToolID    ID
	AttemptID ID
	CauseRef  ID
	ResultRef ID
}

type Batch struct {
	CommandID         ID
	ExpectedRevision  uint64
	WriterToken       uint64
	WriterIncarnation ID
	ProtocolVersion   uint32
	Intent            Command
	Events            []Event
}

type AcceptRunInput struct {
	InputRef                ID
	Origin                  RunOrigin
	GoalID                  ID
	GoalRound               uint32
	InitialStepLimit        uint64
	RetryBudgetLimit        uint32
	ModelRetryPolicyVersion uint32
}

type AdmissionInput struct {
	Token       uint64
	Incarnation ID
}

type StepInput struct{ StepID ID }

type ModelInput struct {
	InvocationID        ID
	AttemptID           ID
	RequestRef          ID
	Fence               uint64
	ExecutorToken       uint64
	ExecutorIncarnation ID
}

type ModelResponseInput struct {
	InvocationID        ID
	AttemptID           ID
	ResponseRef         ID
	Usage               UsageStatus
	UsageRef            ID
	Candidates          []ToolCandidate
	Fence               uint64
	ExecutorToken       uint64
	ExecutorIncarnation ID
}

type ToolInput struct {
	ToolID              ID
	Generation          uint64
	Ref                 ID
	ArgsRef             ID
	ApprovalRef         ID
	AttemptID           ID
	ReceiptRef          ID
	Fence               uint64
	ExecutorToken       uint64
	ExecutorIncarnation ID
}

type ToolResultInput struct {
	ToolID              ID
	AttemptID           ID
	ResultRef           ID
	Disposition         ResultDisposition
	Exposure            Exposure
	Fence               uint64
	ExecutorToken       uint64
	ExecutorIncarnation ID
}

type FinishStepInput struct {
	Next         NextAction
	Continuation ContinuationReason
}
type FinishRunInput struct {
	Outcome   Outcome
	Truncated bool
}
type StopInput struct {
	Reason     StopReason
	Mode       StopMode
	Ref        ID
	BlockerID  ID
	Quiescing  bool
	Actor      ActorKind
	BudgetKind BudgetKind
}
type BudgetExtensionInput struct {
	Kind  BudgetKind
	By    uint64
	Actor ActorKind
}

// Event contains only allowlisted control facts, typed IDs and protected-payload
// references. It intentionally has no prompt, argument, transcript, error-string,
// callback, or executable function fields.
type Event struct {
	ID              ID
	Kind            EventKind
	ProtocolVersion uint32
	RunID           ID
	SessionID       ID

	InputRef             ID
	GoalID               ID
	Ref                  ID
	ResultRef            ID
	CauseRef             ID
	RequestRef           ID
	ReceiptRef           ID
	OwnerEvidenceRef     ID
	TimerID              ID
	WindowID             ID
	BlockerID            ID
	Purpose              TimerPurpose
	InvocationID         ID
	PreviousInvocationID ID
	AttemptID            ID
	PreviousAttemptID    ID
	StepID               ID
	ToolID               ID

	Token                   uint64
	PreviousToken           uint64
	PreviousIncarnation     ID
	Incarnation             ID
	Fence                   uint64
	Generation              uint64
	Revision                uint64
	SourceIndex             uint32
	Next                    NextAction
	Usage                   UsageStatus
	Exposure                Exposure
	Disposition             ResultDisposition
	StopReason              StopReason
	StopMode                StopMode
	Outcome                 Outcome
	PreviewMode             ClosurePreviewMode
	PreviewVersion          uint32
	UnavailableReason       UnavailableReason
	PreviousContextRevision uint64
	ContextRevision         uint64
	ContextRequirement      ContextRequirement
	CauseEventRef           ID
	RecoveryDisposition     RecoveryDisposition
	BlockKind               BlockKind
	Interrupt               InterruptionKind
	Primary                 bool
	Terminal                bool
	Covered                 bool
	WindowOpen              bool
	Truncated               bool
	IsModel                 bool
	Accepted                bool
	Origin                  RunOrigin
	GoalRound               uint32
	Continuation            ContinuationReason
	BudgetKind              BudgetKind
	BudgetBy                uint64
	Actor                   ActorKind
	InitialStepLimit        uint64
	RetryBudgetLimit        uint32
	ModelRetryPolicyVersion uint32
	FailureStage            ModelFailureStage
	FailureClass            ModelFailureClass
	RetryDisposition        RetryDisposition
	RetryOrdinal            uint32
	RetryLimit              uint32
	PolicyVersion           uint32
	DeadlineUnixMilli       int64
	ObservedUnixMilli       int64

	Candidates     []ToolCandidate
	ClosureTargets []ClosureTargetFact
}

type ToolCandidate struct {
	ID          ID
	AttemptID   ID
	ArgsRef     ID
	Descriptor  ID
	SourceIndex uint32
	EffectClass EffectClass
}

type UsageFact struct {
	Status UsageStatus
	Ref    ID
}

type Attempt struct {
	ID                    ID
	InvocationID          ID
	StepID                ID
	State                 AttemptState
	Fence                 uint64
	Token                 uint64
	Incarnation           ID
	PartialRef            ID
	Usage                 UsageFact
	Exposure              Exposure
	FailureRef            ID
	TerminalRef           ID
	ResultRef             ID
	FailureStage          ModelFailureStage
	FailureClass          ModelFailureClass
	RetryDisposition      RetryDisposition
	RetryOrdinal          uint32
	PolicyVersion         uint32
	RetryTimerID          ID
	RetryGeneration       uint64
	RetryCancelled        bool
	AbandonRetryPolicyRef ID
}

type ModelInvocation struct {
	ID                 ID
	StepID             ID
	State              InvocationState
	AttemptIDs         []ID
	SelectedAttempt    ID
	ResponseRef        ID
	Usage              UsageFact
	CandidateIDs       []ID
	FailureRef         ID
	DispositionRef     ID
	Candidates         []ToolCandidate
	Disposition        ResultDisposition
	RetryCount         uint32
	RetryLimit         uint32
	RetryPolicyVersion uint32
}

type ToolAttempt struct {
	ID          ID
	ToolID      ID
	StepID      ID
	State       AttemptState
	Fence       uint64
	Token       uint64
	Incarnation ID
	ArgsRef     ID
	ResultRef   ID
	Usage       UsageFact
	Exposure    Exposure
	TerminalRef ID
}

type ToolInvocation struct {
	ID                        ID
	StepID                    ID
	State                     ToolState
	Descriptor                ID
	EffectClass               EffectClass
	SourceIndex               uint32
	RawArgsRef                ID
	PlanArgsRef               ID
	PlanArgsGeneration        uint64
	ArgsRef                   ID
	CheckGeneration           uint64
	RequestRef                ID
	ApprovalRef               ID
	ResolutionRef             ID
	AttemptIDs                []ID
	SelectedAttempt           ID
	ResultRef                 ID
	Disposition               ResultDisposition
	Exposure                  Exposure
	ReconciliationRef         ID
	ReconciliationDisposition ResultDisposition
}

type Step struct {
	ID              ID
	Index           uint32
	Open            bool
	Aborted         bool
	Next            NextAction
	Continuation    ContinuationReason
	ModelID         ID
	ToolIDs         []ID
	ResponseRef     ID
	SelectedAttempt ID
	CompletedTools  []ID
	Transcript      []TranscriptItem
}

type TranscriptItem struct {
	SourceIndex  uint32
	InvocationID ID
	ResponseRef  ID
	ResultRef    ID
	Disposition  ResultDisposition
	IsModel      bool
}

type Blocker struct {
	ID         ID
	Kind       BlockKind
	Ref        ID
	TargetID   ID
	Revision   uint64
	Generation uint64
}

type Interruption struct {
	ID          ID
	Kind        InterruptionKind
	TargetID    ID
	Resolved    bool
	Accepted    bool
	Disposition RecoveryDisposition
}

type Timer struct {
	ID                ID
	Generation        uint64
	TargetID          ID
	BlockerID         ID
	Purpose           TimerPurpose
	Pending           bool
	Elapsed           bool
	DeadlineUnixMilli int64
	Cancelled         bool
}

type TimeWindow struct {
	ID           ID
	TimerID      ID
	Generation   uint64
	Incarnation  ID
	Open         bool
	Covered      bool
	Reserved     UsageStatus
	ObservedRefs []ID
}

type StopIntent struct {
	Reason     StopReason
	Mode       StopMode
	Ref        ID
	Actor      ActorKind
	BudgetKind BudgetKind
}

type State struct {
	RunID                    ID
	SessionID                ID
	InputRef                 ID
	Accepted                 bool
	Admitted                 bool
	Finished                 bool
	Outcome                  Outcome
	AdmissionToken           uint64
	Incarnation              ID
	OwnershipReceipt         ID
	Origin                   RunOrigin
	GoalID                   ID
	GoalRound                uint32
	Revision                 uint64
	Truncated                bool
	InitialStepLimit         uint64
	RetryBudgetLimit         uint32
	RetriesUsed              uint32
	ModelRetryPolicyVersion  uint32
	StepBudgetExtended       uint64
	StepsStarted             uint64
	BudgetStopActor          ActorKind
	BudgetStopKind           BudgetKind
	ClosureConfirmed         bool
	ClosureRef               ID
	ClosureReceipt           ID
	ClosureToolID            ID
	ClosureAttemptID         ID
	ClosureCauseRef          ID
	ClosureAcceptedUnknown   bool
	ClosurePreviewMode       ClosurePreviewMode
	ClosurePreviewVersion    uint32
	ClosureUnavailableReason UnavailableReason
	ClosureTargets           []ClosureTargetFact
	ContextRevision          uint64
	ContextRequirement       ContextRequirement
	PrimaryStop              StopIntent
	SecondaryStops           []StopIntent
	Suspended                bool
	Quiescing                bool

	Steps          []Step
	CurrentStep    int
	Models         map[ID]ModelInvocation
	ModelAttempts  map[ID]Attempt
	Tools          map[ID]ToolInvocation
	ToolAttempts   map[ID]ToolAttempt
	Blockers       map[ID]Blocker
	Interruptions  map[ID]Interruption
	Timers         map[ID]Timer
	Windows        map[ID]TimeWindow
	Transcript     []TranscriptItem
	Exposures      map[ID]Exposure
	EventIDs       map[ID]Event
	CommandBatches map[ID]Batch
	Lifecycle      Lifecycle
	Phase          Phase
	WaitReasons    []BlockKind
}

func EmptyState() State {
	return State{
		CurrentStep: -1,
		Models:      map[ID]ModelInvocation{}, ModelAttempts: map[ID]Attempt{},
		Tools: map[ID]ToolInvocation{}, ToolAttempts: map[ID]ToolAttempt{},
		Blockers: map[ID]Blocker{}, Interruptions: map[ID]Interruption{},
		Timers: map[ID]Timer{}, Windows: map[ID]TimeWindow{},
		Exposures: map[ID]Exposure{}, EventIDs: map[ID]Event{},
		CommandBatches: map[ID]Batch{},
	}
}
