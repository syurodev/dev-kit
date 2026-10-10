import { describe, expect, it } from "bun:test";
import type * as React from "react";
import { renderToString } from "react-dom/server";
import type { AgentSession, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentComposer } from "./AgentComposer";
import { AgentHubContent } from "./AgentHubContent";
import { AgentRail } from "./AgentRail";
import { TrayStatusOverride, type TrayStatus } from "./composer/useTrayStatus";

/**
 * The composer is one cluster in its own region of the layout. This renders the
 * real conversation and rail and reads the structure back. What cannot be read
 * from markup — focus, scroll position, what a short pane does — is checked in
 * a browser (the dev gallery's "composer cluster" section), not here.
 */

const session: AgentSession = {
  id: "sess-c",
  agentType: "claude",
  projectPath: "/work/api",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: "Retry test",
  acpSessionId: "acp",
};

const permission: PendingPermission = {
  id: "perm-1",
  sessionId: "sess-c",
  caller: "agent:sess-c",
  createdAt: new Date().toISOString(),
  expiresAt: new Date(Date.now() + 60_000).toISOString(),
  title: "Retry test",
  depth: 0,
  params: { toolCall: { title: "Run the tests" }, options: [{ optionId: "allow_once", name: "Allow once" }] },
};

const question: PendingElicitation = {
  id: "q-1",
  sessionId: "sess-c",
  toolCallId: "t",
  message: "Which one?",
  params: {
    mode: "form",
    message: "Which one?",
    requestedSchema: { type: "object", properties: { pick: { type: "string", title: "Pick", oneOf: [{ const: "a", title: "A" }] } } },
  },
  caller: "agent:sess-c",
  title: "Retry test",
  depth: 0,
  createdAt: new Date().toISOString(),
};

const repo = (name: string, over: Partial<TrayStatus["repos"][number]> = {}): TrayStatus["repos"][number] => ({
  name,
  path: `/work/${name}`,
  branch: "main",
  detached: false,
  ahead: 0,
  behind: 0,
  changed: 0,
  conflicted: 0,
  error: "",
  own: false,
  ...over,
});

const status: TrayStatus = {
  workspace: { id: "ws", name: "billing", folders: 3, paths: ["/work/api"] },
  branch: "main",
  repos: [repo("a", { changed: 2 }), repo("b", { conflicted: 1 }), repo("c"), repo("d")],
  hasBoards: true,
  tasks: [],
  others: [],
};

const noop = () => {};
const base = { promptValue: "", onPromptChange: noop, onSend: noop, onCancel: noop };

function render(node: React.ReactNode, withStatus = true): string {
  return renderToString(
    <TooltipProvider>
      <TrayStatusOverride.Provider value={withStatus ? status : null}>{node}</TrayStatusOverride.Provider>
    </TooltipProvider>,
  );
}

const hub = (props: Partial<React.ComponentProps<typeof AgentHubContent>> = {}) =>
  render(<AgentHubContent session={session} items={[]} {...base} {...props} />);

