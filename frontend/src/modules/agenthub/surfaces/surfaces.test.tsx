import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import {
  AgentComposer,
  AgentJumpToLatest,
  AgentMenu,
  AgentNewSession,
  AgentSessionError,
  AgentRow,
  AgentSidebarTree,
  AgentHubContent,
  AgentRail,
  AgentPermissionRequest,
  AgentPermissionToast,
  AgentDockBadge,
} from "./index";
import type { AgentSession } from "@/bridge/agenthub";
import type { TranscriptItem } from "../types";

describe("Agent Hub Phase 3 Surfaces", () => {
  describe("AgentComposer", () => {
    it("renders idle state with meta band and send button in wide size", () => {
      const html = renderToString(
        <AgentComposer
          size="wide"
          value="Test prompt"
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          turnCount={3}
          usage={{ used: 100, size: 200, amount: 0.0142 }}
          vendor="claude"
          version="0.74"
        />,
      );

      expect(html).toContain("data-testid=\"agent-composer\"");
      expect(html).toContain("data-size=\"wide\"");
      expect(html).toContain("data-state=\"idle\"");
      expect(html).toContain("3 turns · $0.0142");
      expect(html).toContain("Claude · 0.74");
      expect(html).toContain("data-testid=\"composer-send-button\"");
      expect(html).toContain("Test prompt");
    });

    it("renders streaming state with stop button", () => {
      const html = renderToString(
        <AgentComposer
          size="rail"
          streaming={true}
          value="Prompt in flight"
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          vendor="mock"
        />,
      );

      expect(html).toContain("data-size=\"rail\"");
      expect(html).toContain("data-state=\"streaming\"");
      expect(html).toContain("data-testid=\"composer-stop-button\"");
      expect(html).not.toContain("data-testid=\"composer-send-button\"");
    });

    it("renders attached state with file chips", () => {
      const html = renderToString(
        <AgentComposer
          value=""
          onChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          attachedFiles={["src/index.ts", "schema.sql"]}
        />,
      );

      expect(html).toContain("data-state=\"attached\"");
      expect(html).toContain("data-testid=\"composer-attached-row\"");
      expect(html).toContain("src/index.ts");
      expect(html).toContain("schema.sql");
    });
  });

  describe("AgentMenu", () => {
    it("renders commands mode with available ACP commands", () => {
      const html = renderToString(
        <AgentMenu
          mode="commands"
          commands={[
            { name: "/help", description: "Show available commands" },
            { name: "/clear", description: "Clear current context" },
          ]}
          onSelectCommand={() => {}}
          onSelectContext={() => {}}
        />,
      );

      expect(html).toContain("data-testid=\"agent-menu\"");
      expect(html).toContain("/help");
      expect(html).toContain("Show available commands");
      expect(html).toContain("/clear");
    });

    it("renders context mode with file items", () => {
      const html = renderToString(
        <AgentMenu
          mode="context"
          contextFiles={[
            { path: "frontend/src/bridge/agenthub.ts", name: "agenthub.ts" },
          ]}
          onSelectCommand={() => {}}
          onSelectContext={() => {}}
        />,
      );

      expect(html).toContain("agenthub.ts");
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

  describe("AgentSessionError", () => {
    it("renders crashed error cause with default code", () => {
      const html = renderToString(
        <AgentSessionError
          cause="crashed"
          message="Process killed by SIGSEGV"
        />,
      );

      expect(html).toContain("data-testid=\"agent-session-error\"");
      expect(html).toContain("Agent Session Crashed");
      expect(html).toContain("DEV-KIT-500");
      expect(html).toContain("Process killed by SIGSEGV");
    });

    it("renders denied error cause with warning badge", () => {
      const html = renderToString(
        <AgentSessionError
          cause="denied"
          errorCode="ERR_PERMISSION_REJECTED"
          message="User denied tool execution"
        />,
      );

      expect(html).toContain("Session Permission Denied");
      expect(html).toContain("ERR_PERMISSION_REJECTED");
      expect(html).toContain("User denied tool execution");
    });
  });

  describe("AgentNewSession", () => {
    it("renders setup dialog with vendor and working directory inputs", () => {
      const html = renderToString(
        <AgentNewSession
          vendors={["mock", "claude"]}
          defaultVendor="mock"
          defaultCwd="/app/project"
          onCreate={() => {}}
        />,
      );

      expect(html).toContain("data-testid=\"agent-new-session\"");
      expect(html).toContain("New Agent Session");
      expect(html).toContain("value=\"/app/project\"");
      expect(html).toContain("Create Session");
    });

    it("renders error banner and mock fallback button when error is present", () => {
      const html = renderToString(
        <AgentNewSession
          defaultVendor="claude"
          error="sandbox_apply: Operation not permitted"
          onCreate={() => {}}
          onSwitchToMock={() => {}}
        />,
      );

      expect(html).toContain("data-testid=\"new-session-error\"");
      expect(html).toContain("Switch to mock vendor");
    });
  });

  describe("AgentRow & AgentSidebarTree", () => {
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

    it("renders row with status indicator, vendor tag, and depth indentation", () => {
      const html = renderToString(
        <AgentRow
          session={mockChildSession}
          selected={true}
          pendingCount={2}
        />,
      );

      expect(html).toContain("data-testid=\"agent-row-sess-child6789\"");
      expect(html).toContain("data-selected=\"true\"");
      expect(html).toContain("Sub-worker");
      expect(html).toContain("mock");
      expect(html).toContain("data-testid=\"status-dot-awaiting\"");
      expect(html).toContain("data-testid=\"agent-row-pending-badge\"");
      expect(html).toContain("2");
    });

    it("renders sidebar swarm tree with session count", () => {
      const html = renderToString(
        <AgentSidebarTree
          sessions={[mockSession, mockChildSession]}
          activeSessionId="sess-abc12345"
          onSelectSession={() => {}}
          onNewSession={() => {}}
        />,
      );

      expect(html).toContain("data-testid=\"agent-sidebar-tree\"");
      expect(html).toContain("Swarm (2)");
      expect(html).toContain("Task Planner");
      expect(html).toContain("Sub-worker");
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
      expect(html).toContain("Allow Once");
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
        expect(html).toContain("OPEN");
        expect(html).toContain('data-testid="permission-deadline"');
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
        expect(html).toContain("LOCKED");
        expect(html).toContain("border-status-warning/60");
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
        expect(html).toContain("fixed bottom-4 right-4");
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
        expect(html).toContain("LOCKED");
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

      it("renders State Running 8px pulsing dot", () => {
        const html = renderToString(
          <AgentDockBadge state="running" />,
        );

        expect(html).toContain('data-testid="agent-dock-badge-running"');
        expect(html).toContain('data-state="running"');
        expect(html).toContain("bg-status-info");
        expect(html).toContain("animate-ping");
      });
    });
  });
});
