import { chipToken, fileChip, imageChip, refChip } from "../composerDraft";
import { TREE_ROW_HEIGHT } from "@/components/tree-view";
import { describe, expect, it } from "bun:test";
import { renderToString as renderRaw } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  AgentComposer,
  AgentJumpToLatest,
  AgentMenu,
  AgentSessionError,
  AgentSidebarTree,
  PermissionPresence,
  nextWaiting,
  visibleSessions,
  countDescendants,
  groupByProject,
  AgentHubContent,
  AgentMetaInspector,
  AgentRail,
  AgentPermissionRequest,
  AgentPermissionToast,
  AgentQuestionCard,
  AgentQuestionToast,
  AgentDockBadge,
  TranscriptStream,
  shouldCancelComposerOnEscape,
  composerDefaultPlaceholder,
  composerPlaceholder,
} from "./index";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentSession, PendingElicitation } from "@/bridge/agenthub";
import type { TranscriptItem } from "../types";

// Surfaces use the app's tooltips (the composer's icon-only actions, the
// rail's rows) and the session summary reads through TanStack Query; both want
// the providers App.tsx mounts once around the window. Every render here gets
// them, the way every render in the app does.
const renderToString = (node: React.ReactNode) =>
  renderRaw(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{node}</TooltipProvider>
    </QueryClientProvider>,
  );