describe("the composer is in the layout, below the transcript", () => {
  it("is a region after the transcript's, not a layer over it", () => {
    const html = hub();
    const transcript = html.indexOf('data-testid="agent-hub-transcript-region"');
    const region = html.indexOf('data-testid="agent-composer-region"');
    expect(transcript).toBeGreaterThan(-1);
    expect(region).toBeGreaterThan(transcript);
    const tag = html.slice(html.lastIndexOf("<div", region), html.indexOf(">", region));
    // Nothing positioned over the transcript, and nothing that lets clicks through it.
    expect(tag).not.toContain("absolute");
    expect(tag).not.toContain("pointer-events-none");
    expect(tag).toContain("composer-region-cap");
    expect(html).not.toContain("pointer-events-none absolute inset-x-0 bottom-0");
  });

  it("no longer pads the transcript by a measured composer height", () => {
    const html = hub({ items: [{ id: "u", kind: "user_message_chunk", text: "hi" }] });
    expect(html).not.toContain("composer-inset");
    const draft = hub({ session: null, draft: { projectPath: "/work/api" } });
    expect(draft).not.toContain("composer-inset");
  });

  it("shares the transcript's reading column and padding", () => {
    const html = hub();
    const region = html.slice(html.indexOf('data-testid="agent-composer-region"'));
    expect(region.slice(0, 400)).toContain("px-6");
    expect(region.slice(0, 400)).toContain("max-w-180");
  });

  it("is the same region in the rail, which always flowed", () => {
    const html = render(<AgentRail activeSession={session} items={[]} {...base} />);
    expect(html).toContain('data-testid="agent-composer-region"');
    expect(html.indexOf('data-testid="agent-composer-region"')).toBeGreaterThan(html.indexOf('data-testid="transcript-stream"'));
    expect(html).toContain("agent-conversation");
    expect(html).toContain('data-size="rail"');
  });
});

describe("one cluster", () => {
  it("places context above one surface containing the panel, field and send", () => {
    const html = hub({ pendingPermission: permission, onResolvePermission: noop });
    const stack = html.indexOf('data-testid="agent-composer-stack"');
    const order = [
      'data-testid="composer-tray-strip"',
      'data-testid="composer-surface"',
      'data-testid="composer-panel-area"',
      'data-testid="agent-permission-request"',
      'data-testid="agent-composer"',
      'data-testid="agent-composer-textarea"',
      'data-testid="composer-toolbar"',
      'data-testid="composer-send-button"',
    ].map((marker) => html.indexOf(marker, stack));
    expect(order.every((at) => at > stack)).toBe(true);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
    // One input surface carries the border and fill; context controls sit above it.
    const surface = html.indexOf('data-testid="composer-surface"');
    const clusterTag = html.slice(surface - 5, html.indexOf(">", surface));
    expect(clusterTag).toContain("border border-border-subtle");
    expect(clusterTag).toContain("rounded-2xl");
    expect(clusterTag).toContain("bg-surface-base");
    expect(html.match(/data-testid="agent-composer-stack"/g)).toHaveLength(1);
    const bar = html.slice(html.indexOf('data-testid="composer-tray-strip"'), html.indexOf('data-testid="composer-tray-strip"') + 300);
    expect(bar).not.toContain("rounded-xl");
    const field = html.slice(html.indexOf('data-testid="agent-composer"'), html.indexOf('data-testid="agent-composer"') + 300);
    expect(field).not.toContain("rounded-xl");
    expect(field).not.toContain("glass-float");
  });

  it("has no beam running round it while a turn runs; running is a small indicator", () => {
    const idle = hub();
    const running = hub({ streaming: true });
    expect(running).not.toContain("border-beam");
    expect(running).not.toContain("data-beam");
    expect(idle).not.toContain('data-testid="composer-running"');
    expect(running).toContain('data-testid="composer-running"');
    expect(running).toContain('aria-label="Agent is working"');
    expect(running).toContain("motion-reduce:animate-none");
  });

  it("puts the failure and error feedback in the region's own flow, above the cluster", () => {
    const html = hub({
      session: { ...session, status: "error" },
      errorMessage: "The agent process exited",
      failure: { id: "f", revision: 1, category: "x", severity: "error", title: "Usage limit", reason: "r", actions: ["retry"] },
      onFailureAction: noop,
    });
    const region = html.indexOf('data-testid="agent-composer-region"');
    const feedback = html.indexOf('data-testid="agent-composer-feedback"');
    expect(feedback).toBeGreaterThan(region);
    expect(html.indexOf('data-testid="agent-session-failure"')).toBeGreaterThan(feedback);
    expect(html.indexOf('data-testid="agent-session-error"')).toBeGreaterThan(html.indexOf('data-testid="agent-session-failure"'));
    expect(html.indexOf('data-testid="agent-composer-stack"')).toBeGreaterThan(html.indexOf('data-testid="agent-session-error"'));
    // Not floating cards: no absolute positioning, no opaque patch under them.
    expect(html).not.toContain("rounded-control bg-surface-base");
    // The field is still there: a failed session keeps its composer.
    expect(html).toContain('data-testid="agent-composer-textarea"');
  });

  it("caps the field, the panel and the extras so a short pane scrolls inside the cluster", () => {
    const html = hub({ pendingPermission: permission, onResolvePermission: noop, queuedPrompts: [{ id: "q", text: "later", attachments: [], images: [], refs: [] }] });
    expect(html).toContain("composer-editor-cap");
    expect(html).toContain("composer-panel-cap");
    expect(html).toMatch(/data-testid="composer-extras"[^>]*overflow-y-auto/);
    // Send never shrinks in the tools row below the field.
    expect(html).toMatch(/data-testid="composer-toolbar"/);
  });
});

