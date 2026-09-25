package agenthub

import (
	"encoding/json"
	"strings"

	"devkit/internal/codexapp"
)

// ACP permission option ids. Each doubles as the option's kind, which is what
// the permission card looks for to draw its buttons.
const (
	codexOptionAllow        = "allow_once"
	codexOptionAllowSession = "allow_always"
	codexOptionDeny         = "reject_once"
)

// codexApproval is a Codex approval request put as an ACP
// session/request_permission, and the way back from the person's answer to
// the decision Codex expects.
//
// Malformed marks a command or file-change approval DevKit could
// not read — unparsable JSON, or no item id. Params is nil in that case: there
// is nothing to build a card from. The runtime's request dispatch must send
// Response(nil) at once — {"decision":"cancel"}, fail closed — and show
// nobody anything. Both approval methods commit Codex to waiting for exactly
// one decision; answering with codexUnsupportedRequest's -32601 instead, as
// approvalFor's ok=false does for a method DevKit does not approve at all,
// would leave the turn hanging the same way never answering would. The
// runtime's request dispatch:
//
//	a, ok := t.approvalFor(sessionID, method, params)
//	switch {
//	case !ok:
//		// not one of DevKit's approval methods
//		return nil, codexUnsupportedRequest(method)
//	case a.Malformed:
//		return a.Response(nil), nil // fail closed, no card
//	default:
//		// show a.Params as session/request_permission, then send
//		// a.Response(outcome) once it resolves
//	}
type codexApproval struct {
	// Params is the ACP request_permission params for PermissionWaiter. Nil
	// when Malformed is set — see above.
	Params json.RawMessage
	// decide maps an ACP option id to a Codex decision.
	decide map[string]string
	// onCancel is the decision for a cancelled, timed-out or unreadable
	// answer: fail closed.
	onCancel string
	// Malformed is documented on the type above.
	Malformed bool
	// elicitation answers in MCP's elicitation shape rather than with a
	// decision: an MCP tool-call approval (mcpToolApprovalFor).
	elicitation bool
}

// codexMalformedApproval is approvalFor's answer to a command or file-change
// approval it could not parse. See codexApproval's Malformed doc.
func codexMalformedApproval() codexApproval {
	return codexApproval{onCancel: codexapp.DecisionCancel, Malformed: true}
}

type codexPermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// approvalFor builds the permission request for an approval server request,
// or ok=false when method is not an approval DevKit puts to a person at all.
// A command or file-change approval DevKit could not parse still returns
// ok=true, with Malformed set — see codexApproval's doc for the
// three-way contract this leaves the caller.
//
// The options come from what Codex will accept. Measured under untrusted, a
// command approval offers accept, an execpolicy amendment and cancel — no
// decline, no acceptForSession — so a fixed button set would send decisions
// Codex rejects. acceptWithExecpolicyAmendment is never offered: it writes a
// permanent rule, and an approval must not.
//
// Safe to call concurrently with Translate on another goroutine —
// the runtime's Handler.Request does, per codexapp.Conn's contract.
func (t *codexTranslator) approvalFor(sessionID, method string, params json.RawMessage) (codexApproval, bool) {
	switch method {
	case codexapp.MethodCommandApproval:
		var p codexapp.CommandApprovalParams
		if json.Unmarshal(params, &p) != nil || p.ItemID == "" {
			return codexMalformedApproval(), true
		}
		call, known := t.openItem(p.ItemID)
		if !known {
			command := ""
			if p.Command != nil {
				command = *p.Command
			}
			call = codexOpenItem{Title: codexCommandTitle(command, p.CommandActions), Kind: "execute"}
		}
		input := map[string]any{}
		if p.Command != nil {
			input["command"] = *p.Command
		}
		if p.Cwd != nil {
			input["cwd"] = *p.Cwd
		}
		if p.Reason != nil {
			input["reason"] = *p.Reason
		}
		call.RawInput, _ = json.Marshal(input)
		return codexApprovalRequest(sessionID, p.ItemID, call, codexAvailable(p.AvailableDecisions)), true
	case codexapp.MethodFileChangeApproval:
		var p codexapp.FileChangeApprovalParams
		if json.Unmarshal(params, &p) != nil || p.ItemID == "" {
			return codexMalformedApproval(), true
		}
		call, known := t.openItem(p.ItemID)
		if !known {
			call = codexOpenItem{Title: "Apply file changes", Kind: "edit"}
		}
		if p.Reason != nil {
			call.RawInput, _ = json.Marshal(map[string]string{"reason": *p.Reason})
		}
		// FileChangeApprovalDecision is all four, always.
		all := map[string]bool{
			codexapp.DecisionAccept: true, codexapp.DecisionAcceptForSession: true,
			codexapp.DecisionDecline: true, codexapp.DecisionCancel: true,
		}
		return codexApprovalRequest(sessionID, p.ItemID, call, all), true
	}
	return codexApproval{}, false
}

