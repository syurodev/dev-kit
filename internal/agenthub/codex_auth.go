package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devkit/internal/codexapp"
	"devkit/internal/identity"
)

// SignInStatus is a vendor's sign-in as the in-app panel shows it.
type SignInStatus struct {
	SignedIn bool `json:"signedIn"`
	// Method is "chatgpt" or "apiKey".
	Method string `json:"method,omitempty"`
	Email  string `json:"email,omitempty"`
	Plan   string `json:"plan,omitempty"`
}

// SignInFlow is a sign-in in progress: a page to open (browser) or a code to
// type on one (device code). LoginID names it to CancelSignIn.
type SignInFlow struct {
	LoginID         string `json:"loginId"`
	Method          string `json:"method"`
	AuthURL         string `json:"authUrl,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
}

// SignInResult is how a flow ended.
type SignInResult struct {
	Agent   AgentType `json:"agent"`
	LoginID string    `json:"loginId"`
	Success bool      `json:"success"`
	Error   string    `json:"error,omitempty"`
}

// Sign-in methods a flow can use.
const (
	SignInBrowser    = "browser"
	SignInDeviceCode = "deviceCode"
)

// InAppSignIn is a vendor that signs in inside DevKit rather than in a
// terminal (AuthProvider): DevKit starts the vendor's own login and shows
// its page or code; the credential lands in the vendor's home, never in
// DevKit (spec §6). UI only — there is no MCP tool for any of it.
type InAppSignIn interface {
	SignInStatus(ctx context.Context) (SignInStatus, error)
	StartBrowserSignIn(ctx context.Context) (SignInFlow, error)
	StartDeviceSignIn(ctx context.Context) (SignInFlow, error)
	CancelSignIn(ctx context.Context, loginID string) error
	SignOut(ctx context.Context) error
}

var _ InAppSignIn = (*CodexAdapter)(nil)

// inAppSignIn finds t's in-app sign-in, for the person only. There is no MCP
// tool for any of it, and this is why that is not the only guard: a caller
// the gateway stamped as an agent or a terminal MCP client is refused here,
// so a door added later by mistake still opens onto nothing. An agent that
// could start a sign-in could put its own account in the home every later
// session uses; one that could sign out could lock the person out mid-work.
func (s *Service) inAppSignIn(ctx context.Context, t AgentType) (InAppSignIn, error) {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return nil, fmt.Errorf("%w: signing in is the person's, from the app", ErrPermissionDenied)
	}
	adapter, ok := s.reg.Get(t)
	if !ok {
		return nil, ErrVendorUnknown
	}
	signIn, ok := adapter.(InAppSignIn)
	if !ok {
		return nil, fmt.Errorf("%w: agent %q does not sign in inside DevKit", ErrUnsupported, t)
	}
	return signIn, nil
}

// SignInStatus reads an agent type's sign-in.
func (s *Service) SignInStatus(ctx context.Context, t AgentType) (SignInStatus, error) {
	signIn, err := s.inAppSignIn(ctx, t)
	if err != nil {
		return SignInStatus{}, err
	}
	return signIn.SignInStatus(ctx)
}

// StartSignIn begins a sign-in by browser or device code.
func (s *Service) StartSignIn(ctx context.Context, t AgentType, method string) (SignInFlow, error) {
	signIn, err := s.inAppSignIn(ctx, t)
	if err != nil {
		return SignInFlow{}, err
	}
	switch method {
	case SignInBrowser:
		return signIn.StartBrowserSignIn(ctx)
	case SignInDeviceCode:
		return signIn.StartDeviceSignIn(ctx)
	}
	return SignInFlow{}, ErrAuthMethodUnknown
}

// CancelSignIn abandons a sign-in in progress.
func (s *Service) CancelSignIn(ctx context.Context, t AgentType, loginID string) error {
	signIn, err := s.inAppSignIn(ctx, t)
	if err != nil {
		return err
	}
	return signIn.CancelSignIn(ctx, loginID)
}

// SignOut removes an agent type's stored sign-in.
func (s *Service) SignOut(ctx context.Context, t AgentType) error {
	signIn, err := s.inAppSignIn(ctx, t)
	if err != nil {
		return err
	}
	return signIn.SignOut(ctx)
}

// codexLoginCallbackPort is where codex's login server takes the browser's
// redirect (redirect_uri=http://localhost:1455/auth/callback, measured on
// 0.147.0). Only a sign-in's jail may listen on it — without that, Seatbelt
// refused the bind and browser sign-in failed on macOS (measured live).
const codexLoginCallbackPort = 1455

// codexSignInTimeout is how long a sign-in waits for the person (spec §6.1).
// A var so a test need not wait it out.
var codexSignInTimeout = 15 * time.Minute

// codexSignIn is the one sign-in in progress: a short-lived app-server under
// the session jail, with the ChatGPT store, alive until the login completes,
// is cancelled or times out.
type codexSignIn struct {
	loginID string
	proc    *codexProcess
	work    string
	timer   *time.Timer
	once    sync.Once
	// early is a completion that arrived before login/start answered.
	early *SignInResult
}

// StartBrowserSignIn is the default (spec §6.1): Codex serves the callback on
// localhost:1455 from inside the jail, which shares the host's loopback.
func (a *CodexAdapter) StartBrowserSignIn(ctx context.Context) (SignInFlow, error) {
	return a.startSignIn(ctx, codexapp.LoginChatGPT, SignInBrowser)
}

// StartDeviceSignIn is for a machine that cannot open a browser. It is off by
// default per ChatGPT account (measured): the person must enable it under
// ChatGPT → Settings → Security first, and Codex does not say so — the UI has
// to.
func (a *CodexAdapter) StartDeviceSignIn(ctx context.Context) (SignInFlow, error) {
	return a.startSignIn(ctx, codexapp.LoginDeviceCode, SignInDeviceCode)
}

func (a *CodexAdapter) startSignIn(ctx context.Context, loginType, method string) (SignInFlow, error) {
	// One start at a time, so two quick clicks cannot both find no flow and
	// leave the first running, holding the callback port, until it times out.
	a.signInStart.Lock()
	defer a.signInStart.Unlock()
	a.signInMu.Lock()
	previous := a.signIn
	a.signInMu.Unlock()
	if previous != nil {
		// One at a time: a second Sign in replaces the first.
		a.finishSignIn(previous, SignInResult{LoginID: previous.loginID, Error: "Replaced by a new sign-in."}, true)
	}

	l, err := a.resolveCommand()
	if err != nil {
		return SignInFlow{}, err
	}
	work, err := codexScratchDir("devkit-codex-signin-")
	if err != nil {
		return SignInFlow{}, err
	}
	s := &codexSignIn{work: work}
	p, err := a.start(work, l, OpenSpec{}, codexStoreFile, &codexSignInHandler{a: a, s: s}, codexLoginCallbackPort)
	if err != nil {
		_ = os.RemoveAll(work)
		return SignInFlow{}, err
	}
	s.proc = p
	fail := func(err error) (SignInFlow, error) {
		p.stop()
		_ = os.RemoveAll(work)
		return SignInFlow{}, err
	}
	callCtx, cancel := p.bounded(ctx, handshakeTimeout)
	defer cancel()
	if err := a.initialize(callCtx, p); err != nil {
		return fail(err)
	}
	var res codexapp.LoginStartResponse
	if err := p.conn.Call(callCtx, codexapp.MethodAccountLoginStart, codexapp.LoginStartParams{Type: loginType}, &res); err != nil {
		return fail(p.failure("account/login/start", err))
	}
	if res.LoginID == "" {
		return fail(fmt.Errorf("%w: account/login/start returned no login id", ErrProcessFailed))
	}

	a.signInMu.Lock()
	s.loginID = res.LoginID
	a.signIn = s
	s.timer = time.AfterFunc(codexSignInTimeout, func() {
		a.finishSignIn(s, SignInResult{LoginID: res.LoginID, Error: "Sign-in timed out."}, true)
	})
	early := s.early
	a.signInMu.Unlock()
	if early != nil {
		early.LoginID = res.LoginID
		go a.finishSignIn(s, *early, false)
	}
	// A process that dies mid-flow ends it now, not at the timeout.
	go func() {
		<-p.serveDone
		a.finishSignIn(s, SignInResult{LoginID: res.LoginID, Error: "Codex exited before the sign-in finished."}, false)
	}()
	log.Printf("agenthub: codex: %s sign-in started", method)
	return SignInFlow{
		LoginID:         res.LoginID,
		Method:          method,
		AuthURL:         res.AuthURL,
		VerificationURL: res.VerificationURL,
		UserCode:        res.UserCode,
	}, nil
}

// finishSignIn ends s once: withdraws the login when asked, stops the
// process and reports the result. Never on the read loop of s's own
// connection — stopping waits for that loop to end.
func (a *CodexAdapter) finishSignIn(s *codexSignIn, result SignInResult, withdraw bool) {
	s.once.Do(func() {
		a.signInMu.Lock()
		if a.signIn == s {
			a.signIn = nil
		}
		if s.timer != nil {
			s.timer.Stop()
		}
		a.signInMu.Unlock()
		if withdraw && s.loginID != "" {
			ctx, cancel := s.proc.bounded(context.Background(), 5*time.Second)
			if err := s.proc.conn.Call(ctx, codexapp.MethodAccountLoginCancel, codexapp.CancelLoginParams{LoginID: s.loginID}, nil); err != nil {
				log.Printf("agenthub: codex: account/login/cancel: %v", err)
			}
			cancel()
		}
		s.proc.stop()
		_ = os.RemoveAll(s.work)
		// The code and the page are never logged; whether it worked is.
		log.Printf("agenthub: codex: sign-in ended (success %v)", result.Success)
		result.Agent = AgentTypeCodex
		if a.opts.OnSignInCompleted != nil {
			a.opts.OnSignInCompleted(result)
		}
	})
}

// CancelSignIn withdraws the sign-in in progress. A login id that is not the
// current one is already over: nothing to do.
func (a *CodexAdapter) CancelSignIn(_ context.Context, loginID string) error {
	a.signInMu.Lock()
	s := a.signIn
	a.signInMu.Unlock()
	if s == nil || s.loginID != loginID {
		return nil
	}
	a.finishSignIn(s, SignInResult{LoginID: loginID, Error: "Sign-in cancelled."}, true)
	return nil
}

// codexSignInHandler waits for the login to complete. account/read is not
// asked on this process: right after account/login/completed it still
// answers "no account" (Phase 0), so status is read by a fresh process.
type codexSignInHandler struct {
	a *CodexAdapter
	s *codexSignIn
}

func (h *codexSignInHandler) Notification(_ context.Context, method string, params json.RawMessage) {
	if method != codexapp.MethodAccountLoginCompleted {
		return
	}
	var n codexapp.AccountLoginCompletedNotification
	if json.Unmarshal(params, &n) != nil {
		return
	}
	result := SignInResult{Success: n.Success}
	if n.Error != nil {
		result.Error = *n.Error
	}
	h.a.signInMu.Lock()
	loginID := h.s.loginID
	if loginID == "" {
		// login/start has not answered yet; it picks this up when it does.
		h.s.early = &result
		h.a.signInMu.Unlock()
		return
	}
	h.a.signInMu.Unlock()
	if n.LoginID != nil && *n.LoginID != loginID {
		return
	}
	result.LoginID = loginID
	// Off the read loop: finishing stops this connection.
	go h.a.finishSignIn(h.s, result, false)
}

func (h *codexSignInHandler) Request(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *codexapp.RPCError) {
	return nil, codexUnsupportedRequest(method)
}

// SignInStatus reads the sign-in a session would use. An API key, when one is
// set, is what sessions sign in with, so it is the answer; otherwise a fresh
// process reads the ChatGPT sign-in in the home.
func (a *CodexAdapter) SignInStatus(ctx context.Context) (SignInStatus, error) {
	if key, err := a.apiKey(ctx); err == nil && key != "" {
		return SignInStatus{SignedIn: true, Method: codexapp.AccountAPIKey}, nil
	}
	var res codexapp.GetAccountResponse
	err := a.oneShot(ctx, func(ctx context.Context, p *codexProcess) error {
		if err := p.conn.Call(ctx, codexapp.MethodAccountRead, map[string]any{"refreshToken": false}, &res); err != nil {
			return p.failure("account/read", err)
		}
		return nil
	})
	if err != nil {
		return SignInStatus{}, err
	}
	if res.Account == nil {
		return SignInStatus{SignedIn: !res.RequiresOpenAIAuth}, nil
	}
	status := SignInStatus{SignedIn: true, Method: res.Account.Type}
	if res.Account.Email != nil {
		status.Email = *res.Account.Email
	}
	if res.Account.PlanType != nil {
		status.Plan = *res.Account.PlanType
	}
	return status, nil
}

// SignOut removes the ChatGPT sign-in from the home. An API key is not
// Codex's to forget: it is unlinked where it was chosen, in Settings.
func (a *CodexAdapter) SignOut(ctx context.Context) error {
	return a.oneShot(ctx, func(ctx context.Context, p *codexProcess) error {
		if err := p.conn.Call(ctx, codexapp.MethodAccountLogout, nil, nil); err != nil {
			return p.failure("account/logout", err)
		}
		return nil
	})
}

// oneShot runs call against a fresh app-server under the jail, then stops it.
func (a *CodexAdapter) oneShot(ctx context.Context, call func(context.Context, *codexProcess) error) error {
	l, err := a.resolveCommand()
	if err != nil {
		return err
	}
	work, err := codexScratchDir("devkit-codex-account-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	p, err := a.start(work, l, OpenSpec{}, codexStoreFile, codexNoHandler{})
	if err != nil {
		return err
	}
	defer p.stop()
	callCtx, cancel := p.bounded(ctx, handshakeTimeout)
	defer cancel()
	if err := a.initialize(callCtx, p); err != nil {
		return err
	}
	return call(callCtx, p)
}

// codexScratchDir is an empty working directory for a process that has no
// project, symlinks resolved so the jail binds what Seatbelt will match.
func codexScratchDir(prefix string) (string, error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("%w: working directory: %v", ErrProcessFailed, err)
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir, nil
}