describe("the context bar", () => {
  it("says the repository total and the number needing attention apart, with a sentence for both", () => {
    const html = hub();
    const chip = html.match(/<button[^>]*data-testid="tray-branch"[^>]*>[\s\S]*?<\/button>/)?.[0] ?? "";
    expect(chip).toContain("4 repositories");
    // 2 of the 4 (one changed, one conflicted): its own badge, danger because of the conflict.
    expect(chip).toMatch(/data-testid="tray-branch-badge"[^>]*data-tone="danger"/);
    expect(chip).toContain("2 of 4 repositories need attention (1 with conflicts, 1 with changes)");
    expect(chip).not.toMatch(/4 repos\s*2/);
    expect(chip).toContain('aria-label="4 repositories · 2 of 4 repositories need attention');
  });

  it("names the workspace without a bare number after it", () => {
    const html = hub();
    const chip = html.match(/<span[^>]*data-testid="tray-workspace"[^>]*>/)?.[0] ?? "";
    expect(html).toContain("billing");
    expect(html).not.toContain("billing (3)");
    expect(chip).toContain("billing · 3 folders");
  });

  it("keeps Tasks and Subagents visible and secondary actions in the menu", () => {
    const html = hub();
    expect(html).toContain('data-testid="composer-tray-menu"');
    expect(html).toContain('aria-label="More about this session"');
    expect(html).toContain('data-testid="tray-tasks"');
    expect(html).toContain('data-testid="tray-subagents"');
    expect(html).not.toContain('data-testid="tray-follow"');
    // No icons-only toggle any more: narrowing is the bar's own container query.
    expect(html).not.toContain('data-testid="composer-tray-compact"');
    expect(html).toContain("@container");
    // A draft has no session: nothing to follow or to list.
    const draft = hub({ session: null, draft: { projectPath: "/work/api" } });
    expect(draft).not.toContain('data-testid="tray-follow"');
  });

  it("shows a folded request as a named chip, with how many, ahead of the other chips", () => {
    // Both a permission and a question: the permission is open, the question waits as a chip.
    // (How many are queued behind the open one is read off the store, which server
    // rendering sees empty; the browser check covers "2 more waiting after this one".)
    const html = hub({ pendingPermission: permission, onResolvePermission: noop, pendingElicitation: question, onRespondElicitation: noop });
    expect(html).toContain('data-testid="agent-permission-request"');
    const chip = html.match(/<button[^>]*data-testid="tray-question"[^>]*>/)?.[0] ?? "";
    expect(chip).toContain('aria-label="The agent is waiting on a question — open it"');
    expect(chip).not.toContain("beat");
    // Permission/question chips come before the repository chip: attention first.
    expect(html.indexOf('data-testid="tray-question"')).toBeLessThan(html.indexOf('data-testid="tray-branch"'));
  });

  it("does not play an attention animation when it merely renders", () => {
    const html = hub({ pendingElicitation: question, onRespondElicitation: noop, pendingPermission: permission, onResolvePermission: noop });
    expect(html).not.toContain("agent-attention-pulse");
    expect(html).not.toContain("agent-attention-flash");
    expect(html).not.toContain("animate-pulse");
  });
});

