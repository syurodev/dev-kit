package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
	"devkit/internal/terminal"
)

// Vendor accounts (Settings → Agents → Accounts & limits). Every method here is the
// window's: there is no MCP tool for any of them (TestNoToolTouchesAgentAccounts),
// each refuses a caller that is not the UI, and the Service refuses again. Audit
// metadata carries ids and booleans only — never a label, a folder, a usage number,
// an error text or anything a login printed.

// errAgentAccountsDesktopOnly: changing accounts, or the rules that act on them, does
// not exist in server mode. That bridge is unauthenticated loopback where an unstamped
// call is the window, so any local process could fabricate the terms acknowledgement,
// start a login or remove an account.
var errAgentAccountsDesktopOnly = errors.New("bridge.agentAccountsDesktopOnly")

// AgentAccountInfo is one vendor account as Settings draws it. Times are Unix seconds
// (0 = none). The account's home folder and every credential stay in Go.
type AgentAccountInfo struct {
	UsageGeneration string `json:"usageGeneration,omitempty"`
	ID              string `json:"id"`
	Vendor          string `json:"vendor"`
	Label           string `json:"label"`
	Builtin         bool   `json:"builtin"`
	SignedIn        bool   `json:"signedIn"`
	Shared          bool   `json:"shared"`
	// ExhaustedUntil and CooldownUntil are when a refusal or a failed probe keeps the
	// account out. They may be past values (nothing clears them when time passes):
	// a consumer must treat a time at or before now as "not limited".
	ExhaustedUntil int64                  `json:"exhaustedUntil"`
	CooldownUntil  int64                  `json:"cooldownUntil"`
	Usage          []agenthub.LimitWindow `json:"usage"`
	UsageSource    string                 `json:"usageSource"`
	ObservedAt     int64                  `json:"observedAt"`
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func accountInfo(a agenthub.AccountRecord) AgentAccountInfo {
	usage := a.Usage
	if usage == nil {
		usage = []agenthub.LimitWindow{}
	}
	return AgentAccountInfo{
		UsageGeneration: a.UsageGeneration, ID: a.ID, Vendor: string(a.Vendor), Label: a.Label, Builtin: a.Builtin(), SignedIn: a.SignedIn, Shared: a.Shared,
		ExhaustedUntil: unixOrZero(a.ExhaustedUntil), CooldownUntil: unixOrZero(a.CooldownUntil),
		Usage: usage, UsageSource: a.UsageSource, ObservedAt: unixOrZero(a.ObservedAt),
	}
}

var accountIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// accountScopeID keeps a caller-chosen id out of the capability scope and the audit
// metadata unless it has the shape DevKit itself generates (a vendor-label slug).
//
// "list" and "new" are fixed scope names ("account:list", "account:new"). They cannot
// collide with an account's scope: DevKit-generated ids always start with "<vendor>-"
// (accountIDCandidate), so none is the bare word "list" or "new".
func accountScopeID(id string) string {
	if accountIDPattern.MatchString(id) {
		return id
	}
	return invalidOpaqueID
}

// agentAccountsCall is the shared shape: a gateway call as the agent hub subject,
// window only, with the agent hub service.
func (s *AppService) agentAccountsCall(ctx context.Context, capability, scope string, meta map[string]any, fn func(context.Context, *agenthub.Service) (any, error)) (any, error) {
	return s.call(ctx, "builtin.agenthub", capability, scope, meta, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if capability != "agenthub:read" && s.inServerMode() {
			return nil, errAgentAccountsDesktopOnly
		}
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return fn(callCtx, svc)
	})
}

// oneAccount runs an operation that names an existing account and answers with it.
func (s *AppService) oneAccount(ctx context.Context, capability, action, id string, extra map[string]any, fn func(context.Context, *agenthub.Service) (agenthub.AccountRecord, error)) (AgentAccountInfo, error) {
	scopeID := accountScopeID(id)
	meta := map[string]any{"action": action, "account": scopeID}
	for k, v := range extra {
		meta[k] = v
	}
	result, err := s.agentAccountsCall(ctx, capability, "account:"+scopeID, meta, func(c context.Context, svc *agenthub.Service) (any, error) {
		rec, err := fn(c, svc)
		if err != nil {
			return nil, err
		}
		return accountInfo(rec), nil
	})
	if err != nil {
		return AgentAccountInfo{}, err
	}
	return result.(AgentAccountInfo), nil
}

