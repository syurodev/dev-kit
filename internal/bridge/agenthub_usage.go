package bridge

import (
	"context"

	"devkit/internal/agenthub"
)

// Token usage (Settings → Agents → Accounts & limits). The window's alone, like the
// account methods: no MCP tool uses the usage:* scope, the call refuses a caller that
// is not the window, and the Service refuses again. It answers with numbers and ids
// only: no prompt, no message, no title, no path. Audit metadata carries the vendor id.

// AgentUsageTotalsInfo is a sum of tokens. A turn with no token data counts in
// Turns and TurnsWithoutData and adds nothing else.
type AgentUsageTotalsInfo struct {
	Known            map[string]int `json:"known"`
	Turns            int            `json:"turns"`
	TurnsWithoutData int            `json:"turnsWithoutData"`
	FreshInput       int64          `json:"freshInput"`
	CacheRead        int64          `json:"cacheRead"`
	CacheWrite       int64          `json:"cacheWrite"`
	Output           int64          `json:"output"`
	Total            int64          `json:"total"`
}

// AgentUsageGroupInfo is one workspace id, UTC day (YYYY-MM-DD) or model.
type AgentUsageGroupInfo struct {
	Key    string               `json:"key"`
	Totals AgentUsageTotalsInfo `json:"totals"`
}

// AgentUsageSessionInfo is one session's totals.
type AgentUsageSessionInfo struct {
	SessionID   string               `json:"sessionId"`
	WorkspaceID string               `json:"workspaceId"`
	Totals      AgentUsageTotalsInfo `json:"totals"`
}

// AgentUsageReportInfo is what a vendor's turns spent, in tokens. It is not a share
// of a subscription limit. Every list is non-null. Workspace ids are named by the
// window from the workspace list it already has.
type AgentUsageReportInfo struct {
	Vendor            string                  `json:"vendor"`
	Totals            AgentUsageTotalsInfo    `json:"totals"`
	ByWorkspace       []AgentUsageGroupInfo   `json:"byWorkspace"`
	ByDay             []AgentUsageGroupInfo   `json:"byDay"`
	ByModel           []AgentUsageGroupInfo   `json:"byModel"`
	TopSessions       []AgentUsageSessionInfo `json:"topSessions"`
	PartialSplitTurns int                     `json:"partialSplitTurns"`
}

func usageTotalsInfo(t agenthub.UsageTotals) AgentUsageTotalsInfo {
	return AgentUsageTotalsInfo{
		Known: map[string]int{
			"freshInput": t.Known.FreshInput, "cacheRead": t.Known.CacheRead,
			"cacheWrite": t.Known.CacheWrite, "output": t.Known.Output, "total": t.Known.Total,
		},
		Turns: t.Turns, TurnsWithoutData: t.TurnsWithoutData, FreshInput: t.FreshInput,
		CacheRead: t.CacheRead, CacheWrite: t.CacheWrite, Output: t.Output, Total: t.Total,
	}
}

func usageGroupsInfo(groups []agenthub.UsageGroup) []AgentUsageGroupInfo {
	out := make([]AgentUsageGroupInfo, 0, len(groups))
	for _, g := range groups {
		out = append(out, AgentUsageGroupInfo{Key: g.Key, Totals: usageTotalsInfo(g.UsageTotals)})
	}
	return out
}

func usageReportInfo(r agenthub.UsageReport) AgentUsageReportInfo {
	sessions := make([]AgentUsageSessionInfo, 0, len(r.TopSessions))
	for _, s := range r.TopSessions {
		sessions = append(sessions, AgentUsageSessionInfo{SessionID: s.SessionID, WorkspaceID: s.WorkspaceID, Totals: usageTotalsInfo(s.UsageTotals)})
	}
	return AgentUsageReportInfo{
		Vendor:            string(r.Vendor),
		Totals:            usageTotalsInfo(r.Totals),
		ByWorkspace:       usageGroupsInfo(r.ByWorkspace),
		ByDay:             usageGroupsInfo(r.ByDay),
		ByModel:           usageGroupsInfo(r.ByModel),
		TopSessions:       sessions,
		PartialSplitTurns: r.PartialSplitTurns,
	}
}

// AgentUsageReport reports what a vendor's turns spent, in tokens.
func (s *AppService) AgentUsageReport(ctx context.Context, vendor string) (AgentUsageReportInfo, error) {
	v := agentRuntimeID(vendor)
	result, err := s.agentAccountsCall(ctx, "agenthub:read", "usage:report", map[string]any{"action": "agent_usage_report", "vendor": v}, func(c context.Context, svc *agenthub.Service) (any, error) {
		report, err := svc.UsageReport(c, agenthub.AgentType(v))
		if err != nil {
			return nil, err
		}
		return usageReportInfo(report), nil
	})
	if err != nil {
		return AgentUsageReportInfo{}, err
	}
	return result.(AgentUsageReportInfo), nil
}
