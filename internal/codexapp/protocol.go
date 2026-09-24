package codexapp

import "encoding/json"

// The lifecycle half of the app-server protocol: what the adapter calls to
// start, drive and stop a session, and to sign in. Field names are the
// app-server's, from `codex app-server generate-ts` for the pinned version
// (0.147.0); protocol_test.go holds them against a real recorded session.
// Item and turn notifications — the translator's input — are added with the
// translator.

const (
	MethodInitialize            = "initialize"
	MethodInitialized           = "initialized"
	MethodThreadStart           = "thread/start"
	MethodThreadResume          = "thread/resume"
	MethodTurnStart             = "turn/start"
	MethodTurnInterrupt         = "turn/interrupt"
	MethodAccountRead           = "account/read"
	MethodAccountLoginStart     = "account/login/start"
	MethodAccountLoginCancel    = "account/login/cancel"
	MethodAccountLogout         = "account/logout"
	MethodAccountLoginCompleted = "account/login/completed"
	MethodAccountUpdated        = "account/updated"
	MethodModelList             = "model/list"
	MethodServerRequestResolved = "serverRequest/resolved"
)

// AskForApproval's string forms. The granular object form is not used.
const (
	ApprovalUntrusted = "untrusted"
	ApprovalOnRequest = "on-request"
	ApprovalNever     = "never"
)

// ReviewerUser routes every approval to the client. DevKit always sends it:
// the default is "user" today, and a project config can ask for
// "auto_review" — per-turn parameters win over that (Phase 0).
const ReviewerUser = "user"

// Turn statuses.
const (
	TurnInProgress  = "inProgress"
	TurnCompleted   = "completed"
	TurnInterrupted = "interrupted"
	TurnFailed      = "failed"
)

// Account and login types.
const (
	AccountChatGPT  = "chatgpt"
	AccountAPIKey   = "apiKey"
	LoginChatGPT    = "chatgpt"
	LoginDeviceCode = "chatgptDeviceCode"
	LoginAPIKey     = "apiKey"
)

type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type InitializeCapabilities struct {
	ExperimentalAPI    bool `json:"experimentalApi"`
	RequestAttestation bool `json:"requestAttestation"`
}

type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

// ThreadStartParams deliberately has no sandbox field: the app-server's is a
// SandboxMode (read-only / workspace-write / danger-full-access) with no
// external variant. externalSandbox is sent on every turn/start instead.
type ThreadStartParams struct {
	Cwd               string `json:"cwd,omitempty"`
	ApprovalPolicy    string `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string `json:"approvalsReviewer,omitempty"`
	Model             string `json:"model,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID          string `json:"threadId"`
	Cwd               string `json:"cwd,omitempty"`
	ApprovalPolicy    string `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string `json:"approvalsReviewer,omitempty"`
	Model             string `json:"model,omitempty"`
}

type Thread struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
	Cwd  string  `json:"cwd"`
}

// ThreadStartResponse is also thread/resume's response: same fields.
type ThreadStartResponse struct {
	Thread          Thread  `json:"thread"`
	Model           string  `json:"model"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

// UserInput is one element of turn/start's input, already encoded.
type UserInput = json.RawMessage

// TextInput is a text prompt. text_elements is required on the wire even when
// empty (measured).
func TextInput(text string) UserInput {
	b, _ := json.Marshal(struct {
		Type         string            `json:"type"`
		Text         string            `json:"text"`
		TextElements []json.RawMessage `json:"text_elements"`
	}{Type: "text", Text: text, TextElements: []json.RawMessage{}})
	return b
}

type SandboxPolicy struct {
	Type          string `json:"type"`
	NetworkAccess string `json:"networkAccess,omitempty"`
}

// ExternalSandbox tells Codex that something outside it — DevKit's jail —
// already confines the process, so it must not sandbox commands itself.
func ExternalSandbox(network bool) SandboxPolicy {
	access := "restricted"
	if network {
		access = "enabled"
	}
	return SandboxPolicy{Type: "externalSandbox", NetworkAccess: access}
}

type TurnStartParams struct {
	ThreadID          string         `json:"threadId"`
	Input             []UserInput    `json:"input"`
	ApprovalPolicy    string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string         `json:"approvalsReviewer,omitempty"`
	SandboxPolicy     *SandboxPolicy `json:"sandboxPolicy,omitempty"`
	Model             string         `json:"model,omitempty"`
	Effort            string         `json:"effort,omitempty"`
}

type TurnError struct {
	Message           string          `json:"message"`
	CodexErrorInfo    json.RawMessage `json:"codexErrorInfo"`
	AdditionalDetails *string         `json:"additionalDetails"`
}

type Turn struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *TurnError `json:"error"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type Account struct {
	Type     string  `json:"type"`
	Email    *string `json:"email"`
	PlanType *string `json:"planType"`
}

type GetAccountResponse struct {
	Account            *Account `json:"account"`
	RequiresOpenAIAuth bool     `json:"requiresOpenaiAuth"`
}

type LoginStartParams struct {
	Type   string `json:"type"`
	APIKey string `json:"apiKey,omitempty"`
}

type LoginStartResponse struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId,omitempty"`
	AuthURL         string `json:"authUrl,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
}

type CancelLoginParams struct {
	LoginID string `json:"loginId"`
}

type AccountLoginCompletedNotification struct {
	LoginID *string `json:"loginId"`
	Success bool    `json:"success"`
	Error   *string `json:"error"`
}

type ModelListParams struct {
	Cursor *string `json:"cursor,omitempty"`
	Limit  *int    `json:"limit,omitempty"`
}

type Model struct {
	ID                     string `json:"id"`
	Model                  string `json:"model"`
	DisplayName            string `json:"displayName"`
	Description            string `json:"description"`
	Hidden                 bool   `json:"hidden"`
	IsDefault              bool   `json:"isDefault"`
	DefaultReasoningEffort string `json:"defaultReasoningEffort"`
}

type ModelListResponse struct {
	Data       []Model `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type ServerRequestResolvedNotification struct {
	ThreadID  string          `json:"threadId"`
	RequestID json.RawMessage `json:"requestId"`
}
