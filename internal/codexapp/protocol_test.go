package codexapp

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type captureLine struct {
	Dir     string  `json:"dir"`
	Session string  `json:"session"`
	Msg     Message `json:"msg"`
}

func loadCapture(t *testing.T) []captureLine {
	t.Helper()
	f, err := os.Open("../agenthub/testdata/codex_session_capture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []captureLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		var l captureLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// sent returns the params of the first request with method in session, and
// the result of its response.
func sent(t *testing.T, lines []captureLine, session, method string) (params, result json.RawMessage) {
	t.Helper()
	for i, l := range lines {
		if l.Session != session || l.Dir != "out" || l.Msg.Method != method {
			continue
		}
		for _, r := range lines[i+1:] {
			if r.Session == session && r.Dir == "in" && r.Msg.IsResponse() && string(r.Msg.ID) == string(l.Msg.ID) {
				return l.Msg.Params, r.Msg.Result
			}
		}
		return l.Msg.Params, nil
	}
	t.Fatalf("no %s request in session %s", method, session)
	return nil, nil
}

func sameJSON(t *testing.T, what string, got any, want json.RawMessage) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	_ = json.Unmarshal(gotRaw, &g)
	_ = json.Unmarshal(want, &w)
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s:\n got  %s\n want %s", what, gotRaw, want)
	}
}

func TestInitializeMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, result := sent(t, lines, "main", MethodInitialize)
	sameJSON(t, "initialize params", InitializeParams{
		ClientInfo:   ClientInfo{Name: "devkit-probe", Title: "DevKit probe", Version: "0"},
		Capabilities: &InitializeCapabilities{},
	}, params)

	var resp InitializeResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.CodexHome != "/probe/home" || resp.PlatformOS != "linux" || resp.UserAgent == "" {
		t.Fatalf("initialize response = %+v", resp)
	}
}

func TestThreadStartMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, result := sent(t, lines, "main", MethodThreadStart)
	sameJSON(t, "thread/start params", ThreadStartParams{
		Cwd: "/probe/repo", ApprovalPolicy: ApprovalUntrusted, ApprovalsReviewer: ReviewerUser,
	}, params)

	var resp ThreadStartResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Thread.ID == "" || resp.Model == "" {
		t.Fatalf("thread/start response = %+v", resp)
	}
}

func TestTurnStartMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, result := sent(t, lines, "main", MethodTurnStart)
	var captured struct {
		ThreadID string `json:"threadId"`
		Input    []struct {
			Text string `json:"text"`
		} `json:"input"`
	}
	if err := json.Unmarshal(params, &captured); err != nil {
		t.Fatal(err)
	}
	sandbox := ExternalSandbox(true)
	sameJSON(t, "turn/start params", TurnStartParams{
		ThreadID:          captured.ThreadID,
		Input:             []UserInput{TextInput(captured.Input[0].Text)},
		ApprovalPolicy:    ApprovalUntrusted,
		ApprovalsReviewer: ReviewerUser,
		SandboxPolicy:     &sandbox,
	}, params)

	var resp TurnStartResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Turn.ID == "" || resp.Turn.Status != TurnInProgress {
		t.Fatalf("turn/start response = %+v", resp)
	}
}

func TestTurnInterruptMatchesCapture(t *testing.T) {
	lines := loadCapture(t)
	params, _ := sent(t, lines, "interrupt", MethodTurnInterrupt)
	var captured TurnInterruptParams
	if err := json.Unmarshal(params, &captured); err != nil {
		t.Fatal(err)
	}
	if captured.ThreadID == "" || captured.TurnID == "" {
		t.Fatalf("captured interrupt = %+v", captured)
	}
	sameJSON(t, "turn/interrupt params", TurnInterruptParams{ThreadID: captured.ThreadID, TurnID: captured.TurnID}, params)
}

func TestAccountAndLoginShapes(t *testing.T) {
	// Response bodies measured in Phase 0 (research doc), not in the capture.
	var acct GetAccountResponse
	if err := json.Unmarshal([]byte(`{"account":null,"requiresOpenaiAuth":true}`), &acct); err != nil {
		t.Fatal(err)
	}
	if acct.Account != nil || !acct.RequiresOpenAIAuth {
		t.Fatalf("signed-out account = %+v", acct)
	}
	if err := json.Unmarshal([]byte(`{"account":{"type":"chatgpt","email":"user@example.com","planType":"plus"},"requiresOpenaiAuth":true}`), &acct); err != nil {
		t.Fatal(err)
	}
	if acct.Account == nil || acct.Account.Type != AccountChatGPT || acct.Account.PlanType == nil || *acct.Account.PlanType != "plus" {
		t.Fatalf("signed-in account = %+v", acct)
	}

	sameJSON(t, "browser login params", LoginStartParams{Type: LoginChatGPT}, json.RawMessage(`{"type":"chatgpt"}`))
	sameJSON(t, "api key login params", LoginStartParams{Type: LoginAPIKey, APIKey: "sk-test"}, json.RawMessage(`{"type":"apiKey","apiKey":"sk-test"}`))

	var login LoginStartResponse
	if err := json.Unmarshal([]byte(`{"type":"chatgptDeviceCode","loginId":"l1","verificationUrl":"https://auth.openai.com/codex/device","userCode":"ABCD-EFGH"}`), &login); err != nil {
		t.Fatal(err)
	}
	if login.LoginID != "l1" || login.VerificationURL == "" || login.UserCode != "ABCD-EFGH" {
		t.Fatalf("device login = %+v", login)
	}
	var done AccountLoginCompletedNotification
	if err := json.Unmarshal([]byte(`{"loginId":"l1","success":true,"error":null,"onboardingEntrypoint":null}`), &done); err != nil {
		t.Fatal(err)
	}
	if !done.Success || done.LoginID == nil || *done.LoginID != "l1" {
		t.Fatalf("login completed = %+v", done)
	}
}