// codexAvailable reads availableDecisions: plain strings are decisions, and
// objects (the execpolicy/network amendments) are skipped. Absent means every
// plain decision.
func codexAvailable(raw []json.RawMessage) map[string]bool {
	if raw == nil {
		return map[string]bool{
			codexapp.DecisionAccept: true, codexapp.DecisionAcceptForSession: true,
			codexapp.DecisionDecline: true, codexapp.DecisionCancel: true,
		}
	}
	out := map[string]bool{}
	for _, d := range raw {
		var s string
		if json.Unmarshal(d, &s) == nil {
			out[s] = true
		}
	}
	return out
}

func codexApprovalRequest(sessionID, itemID string, call codexOpenItem, available map[string]bool) codexApproval {
	a := codexApproval{decide: map[string]string{}, onCancel: codexapp.DecisionCancel}
	if !available[codexapp.DecisionCancel] {
		a.onCancel = codexapp.DecisionDecline
	}
	var options []codexPermissionOption
	if available[codexapp.DecisionAccept] {
		options = append(options, codexPermissionOption{codexOptionAllow, "Allow", codexOptionAllow})
		a.decide[codexOptionAllow] = codexapp.DecisionAccept
	}
	if available[codexapp.DecisionAcceptForSession] {
		options = append(options, codexPermissionOption{codexOptionAllowSession, "Allow for this session", codexOptionAllowSession})
		a.decide[codexOptionAllowSession] = codexapp.DecisionAcceptForSession
	}
	// Deny is decline when Codex takes it (the turn goes on without the
	// action), else cancel (the turn stops) — there is always a way to say no.
	deny := codexapp.DecisionDecline
	if !available[deny] {
		deny = codexapp.DecisionCancel
	}
	options = append(options, codexPermissionOption{codexOptionDeny, "Deny", codexOptionDeny})
	a.decide[codexOptionDeny] = deny

	a.Params = marshalUpdate(map[string]any{
		"sessionId": sessionID,
		"toolCall": codexToolCallWire{
			ToolCallID: itemID,
			Title:      call.Title,
			Kind:       call.Kind,
			Status:     "pending",
			Content:    call.Content,
			Locations:  call.Locations,
			RawInput:   call.RawInput,
		},
		"options": options,
	})
	return a
}

// Response turns the ACP outcome — what the permission gate returned, even
// alongside an error — into Codex's {"decision": …}.
func (a codexApproval) Response(outcome json.RawMessage) json.RawMessage {
	var wrap struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	decision := a.onCancel
	if json.Unmarshal(outcome, &wrap) == nil && strings.EqualFold(wrap.Outcome.Outcome, "selected") {
		if d, ok := a.decide[wrap.Outcome.OptionID]; ok {
			decision = d
		}
	}
	if a.elicitation {
		return codexElicitationAnswer(decision)
	}
	return marshalUpdate(codexapp.ApprovalResponse{Decision: decision})
}

// codexElicitationAnswer is a decision said the way an MCP tool-call approval
// takes it. Accept with no persist is "this once" — Codex reads an accept it
// cannot parse further as a plain Allow. "session" remembers the tool for the
// rest of the session, in Codex's memory only.
func codexElicitationAnswer(decision string) json.RawMessage {
	answer := codexapp.McpElicitationResponse{Action: codexapp.ElicitCancel}
	switch decision {
	case codexapp.DecisionAccept:
		answer.Action = codexapp.ElicitAccept
	case codexapp.DecisionAcceptForSession:
		answer.Action = codexapp.ElicitAccept
		answer.Meta = marshalUpdate(map[string]string{codexapp.McpPersistKey: codexapp.McpPersistSession})
	case codexapp.DecisionDecline:
		answer.Action = codexapp.ElicitDecline
	}
	return marshalUpdate(answer)
}

