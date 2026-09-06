import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import {
  AgentMessage,
  AgentCodeBlock,
  AgentThinking,
  AgentToolCall,
  AgentToolDiff,
  AgentToolTerminal,
  AgentFileChip,
  AgentPlan,
  AgentInvokeResult,
  AgentRateLimit,
  AgentImage,
} from "./index";

describe("Agent Hub Transcript Leaves", () => {
  describe("AgentMessage", () => {
    it("renders agent message with start alignment", () => {
      const html = renderToString(
        <AgentMessage
          align="start"
          author="Claude"
          text="I have analyzed the codebase."
          timestamp="10:30 AM"
        />,
      );
      expect(html).toContain('data-align="start"');
      expect(html).toContain("Claude");
      expect(html).toContain("10:30 AM");
      expect(html).toContain("I have analyzed the codebase.");
    });

    it("renders user message with end alignment", () => {
      const html = renderToString(
        <AgentMessage
          align="end"
          text="Please refactor the user controller."
        />,
      );
      expect(html).toContain('data-align="end"');
      expect(html).toContain("You");
      expect(html).toContain("Please refactor the user controller.");
    });
  });

  describe("AgentCodeBlock", () => {
    it("renders code block in default expanded state", () => {
      const code = 'const hello = "world";\nconsole.log(hello);';
      const html = renderToString(
        <AgentCodeBlock
          code={code}
          path="src/index.ts"
          language="typescript"
        />,
      );
      expect(html).toContain("src/index.ts");
      expect(html).toContain("(typescript)");
      expect(html).toContain("const hello = &quot;world&quot;;");
      expect(html).toContain("overflow-x-auto");
    });

    it("renders collapsed code block without code body", () => {
      const html = renderToString(
        <AgentCodeBlock
          code="secret code"
          path="config.env"
          collapsed={true}
        />,
      );
      expect(html).toContain("config.env");
      expect(html).not.toContain("secret code");
    });
  });

  describe("AgentThinking", () => {
    it("renders collapsed thinking block", () => {
      const html = renderToString(
        <AgentThinking
          text="Looking into sqlite schema..."
          duration="2.1s"
          expanded={false}
        />,
      );
      expect(html).toContain("Thinking (2.1s)");
      expect(html).not.toContain("Looking into sqlite schema...");
    });

    it("renders expanded thinking block", () => {
      const html = renderToString(
        <AgentThinking
          text="Looking into sqlite schema..."
          expanded={true}
        />,
      );
      expect(html).toContain("Thinking...");
      expect(html).toContain("Looking into sqlite schema...");
    });
  });

  describe("AgentToolCall", () => {
    it("renders completed tool call with locations and diff", () => {
      const html = renderToString(
        <AgentToolCall
          call={{
            id: "call-1",
            kind: "edit",
            title: "Update config.json",
            status: "completed",
            locations: [{ path: "config.json", line: 12 }],
            content: [
              {
                type: "diff",
                path: "config.json",
                oldText: '{\n  "version": 1\n}',
                newText: '{\n  "version": 2\n}',
              },
            ],
          }}
        />,
      );
      expect(html).toContain("Update config.json");
      expect(html).toContain("DONE");
      expect(html).toContain("config.json");
      expect(html).toContain(":12");
      expect(html).toContain("+1");
      expect(html).toContain("-1");
    });

    it("design for absence: renders intact row when content and locations are absent", () => {
      const html = renderToString(
        <AgentToolCall
          call={{
            id: "call-2",
            kind: "read",
            title: "Read package.json",
            status: "completed",
          }}
        />,
      );
      expect(html).toContain("Read package.json");
      expect(html).toContain("DONE");
      expect(html).toContain('data-testid="agent-tool-call"');
      // No broken state or empty diff container
      expect(html).not.toContain("empty diff");
    });

    it("renders running and awaiting statuses", () => {
      const runningHtml = renderToString(
        <AgentToolCall
          call={{
            id: "call-3",
            kind: "execute",
            title: "Run tests",
            status: "in_progress",
          }}
        />,
      );
      expect(runningHtml).toContain("RUNNING");

      const awaitingHtml = renderToString(
        <AgentToolCall
          call={{
            id: "call-4",
            kind: "execute",
            title: "Deploy to prod",
            status: "awaiting_permission",
          }}
        />,
      );
      expect(awaitingHtml).toContain("AWAITING");
    });
  });

  describe("AgentToolDiff", () => {
    it("renders computed diff with additions and deletions", () => {
      const html = renderToString(
        <AgentToolDiff
          path="main.go"
          oldText="func old() {}"
          newText="func new() {}"
        />,
      );
      expect(html).toContain("main.go");
      expect(html).toContain("+1");
      expect(html).toContain("-1");
      expect(html).toContain("func old() {}");
      expect(html).toContain("func new() {}");
    });

    it("handles absent/empty texts cleanly", () => {
      const html = renderToString(<AgentToolDiff path="empty.txt" />);
      expect(html).toContain("empty.txt");
      expect(html).toContain("(empty diff)");
    });
  });

  describe("AgentToolTerminal", () => {
    it("renders terminal with status and non-wrapping output", () => {
      const html = renderToString(
        <AgentToolTerminal
          terminalId="term-1"
          output="build succeeded in 1.4s"
          status="exited"
          exitCode={0}
        />,
      );
      expect(html).toContain("term-1");
      expect(html).toContain("EXITED (0)");
      expect(html).toContain("build succeeded in 1.4s");
      expect(html).toContain("overflow-x-auto");
    });
  });

  describe("AgentFileChip", () => {
    it("renders file path and optional line", () => {
      const htmlWithLine = renderToString(
        <AgentFileChip path="src/app.tsx" line={42} />,
      );
      expect(htmlWithLine).toContain("src/app.tsx");
      expect(htmlWithLine).toContain(":42");

      const htmlWithoutLine = renderToString(
        <AgentFileChip path="README.md" />,
      );
      expect(htmlWithoutLine).toContain("README.md");
      expect(htmlWithoutLine).not.toContain(":undefined");
      expect(htmlWithoutLine).not.toContain(":null");
    });
  });

  describe("AgentPlan", () => {
    it("renders plan entries with progress indicator", () => {
      const html = renderToString(
        <AgentPlan
          entries={[
            { content: "Explore repository", status: "completed" },
            { content: "Implement components", status: "in_progress" },
            { content: "Run verification tests", status: "pending" },
          ]}
        />,
      );
      expect(html).toContain("Plan");
      expect(html).toContain("1 of 3 completed");
      expect(html).toContain("Explore repository");
      expect(html).toContain("Implement components");
      expect(html).toContain("Run verification tests");
    });
  });

  describe("AgentInvokeResult", () => {
    it("renders task completion with files modified and cost", () => {
      const html = renderToString(
        <AgentInvokeResult
          summary="Successfully refactored module."
          filesChanged={["src/a.ts", "src/b.ts"]}
          duration="4.2s"
          cost={{ amount: 0.0125 }}
        />,
      );
      expect(html).toContain("Task Complete");
      expect(html).toContain("Successfully refactored module.");
      expect(html).toContain("Files Modified (2)");
      expect(html).toContain("src/a.ts");
      expect(html).toContain("src/b.ts");
      expect(html).toContain("4.2s");
      expect(html).toContain("$0.0125");
    });

    it("absence test: renders when filesChanged and cost are absent", () => {
      const html = renderToString(
        <AgentInvokeResult summary="All tasks finished." />,
      );
      expect(html).toContain("Task Complete");
      expect(html).toContain("All tasks finished.");
      expect(html).not.toContain("Files Modified");
    });
  });

  describe("AgentRateLimit", () => {
    it("renders warning level", () => {
      const html = renderToString(
        <AgentRateLimit level="warning" remaining={3} resetTime="5m" />,
      );
      expect(html).toContain("3 requests remaining");
      expect(html).toContain("Resets in 5m");
      expect(html).toContain('data-level="warning"');
    });

    it("renders exhausted level", () => {
      const html = renderToString(
        <AgentRateLimit level="exhausted" resetTime="12m" />,
      );
      expect(html).toContain("Rate limit exhausted");
      expect(html).toContain("Resets in 12m");
      expect(html).toContain('data-level="exhausted"');
    });
  });

  describe("AgentImage", () => {
    it("renders image with name, dimensions, and size", () => {
      const html = renderToString(
        <AgentImage
          src="data:image/png;base64,iVBORw0KGgo="
          name="diagram.png"
          dimensions="1920x1080"
          mimeType="image/png"
          size="142 KB"
        />,
      );
      expect(html).toContain("diagram.png");
      expect(html).toContain("1920x1080 · PNG · 142 KB");
      expect(html).toContain('src="data:image/png;base64,iVBORw0KGgo="');
    });

    it("absence test: renders fallback safely when src is absent", () => {
      const html = renderToString(
        <AgentImage name="missing.png" size="0 KB" />,
      );
      expect(html).toContain("missing.png");
      expect(html).not.toContain("<img");
      expect(html).toContain("Image");
    });
  });
});
