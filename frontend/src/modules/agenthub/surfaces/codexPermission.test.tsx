import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { permissionDetail, type AgentSession, type PendingPermission } from "@/bridge/agenthub";
import { AgentPermissionRequest } from "./AgentPermissionRequest";
import { allowAlwaysLabel } from "./permissionCard";
import { bypassSandboxWarning, isPermissionBypassChoice } from "./AgentComposer";

const render = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);

const session: AgentSession = {
  id: "sess-codex",
  agentType: "codex",
  projectPath: "/workspace/app",
  status: "awaiting_permission",
  parentSessionId: null,
  depth: 0,
  title: "Codex",
  acpSessionId: "th-1",
};

// The request internal/agenthub/codex_approvals.go (mcpToolApprovalFor) puts
// to the person when Codex asks before running a DevKit MCP tool and no open
// call can be matched to it: it stands on its own as "mcp-approval-<n>".
function mcpApproval(over: Record<string, unknown> = {}): PendingPermission {
  return {
    id: "perm-mcp",
    sessionId: session.id,
    caller: "agent:sess-codex",
    title: "Codex",
    depth: 0,
    createdAt: new Date().toISOString(),
    expiresAt: new Date(Date.now() + 60_000).toISOString(),
    params: {
      sessionId: session.id,
      toolCall: {
        toolCallId: "mcp-approval-3",
        title: "db.query",
        kind: "other",
        status: "pending",
        content: [{ type: "content", content: { type: "text", text: 'Allow the devkit MCP server to run tool "db.query"?' } }],
        rawInput: { profileId: "0123456789abcdef01234567", sql: "select 1" },
        ...over,
      },
      options: [
        { optionId: "allow_once", name: "Allow", kind: "allow_once" },
        { optionId: "allow_always", name: "Allow for this session", kind: "allow_always" },
        { optionId: "reject_once", name: "Deny", kind: "reject_once" },
      ],
    },
  };
}

describe("Codex's MCP tool approval card", () => {
  it("says which tool, what Codex asked, and with what arguments", () => {
    const html = render(<AgentPermissionRequest pending={mcpApproval()} session={session} onResolve={() => {}} size="wide" />);
    expect(html).toContain("db.query");
    expect(html).toContain('data-testid="permission-message"');
    expect(html).toContain("Allow the devkit MCP server to run tool");
    expect(html).toContain('data-testid="permission-args"');
    expect(html).toContain("select 1");
  });

  it("offers the session grant as what it is, never as always", () => {
    const html = render(<AgentPermissionRequest pending={mcpApproval()} session={session} onResolve={() => {}} />);
    expect(html).toContain('data-testid="permission-allow-always"');
    expect(html).toContain("Allow for session");
    expect(html).not.toContain("Allow always");
    expect(allowAlwaysLabel({ optionId: "allow_always", name: "Always allow", kind: "allow_always" })).toBe("Allow always");
    expect(allowAlwaysLabel(undefined)).toBe("Allow always");
  });

  it("a card attached to the open call reads the same", () => {
    const html = render(
      <AgentPermissionRequest
        pending={mcpApproval({ toolCallId: "call-7", title: "devkit.db.query" })}
        session={session}
        onResolve={() => {}}
      />,
    );
    expect(html).toContain("devkit.db.query");
    expect(html).toContain("select 1");
  });
});

describe("permissionDetail", () => {
  it("leaves an edit to its diff and a command to its well", () => {
    expect(permissionDetail({ toolCall: { kind: "edit", rawInput: { file_path: "a.ts", content: "x".repeat(5000) } } })).toEqual({});
    expect(permissionDetail({ toolCall: { kind: "read", rawInput: { path: "a.ts" } } })).toEqual({});
  });

  it("elides long arguments and ignores empty ones", () => {
    const long = permissionDetail({ toolCall: { kind: "other", rawInput: { sql: "x".repeat(1000) } } });
    expect(long.args?.length).toBe(240);
    expect(long.args?.endsWith("…")).toBe(true);
    expect(permissionDetail({ toolCall: { rawInput: {} } })).toEqual({});
    expect(permissionDetail(null)).toEqual({});
  });
});

describe("Codex's Full auto", () => {
  // internal/agenthub/adapter_codex.go codexModes: the ids are ACP's, and
  // bypassPermissions is the one that must open the confirmation.
  it("is the bypass the confirmation guards; Ask and Auto are not", () => {
    expect(isPermissionBypassChoice({ value: "bypassPermissions", name: "Full auto" })).toBe(true);
    expect(isPermissionBypassChoice({ value: "auto", name: "Auto" })).toBe(false);
    expect(isPermissionBypassChoice({ value: "ask", name: "Ask" })).toBe(false);
  });

  it("the confirmation says what the jail leaves open", () => {
    expect(bypassSandboxWarning({})).toContain("Docker socket and your $HOME");
    expect(bypassSandboxWarning(null)).toContain("Docker socket and your $HOME");
    expect(bypassSandboxWarning({ shareHome: false })).toStartWith(
      "The Docker socket is open to this session — the sandbox does not stop the agent from using it.",
    );
    expect(bypassSandboxWarning({ allowDocker: false })).toStartWith(
      "Your $HOME is open to this session — the sandbox does not stop the agent from using it.",
    );
  });

  it("settings turned off after the session started do not silence it", () => {
    // The jail was fixed at spawn: Docker switched off now may still be open
    // in this process, so the line says so instead of disappearing.
    const started = "if this one started with more open, it still has it.";
    expect(bypassSandboxWarning({ shareHome: false, allowDocker: false })).toContain(started);
    expect(bypassSandboxWarning({ allowDocker: false })).toContain(started);
    expect(bypassSandboxWarning({ shareHome: false })).toContain(started);
    expect(bypassSandboxWarning({})).not.toContain(started);
  });
});