// AgentAccountsList lists a vendor's accounts, its built-in one first.
func (s *AppService) AgentAccountsList(ctx context.Context, vendor string) ([]AgentAccountInfo, error) {
	v := agentRuntimeID(vendor)
	result, err := s.agentAccountsCall(ctx, "agenthub:read", "account:list", map[string]any{"action": "list_agent_accounts", "vendor": v}, func(c context.Context, svc *agenthub.Service) (any, error) {
		list, err := svc.ListAccounts(c, agenthub.AgentType(v))
		if err != nil {
			return nil, err
		}
		out := make([]AgentAccountInfo, 0, len(list))
		for _, a := range list {
			out = append(out, accountInfo(a))
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]AgentAccountInfo), nil
}

// AgentAccountsAdd adds an account: its folder and its link to the shared
// conversation store. Nothing is signed in yet.
func (s *AppService) AgentAccountsAdd(ctx context.Context, vendor, label string) (AgentAccountInfo, error) {
	v := agentRuntimeID(vendor)
	result, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:new", map[string]any{"action": "add_agent_account", "vendor": v}, func(c context.Context, svc *agenthub.Service) (any, error) {
		rec, err := svc.AddAccount(c, agenthub.AgentType(v), label)
		if err != nil {
			return nil, err
		}
		return accountInfo(rec), nil
	})
	if err != nil {
		return AgentAccountInfo{}, err
	}
	return result.(AgentAccountInfo), nil
}

// AgentAccountsRename renames an added account; the built-in one has a fixed name.
func (s *AppService) AgentAccountsRename(ctx context.Context, id, label string) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", "rename_agent_account", id, nil,
		func(c context.Context, svc *agenthub.Service) (agenthub.AccountRecord, error) {
			return svc.RenameAccount(c, id, label)
		})
}

// AgentAccountsSetShared says whether other people also use the login, which makes
// what DevKit saw of its usage a lower bound.
func (s *AppService) AgentAccountsSetShared(ctx context.Context, id string, shared bool) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", "set_agent_account_shared", id, map[string]any{"shared": shared},
		func(c context.Context, svc *agenthub.Service) (agenthub.AccountRecord, error) {
			return svc.SetAccountShared(c, id, shared)
		})
}

// AgentAccountsRemove removes an added account and its folder.
func (s *AppService) AgentAccountsRemove(ctx context.Context, id string) error {
	scopeID := accountScopeID(id)
	_, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:"+scopeID, map[string]any{"action": "remove_agent_account", "account": scopeID}, func(c context.Context, svc *agenthub.Service) (any, error) {
		return nil, svc.RemoveAccount(c, id)
	})
	return err
}

// AgentAccountsRefreshStatus re-reads whether the account is signed in.
func (s *AppService) AgentAccountsRefreshStatus(ctx context.Context, id string) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", "refresh_agent_account_status", id, nil,
		func(c context.Context, svc *agenthub.Service) (agenthub.AccountRecord, error) {
			return svc.RefreshAccountStatus(c, id)
		})
}

// AgentAccountsRefreshUsage asks the vendor what the account has left. It starts a
// short-lived vendor process, so the Service limits how often it runs per account.
func (s *AppService) AgentAccountsRefreshUsage(ctx context.Context, id string) (AgentAccountInfo, error) {
	return s.oneAccount(ctx, "agenthub:execute", "refresh_agent_account_usage", id, nil,
		func(c context.Context, svc *agenthub.Service) (agenthub.AccountRecord, error) {
			return svc.RefreshAccountUsage(c, id)
		})
}

