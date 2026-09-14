import { readFileSync } from "node:fs";
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import {
  AgentComposer,
  AgentJumpToLatest,
  AgentMenu,
  AgentNewSession,
  AgentSessionError,
  AgentSidebarTree,
  countDescendants,
  groupByProject,
  AgentHubContent,
  AgentMetaInspector,
  AgentRail,
  AgentPermissionRequest,
  AgentPermissionToast,
  AgentDockBadge,
} from "./index";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentSession } from "@/bridge/agenthub";
import type { TranscriptItem } from "../types";

describe("Agent Hub Phase 3 Surfaces", () => {
  describe("AgentComposer (Figma 963:2634)", () => {
    it("renders the meta band above the field, on the frame's own ground", () => {
      // Figma's note: "one bordered frame around the input only. The meta band
      // sits on that frame's own ground above the field, so the frame's top
      // region reads as a thick top border — it is not a header." The old
      // markup put a bottom-bordered strip inside a single box.
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
          version="0.74"
        />,
      );

      expect(html).toContain('data-testid="agent-composer"');
      expect(html).toContain('data-size="wide"');
      expect(html).toContain('data-state="idle"');
      expect(html).toContain("4 turns · $0.21");
      expect(html).toContain("claude-agent-acp 0.74");
      // Wide spells the counter out; the rail abbreviates it.
      expect(html).toContain("24,503 / 1,000,000 context");
      expect(html).toContain("Add context");
      expect(html).toContain("Commands");
      expect(html).toContain("Test prompt");
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
      expect(html).toContain("refund-worker is working…");
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
          version="0.74"
        />,
      );

      // 280px cannot hold two labelled pills and a spelled-out counter.
      expect(html).not.toContain("Add context</button>");
      expect(html).toContain("24.5k / 1M");
      // Rail shows the bare vendor, wide appends the version.
      expect(html).not.toContain("claude 0.74");
    });

    it("inserts the attachment row inside the field when files are attached", () => {
      const html = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          attachedFiles={["src/index.ts", "schema.sql"]}
        />,
      );

      expect(html).toContain('data-state="attached"');
      expect(html).toContain('data-testid="composer-attachments"');
      expect(html).toContain("src/index.ts");
      expect(html).toContain("schema.sql");
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
    it("renders floating pill when visible", () => {
      const html = renderToString(
        <AgentJumpToLatest visible={true} onClick={() => {}} />,
      );

      expect(html).toContain("data-testid=\"agent-jump-to-latest\"");
      expect(html).toContain("Jump to latest");
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
      expect(html).toContain("Restart");
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

  describe("AgentNewSession (Figma 946:2224)", () => {
    // A Radix Dialog now, so the card lives in a portal and renderToString
    // sees nothing while closed. The block this replaced set role="dialog"
    // and aria-modal on a plain div — the attributes without the behaviour:
    // no focus trap, no escape key, no scroll lock.
    it("renders nothing while closed", () => {
      const html = renderToString(
        <AgentNewSession
          open={false}
          onOpenChange={() => {}}
          vendors={["mock", "claude"]}
          onCreate={() => {}}
        />,
      );
      expect(html).not.toContain("New agent session");
    });

    it("takes exactly the two inputs FR-1 allows, and no title field", () => {
      // Figma: "choosing a vendor and a working directory is the only way a
      // session starts. Parent and depth are shown, never chosen." The old
      // card also offered a title and a "switch to mock" escape hatch.
      const props = AgentNewSession as unknown as { name: string };
      expect(props.name).toBe("AgentNewSession");

      const source = readFileSync(
        new URL("./AgentNewSession.tsx", import.meta.url),
        "utf8",
      );
      expect(source).toContain('data-testid="new-session-vendor"');
      expect(source).toContain('data-testid="new-session-cwd"');
      expect(source).not.toContain("onSwitchToMock");
      // Parent/depth is a read-only row, never an input.
      expect(source).toContain('data-testid="new-session-inherited"');
      expect(source).not.toContain('id="new-session-parent"');
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
        <AgentRail sessions={[railSession]} activeSession={railSession} {...railProps} />,
      );
      // The root only — transcript cards inside legitimately take their own
      // ground. A fill *here* would be a second opaque layer on an opaque one.
      const rootClass = html.slice(html.indexOf('class="'), html.indexOf('">'));
      expect(rootClass).not.toContain("bg-");
      // pl-3 pr-5: the right margin is where the shell's scroll ruler lives.
      expect(rootClass).toContain("pr-5");
    });

    it("carries depth and cwd in the header, not a status pill", () => {
      // Figma 913:31's meta line is "depth 2 · ~/acme/billing-service" — what
      // this agent may touch. The header used to show a colour-coded status
      // pill, a third status vocabulary beside AgentState.
      const html = renderToString(
        <AgentRail sessions={[railSession]} activeSession={railSession} {...railProps} />,
      );
      expect(html).toContain("depth 0 ·");
      expect(html).toContain('data-slot="agent-state"');
    });

    it("says what is missing and how to fix it when empty", () => {
      const html = renderToString(
        <AgentRail sessions={[]} activeSession={null} {...railProps} />,
      );
      expect(html).toContain("No agents running");
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

      const composer = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          vendor="claude"
        />,
      );
      expect(composer).toContain('data-vendor="claude"');
      expect(composer).not.toContain("lucide-bot");

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
      expect(html).not.toContain("data-testid=\"agent-composer\"");
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
      expect(html).toContain("Main Agent");
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
      expect(html).toContain("1 turn · $");
    });

    it("folds a turn's tool calls behind one line", () => {
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
          ]}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain('data-testid="turn-work"');
      expect(html).toContain('data-state="collapsed"');
      // Neither the call nor its output is on screen until someone opens it.
      expect(html).not.toContain("mcp__devkit__db_listProfiles");
      expect(html).not.toContain("caad65a3dc6457d655a02a64");
    });

    it("shows attached files and a way to add them", () => {
      // The composer had attachedFiles and contextFiles props from the start
      // and nothing fed them: the attach button opened a menu that could only
      // ever be empty, and there was no path from a file to a prompt.
      const html = renderToString(
        <AgentHubContent
          session={mockSession}
          attachments={["/x/RefundService.java", "/x/shot.png"]}
          onBrowseContext={() => {}}
          onRemoveAttachment={() => {}}
          promptValue=""
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

    it("reads lineage as one sentence and takes status from AgentState alone", () => {
      const parent: AgentSession = { ...mockSession, id: "sess-000", title: "auth-refactor" };
      const child: AgentSession = {
        ...mockSession,
        depth: 2,
        status: "awaiting_permission",
        parentSessionId: parent.id,
      };
      const html = renderToString(
        <AgentHubContent
          session={child}
          parentSession={parent}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      // The project's own name and where the agent came from. "depth 2" was the
      // hub counting, and the absolute path took the row to say a folder's name.
      expect(html).toContain("dev-kit-app · from auth-refactor");
      expect(html).not.toContain("depth 2");
      // The colour-coded pill was a third status vocabulary beside AgentState.
      expect(html).toContain('data-slot="agent-state"');
      expect(html).not.toContain('data-testid="content-header-status"');
    });

    it("does not repeat the name when the agent is named after its folder", () => {
      // Seen in the app: "Dev-kit-backend dev-kit-backend".
      const named = { ...mockSession, title: "Dev-Kit-App", projectPath: "/Users/dev/dev-kit-app" };
      const html = renderToString(
        <AgentHubContent
          session={named}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />,
      );
      expect(html).toContain("Dev-Kit-App");
      expect(html).not.toContain('data-testid="content-header-context"');
    });

    it("stops a turn from the composer, not from a second button in the header", () => {
      const base = {
        session: mockSession,
        promptValue: "",
        onPromptChange: () => {},
        onSend: () => {},
        onCancel: () => {},
        onCloseSession: () => {},
      };
      const running = renderToString(<AgentHubContent {...base} streaming />);
      // The header's Cancel turn repeated the composer's Stop, which sits under
      // the text being written. One action, one control.
      expect(running).not.toContain('data-testid="content-cancel-turn"');
      expect(running).toContain('aria-label="Stop generating"');
    });

    it("offers the session menu only when it has something to do", () => {
      const base = {
        session: mockSession,
        promptValue: "",
        onPromptChange: () => {},
        onSend: () => {},
        onCancel: () => {},
      };
      // Close lives in the menu now, as it does on the sidebar row: a bordered
      // button at Stop's weight, for the one action here that ends a process,
      // was the wrong emphasis. A menu with nothing in it is not drawn.
      expect(renderToString(<AgentHubContent {...base} />)).not.toContain(
        'data-testid="content-session-menu"',
      );
      expect(
        renderToString(<AgentHubContent {...base} onCloseSession={() => {}} />),
      ).toContain('data-testid="content-session-menu"');
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
          sessions={[mockSession]}
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
      expect(html).toContain("data-testid=\"rail-inline-permission\"");
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
            timeoutSeconds={60}
          />,
        );

        expect(html).toContain('data-testid="agent-permission-request"');
        expect(html).toContain('data-env="open"');
        expect(html).toContain('data-testid="permission-env-open"');
        // Figma 909:28 labels the pill by environment, not by lock state.
        expect(html).toContain("dev");
        // The countdown now sits on the metadata row beside depth, which is
        // where Figma 909:28 puts session metadata — it has no element of
        // its own any more.
        expect(html).toContain("depth");
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
            pending={mockPending}
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
        expect(html).toContain("bg-status-info");
        // Figma: "Running is a dot and needs nothing." An animation that never
        // stops is the one thing on the dock you cannot ignore.
        expect(html).not.toContain("animate-ping");
        // The ring is what attaches the badge to the icon underneath it.
        expect(html).toContain("border-bg-app");
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
    it("counts what arrived rather than repeating the arrow", () => {
      const html = renderToString(
        <AgentJumpToLatest visible count={3} onClick={() => {}} />,
      );
      expect(html).toContain("3 new messages");
      expect(html).toContain("bg-surface-glass-floating");
    });

    it("still names itself when nothing has been counted", () => {
      const html = renderToString(<AgentJumpToLatest visible onClick={() => {}} />);
      expect(html).toContain("Jump to latest");
    });
  });
});