// mcpToolApprovalFor puts Codex's question "may this MCP tool run?" to the
// person, or ok=false for an elicitation that is not that question — an MCP
// server asking something of its own, which DevKit refuses (spec §7.2).
//
// Under Ask and Auto, Codex asks before every call to a tool that does not
// declare itself read-only and non-destructive, which is most of DevKit's own
// (codex-rs core/src/mcp_tool_call.rs, 0.147.0). It asks through an
// elicitation, not an approval request, so without this every DevKit tool
// call in those modes would be refused. Full auto never asks: Codex approves
// MCP calls itself under never + externalSandbox, and DevKit's own gate for
// prod environments still asks inside the tool.
//
// "Allow and don't ask me again" is never offered. Codex would write it into
// config.toml as a permanent rule, and an approval must not do that — the
// jail cannot write that file anyway.
//
// The request names the server but not the call. The card is the open call's
// when exactly one call on that server is open (Codex starts the item before
// it asks); otherwise it stands on its own, as "mcp-approval-<ref>" — ref is
// the runtime's own count, unique within the session.
func (t *codexTranslator) mcpToolApprovalFor(sessionID, ref string, params json.RawMessage) (codexApproval, bool) {
	var p codexapp.McpElicitationParams
	if json.Unmarshal(params, &p) != nil {
		return codexApproval{}, false
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(p.Meta, &meta) != nil {
		return codexApproval{}, false
	}
	var kind string
	if json.Unmarshal(meta[codexapp.McpApprovalKindKey], &kind) != nil || kind != codexapp.McpApprovalKindToolCall {
		return codexApproval{}, false
	}

	id, call, known := t.openMcpCall(p.ServerName)
	if !known {
		id = "mcp-approval-" + ref
		var title string
		_ = json.Unmarshal(meta[codexapp.McpToolTitleKey], &title)
		if title == "" {
			title = p.ServerName
		}
		call = codexOpenItem{Title: title, Kind: "other", RawInput: meta[codexapp.McpToolParamsKey]}
	}
	if strings.TrimSpace(p.Message) != "" {
		call.Content = []ToolCallContent{codexTextContent(p.Message)}
	}
	available := map[string]bool{
		codexapp.DecisionAccept:  true,
		codexapp.DecisionDecline: true,
		codexapp.DecisionCancel:  true,
	}
	if codexPersistOffers(meta[codexapp.McpPersistKey], codexapp.McpPersistSession) {
		available[codexapp.DecisionAcceptForSession] = true
	}
	a := codexApprovalRequest(sessionID, id, call, available)
	a.elicitation = true
	return a, true
}

// codexPersistOffers reads _meta.persist, a string or a list of them.
func codexPersistOffers(raw json.RawMessage, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, s := range many {
			if s == want {
				return true
			}
		}
	}
	return false
}

// codexPermissionsRequest answers item/permissions/requestApproval without
// asking anyone. Under externalSandbox Codex granting itself more access
// changes nothing — DevKit's jail decides — so an Allow button would be a
// lie. It grants nothing and leaves a line in the transcript saying where
// access is actually decided.
func codexPermissionsRequest(params json.RawMessage) (response json.RawMessage, notice []codexUpdate) {
	response = marshalUpdate(codexapp.PermissionsApprovalResponse{Scope: "turn"})
	var p codexapp.PermissionsApprovalParams
	if json.Unmarshal(params, &p) != nil {
		return response, nil
	}
	asked := strings.TrimSpace(string(p.Permissions))
	if asked == "" {
		asked = "more access"
	}
	text := "Codex asked for " + asked + ". Access is decided by DevKit's sandbox — Settings → Agents → Sandbox."
	return response, []codexUpdate{{Update: marshalUpdate(codexToolCallWire{
		SessionUpdate: string(UpdateToolCall),
		ToolCallID:    "permissions-" + p.ItemID,
		Title:         "Codex asked for more access",
		Kind:          "other",
		Status:        "failed",
		Content:       []ToolCallContent{codexTextContent(text)},
		RawInput:      p.Permissions,
	})}}
}

// codexUnsupportedRequest refuses a server request DevKit does not handle, at
// once, so the turn is not left waiting: user-input questions, an MCP
// server's own elicitation, token refresh, attestation, the v1 approval methods, and
// anything new. Each becomes supported by one case in the runtime's request
// dispatch (spec §5.4).
func codexUnsupportedRequest(method string) *codexapp.RPCError {
	return &codexapp.RPCError{Code: -32601, Message: "DevKit does not support " + method}
}