describe("the tools row", () => {
  const props = {
    value: "",
    onChange: noop,
    onSend: noop,
    onCancel: noop,
    configOptions: [
      { id: "model", name: "Model", category: "model", currentValue: "sonnet", options: [{ value: "sonnet", name: "Sonnet 4.5" }, { value: "opus", name: "Opus" }] },
    ],
    onSetConfigOption: noop,
    mode: "acceptEdits",
    availableModes: [{ id: "default", name: "Ask" }, { id: "acceptEdits", name: "Auto" }],
    onSetMode: noop,
  };

  it("names the model and keeps permission mode as an accessible icon", () => {
    const html = render(<AgentComposer {...props} />);
    const model = html.match(/<button[^>]*data-testid="composer-config"[^>]*>.*?<\/button>/)?.[0] ?? "";
    const mode = html.match(/<button[^>]*data-testid="composer-mode"[^>]*>.*?<\/button>/)?.[0] ?? "";
    expect(model).toContain("Sonnet 4.5");
    expect(model).toContain("lucide-chevron-down");
    expect(mode).toContain('aria-label="Permission mode: Auto"');
    expect(mode).not.toContain(">Auto</span>");
    expect(mode).not.toContain("lucide-chevron-down");
    expect(mode).toContain("lucide-shield-check");
    expect(model).not.toContain("lucide-sliders-horizontal");
    // The mode glyph keeps its accessible name without a visible label.
    expect(mode).toContain('title="Permission mode: Auto"');
  });

  it("keeps slash commands, the goal and attaching behind the + menu, and the goal on the row once set", () => {
    const unset = render(<AgentComposer {...props} commands={[{ name: "compact", description: "x" }]} onSetGoal={noop} onBrowseContext={noop} />);
    expect(unset).toContain('data-testid="composer-menu-button"');
    expect(unset).toContain('aria-label="Attach, commands and more"');
    expect(unset).not.toContain('data-testid="composer-goal"');
    const set = render(<AgentComposer {...props} goal={{ objective: "a green build", status: "active" }} onSetGoal={noop} />);
    expect(set).toContain('data-testid="composer-goal"');
    expect(set).toContain("a green build");
  });

  it("offers the vendor choice only to a draft, and names an unknown vendor by its own id", () => {
    const draft = render(<AgentComposer {...props} agents={["claude", "mystery"]} agent="mystery" onAgentChange={noop} />);
    const picker = draft.match(/<button[^>]*data-testid="composer-agent"[^>]*>.*?<\/button>/)?.[0] ?? "";
    // Not Claude's row: an unknown vendor reads as itself, with the generic mark.
    expect(picker).toContain("Mystery");
    expect(picker).not.toContain("Claude");
    expect(picker).toContain('aria-label="Agent: Mystery"');
    expect(picker).toContain("lucide-bot");
    expect(render(<AgentComposer {...props} />)).not.toContain('data-testid="composer-agent"');
  });

  it("sends and stops from the same place, with the same names", () => {
    const idle = render(<AgentComposer {...props} />);
    expect(idle).toContain('aria-label="Send message"');
    const running = render(<AgentComposer {...props} streaming />);
    expect(running).toContain('aria-label="Stop generating"');
    expect(running.match(/data-testid="composer-send-button"/g)).toHaveLength(1);
  });

  it("is two menus on the left, so they fit beside the pickers", () => {
    const html = render(<AgentComposer value="" onChange={noop} onSend={noop} onCancel={noop} commands={[{ name: "compact", description: "x" }]} contextFiles={[{ path: "a.ts" }]} />);
    expect((html.match(/aria-haspopup/g) ?? []).length).toBe(2);
    expect(html).toContain('aria-label="Mention a file"');
  });
});
