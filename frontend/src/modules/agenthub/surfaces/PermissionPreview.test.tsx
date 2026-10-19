import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentSession, PendingPermission } from "@/bridge/agenthub";
import { permissionDetail } from "@/bridge/agenthub";
import { AgentPermissionRequest } from "./AgentPermissionRequest";
import { PermissionPreview } from "./PermissionPreview";

const render = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);

const session: AgentSession = {
  id: "sess-1",
  agentType: "codex",
  projectPath: "/workspace/app",
  status: "awaiting_permission",
  parentSessionId: null,
  depth: 0,
  title: "Codex",
  acpSessionId: "th-1",
};

const LONG_MESSAGE =
  "Bạn là session con của 8d2fcfa7-8e6a-4f5f-a8d6-460d798e85cc. Nhiệm vụ dễ, CHỈ ĐỌC / PHÂN TÍCH, không sửa file, không commit/push, không mở thêm agent.\n" +
  "User muốn từ Đề xuất mua ở medical-examination mở nhanh POS điền sẵn khách, thuốc, số lượng và ghi chú.\n" +
  "Hãy tìm điểm vào, các màn hình liên quan và nêu những file cần đổi.";

function mcpApproval(rawInput: Record<string, unknown>, options?: unknown[]): PendingPermission {
  return {
    id: "perm-send",
    sessionId: session.id,
    caller: "agent:sess-1",
    title: "Codex",
    depth: 0,
    createdAt: "",
    expiresAt: "",
    params: {
      toolCall: {
        toolCallId: "mcp-approval-1",
        title: "devkit.agenthub.sendMessage",
        kind: "other",
        status: "pending",
        content: [{ type: "content", content: { type: "text", text: 'Allow the devkit MCP server to run tool "agenthub.sendMessage"?' } }],
        rawInput,
      },
      options: options ?? [
        { optionId: "allow_once", name: "Allow", kind: "allow_once" },
        { optionId: "allow_always", name: "Allow for this session", kind: "allow_always" },
        { optionId: "reject_once", name: "Deny", kind: "reject_once" },
      ],
    },
  };
}

describe("permissionDetail carries the whole request beside the elided line", () => {
  it("keeps argsFull complete while args stays one elided line", () => {
    const input = { message: LONG_MESSAGE };
    const detail = permissionDetail({ toolCall: { kind: "other", rawInput: input } });
    expect(detail.args?.length).toBe(240);
    expect(detail.argsFull).toBe(JSON.stringify(input));
    expect(JSON.parse(detail.argsFull ?? "")).toEqual(input);
  });
});

describe("PermissionPreview", () => {
  it("folds a long request to three lines with a labelled, wired toggle", () => {
    const html = render(<PermissionPreview text={JSON.stringify({ message: LONG_MESSAGE })} testId="permission-args" />);
    expect(html).toContain("line-clamp-3");
    expect(html).toContain('data-expanded="false"');
    expect(html).toContain("Show full request");
    expect(html).toContain('aria-expanded="false"');
    const controls = /aria-controls="([^"]+)"/.exec(html)?.[1];
    expect(controls).toBeTruthy();
    expect(html).toContain(`id="${controls}"`);
    expect(html).toContain('data-testid="permission-preview-copy"');
    // Folded, the text is still the exact compact input — no ellipsis written into it.
    expect(html).not.toContain("…");
  });

  it("opens in place to the whole request, indented, wrapped and scrollable", () => {
    const html = render(<PermissionPreview text={JSON.stringify({ message: LONG_MESSAGE })} testId="permission-args" defaultOpen />);
    expect(html).toContain('data-expanded="true"');
    expect(html).toContain('aria-expanded="true"');
    expect(html).toContain("Show less");
    expect(html).toContain("permission-preview-cap");
    expect(html).toContain("overflow-y-auto");
    expect(html).toContain("whitespace-pre-wrap");
    expect(html).not.toContain("line-clamp-3");
    // The whole message, Vietnamese intact, with its real line breaks.
    expect(html).toContain("CHỈ ĐỌC / PHÂN TÍCH");
    expect(html).toContain("nêu những file cần đổi.");
    expect(html).toContain("không mở thêm agent.\nUser muốn");
    expect(html).toContain('role="region"');
    expect(html).toContain('aria-label="Full request"');
  });

  it("offers no toggle for a short request", () => {
    const html = render(<PermissionPreview text='{"sql":"select 1"}' testId="permission-args" />);
    expect(html).not.toContain("Show full request");
    expect(html).not.toContain('data-testid="permission-preview-copy"');
  });

  it("plain command text is shown as given", () => {
    const html = render(<PermissionPreview text={"git status\ngit diff --stat"} testId="permission-command" defaultOpen />);
    expect(html).toContain("git status\ngit diff --stat");
    expect(html).toContain('data-testid="permission-command"');
  });
});

describe("the permission card with a long request", () => {
  it("shows all of a long sendMessage argument behind Show full request", () => {
    const html = render(<AgentPermissionRequest pending={mcpApproval({ message: LONG_MESSAGE })} session={session} onResolve={() => {}} panel />);
    expect(html).toContain("Show full request");
    expect(html).toContain('data-testid="permission-message"');
    expect(html).toContain('data-testid="permission-args"');
  });

  it("keeps Deny, the grant and Allow once in one row under the body, in that order", () => {
    const html = render(<AgentPermissionRequest pending={mcpApproval({ message: "hi" })} session={session} onResolve={() => {}} />);
    const footer = html.slice(html.indexOf('data-testid="permission-actions"'));
    const deny = footer.indexOf('data-testid="permission-deny"');
    const always = footer.indexOf('data-testid="permission-allow-always"');
    const once = footer.indexOf('data-testid="permission-allow-once"');
    expect(deny).toBeGreaterThan(-1);
    expect(always).toBeGreaterThan(deny);
    expect(once).toBeGreaterThan(always);
    // Allow once is the only filled control; the others are outlined at the same size.
    expect(footer.match(/h-\(--control-height-sm\)/g)?.length).toBe(3);
    expect(footer.match(/bg-action-primary/g)?.length).toBe(1);
    expect(html).toContain("Allow for session");
  });

  it("still never offers allow-always in a locked environment", () => {
    const html = render(<AgentPermissionRequest pending={mcpApproval({ message: "hi" })} session={session} env="locked" onResolve={() => {}} />);
    expect(html).not.toContain('data-testid="permission-allow-always"');
    expect(html).toContain('data-testid="permission-allow-once"');
  });

  it("offers no grant when the vendor offers none (opencode)", () => {
    const html = render(
      <AgentPermissionRequest
        pending={mcpApproval({ message: "hi" }, [
          { optionId: "once", name: "Allow once", kind: "allow_once" },
          { optionId: "reject", name: "Deny", kind: "reject_once" },
        ])}
        session={session}
        onResolve={() => {}}
      />,
    );
    expect(html).not.toContain('data-testid="permission-allow-always"');
  });
});