// AgentAccountsStartSignIn runs the vendor's terminal sign-in (Claude) for an added
// account in a new terminal session and returns its id, for a terminal tab to attach
// to. Only a method id crosses the bridge, never a command. It mirrors
// AgentHubStartSignIn; when the terminal exits the account's status is read again
// (AfterAccountSignIn, which has no caller identity and is only ever reached from
// here, never from the bridge or an MCP tool).
func (s *AppService) AgentAccountsStartSignIn(ctx context.Context, id, methodID string, rows, cols int) (string, error) {
	scopeID := accountScopeID(id)
	result, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:"+scopeID, map[string]any{
		"action":  "start_agent_account_sign_in",
		"account": scopeID,
		"method":  agentRuntimeID(methodID),
	}, func(c context.Context, svc *agenthub.Service) (any, error) {
		if s.container.Terminal == nil {
			return nil, errAgentHubUnavailable
		}
		cmd, err := svc.AccountSignInCommand(c, id, methodID)
		if err != nil {
			return nil, err
		}
		owned := true
		defer func() {
			if owned && cmd.Release != nil {
				cmd.Release()
			}
		}()
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%w: sign-in: home directory: %v", agenthub.ErrProcessFailed, err)
		}
		pty, err := terminal.NewLocalCommandPTY(cmd.Path, cmd.Args, cmd.Env, home, rows, cols)
		if err != nil {
			return nil, fmt.Errorf("%w: sign-in: start %s: %v", agenthub.ErrProcessFailed, cmd.Label, err)
		}
		owned = false
		go func() {
			pty.Wait()
			if cmd.Release != nil {
				cmd.Release()
			}
			svc.AfterAccountSignIn(id)
		}()
		return s.container.Terminal.Register("builtin.agenthub", pty), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// AgentAccountsStartBrowserSignIn starts the in-app sign-in (Codex) at an added
// account's home. The flow ends with agenthub:signInCompleted, which carries the
// account id; AgentHubCancelSignIn abandons it.
func (s *AppService) AgentAccountsStartBrowserSignIn(ctx context.Context, id string) (agenthub.SignInFlow, error) {
	return s.startAccountFlow(ctx, id, agenthub.SignInBrowser)
}

// AgentAccountsStartDeviceSignIn is the same by code, for a machine with no browser.
func (s *AppService) AgentAccountsStartDeviceSignIn(ctx context.Context, id string) (agenthub.SignInFlow, error) {
	return s.startAccountFlow(ctx, id, agenthub.SignInDeviceCode)
}

// startAccountFlow records how, never what: the page and the code in the answer are
// the person's to use once and stay out of the audit log.
func (s *AppService) startAccountFlow(ctx context.Context, id, method string) (agenthub.SignInFlow, error) {
	scopeID := accountScopeID(id)
	result, err := s.agentAccountsCall(ctx, "agenthub:execute", "account:"+scopeID, map[string]any{
		"action":  "start_agent_account_in_app_sign_in",
		"account": scopeID,
		"method":  method,
	}, func(c context.Context, svc *agenthub.Service) (any, error) {
		return svc.StartAccountSignIn(c, id, method)
	})
	if err != nil {
		return agenthub.SignInFlow{}, err
	}
	return result.(agenthub.SignInFlow), nil
}

// AgentAccountVendorPolicyState is one vendor's switching rules as Settings edits them.
type AgentAccountVendorPolicyState struct {
	Enabled                bool     `json:"enabled"`
	ThresholdPercent       int      `json:"thresholdPercent"`
	WeeklyThresholdPercent int      `json:"weeklyThresholdPercent"`
	UsageMaxAgeSeconds     int      `json:"usageMaxAgeSeconds"`
	Order                  []string `json:"order"`
	OnLimitRefusal         string   `json:"onLimitRefusal"`
	// Strategy is "order", "best" or "consumeFirst"; "" reads as "order".
	Strategy string `json:"strategy"`
	// AccountThresholds is each account's own limit by id; an account without an entry uses ThresholdPercent.
	AccountThresholds map[string]int `json:"accountThresholds"`
}

// AgentAccountPolicyState is preferences.AgentAccountPolicy as the window sees it.
// RiskAcknowledgedAt is read-only here: DevKit stamps it itself (AgentAccountPolicySet).
type AgentAccountPolicyState struct {
	Revision           int64                                    `json:"revision"`
	RiskAcknowledgedAt string                                   `json:"riskAcknowledgedAt"`
	Vendors            map[string]AgentAccountVendorPolicyState `json:"vendors"`
}

