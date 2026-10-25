import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { AgentRail } from "./AgentRail";

const session: AgentSession = {
  id: "session-rail",
  agentType: "claude",
  projectPath: "/workspace/app",
  status: "awaiting_permission",
  parentSessionId: null,
  depth: 0,
  title: "Agent",
  acpSessionId: "acp-session-rail",
};

const pending: PendingPermission = {
  id: "permission-rail",
  sessionId: session.id,
  caller: "agent:session-rail",
  title: "Agent",
  depth: 0,
  createdAt: new Date().toISOString(),
  expiresAt: "",
  scopedApprovalEligible: true,
  params: {
    origin: "devkit_gate",
    env: "dev",
    locked: false,
    tool: "db.query",
    profileId: "profile-1",
    summary: "SELECT 1",
    options: [{ optionId: "allow_once", name: "Allow once" }],
  },
};

function render(node: React.ReactNode) {
  return renderRaw(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{node}</TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("scoped approval action in the companion Agent Rail", () => {
  it("passes the callback through the rail tray to its permission card", () => {
    const html = render(
      <AgentRail
        activeSession={session}
        promptValue=""
        onPromptChange={() => {}}
        onSend={() => {}}
        onCancel={() => {}}
        pendingPermission={pending}
        onResolvePermission={() => {}}
        onApproveScopedPermission={() => {}}
      />,
    );

    expect(html).toContain('data-testid="agent-rail"');
    expect(html).toContain('data-testid="permission-approve-scoped"');
    expect(html).toContain("Approve exact query");
  });
});