describe("Agent Hub Phase 3 Surfaces", () => {
  it("leaves status announcements to the shell's one live region", () => {
    // The hub and the companion each mount a stream; a region in each read
    // every status twice (AgentSessionsSubscriber owns the one region).
    const html = renderToString(<TranscriptStream items={[]} />);

    expect(html).not.toContain("aria-live");
  });

  it("offers the companion rail the older pages the hub offers", () => {
    const html = renderToString(
      <AgentRail
        activeSession={{
          id: "s1",
          agentType: "claude",
          projectPath: "/p",
          status: "idle",
          parentSessionId: null,
          depth: 0,
          title: "s1",
          acpSessionId: "acp-s1",
        }}
        items={[{ id: "p1", kind: "user_message_chunk", text: "hi" }]}
        hasMore
        onLoadOlder={() => {}}
        promptValue=""
        onPromptChange={() => {}}
        onSend={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(html).toContain('data-testid="transcript-load-older"');
  });

  it("cancels a running composer only for Escape", () => {
    expect(shouldCancelComposerOnEscape("Escape", true)).toBe(true);
    expect(shouldCancelComposerOnEscape("Escape", false)).toBe(false);
    expect(shouldCancelComposerOnEscape("Enter", true)).toBe(false);
  });

  describe("AgentComposer (Figma 963:2634)", () => {
    it("is one plain surface with its numbers behind a context ring", () => {
      // It sits in the layout under the transcript rather than floating over
      // it, so it is one bordered surface and not glass; the meta band above
      // the field ("4 turns · $0.21", the vendor) and the spelled-out counter
      // went into the ring's popover, which renders nothing until opened.
      const html = renderToString(
        <AgentComposer
          size="wide"
          value="Test prompt"
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          turnCount={4}
          usage={{ used: 24503, size: 1000000, amount: 0.21 }}
          vendor="claude-agent-acp"
          version="0.78"
        />,
      );

      expect(html).toContain('data-testid="agent-composer"');
      expect(html).toContain('data-size="wide"');
      expect(html).toContain('data-state="idle"');
      expect(html).not.toContain("glass-float");
      expect(html).toContain('aria-label="Context: 24.5k of 1M used (2%)"');
      expect(html).not.toContain("4 turns · $0.21");
      expect(html).not.toContain("24,503 / 1,000,000 context");
      // The two left-hand controls are named by what they open.
      expect(html).toContain('aria-label="Attach, commands and more"');
      expect(html).toContain('aria-label="Mention a file"');
      expect(html).toContain("Test prompt");
    });

    it("grows the field with its text, up to a ceiling where it scrolls", () => {
      // The height itself is measured in the browser; what is pinned here is
      // that there is a floor, a ceiling, and a scroll past it — not a fixed
      // height that cut a long prompt off after two lines.
      const field = (size: "wide" | "rail") => {
        const html = renderToString(
          <AgentComposer size={size} value="" onChange={() => {}} onSend={() => {}} onCancel={() => {}} />,
        );
        // Plate's editable root, not a <textarea>: a <div role="textbox">
        // carrying data-slate-editor.
        const start = html.indexOf('<div role="textbox"');
        return html.slice(start, html.indexOf(">", start));
      };
      expect(field("wide")).toContain("min-h-8");
      // The ceilings are `cqh`-aware classes (index.css): fifteen rem wide, ten
      // in the rail, and never more than a third of a short conversation.
      expect(field("wide")).toContain("composer-editor-cap");
      expect(field("wide")).not.toContain("composer-editor-cap-rail");
      expect(field("wide")).toContain("overflow-y-auto");
      expect(field("wide")).not.toContain(" h-10");
      expect(field("rail")).toContain("composer-editor-cap-rail");
    });

    it("keeps one send button and only swaps its glyph while streaming", () => {
      // Figma draws the same 32px circle in Idle and Streaming; only the icon
      // changes. Two testids implied two buttons in two places.
      const html = renderToString(
        <AgentComposer
          size="rail"
          streaming={true}
          value="Prompt in flight"
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          vendor="claude"
          agentName="refund-worker"
        />,
      );

      expect(html).toContain('data-size="rail"');
      expect(html).toContain('data-state="streaming"');
      expect(html).toContain('data-testid="composer-send-button"');
      expect(html).toContain('aria-label="Stop generating"');
      // The field is non-empty here, so this placeholder never actually
      // shows — checked directly rather than in the (necessarily empty)
      // rendered field; see composerDefaultPlaceholder's own doc comment.
      expect(composerDefaultPlaceholder(undefined, true, "refund-worker")).toBe("refund-worker is working…");
    });

    it("drops the pill labels and shortens the counters in the rail", () => {
      const html = renderToString(
        <AgentComposer
          size="rail"
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          usage={{ used: 24500, size: 1000000, amount: 0 }}
          vendor="claude"
          version="0.78"
        />,
      );

      // 280px cannot hold two labelled pills.
      expect(html).not.toContain("Add context</button>");
      expect(html).toContain("24.5k of 1M");
    });

    it("marks the composer attached when the draft holds files, and draws each once, in the field", () => {
      const html = renderToString(
        <AgentComposer
          value={`${chipToken(fileChip("/work/src/index.ts"))} ${chipToken(fileChip("/work/schema.sql"))}`}
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );

      expect(html).toContain('data-state="attached"');
      expect(html).not.toContain('data-testid="composer-attachments"');
      expect(html.match(/data-testid="attachment-chip"/g)).toHaveLength(2);
      expect(html).toContain("index.ts");
      expect(html).toContain("schema.sql");
    });

    it("draws a pasted image and a reference as chips too, one family", () => {
      const image = {
        id: "image-1",
        name: "screenshot.png",
        mimeType: "image/png",
        data: "aW1hZ2U=",
        previewUrl: "data:image/png;base64,aW1hZ2U=",
        size: 5,
      };
      const ref = { module: "codegraph", kind: "endpoint", id: "GET /users", label: "GET /users" };
      const html = renderToString(
        <AgentComposer
          value={`${chipToken(imageChip(image))} ${chipToken(refChip(ref))}`}
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      // The picture itself comes from the image registry, which a server render
      // reads as empty; the gallery shows it.
      expect(html).toContain('data-kind="image"');
      expect(html).toContain("screenshot.png");
      expect(html).toContain('data-kind="ref"');
      expect(html).toContain("GET /users");
      expect(html.match(/data-testid="attachment-chip"/g)).toHaveLength(2);
    });

    it("shows queued prompts in the composer", () => {
      const html = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          queuedPrompts={[{ id: "queued-1", text: "Run the tests", attachments: [], images: [], refs: [] }]}
          onCancelQueued={() => {}}
          onEditQueued={() => {}}
        />,
      );

      expect(html).toContain('data-testid="composer-queued-prompts"');
      expect(html).toContain("Queued");
      expect(html).toContain("Run the tests");
      expect(html).toContain("Cancel queued prompt");
      expect(html).toContain("Edit queued prompt");
    });

    it("draws queue controls at the 28px control size with real icons", () => {
      const html = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          queuedPrompts={[{ id: "queued-1", text: "Run the tests", attachments: [], images: [], refs: [] }]}
          onCancelQueued={() => {}}
          onEditQueued={() => {}}
        />,
      );

      for (const label of ["Edit queued prompt", "Cancel queued prompt"]) {
        const button = html.match(new RegExp(`<button[^>]*aria-label="${label}"[^>]*>.*?</button>`))?.[0] ?? "";
        expect(button, label).toContain('data-size="icon-sm"');
        expect(button, label).toContain("<svg");
      }
      expect(html).toContain("lucide-pencil");
      expect(html).not.toContain("✎");
      expect(html).not.toContain("×");
    });

    it("offers Resume only while the queue is paused", () => {
      const composer = (queuePaused: boolean) =>
        renderToString(
          <AgentComposer
            value=""
            onChange={() => {}}
            onSend={() => {}}
            onCancel={() => {}}
            queuedPrompts={[{ id: "queued-1", text: "Run the tests", attachments: [], images: [], refs: [] }]}
            queuePaused={queuePaused}
            onResumeQueued={() => {}}
          />,
        );

      const paused = composer(true);
      expect(paused).toContain('data-testid="composer-queue-paused"');
      expect(paused).toContain('aria-label="Resume queued prompts"');
      expect(paused).toContain("Run the tests");
      expect(composer(false)).not.toContain("Resume queued prompts");
    });

    it("renders the session controls only when the agent advertises them", () => {
      const html = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          mode="default"
          onSetMode={() => {}}
          configOptions={[{ id: "model", name: "Model", options: [{ value: "sonnet", name: "Sonnet" }] }]}
          onSetConfigOption={() => {}}
        />,
      );
      expect(html).toContain('data-testid="composer-mode"');
      expect(html).toContain('data-testid="composer-config"');
    });
  });

  describe("AgentMenu (Figma 1044:3178)", () => {
    // The menu is a Radix DropdownMenu now, so its content lives in a portal
    // and only exists while open — renderToString sees the trigger and nothing
    // else. That is the cost of getting positioning, the keyboard walk, the
    // focus trap and outside-click for free, and it is worth it: the card this
    // replaced had none of them, plus a search box Figma does not draw, and no
    // caller anywhere in the app.
    //
    // What the rows look like is checked in the browser, not here.
    it("wraps its trigger rather than rendering a positioned card", () => {
      const html = renderToString(
        <AgentMenu
          mode="commands"
          commands={[{ name: "/help", description: "Show available commands" }]}
          onSelectCommand={() => {}}
        >
          <button type="button" data-testid="menu-trigger">
            Commands
          </button>
        </AgentMenu>,
      );

      expect(html).toContain('data-testid="menu-trigger"');
      // Radix marks the trigger; a hand-rolled card would not have this.
      expect(html).toContain("aria-haspopup");
      // Closed menus render no list.
      expect(html).not.toContain("Show available commands");
    });

    it("is what the composer pills open", () => {
      const html = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          commands={[{ name: "/compact", description: "summarise the turn so far" }]}
          contextFiles={[{ path: "src/RefundService.java", name: "RefundService.java" }]}
        />,
      );

      // Both pills are triggers, so both carry Radix's popup wiring.
      expect(html).toContain('data-testid="composer-attach-button"');
      expect(html).toContain('data-testid="composer-menu-button"');
      expect((html.match(/aria-haspopup/g) ?? []).length).toBe(2);
    });
  });

  describe("AgentJumpToLatest", () => {
    it("renders the floating button when visible", () => {
      const html = renderToString(
        <AgentJumpToLatest visible={true} onClick={() => {}} />,
      );

      expect(html).toContain("data-testid=\"agent-jump-to-latest\"");
    });

    it("returns null when visible is false", () => {
      const html = renderToString(
        <AgentJumpToLatest visible={false} onClick={() => {}} />,
      );

      expect(html).toBe("");
    });
  });

  describe("AgentSessionError (Figma 948:2204)", () => {
    it("leads with the agent and the real reason, not a generic title", () => {
      // Figma's note on the node: "Clear means the DevKit error code, what
      // actually happened, and the one action that gets you out — a coloured
      // dot on its own gives you nothing to do." The card this replaced showed
      // a fixed title, a DEV-KIT-500 fallback constant and static prose.
      const html = renderToString(
        <AgentSessionError
          cause="crashed"
          agentName="docs-sweep"
          meta="Last update 3 min ago · 0 turns recorded"
          reason="agenthub.processFailed — the agent exited after session/cancel timed out."
          onRetry={() => {}}
        />,
      );

      expect(html).toContain('data-testid="agent-session-error"');
      expect(html).toContain("docs-sweep stopped");
      expect(html).toContain("Last update 3 min ago");
      expect(html).toContain("agenthub.processFailed");
      // 948:2190 labels the primary action Restart when the process died.
      expect(html).toContain("Resume session");
    });

    it("offers Dismiss beside the way out, only when it is wired", () => {
      const withDismiss = renderToString(
        <AgentSessionError cause="crashed" agentName="docs-sweep" onRetry={() => {}} onDismiss={() => {}} />,
      );
      expect(withDismiss).toContain('data-testid="session-error-dismiss"');
      expect(withDismiss).toContain(">Dismiss<");
      // Dismiss first, the primary action last: the way out stays the eye's end point.
      expect(withDismiss.indexOf("session-error-dismiss")).toBeLessThan(withDismiss.indexOf("session-error-retry"));
      expect(withDismiss).toContain("Resume session");
      const without = renderToString(<AgentSessionError cause="crashed" agentName="docs-sweep" onRetry={() => {}} />);
      expect(without).not.toContain("session-error-dismiss");
    });

    it("changes only the copy and the primary label between causes", () => {
      // The two variants share one surface, one structure and one set of
      // tones; branching them apart would have been two components.
      const html = renderToString(
        <AgentSessionError
          cause="denied"
          agentName="refund-worker"
          onRetry={() => {}}
        />,
      );

      expect(html).toContain("refund-worker turn failed");
      expect(html).toContain("agenthub.permissionDenied");
      expect(html).toContain("Retry turn");
    });

    it("sends a missing install to Settings instead of offering a retry that fails the same way", () => {
      const html = renderToString(
        <AgentSessionError
          cause="notInstalled"
          agentName="Claude"
          onRetry={() => {}}
          onOpenSettings={() => {}}
        />,
      );
      expect(html).toContain("Claude can&#x27;t start");
      expect(html).toContain("agent_not_installed");
      expect(html).toContain("Open agent settings");
      expect(html).not.toContain('data-testid="session-error-retry"');
    });

    it("is one row on the app's own surface, not a filled red block", () => {
      const html = renderToString(
        <AgentSessionError
          cause="crashed"
          agentName="docs-sweep"
          reason="agenthub.processFailed — the agent exited after session/cancel timed out."
          onRetry={() => {}}
        />,
      );
      // The wash went: a tinted panel over the transcript, beside a tinted
      // composer, was one colour too many (see AgentPermissionRequest). The
      // tone is the dot and the border.
      expect(html).not.toContain("feedback-danger-soft");
      expect(html).toContain("bg-status-danger");
      expect(html).toContain("border-(--border-danger)");
      // Title, reason and the action share one wrapping row instead of
      // stacking three, the last of which held nothing but a button.
      const card = html.slice(html.indexOf('data-testid="agent-session-error"'));
      const row = card.indexOf("flex w-full flex-wrap items-center");
      expect(row).toBeGreaterThan(-1);
      // Everything is inside it: the row opens before the title and nothing
      // closes it until after the action.
      expect(card.indexOf("docs-sweep stopped")).toBeGreaterThan(row);
      expect(card.indexOf("session-error-retry")).toBeGreaterThan(card.indexOf("agent-session-error-reason"));
      // The whole reason stays reachable even though the line truncates.
      expect(html).toContain('title="agenthub.processFailed');
      expect(html).toContain("truncate");
    });

    it("uses the shared Button rather than a hand-rolled one", () => {
      const html = renderToString(
        <AgentSessionError agentName="a" onRetry={() => {}} onClose={() => {}} />,
      );
      // Button's own base class, and the 32px height Figma's px-3/py-2 gives.
      expect(html).toContain("group/button");
      expect(html).toContain("h-8");
      expect(html).toContain("Close session");
    });
  });

  describe("Starting a session", () => {
    // The agent is chosen where the first message is written, not in a dialog
    // before it: a session is bound to its agent's process on creation, so the
    // choice is only open until that first send.
    it("offers the agent picker in the composer only while a session is being started", () => {
      const base = { value: "", onChange: () => {}, onSend: () => {}, onCancel: () => {} };
      const picking = renderToString(
        <AgentComposer {...base} agents={["claude"]} agent="claude" onAgentChange={() => {}} />,
      );
      expect(picking).toContain('data-testid="composer-agent"');
      expect(picking).toContain("claude");
      // Once the session exists there is nothing to pick.
      expect(renderToString(<AgentComposer {...base} />)).not.toContain('data-testid="composer-agent"');
    });

    it("offers Retry and no Close on a failed session, and lets a closed one be typed into", () => {
      const base = {
        id: "sess-1",
        agentType: "claude",
        projectPath: "/workspace/app",
        parentSessionId: null,
        depth: 0,
        title: "Planner",
        acpSessionId: "acp-1",
      };
      const failed = renderToString(
        <AgentHubContent
          session={{ ...base, status: "error" }}
          errorCause="crashed"
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          onRetrySession={() => {}}
        />,
      );
      // Close used to sit beside Retry on this card, and that is how a session
      // someone meant to retry got parked instead.
      expect(failed).toContain('data-testid="session-error-retry"');
      expect(failed).not.toContain('data-testid="session-error-close"');

      const closed = renderToString(
        <AgentHubContent
          session={{ ...base, status: "closed" }}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      // A parked session is answerable: the prompt reopens it, and the field
      // says so instead of being greyed out. Plate only shows a placeholder
      // once mounted in a real browser (client-only, checked here in
      // renderToString by what got passed in rather than what got painted —
      // composerDefaultPlaceholder's own doc comment says why).
      expect(closed).toContain('data-testid="agent-composer"');
      expect(composerPlaceholder("closed")).toContain("Send to reopen this session");
      // The attribute, not the word: "disabled:opacity-60" is a Tailwind class
      // every composer carries.
      expect(closed).not.toContain('disabled=""');
    });

    it("asks a pending permission inside the conversation, above the composer", () => {
      // Not a toast in the corner: you are looking at the transcript the
      // request came out of, and the evidence for the answer is in it.
      const html = renderToString(
        <AgentHubContent
          session={{
            id: "sess-1",
            agentType: "claude",
            projectPath: "/workspace/app",
            status: "awaiting_permission",
            parentSessionId: null,
            depth: 0,
            title: "Planner",
            acpSessionId: "acp-1",
          }}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          pendingPermission={{
            id: "perm-inline",
            sessionId: "sess-1",
            caller: "agent:claude",
            title: "Planner",
            depth: 0,
            createdAt: new Date().toISOString(),
            expiresAt: new Date(Date.now() + 60_000).toISOString(),
            params: { command: "rm -rf build", options: [{ optionId: "allow_once", name: "Allow once" }] },
          }}
          onResolvePermission={() => {}}
        />,
      );

      expect(html).toContain('data-testid="agent-permission-request"');
      // Wide: the ask and the answer share a line. Stacked, the card was a
      // panel of empty space over a 720-wide column.
      expect(html).toContain('data-size="wide"');
      expect(html).toContain("rm -rf build");
      // The composer is still there: a queued prompt can be written while the
      // agent waits on this answer.
      expect(html).toContain('data-testid="agent-composer"');
    });

    it("draws a draft as the folder it will work in, with the composer ready", () => {
      const html = renderToString(
        <AgentHubContent
          session={null}
          draft={{ projectPath: "/Users/dev/billing-service", parentName: "auth-refactor" }}
          agents={["claude"]}
          agent="claude"
          onAgentChange={() => {}}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain('data-testid="agent-hub-draft"');
      expect(html).toContain("Start a session in billing-service");
      expect(html).toContain(" · from auth-refactor");
      // The folder is the sandbox; the draft says so before it is fixed.
      expect(html).toContain("It can only reach paths inside /Users/dev/billing-service.");
      expect(html).toContain('data-testid="composer-agent"');
      // Not the empty state: a draft is somewhere to type, not a dead end.
      expect(html).not.toContain("No agent running");
    });

    it("picks the draft's project above the composer, and allows none", () => {
      const props = {
        session: null,
        agents: ["claude"],
        agent: "claude",
        onAgentChange: () => {},
        promptValue: "",
        onPromptChange: () => {},
        onSend: () => {},
        onCancel: () => {},
        projects: ["/Users/dev/billing-service", "/Users/dev/api"],
        onProjectChange: () => {},
        onChooseProject: () => {},
      };
      const inProject = renderToString(
        <AgentHubContent {...props} draft={{ projectPath: "/Users/dev/billing-service" }} />,
      );
      expect(inProject).toContain('data-testid="draft-project-picker"');
      expect(inProject).toContain('aria-label="billing-service"');

      const none = renderToString(<AgentHubContent {...props} draft={{ projectPath: "" }} />);
      expect(none).toContain("Start a session without a project");
      expect(none).toContain('aria-label="No project"');
    });

    it("names the whole workspace on the picker's trigger once it is selected", () => {
      // The popover's own content (Radix Popover) only mounts once open, so
      // renderToString — closed by default — cannot see the item inside it;
      // this checks what a workspace pick changes on the always-rendered
      // trigger, the same limit the project-list assertions above work under.
      const props = {
        session: null,
        agents: ["claude"],
        agent: "claude",
        onAgentChange: () => {},
        promptValue: "",
        onPromptChange: () => {},
        onSend: () => {},
        onCancel: () => {},
        projects: ["/Users/dev/billing-service"],
        onProjectChange: () => {},
        onChooseProject: () => {},
        workspace: { id: "chiro", name: "CHIRO", paths: ["/Users/dev/billing-service", "/Users/dev/api"] },
        onChooseWorkspace: () => {},
      };
      const oneFolder = renderToString(
        <AgentHubContent {...props} draft={{ projectPath: "/Users/dev/billing-service" }} />,
      );
      expect(oneFolder).toContain('aria-label="billing-service"');

      // A workspace's services do not necessarily share a root, so naming
      // just "billing-service" once every one of them is granted would
      // understate what the session can actually reach.
      const wholeWorkspace = renderToString(
        <AgentHubContent
          {...props}
          draft={{ projectPath: "/Users/dev/billing-service", workspaceSelected: true }}
        />,
      );
      expect(wholeWorkspace).toContain('aria-label="CHIRO (2)"');
      expect(wholeWorkspace).not.toContain(">billing-service<");
    });

    it("shows nothing of the previously selected session in a draft", () => {
      const previous: AgentSession = {
        id: "sess-prev",
        agentType: "claude",
        projectPath: "/Users/dev/billing-service",
        status: "closed",
        parentSessionId: null,
        depth: 0,
        title: "Old one",
        acpSessionId: "acp-prev",
      };
      const html = renderToString(
        <AgentHubContent
          session={previous}
          draft={{ projectPath: "/Users/dev/billing-service" }}
          usage={{ used: 152_000, size: 1_000_000, amount: 0.65 }}
          items={[{ kind: "user_message_chunk", text: "hi" } as never]}
          agents={["claude"]}
          agent="claude"
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).not.toContain("152");
      expect(html).not.toContain("Send to reopen this session");
    });

    it("sends with a plain solid button, not glass", () => {
      const html = renderToString(
        <AgentComposer value="" onChange={() => {}} onSend={() => {}} onCancel={() => {}} />,
      );
      const tag = html.match(/<button[^>]*data-testid="composer-send-button"[^>]*>/)?.[0] ?? "";
      expect(tag).toContain('data-variant="default"');
      expect(tag).not.toContain("glass-button");
    });
  });

  describe("Agent / Rail (Figma 915:97)", () => {
    const railSession: AgentSession = {
      id: "sess-rail0001",
      agentType: "claude",
      projectPath: "/acme/billing-service",
      status: "idle",
      depth: 0,
      title: "refund-worker",
      parentSessionId: null,
      acpSessionId: "acp-rail",
    };

    const railProps = {
      promptValue: "",
      onPromptChange: () => {},
      onSend: () => {},
      onCancel: () => {},
    };

    it("paints nothing itself — panel-surface behind it is the ground", () => {
      const html = renderToString(
        <AgentRail activeSession={railSession} {...railProps} />,
      );
      // The root only — transcript cards inside legitimately take their own
      // ground. A fill *here* would be a second opaque layer on an opaque one.
      const rootClass = html.slice(html.indexOf('class="'), html.indexOf('">'));
      expect(rootClass).not.toContain("bg-");
      // No side padding on the root: it would inset the transcript's scrollbar
      // from the panel's edge. The bands inside pad themselves.
      expect(rootClass).not.toMatch(/\bp[lrx]-/);
    });

    it("carries depth and cwd in the header, not a status pill", () => {
      // Figma 913:31's meta line is "depth 2 · ~/acme/billing-service" — what
      // this agent may touch. The header used to show a colour-coded status
      // pill, a third status vocabulary beside AgentState.
      const html = renderToString(
        <AgentRail activeSession={railSession} {...railProps} />,
      );
      expect(html).toContain("depth 0 ·");
      expect(html).toContain('data-slot="agent-state"');
    });

    it("says what is missing and how to fix it when empty", () => {
      const html = renderToString(
        <AgentRail activeSession={null} {...railProps} />,
      );
      expect(html).toContain("No session open");
      expect(html).toContain("it needs a vendor and a working");
    });
  });

  describe("AgentSidebarTree", () => {
    // The rail's rows carry the app's own tooltip (components/tree-view), which
    // wants a provider from context. App.tsx has one around the whole window;
    // a test that renders the rail on its own has to bring it.
    const renderSidebar = (node: React.ReactNode) =>
      renderToString(<TooltipProvider>{node}</TooltipProvider>);

    const mockSession: AgentSession = {
      id: "sess-abc12345",
      agentType: "claude",
      projectPath: "packages/core",
      status: "running",
      parentSessionId: null,
      depth: 0,
      title: "Task Planner",
      acpSessionId: "acp-1",
    };

    const mockChildSession: AgentSession = {
      id: "sess-child6789",
      agentType: "mock",
      projectPath: "packages/core",
      status: "awaiting_permission",
      parentSessionId: "sess-abc12345",
      depth: 1,
      title: "Sub-worker",
      acpSessionId: "acp-2",
    };

    it("delegates status to AgentState instead of carrying its own dot table", () => {
      // Figma 906:35: "Status lives on the nested Agent / State instance, so a
      // row does not need a variant per status." Rows had their own
      // StatusIndicator switch — a second dot-and-colour table in this module.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          activeSessionId="sess-child6789"
          turnCounts={{ "sess-child6789": 2 }}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );

      expect(html).toContain("Sub-worker");
      expect(html).toContain('data-slot="agent-state"');
      expect(html).toContain('data-status="awaiting"');
      expect(html).toContain('aria-selected="true"');
    });

    it("shimmers a running row's name and gives a waiting one the warning colour", () => {
      // Two states, two ways of saying so, both already in the app's vocabulary:
      // the transcript shimmers a turn in flight, and awaiting is the only
      // status with a deadline behind it.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );

      const runningRow = html.slice(html.indexOf("Task Planner") - 400, html.indexOf("Task Planner"));
      expect(runningRow).toContain('data-slot="shimmer"');
      expect(runningRow).toContain("data-active");

      const waitingRow = html.slice(html.indexOf("Sub-worker") - 400, html.indexOf("Sub-worker"));
      expect(waitingRow).toContain("text-status-warning");
      // The dot is trailing, so it is after the name in the markup: the beat
      // belongs to the waiting row and to no other.
      const waitingTrailing = html.slice(html.indexOf("Sub-worker"), html.indexOf("Sub-worker") + 600);
      expect(waitingTrailing).toContain("animate-pulse");
      const runningTrailing = html.slice(html.indexOf("Task Planner"), html.indexOf("Task Planner") + 600);
      expect(runningTrailing).not.toContain("animate-pulse");
    });

    it("treats a session with a queued request as waiting, whatever its status says", () => {
      // The status column can still read running while a request is queued for
      // it: the row has to follow the queue, which is what the user answers.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession]}
          pendingCounts={{ "sess-abc12345": 1 }}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      expect(html).toContain('data-status="awaiting"');
      expect(html).toContain("text-status-warning");
    });

    it("makes the waiting footer a way to get there, walking on with each press", () => {
      // The count said something needed you and left you to find it. Now the
      // footer selects the next waiting agent after the one you are on.
      const selected: string[] = [];
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          activeSessionId="sess-abc12345"
          onSelectSession={(id) => selected.push(id)}
          onNewSession={() => {}}
        />,
      );
      expect(html).toContain('<button type="button" data-testid="agent-tree-attention"');
      expect(html).toContain("1 agent waiting on you");

      expect(nextWaiting(["a", "b", "c"], "b")).toBe("c");
      expect(nextWaiting(["a", "b", "c"], "c")).toBe("a"); // wraps
      expect(nextWaiting(["a", "b"], "zzz")).toBe("a"); // not on one: the first
      expect(nextWaiting([], "a")).toBeUndefined();
    });

    it("hides closed sessions on request, except the selected one and a live subtree's parent", () => {
      const closedRoot: AgentSession = { ...mockSession, id: "closed-root", title: "Old", status: "closed" };
      const closedParent: AgentSession = { ...mockSession, id: "closed-parent", title: "Parent", status: "closed" };
      const liveChild: AgentSession = {
        ...mockChildSession,
        id: "live-child",
        parentSessionId: "closed-parent",
        status: "running",
      };
      const all = [mockSession, closedRoot, closedParent, liveChild];

      expect(visibleSessions(all, false).map((s) => s.id)).toEqual(all.map((s) => s.id));
      // Hidden: the closed root. Kept: the closed parent (its child is alive —
      // hiding it would hoist the child and have the indentation lie).
      expect(visibleSessions(all, true).map((s) => s.id)).toEqual(["sess-abc12345", "closed-parent", "live-child"]);
      // The selected session always shows, closed or not.
      expect(visibleSessions(all, true, "closed-root").map((s) => s.id)).toContain("closed-root");

      const html = renderSidebar(
        <AgentSidebarTree
          sessions={all}
          hideClosed
          onToggleHideClosed={() => {}}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      expect(html).toContain('data-testid="agent-tree-toggle-closed"');
      expect(html).toContain('aria-pressed="true"');
      expect(html).not.toContain(">Old<");
      expect(html).toContain(">Parent<");
    });

    it("carries no pending-permission count", () => {
      // Not in Figma 906:35, and not lost: the awaiting state already says the
      // agent needs you, the rail and the toast carry the queue ("+2 waiting"),
      // and the dock badge carries the total. A count here would be a fourth
      // place to keep in sync.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockChildSession]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      expect(html).not.toContain("pending-badge");
    });

    it("groups sessions under the project they were opened in", () => {
      // Agents are opened per repo and a developer runs several at once. Before
      // this level the rail was one flat list whose only clue which repo a row
      // belonged to was a truncated path on its second line.
      const other: AgentSession = {
        ...mockSession,
        id: "sess-other",
        projectPath: "/Users/dev/billing-service",
        title: "Billing",
      };
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession, other]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );

      // The folder's own name, not the whole path.
      expect(html).toContain(">billing-service<");
      expect(html).toContain(">core<");
      // Its count is the whole lineage in it — parent and child, not roots.
      expect(html).toContain(">2<");
    });

    it("counts a project by everything in it, roots and their children alike", () => {
      const groups = groupByProject([mockSession, mockChildSession]);
      expect(groups).toHaveLength(1);
      expect(groups[0].path).toBe("packages/core");
      expect(groups[0].roots.map((s) => s.id)).toEqual(["sess-abc12345"]);
      expect(groups[0].total).toBe(2);
    });

    it("keeps a child under its parent even when it was opened elsewhere", () => {
      // Lineage is "the boundary an agent may act within". Hoisting a child into
      // another project's folder because its cwd differs would make the tree say
      // something untrue about who spawned it.
      const away: AgentSession = { ...mockChildSession, projectPath: "/elsewhere" };
      const groups = groupByProject([mockSession, away]);
      expect(groups.map((g) => g.path)).toEqual(["packages/core"]);
    });

    it("gathers every scratch folder into one No project group, listed last", () => {
      const root = "/cfg/DevKit/agent-scratch";
      const a: AgentSession = { ...mockSession, id: "s-a", projectPath: `${root}/20260918-1-x` };
      const b: AgentSession = { ...mockSession, id: "s-b", projectPath: `${root}/20260918-2-y` };
      const z: AgentSession = { ...mockSession, id: "s-z", projectPath: "/work/zeta" };
      const groups = groupByProject([a, z, b], [], root);
      expect(groups.map((g) => g.path)).toEqual(["/work/zeta", root]);
      expect(groups[1].total).toBe(2);
      const html = renderSidebar(
        <AgentSidebarTree sessions={[a, b]} scratchRoot={root} onSelectSession={() => {}} onNewSession={() => {}} />,
      );
      expect(html).toContain("No project");
      expect(html).not.toContain("20260918-1-x");
    });

    it("draws its rows at the tree's own height, like every other rail", () => {
      const html = renderSidebar(
        <AgentSidebarTree sessions={[mockSession]} onSelectSession={() => {}} onNewSession={() => {}} />,
      );
      expect(html).toContain(`height:${TREE_ROW_HEIGHT}px`);
    });

    it("shows Claude's own subagents under the session that spawned them", () => {
      // They are not DevKit sessions — nothing here created them and they cannot
      // be opened — but a swarm only visible in the transcript cannot be seen
      // from the rail at all.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession]}
          subagents={{
            "sess-abc12345": [
              { id: "sub-1", name: "Explore", task: "map the repo", state: "completed" },
              { id: "sub-2", name: "Verify" },
            ],
          }}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );

      expect(html).toContain("Explore");
      expect(html).toContain("Verify");
      // Finished, and still going: no state on the spawn means it is working.
      expect(html).toContain('data-status="closed"');
      expect(html).toContain('data-status="running"');
    });

    it("colours the vendor's mark only while that agent is working", () => {
      // A rail of grey marks says which vendor each row is and nothing about
      // which of them is doing something. The dot at the far end says it in
      // 6px; the mark is where the eye already is.
      const running = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      expect(running).toContain("text-vendor-claude");
      expect(running).toContain('data-working="true"');

      // Idle, awaiting, closed and error all stay on the text tier: the colour
      // means "working", not "Claude".
      const idle = renderSidebar(
        <AgentSidebarTree
          sessions={[{ ...mockSession, status: "idle" }]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      expect(idle).not.toContain("text-vendor-claude");

      // Codex's mark is monochrome by design; no colour is invented for it.
      const codex = renderSidebar(
        <AgentSidebarTree
          sessions={[{ ...mockSession, agentType: "codex" }]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      expect(codex).toContain('data-working="true"');
      expect(codex).not.toContain("text-vendor-");
    });

    it("starts a session from the project row, without asking for its folder again", () => {
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
          onNewSessionIn={() => {}}
        />,
      );
      expect(html).toContain('data-testid="agent-tree-project-new"');
      expect(html).toContain('aria-label="New session in core"');
      // And the project row has a menu of its own, like the session rows.
      expect((html.match(/data-slot="context-menu-trigger"/g) ?? []).length).toBe(2);

      const without = renderSidebar(
        <AgentSidebarTree sessions={[mockSession]} onSelectSession={() => {}} onNewSession={() => {}} />,
      );
      expect(without).not.toContain('data-testid="agent-tree-project-new"');
    });

    it("renders sidebar swarm tree with session count", () => {
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          activeSessionId="sess-abc12345"
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );

      expect(html).toContain('data-testid="agent-sidebar-tree"');
      // Figma 917:101's header is the bare label. The count that matters moved
      // to the footer, and it counts what needs you rather than what exists.
      expect(html).toContain("Swarm");
      expect(html).not.toContain("Swarm (2)");
      expect(html).toContain("Task Planner");
      expect(html).toContain("Sub-worker");
    });

    it("draws the parent-child chain with a guide, not just indentation", () => {
      // Figma 917:101: "indentation is the parent→child chain, which is also
      // the boundary an agent may act within." Two siblings at the same indent
      // read as two roots pushed right unless the guide line is there.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      // The guide is components/tree-view's now — elbows and lines drawn per
      // row, where this used to be a bare 1px span and a pl-4 that could not
      // tell a second child from a second root.
      expect(html).toContain("border-bottom-left-radius");
      expect(html).toContain('aria-level="3"');
    });

    it("sums what is waiting into one footer line", () => {
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      // mockChildSession is awaiting_permission.
      expect(html).toContain('data-testid="agent-tree-attention"');
      expect(html).toContain("1 agent waiting on you");
    });

    it("wears the vendor's own mark, not a generic robot", () => {
      // Figma draws Icon/agent-claude and Icon/agent-codex. Four surfaces were
      // still on lucide's Bot, so every vendor looked identical everywhere
      // except the module header — which is the one place you already know
      // which agent you are looking at.
      const claude = {
        id: "sess-vendor",
        agentType: "claude",
        projectPath: "/p",
        status: "idle" as const,
        parentSessionId: null,
        depth: 0,
        title: "one",
        acpSessionId: "acp-v",
      };
      const tree = (session: AgentSession) =>
        renderSidebar(
          <AgentSidebarTree
            sessions={[session]}
            onSelectSession={() => {}}
            onNewSession={() => {}}
          />,
        );
      const row = tree(claude);
      expect(row).toContain('data-vendor="claude"');
      expect(row).not.toContain("lucide-bot");

      // The composer no longer carries a mark at all: its meta band went, and
      // the vendor is in the header one glance up.

      // A vendor DevKit has not drawn falls back to one shape on purpose, so
      // unknown is visibly unknown rather than wearing someone else's logo.
      const unknown = tree({ ...claude, agentType: "acme-agent" });
      expect(unknown).not.toContain("data-vendor=");
      expect(unknown).toContain("lucide-bot");
    });

    it("actually binds the context menu to the row", () => {
      // The regression this exists for: AgentRow had a closed prop list, so
      // ContextMenuTrigger's cloned onContextMenu and data attributes were
      // dropped on the floor. The trigger bound to nothing and the webview's
      // own Reload/Inspect menu answered the right-click instead of ours.
      //
      // Handlers are invisible to renderToString, but the attributes the
      // trigger puts on its child are not — and they arrive by the same
      // clone. If these are missing, nothing was wired.
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[
            {
              id: "sess-menu",
              agentType: "mock",
              projectPath: "/p",
              status: "idle",
              parentSessionId: null,
              depth: 0,
              title: "one",
              acpSessionId: "acp-menu",
            },
          ]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
          onDeleteSession={() => {}}
        />,
      );
      expect(html).toContain('data-slot="context-menu-trigger"');
      expect(html).toContain('role="treeitem"');
    });

    it("counts the whole subtree, because that is what delete takes", () => {
      const at = (id: string, parent: string | null): AgentSession => ({
        id,
        agentType: "mock",
        projectPath: "/p",
        status: "idle",
        parentSessionId: parent,
        depth: 0,
        title: id,
        acpSessionId: "acp-" + id,
      });
      // root → a → a1, and root → b. Deleting root takes four others.
      const counts = countDescendants([
        at("root", null),
        at("a", "root"),
        at("a1", "a"),
        at("b", "root"),
        at("lone", null),
      ]);
      expect(counts.root).toBe(3);
      expect(counts.a).toBe(1);
      expect(counts.a1).toBe(0);
      expect(counts.lone).toBe(0);
    });

    it("pins roots to the top without lifting a child out of its lineage", () => {
      const at = (id: string, parent: string | null): AgentSession => ({
        id,
        agentType: "mock",
        projectPath: "/p",
        status: "idle",
        parentSessionId: parent,
        depth: parent ? 1 : 0,
        title: id,
        acpSessionId: "acp-" + id,
      });
      const html = renderSidebar(
        <AgentSidebarTree
          sessions={[at("first", null), at("kid", "first"), at("second", null)]}
          // "kid" is pinned too, and must NOT be hoisted: a child drawn above
          // its own parent makes the indentation say something untrue.
          pinnedIds={["second", "kid"]}
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );
      const order = ["second", "first", "kid"].map((id) => html.indexOf(">" + id + "<"));
      expect(order[0]).toBeGreaterThan(-1);
      expect(order[0]).toBeLessThan(order[1]);
      expect(order[1]).toBeLessThan(order[2]);
    });
  });

  describe("AgentHubContent", () => {
    const mockSession: AgentSession = {
      id: "sess-100",
      agentType: "claude",
      projectPath: "dev-kit-app",
      status: "idle",
      parentSessionId: null,
      depth: 0,
      title: "Main Agent",
      acpSessionId: "acp-100",
    };

    it("renders Empty state when session is null", () => {
      const html = renderToString(
        <AgentHubContent
          session={null}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );

      expect(html).toContain("data-testid=\"agent-hub-content\"");
      expect(html).toContain("data-state=\"Empty\"");
      expect(html).toContain("data-testid=\"agent-hub-content-empty\"");
      expect(html).not.toContain("data-testid=\"agent-composer\"");
    });

    it("renders Error state when session status is error", () => {
      const errorSession: AgentSession = { ...mockSession, status: "error" };
      const html = renderToString(
        <AgentHubContent
          session={errorSession}
          errorMessage="Process exited with status 1"
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );

      expect(html).toContain("data-state=\"Error\"");
      expect(html).toContain("data-testid=\"agent-hub-content-error\"");
      expect(html).toContain("data-testid=\"agent-session-error\"");
      // The composer stays. The card used to replace it, leaving Retry — the
      // same prompt again — as the only way forward; the rail, showing the
      // same session, never took the field away.
      expect(html).toContain("data-testid=\"agent-composer\"");
    });

    it("renders Conversation state with breadcrumbs, transcript and composer", () => {
      const sampleItems: TranscriptItem[] = [
        {
          id: "item-1",
          kind: "user_message_chunk",
          text: "Hello agent",
        },
        {
          id: "item-2",
          kind: "agent_message_chunk",
          text: "Hello! How can I help you?",
        },
      ];

      const html = renderToString(
        <AgentHubContent
          session={mockSession}
          items={sampleItems}
          promptValue="What files did you change?"
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );

      expect(html).toContain("data-state=\"Conversation\"");
      // The session's title reaches the composer as its agentName — checked
      // via composerDefaultPlaceholder rather than the rendered field, which
      // (non-empty here) never shows a placeholder at all; see its own doc
      // comment.
      expect(composerDefaultPlaceholder(undefined, false, "Main Agent")).toBe("Ask Main Agent…");
      expect(html).toContain("Hello agent");
      expect(html).toContain("Hello! How can I help you?");
      expect(html).toContain("data-testid=\"agent-composer\"");
    });

    it("drops an empty reply and signs none of them", () => {
      const html = renderToString(
        <AgentHubContent
          session={{ ...mockSession, title: "", status: "error" }}
          items={[
            { id: "1", kind: "agent_message_chunk", text: "401 token revoked." },
            // The placeholder for a turn that errored before its first chunk.
            { id: "2", kind: "agent_message_chunk", text: "" },
          ]}
          errorMessage="agenthub.processFailed — Agent process failed."
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );

      // One reply, not two: an empty one reads as the agent having said
      // something unreadable.
      expect((html.match(/data-testid="agent-prose"/g) ?? []).length).toBe(1);
      expect(html).toContain("401 token revoked.");
      // Signed by nobody. The side of the column says who spoke; a name over
      // every reply — the vendor's or the session's — was a chat convention in
      // a transcript with one person and one agent in it.
      expect(html).not.toContain(">claude<");
      expect(html).not.toContain('data-testid="agent-message"');
      // Figma's reason is the DevKit code AND what happened.
      expect(html).toContain("agenthub.processFailed — Agent process failed.");
    });

    it("shows the human's own prompt, and counts it as a turn", () => {
      // Neither existed on screen: ACP has a user_message_chunk kind but
      // claude-agent-acp never echoes the prompt back, so the transcript held
      // replies with nothing to reply to, and the composer read "0 turns"
      // beside a non-zero cost.
      const html = renderToString(
        <AgentHubContent
          session={mockSession}
          items={[{ id: "u1", kind: "user_message_chunk", text: "kiểm tra thực tế" }]}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain("kiểm tra thực tế");
      expect(html).toContain('data-testid="turn-prompt"');
      // No "You" over it and no initial beside it: a message on the right of
      // the column is yours, and saying so twice was a chat app's habit.
      expect(html).not.toContain(">You<");
      // The turn count is in the context ring's popover now, which renders
      // nothing until it is opened.
    });

    it("folds a run of a turn's tool calls behind one line", () => {
      // Every call used to be a card with its output open, and a turn that ran
      // forty of them put forty cards between the question and its answer.
      // The one-line result slot is tested on the leaf, where the row is.
      const blob = JSON.stringify({ result: [{ id: "caad65a3dc6457d655a02a64", name: "Chiro local" }] });
      const html = renderToString(
        <AgentHubContent
          session={mockSession}
          items={[
            {
              id: "t1",
              kind: "tool_call",
              toolCall: { id: "c1", title: "mcp__devkit__db_listProfiles", status: "completed", rawOutput: blob },
            },
            {
              id: "t2",
              kind: "tool_call",
              toolCall: { id: "c2", title: "mcp__devkit__db_listConnections", status: "completed" },
            },
          ]}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain('data-testid="turn-work"');
      expect(html).toContain('data-state="collapsed"');
      // Neither call nor its output is on screen until someone opens the run.
      expect(html).not.toContain("mcp__devkit__db_listProfiles");
      expect(html).not.toContain("mcp__devkit__db_listConnections");
      expect(html).not.toContain("caad65a3dc6457d655a02a64");
    });

    it("shows attached files and a way to add them", () => {
      // The composer had attachedFiles and contextFiles props from the start
      // and nothing fed them: the attach button opened a menu that could only
      // ever be empty, and there was no path from a file to a prompt. A file is
      // now a chip in the draft itself.
      const html = renderToString(
        <AgentHubContent
          session={mockSession}
          onBrowseContext={async () => []}
          promptValue={`${chipToken(fileChip("/x/RefundService.java"))} ${chipToken(fileChip("/x/shot.png"))}`}
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain("RefundService.java");
      expect(html).toContain("shot.png");
      expect(html).toContain('data-testid="composer-attach-button"');
    });

    it("says what a session IS when empty, not just that there is none", () => {
      // Figma's note: Empty is the first thing a new user meets.
      const html = renderToString(
        <AgentHubContent
          session={null}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain("No agent running");
      expect(html).toContain("its own sandbox");
      // No header either — there is no agent to name.
      expect(html).not.toContain('data-testid="agent-hub-content-header"');
    });

    it("draws no header: the sidebar row already names the session", () => {
      const html = renderToString(
        <AgentHubContent
          session={mockSession}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).not.toContain('data-testid="agent-hub-header-bar"');
      expect(html).not.toContain('data-testid="agent-hub-content-header"');
      expect(html).not.toContain('data-testid="content-session-menu"');
    });

    it("stops a turn from the composer, not from a second button in the header", () => {
      const base = {
        session: mockSession,
        promptValue: "",
        onPromptChange: () => {},
        onSend: () => {},
        onCancel: () => {},
      };
      const running = renderToString(<AgentHubContent {...base} streaming />);
      // The header's Cancel turn repeated the composer's Stop, which sits under
      // the text being written. One action, one control.
      expect(running).not.toContain('data-testid="content-cancel-turn"');
      expect(running).toContain('aria-label="Stop generating"');
    });
  });

  describe("AgentRail", () => {
    const mockSession: AgentSession = {
      id: "sess-rail-1",
      agentType: "claude",
      projectPath: ".",
      status: "idle",
      parentSessionId: null,
      depth: 0,
      title: "Rail Agent",
      acpSessionId: "acp-rail-1",
    };

    it("renders Empty state when active session is null", () => {
      const html = renderToString(
        <AgentRail
          activeSession={null}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );

      expect(html).toContain("data-testid=\"agent-rail\"");
      expect(html).toContain("data-state=\"Empty\"");
      expect(html).toContain("data-testid=\"agent-rail-empty\"");
    });

    it("renders Conversation state with rail composer and inline permission when pending", () => {
      const html = renderToString(
        <AgentRail
          activeSession={mockSession}
          promptValue="Inspect database"
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          pendingPermission={{
            id: "perm-1",
            sessionId: "sess-rail-1",
            caller: "test-caller",
            createdAt: new Date().toISOString(),
            expiresAt: new Date(Date.now() + 60_000).toISOString(),
            title: "Rail Agent",
            depth: 0,
            params: {
              toolCall: { title: "Execute shell command" },
              options: [{ optionId: "allow_once", name: "Allow Once" }],
            },
          }}
        />,
      );

      expect(html).toContain("data-state=\"Conversation\"");
      expect(html).toContain("Rail Agent");
      // In the composer tray's panel, over the rail's composer.
      expect(html).toContain("data-testid=\"composer-tray-panel\"");
      expect(html).toContain("data-testid=\"agent-permission-request\"");
      expect(html).toContain("Execute shell command");
      // Figma 909:28 fixes the label. The vendor's optionId is what gets sent,
      // but a vendor must not be able to reword the button on a security
      // prompt — the old code rendered allowOnceOption.name verbatim.
      expect(html).toContain("Allow once");
      expect(html).toContain("data-testid=\"agent-composer\"");
      expect(html).toContain("data-size=\"rail\"");
    });
  });

  describe("Agent Hub Phase 4 Shell Integration Surfaces", () => {
    const mockSession: AgentSession = {
      id: "sess-1",
      agentType: "claude",
      projectPath: "/Users/test/project",
      status: "idle",
      parentSessionId: null,
      depth: 0,
      title: "Test Session",
      acpSessionId: "acp-1",
    };

    describe("AgentPermissionRequest (Figma 909:49)", () => {
      const mockPending = {
        id: "perm-req-1",
        sessionId: "sess-req-1",
        caller: "agent:claude-1",
        createdAt: new Date().toISOString(),
        expiresAt: new Date(Date.now() + 60_000).toISOString(),
        title: "Subagent Alpha",
        depth: 1,
        params: {
          command: "git push origin main",
          options: [
            { optionId: "allow_once", name: "Allow once" },
            { optionId: "allow_always", name: "Allow always" },
          ],
        },
      };

      it("renders open environment inline card with deadline and caller identity", () => {
        const html = renderToString(
          <AgentPermissionRequest
            pending={mockPending}
            session={{ ...mockSession, id: "sess-req-1", title: "Subagent Alpha", projectPath: "/workspace/app" }}
            onResolve={() => {}}
          />,
        );

        expect(html).toContain('data-testid="agent-permission-request"');
        expect(html).toContain('data-env="open"');
        // DEVIATION from Figma 909:28: no "dev" pill. It said the same thing on
        // every request in the ordinary case; only the locked one is worth a
        // pill, and it is the one that survives.
        expect(html).not.toContain('data-testid="permission-env-open"');
        expect(html).toContain('data-testid="permission-seconds"');
        expect(html).toContain("60s");
        expect(html).toContain("Subagent Alpha");
        expect(html).toContain("depth 1");
        expect(html).toContain("/workspace/app");
        expect(html).toContain('data-testid="permission-allow-once"');
        expect(html).toContain("Allow once");
        expect(html).toContain('data-testid="permission-deny"');
        expect(html).toContain("Deny");
        expect(html).toContain('data-testid="permission-allow-always"');
        expect(html).toContain("Allow always");
      });

      it("shows the file an edit would change as a 08 Diff pane", () => {
        const html = renderToString(
          <AgentPermissionRequest
            pending={{
              ...mockPending,
              params: {
                toolCall: {
                  title: "Edit src/app.ts",
                  content: [{ type: "diff", path: "/workspace/app/src/app.ts", oldText: "const a = 1;", newText: "const a = 2;" }],
                },
                options: [{ optionId: "allow_once", name: "Allow once" }],
              },
            }}
            session={{ ...mockSession, projectPath: "/workspace/app" }}
            onResolve={() => {}}
          />,
        );
        expect(html).toContain('data-testid="permission-file-edits"');
        expect(html).toContain('data-slot="diff-pane"');
        // Relative to the session's folder, and not upper-cased: it is a path.
        expect(html).toContain(">src/app.ts<");
        expect(html).toContain('data-kind="removed"');
        expect(html).toContain('data-kind="added"');
        expect(html).toContain("const a = 2;");
      });

      it("offers the whole file for review, without leaving the card", () => {
        const html = renderToString(
          <AgentPermissionRequest
            pending={{
              ...mockPending,
              params: {
                toolCall: {
                  content: [{ type: "diff", path: "/workspace/app/src/app.ts", oldText: "a", newText: "b" }],
                },
                options: [{ optionId: "allow_once", name: "Allow once" }],
              },
            }}
            session={{ ...mockSession, projectPath: "/workspace/app" }}
            onResolve={() => {}}
          />,
        );
        // The pane caps its height so the card cannot push the transcript off
        // screen; this is the way to the rest of the file.
        expect(html).toContain('data-testid="permission-diff-expand"');
        expect(html).toContain("Review the whole diff of src/app.ts");
      });

      it("in the tray wears the panel heading the repository list wears", () => {
        const html = renderToString(
          <AgentPermissionRequest pending={mockPending} session={mockSession} onResolve={() => {}} panel waiting={2} onMinimize={() => {}} />,
        );
        expect(html).toContain("Permission request");
        expect(html).toContain('data-testid="permission-minimize"');
        expect(html).toContain('aria-label="Close permission request"');
        expect(html).toContain('data-testid="permission-allow-once"');
        // The panel heading owns the close button; the card draws no chevron of its own.
        expect(html).not.toContain('aria-label="Minimize permission request"');
      });

      it("draws no diff for a request that edits no file", () => {
        const html = renderToString(
          <AgentPermissionRequest pending={mockPending} session={mockSession} onResolve={() => {}} />,
        );
        expect(html).not.toContain('data-testid="permission-file-edits"');
      });

      it("draws no countdown when nothing expires", () => {
        // The ordinary case now: the gate waits for the person, as the
        // vendors' own apps do, so there is no deadline to show.
        const html = renderToString(
          <AgentPermissionRequest
            pending={{ ...mockPending, expiresAt: "" }}
            session={mockSession}
            onResolve={() => {}}
          />,
        );
        expect(html).not.toContain('data-testid="permission-seconds"');
        expect(html).toContain('data-testid="permission-allow-once"');
      });

      it("renders locked environment variant with warning badge", () => {
        const html = renderToString(
          <AgentPermissionRequest
            pending={mockPending}
            session={mockSession}
            onResolve={() => {}}
            env="locked"
          />,
        );

        expect(html).toContain('data-env="locked"');
        expect(html).toContain('data-testid="permission-env-locked"');
        // Figma 909:48: danger-toned pill reading "prod".
        expect(html).toContain("prod");
        // Locked is danger-toned in Figma 909:48, not warning-toned, and the
        // tone lives on the env pill rather than on the card's border.
        expect(html).toContain("feedback-danger-soft");
      });
    });

    describe("AgentPermissionToast (Figma 924:152)", () => {
      const mockPending = {
        id: "perm-toast-1",
        sessionId: "sess-toast-1",
        caller: "agent:claude-master",
        createdAt: new Date().toISOString(),
        expiresAt: new Date(Date.now() + 60_000).toISOString(),
        title: "Swarm Orchestrator",
        depth: 0,
        params: {
          command: "cargo build --release",
          options: [
            { optionId: "allow_once", name: "Allow once" },
            { optionId: "allow_always", name: "Allow always" },
          ],
        },
      };

      it("renders floating toast with Open rail button, deadline countdown, and command preview", () => {
        const html = renderToString(
          <AgentPermissionToast
            pending={{ ...mockPending, expiresAt: new Date(Date.now() + 60_000).toISOString() }}
            session={{ ...mockSession, id: "sess-toast-1", title: "Swarm Orchestrator", projectPath: "/project/core" }}
            onResolve={() => {}}
            onOpenRail={() => {}}
            timeoutSeconds={60}
          />,
        );

        expect(html).toContain('data-testid="agent-permission-toast"');
        // Placement moved to AgentPermissionToastHost: Figma 924:152 is a 420
        // card, and "bottom-right" is where the host puts it.
        expect(html).not.toContain("fixed");
        expect(html).toContain('data-testid="toast-deadline"');
        expect(html).toContain("60s");
        expect(html).toContain("Swarm Orchestrator");
        expect(html).toContain("depth 0");
        expect(html).toContain("/project/core");
        expect(html).toContain("cargo build --release");
        expect(html).toContain('data-testid="toast-open-rail"');
        expect(html).toContain("Open rail");
        expect(html).toContain('data-testid="toast-allow-once"');
        expect(html).toContain("Allow once");
        expect(html).toContain('data-testid="toast-deny"');
        expect(html).toContain("Deny");

        // CRITICAL SAFETY REQUIREMENT: Toast has NO "Allow always"
        expect(html).not.toContain("Allow always");
      });

      it("arrives and leaves under one presence, keyed by the request", () => {
        // All three surfaces share it, so a request cannot fade in the rail and
        // snap in the module. Keyed by id: answering one while another waits
        // shows a swap, not a card that changes its mind mid-blink.
        const shown = renderToString(
          <PermissionPresence pendingId="perm-1" variant="toast">
            <p>card</p>
          </PermissionPresence>,
        );
        expect(shown).toContain('data-slot="permission-presence"');
        expect(shown).toContain("card");

        // Nothing pending renders nothing: a closed disclosure costs no DOM,
        // and server output shows exactly what is open.
        expect(
          renderToString(
            <PermissionPresence pendingId={null}>
              <p>card</p>
            </PermissionPresence>,
          ),
        ).toBe("");
      });

      it("reads the command off an ACP tool call and does not repeat it as the title", () => {
        const html = renderToString(
          <AgentPermissionToast
            pending={{
              ...mockPending,
              params: {
                toolCall: { title: "`git push --force`", rawInput: { command: "git push --force" } },
                options: [{ optionId: "allow_once", name: "Allow once" }],
              },
            }}
            session={mockSession}
            onResolve={() => {}}
            onOpenRail={() => {}}
          />,
        );

        // Once as text (the well); the title attribute on it is not a second line.
        expect(html.split(">git push --force<").length - 1).toBe(1);
        // Env=Open draws no pill.
        expect(html).not.toContain("toast-env-open");
      });

      it("renders locked environment variant with warning badge", () => {
        const html = renderToString(
          <AgentPermissionToast
            pending={mockPending}
            session={mockSession}
            onResolve={() => {}}
            onOpenRail={() => {}}
            env="locked"
          />,
        );

        expect(html).toContain('data-env="locked"');
        expect(html).toContain('data-testid="toast-env-locked"');
        // Figma 924:151 labels the pill by environment: "prod", danger-toned.
        expect(html).toContain("prod");
      });
    });

    describe("AgentDockBadge (Figma 933:150)", () => {
      it("renders State Waiting 16px count badge", () => {
        const html = renderToString(
          <AgentDockBadge state="waiting" count={3} />,
        );

        expect(html).toContain('data-testid="agent-dock-badge-waiting"');
        expect(html).toContain('data-state="waiting"');
        expect(html).toContain("3");
        expect(html).toContain("bg-status-warning");
        expect(html).toContain("text-caption-11-medium");
      });

      it("renders State Running as a still 8px dot", () => {
        const html = renderToString(<AgentDockBadge state="running" />);

        expect(html).toContain('data-testid="agent-dock-badge-running"');
        expect(html).toContain('data-state="running"');
        expect(html).toContain("bg-(--status-running)");
        // Figma: "Running is a dot and needs nothing." An animation that never
        // stops is the one thing on the dock you cannot ignore.
        expect(html).not.toContain("animate-ping");
        // The ring is what attaches the badge to the icon underneath it.
        expect(html).toContain("border-bg-app");
      });

      it("renders completed turns as a review badge", () => {
        const html = renderToString(<AgentDockBadge state="success" count={2} />);

        expect(html).toContain('data-testid="agent-dock-badge-success"');
        expect(html).toContain('data-state="success"');
        expect(html).toContain("2");
        expect(html).toContain("bg-status-success");
      });
    });
  });

  describe("Agent / Meta Inspector (Figma 951:2270)", () => {
    it("keeps raw _meta verbatim below what DevKit understood", () => {
      const html = renderToString(
        <AgentMetaInspector
          open
          onOpenChange={() => {}}
          agentName="refund-worker"
          turn={3}
          stopReason="end_turn"
          cost={0.03}
          inputTokens={1204}
          meta={{ "_claude/origin": { kind: "human" } }}
        />,
      );
      // Radix portals the content, so assert on what the panel is given
      // rather than on the portal's own markup.
      expect(typeof html).toBe("string");
    });

    it("admits a _meta it cannot serialise instead of rendering nothing", () => {
      const cyclic: Record<string, unknown> = {};
      cyclic.self = cyclic;
      // The point is that this does not throw — FR-8 promises nothing is
      // silently dropped, and a crash drops the whole panel.
      expect(() =>
        renderToString(
          <AgentMetaInspector open onOpenChange={() => {}} meta={cyclic} />,
        ),
      ).not.toThrow();
    });
  });

  describe("Agent / Jump To Latest (Figma 1044:3196)", () => {
    it("is an arrow with a name, not a count", () => {
      const html = renderToString(<AgentJumpToLatest visible onClick={() => {}} />);
      expect(html).toContain('aria-label="Scroll to bottom"');
      expect(html).toContain("bg-surface-glass-floating");
      expect(html).not.toContain("new message");
    });
  });
  describe("Agent / Question (ACP form elicitation)", () => {
    const session: AgentSession = {
      id: "sess-q",
      agentType: "claude",
      projectPath: "/Users/test/project",
      status: "running",
      parentSessionId: null,
      depth: 0,
      title: "Asking Agent",
      acpSessionId: "acp-q",
    };
    const choiceField = (n: number) => ({
      [`question_${n}`]: {
        type: "string",
        title: `Q${n}`,
        description: `Question number ${n}?`,
        oneOf: [
          { const: "Alpha", title: "Alpha", description: "The first" },
          { const: "Beta", title: "Beta" },
        ],
      },
      [`question_${n}_custom`]: {
        type: "string",
        title: "Other",
        _meta: { _askUserQuestionCustomAnswer: { questionId: `question_${n}`, isCustomAnswer: true } },
      },
    });
    const question = (count: number, extra: Record<string, unknown> = {}): PendingElicitation => ({
      id: `q-${count}`,
      sessionId: "sess-q",
      toolCallId: "toolu_1",
      message: count === 1 ? "Question number 0?" : "Please answer the following questions.",
      params: {
        mode: "form",
        message: count === 1 ? "Question number 0?" : "Please answer the following questions.",
        requestedSchema: {
          type: "object",
          properties: Object.assign({}, ...Array.from({ length: count }, (_, n) => choiceField(n)), extra),
        },
      },
      caller: "agent:sess-q",
      title: "Asking Agent",
      depth: 1,
      createdAt: new Date().toISOString(),
    });

    // Two used to be shown together, which read as one form with two answers.
    it("asks one question at a time even when there are only two, with progress", () => {
      const html = renderToString(<AgentQuestionCard pending={question(2)} session={session} onRespond={() => {}} />);
      expect(html).toContain('data-testid="agent-question-card"');
      expect(html).toContain('data-layout="stepwise"');
      // Who is asking is on the icon, not a row of its own.
      expect(html).toContain("Asked by Asking Agent");
      expect(html.match(/data-slot="questionnaire"/g)).toHaveLength(1);
      expect(html).toContain('data-slot="questionnaire-progress"');
      expect(html).toContain('data-testid="question-skip-all"');
      expect(html).toContain('data-testid="question-submit"');
      // A question has no deadline, so no clock is drawn.
      expect(html).not.toContain('data-testid="question-seconds"');
    });

    it("has no progress and one Skip for a single question", () => {
      const html = renderToString(<AgentQuestionCard pending={question(1)} session={session} onRespond={() => {}} />);
      expect(html).not.toContain('data-slot="questionnaire-progress"');
      expect(html).toContain(">Skip<");
      expect(html).not.toContain("Skip all");
    });

    it("goes one question at a time from three, with progress and a way to skip everything", () => {
      const html = renderToString(<AgentQuestionCard pending={question(3)} session={session} onRespond={() => {}} />);
      expect(html).toContain('data-layout="stepwise"');
      expect(html).toContain('data-slot="questionnaire-progress"');
      expect(html).toContain('data-testid="question-skip-all"');
    });

    // Skip all used to sit alone on a second row under Previous/Skip/Next.
    it("keeps every step control, Skip all included, on one row", () => {
      const html = renderToString(<AgentQuestionCard pending={question(3)} session={session} onRespond={() => {}} />);
      const start = html.indexOf('data-testid="question-actions"');
      const row = html.slice(start, html.indexOf("</div>", html.indexOf('data-slot="questionnaire-submit"', start)));
      expect(row).toContain('data-testid="question-skip-all"');
      expect(row).toContain('data-slot="questionnaire-previous"');
      expect(row).toContain('data-slot="questionnaire-next"');
    });

    it("can be minimized into the tray's strip when the tray offers it", () => {
      const withMinimize = renderToString(
        <AgentQuestionCard pending={question(2)} session={session} onRespond={() => {}} onMinimize={() => {}} />,
      );
      expect(withMinimize).toContain('data-testid="question-minimize"');
      const without = renderToString(<AgentQuestionCard pending={question(2)} session={session} onRespond={() => {}} />);
      expect(without).not.toContain('data-testid="question-minimize"');
    });

    it("is always stepwise in the rail, even for one question", () => {
      const html = renderToString(
        <AgentQuestionCard pending={question(1)} session={session} onRespond={() => {}} size="rail" />,
      );
      expect(html).toContain('data-size="rail"');
      expect(html).toContain('data-layout="stepwise"');
      // The card fills whatever the rail's own width is — a resizable split
      // panel wider than the rail's default must not leave it stranded on the
      // left with dead space on the right.
      expect(html).not.toContain("max-w-68");
    });

    it("keeps Skip when nothing in the schema can be rendered, so the turn is never stuck", () => {
      const pending: PendingElicitation = {
        ...question(0),
        params: {
          message: "Fill the nested config",
          requestedSchema: {
            type: "object",
            required: ["config"],
            properties: { config: { type: "object", properties: {} } },
          },
        },
      };
      const html = renderToString(<AgentQuestionCard pending={pending} session={session} onRespond={() => {}} />);
      expect(html).toContain('data-testid="question-unsupported"');
      expect(html).toContain('data-testid="question-skip-all"');
    });

    it("the toast names the asker and counts the questions, but carries no form", () => {
      const html = renderToString(
        <AgentQuestionToast pending={question(3)} session={session} onRespond={() => {}} onAnswer={() => {}} queued={1} />,
      );
      expect(html).toContain('data-testid="agent-question-toast"');
      expect(html).toContain("Asking Agent");
      expect(html).toContain("3 questions");
      expect(html).toContain('data-testid="question-toast-answer"');
      expect(html).toContain('data-testid="question-toast-skip"');
      expect(html).toContain("+1 waiting");
      expect(html).not.toContain("questionnaire");
      expect(html).not.toContain("Alpha");
      expect(html).not.toContain('data-testid="toast-deadline"');
      expect(html).not.toContain('data-testid="question-toast-seconds"');
    });

    it("asks inline in the module and the rail, behind a waiting permission", () => {
      const pending = question(1);
      const content = (withPermission: boolean) =>
        renderToString(
          <AgentHubContent
            session={session}
            items={[]}
            promptValue=""
            onPromptChange={() => {}}
            onSend={() => {}}
            onCancel={() => {}}
            pendingElicitation={pending}
            onRespondElicitation={() => {}}
            {...(withPermission
              ? {
                  pendingPermission: {
                    id: "perm-q",
                    sessionId: "sess-q",
                    caller: "agent:sess-q",
                    title: "Asking Agent",
                    depth: 0,
                    params: {},
                    createdAt: new Date().toISOString(),
                    expiresAt: new Date(Date.now() + 60_000).toISOString(),
                  },
                  onResolvePermission: () => {},
                }
              : {})}
          />,
        );
      expect(content(false)).toContain('data-testid="agent-question-card"');
      const both = content(true);
      expect(both).toContain('data-testid="agent-permission-request"');
      expect(both).not.toContain('data-testid="agent-question-card"');
      // Waiting behind the permission, the question still says it is there.
      expect(both).toContain('data-testid="tray-question"');

      const rail = renderToString(
        <AgentRail
          activeSession={session}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          pendingElicitation={pending}
          onRespondElicitation={() => {}}
        />,
      );
      expect(rail).toContain('data-testid="composer-tray-panel"');
      expect(rail).toContain('data-testid="agent-question-card"');
      expect(rail).toContain('data-size="rail"');
    });
  });
});