func accountPolicyState(p preferences.AgentAccountPolicy) AgentAccountPolicyState {
	out := AgentAccountPolicyState{Revision: p.Revision, RiskAcknowledgedAt: p.RiskAcknowledgedAt, Vendors: make(map[string]AgentAccountVendorPolicyState, len(p.Vendors))}
	for name, v := range p.Vendors {
		order := v.Order
		if order == nil {
			order = []string{}
		}
		limits := map[string]int{}
		for id, limit := range v.AccountThresholds {
			limits[id] = limit
		}
		out.Vendors[name] = AgentAccountVendorPolicyState{
			Enabled: v.Enabled, ThresholdPercent: v.ThresholdPercent, WeeklyThresholdPercent: v.WeeklyThresholdPercent,
			UsageMaxAgeSeconds: v.UsageMaxAgeSeconds, Order: order, OnLimitRefusal: v.OnLimitRefusal,
			Strategy: v.Strategy, AccountThresholds: limits,
		}
	}
	return out
}

// AgentAccountPolicyGet reads the switching rules.
func (s *AppService) AgentAccountPolicyGet(ctx context.Context) (AgentAccountPolicyState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:read", "agent_account_policy", map[string]any{"action": "get_agent_account_policy"}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		value, err := s.container.Preferences.AgentAccountPolicy()
		if err != nil {
			return nil, err
		}
		return accountPolicyState(value), nil
	})
	if err != nil {
		return AgentAccountPolicyState{}, err
	}
	return result.(AgentAccountPolicyState), nil
}

// AgentAccountPolicySet replaces the rules when state.Revision is the stored one.
// acknowledgeRisk records that the person accepted the terms warning; the timestamp is
// DevKit's own (RiskAcknowledgedAt in state is ignored) and a stored acknowledgement
// is kept on later saves. A vendor cannot be enabled without one: the preferences
// validator refuses with the terms sentence.
func (s *AppService) AgentAccountPolicySet(ctx context.Context, state AgentAccountPolicyState, acknowledgeRisk bool) (AgentAccountPolicyState, error) {
	result, err := s.call(ctx, "builtin.settings", "preferences:write", "agent_account_policy", map[string]any{
		"action":      "set_agent_account_policy",
		"vendors":     len(state.Vendors),
		"acknowledge": acknowledgeRisk,
	}, func(callCtx context.Context) (any, error) {
		if err := uiOnly(callCtx); err != nil {
			return nil, err
		}
		if s.inServerMode() {
			return nil, errAgentAccountsDesktopOnly
		}
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		current, err := s.container.Preferences.AgentAccountPolicy()
		if err != nil {
			return nil, err
		}
		ack := current.RiskAcknowledgedAt
		if acknowledgeRisk && strings.TrimSpace(ack) == "" {
			ack = time.Now().UTC().Format(time.RFC3339)
		}
		next := preferences.AgentAccountPolicy{Revision: state.Revision, RiskAcknowledgedAt: ack, Vendors: map[string]preferences.AccountVendorPolicy{}}
		for name, v := range state.Vendors {
			next.Vendors[name] = preferences.AccountVendorPolicy{
				Enabled: v.Enabled, ThresholdPercent: v.ThresholdPercent, WeeklyThresholdPercent: v.WeeklyThresholdPercent,
				UsageMaxAgeSeconds: v.UsageMaxAgeSeconds, Order: v.Order, OnLimitRefusal: v.OnLimitRefusal,
				Strategy: v.Strategy, AccountThresholds: v.AccountThresholds,
			}
		}
		saved, err := s.container.Preferences.SetAgentAccountPolicy(next)
		if err != nil {
			return nil, err
		}
		if hub := s.container.AgentHub; hub != nil {
			hub.InvalidateAccountPolicy() // the next turn reads the new rules, not the cached ones
			hub.NotifyAccountsChanged()
		}
		return accountPolicyState(saved), nil
	})
	if err != nil {
		return AgentAccountPolicyState{}, err
	}
	return result.(AgentAccountPolicyState), nil
}
