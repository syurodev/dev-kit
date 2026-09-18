import { describe, expect, it } from "bun:test";
import {
  applySessionUpdate,
  createInitialTranscriptModel,
  initTranscript,
} from "../transcript";
import { renderToString as renderRaw } from "react-dom/server";
import {
  AgentMessage,
  AgentCodeBlock,
  AgentThinking,
  AgentToolCall,
  AgentToolDiff,
  AgentToolTerminal,
  AgentFileChip,
  AgentPlan,
  AgentSubagent,
  AgentAsyncTask,
} from "./index";
import { formatToolOutput, toolCallFromDetail, toolOutputText } from "./AgentToolCall";
import { TooltipProvider } from "@/components/ui/tooltip";

// The provider is mounted once in App.tsx; these leaves carry tooltips of
// their own (the diff's "review the whole file"), so the harness supplies it.
const renderToString = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);

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
      expect(html).toContain("typescript");
      expect(html).toContain("const hello = &quot;world&quot;;");
      expect(html).toContain("overflow-x-auto");
    });

    it("stacks a surface/base well inside the raised card", () => {
      // Figma 1023:2660: the code sits one step darker than its own card, the
      // same relationship the diff and the terminal use.
      const html = renderToString(<AgentCodeBlock code="x" path="a.ts" language="ts" />);
      expect(html).toContain("bg-surface-raised");
      expect(html).toContain("bg-surface-base");
      expect(html).toContain("Copy");
    });
  });

  describe("AgentMessage avatars", () => {
    it("marks the agent with its vendor and you with your initial", () => {
      // An initial on both sides read as two people, and an untitled session
      // gave the agent "S" — for the word "Session".
      const agent = renderToString(
        <AgentMessage align="start" author="Session 506ecea1" vendor="claude" text="hi" />,
      );
      expect(agent).toContain('data-vendor="claude"');
      expect(agent).not.toContain(">S</span>");

      const you = renderToString(<AgentMessage align="end" author="You" text="hi" />);
      expect(you).toContain(">Y<");
      expect(you).not.toContain("data-vendor=");
    });

    it("falls back to the one shape for a vendor DevKit has not drawn", () => {
      const html = renderToString(
        <AgentMessage align="start" author="a" vendor="acme-agent" text="hi" />,
      );
      expect(html).toContain("lucide-bot");
      expect(html).not.toContain("data-vendor=");
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
      // Past tense: by the time the chunk lands the thinking is over.
      expect(html).toContain("Thought for 2.1s");
      expect(html).not.toContain("Looking into sqlite schema...");
    });

    it("renders expanded thinking block", () => {
      const html = renderToString(
        <AgentThinking
          text="Looking into sqlite schema..."
          expanded={true}
        />,
      );
      expect(html).toContain("Thought");
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
          defaultExpanded
        />,
      );
      expect(html).toContain("Update config.json");
      // No uppercase status badge: Figma spends that slot on the result.
      expect(html).not.toContain("DONE");
      expect(html).toContain('data-state="done"');
      expect(html).toContain("config.json");
      expect(html).toContain(":12");
      expect(html).toContain("+1");
      // A real minus sign, as Figma has it — a hyphen beside a lime plus reads
      // as a dash.
      expect(html).toContain("\u22121");
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
      expect(html).toContain('data-state="done"');
      expect(html).toContain('data-testid="agent-tool-call"');
      // No broken state or empty diff container
      expect(html).not.toContain("empty diff");
    });

    it("offers the full output when the page read truncated it", () => {
      const html = renderToString(
        <AgentToolCall
          sessionId="s1"
          call={{
            id: "call-big",
            kind: "execute",
            title: "Run tests",
            status: "completed",
            truncated: true,
          }}
          defaultExpanded
        />,
      );
      expect(html).toContain('data-testid="tool-call-load-full"');
      expect(html).toContain("Show full output");
      // Without a session the preview is all there is — nothing to fetch.
      const bare = renderToString(
        <AgentToolCall
          call={{ id: "call-big", kind: "execute", status: "completed", truncated: true }}
        />,
      );
      expect(bare).not.toContain('data-testid="tool-call-load-full"');
    });

    it("reads the full call from a detail row whether Payload arrives decoded or as text", () => {
      // Payload is a json.RawMessage: the bridge delivers it as an object, and
      // parsing String(object) left "Show full output" empty every time.
      const full = "line of output\n".repeat(2000);
      const payload = { kind: "tool_call", toolCall: { id: "call-big", rawOutput: full } };
      expect(toolOutputText(toolCallFromDetail(payload)?.rawOutput)).toBe(full);
      expect(toolOutputText(toolCallFromDetail(JSON.stringify(payload))?.rawOutput)).toBe(full);
      const json = { toolCall: { id: "call-mcp", rawOutput: { exit: 1 } } };
      expect(toolOutputText(toolCallFromDetail(json)?.rawOutput)).toBe('{\n  "exit": 1\n}');
    });

    it("says a diff was too large to preview instead of calling it unchanged", () => {
      const html = renderToString(
        <AgentToolCall
          sessionId="s1"
          call={{
            id: "call-edit",
            kind: "edit",
            title: "Edit main.go",
            status: "completed",
            truncated: true,
            content: [{ type: "diff", path: "main.go", truncated: true }],
          }}
          defaultExpanded
        />,
      );
      expect(html).toContain("main.go");
      expect(html).toContain("too large to preview");
      expect(html).not.toContain("unchanged");
      expect(html).not.toContain("No textual change");
      expect(html).toContain('data-testid="tool-call-load-full"');
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
      expect(runningHtml).toContain("running…");

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
      expect(awaitingHtml).toContain("awaiting permission");
    });

    it("keeps its output folded until asked", () => {
      // A directory listing opened in place turned one call into a screen of
      // text, and a turn makes dozens of calls.
      const call = {
        id: "call-5",
        kind: "execute",
        title: "find . -type d",
        status: "completed",
        content: [{ type: "content", content: { type: "text", text: "./deploy\n./docs" } }],
      };
      const folded = renderToString(<AgentToolCall call={call} />);
      expect(folded).toContain("find . -type d");
      expect(folded).toContain('aria-expanded="false"');
      expect(folded).not.toContain("./deploy");
      expect(renderToString(<AgentToolCall call={call} defaultExpanded />)).toContain("./deploy");
    });

    it("keeps a tool's whole output out of the one-line result slot", () => {
      // An MCP tool returns a JSON document. Figma's right-hand slot holds
      // "running…" or "exit 1"; a document in a nowrap, no-shrink span made
      // the row wider than the module and gave the transcript a horizontal
      // scrollbar.
      const blob = JSON.stringify({ result: [{ id: "caad65a3dc6457d655a02a64", name: "Chiro local" }] });
      const html = renderToString(
        <AgentToolCall call={{ id: "c1", title: "mcp__devkit__db_listProfiles", status: "completed", rawOutput: blob }} />,
      );
      expect(html).toContain("mcp__devkit__db_listProfiles");
      expect(html).not.toContain("caad65a3dc6457d655a02a64");
      // A short result still belongs there.
      const short = renderToString(
        <AgentToolCall call={{ id: "c2", title: "Read", status: "completed", rawOutput: "42 lines" }} />,
      );
      expect(short).toContain("42 lines");
    });

    it("shows an MCP tool's JSON indented, wrapped and capped instead of one cut-off line", () => {
      const blob = JSON.stringify({ result: [{ nodeId: "com.synx.devkit.ApplicationConfiguration.arbitrationPolicy" }] });
      expect(formatToolOutput(blob)).toEqual({
        code: '{\n  "result": [\n    {\n      "nodeId": "com.synx.devkit.ApplicationConfiguration.arbitrationPolicy"\n    }\n  ]\n}',
        language: "json",
        wrap: true,
      });
      // A preview cut mid-document is not JSON; it stays as it came.
      expect(formatToolOutput('{"result":[{"nodeId":"com.syn')).toEqual({
        code: '{"result":[{"nodeId":"com.syn',
        language: "text",
        wrap: true,
      });
      expect(formatToolOutput("./deploy\n./docs").language).toBe("text");

      const html = renderToString(
        <AgentToolCall
          call={{
            id: "c-mcp",
            title: "mcp__devkit__codegraph_searchSymbol",
            status: "completed",
            content: [{ type: "content", content: { type: "text", text: blob } }],
          }}
          defaultExpanded
        />,
      );
      expect(html).toContain("&quot;result&quot;: [");
      expect(html).toContain("whitespace-pre-wrap");
      expect(html).toContain("max-h-80");
      expect(html).toContain('data-testid="code-block-copy"');
    });

    it("shows a file a tool read as code in its language, numbers in a gutter, no fence", () => {
      const text = "```\n1\tpackage com.synx.devkit.replication;\n2\t\n3\tpublic final class ArbitrationPolicy {}\n```";
      expect(formatToolOutput(text, "/repo/src/ArbitrationPolicy.java")).toEqual({
        code: "package com.synx.devkit.replication;\n\npublic final class ArbitrationPolicy {}",
        language: "java",
        wrap: false,
        startLine: 1,
      });

      const html = renderToString(
        <AgentToolCall
          call={{
            id: "c-read",
            kind: "read",
            title: "Read ArbitrationPolicy.java",
            status: "completed",
            locations: [{ path: "/repo/src/ArbitrationPolicy.java", line: 1 }],
            content: [{ type: "content", content: { type: "text", text } }],
          }}
          defaultExpanded
        />,
      );
      expect(html).not.toContain("```");
      expect(html).toContain('data-testid="code-block-gutter"');
      expect(html).toMatch(/aria-hidden="true"[^>]*>1\n2\n3</);
      expect(html).toContain("public final class ArbitrationPolicy {}");
      expect(html).toContain(">java<");
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
      // A real minus sign, as Figma has it — a hyphen beside a lime plus reads
      // as a dash.
      expect(html).toContain("\u22121");
      expect(html).toContain("func old() {}");
      expect(html).toContain("func new() {}");
    });

    it("handles absent/empty texts cleanly", () => {
      const html = renderToString(<AgentToolDiff path="empty.txt" />);
      expect(html).toContain("empty.txt");
      expect(html).toContain("no textual change");
    });
  });

  describe("AgentToolTerminal", () => {
    it("heads with the command, and says nothing when the exit was clean", () => {
      const html = renderToString(
        <AgentToolTerminal
          command="mvn -q -pl billing-service test"
          terminalId="term-1"
          status="exited"
          exitCode={0}
        />,
      );
      expect(html).toContain("mvn -q -pl billing-service test");
      // A zero exit is the expected case; Figma gives it no label.
      expect(html).not.toContain("EXITED");
      expect(html).not.toContain("exit 0");
      expect(html).not.toContain("terminal output is available in the terminal");
    });

    it("falls back to the terminal id when no command reached us", () => {
      const html = renderToString(
        <AgentToolTerminal terminalId="term-1" status="failed" exitCode={1} />,
      );
      expect(html).toContain("term-1");
      expect(html).toContain("exit 1");
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

    it("does not make a location look clickable without an opener", () => {
      const html = renderToString(<AgentFileChip path="src/main.ts" line={8} />);
      expect(html).not.toContain("<button");
      const actionable = renderToString(
        <AgentFileChip path="src/main.ts" line={8} onClick={() => {}} />,
      );
      expect(actionable).toContain("<button");
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
      expect(html).toContain("1 of 3");
      // ACP's own status words, per step — that is where the agent is.
      expect(html).toContain("in progress");
      expect(html).toContain("pending");
      expect(html).toContain("Explore repository");
      expect(html).toContain("Implement components");
      expect(html).toContain("Run verification tests");
    });
  });

  describe("AgentSubagent", () => {
    it("pairs spawn and state into one card", () => {
      // Two updates say half a thing each: spawn carries the name and the
      // task, the state update carries only the id and the new state. Rendering
      // them as two rows would show "List directory files" once and
      // "completed" attached to nothing.
      let model = createInitialTranscriptModel();
      model = applySessionUpdate(model, {
        kind: "subagent_spawned",
        subagent: { id: "a3df", name: "List directory files", task: "List the files." },
      } as never);
      model = applySessionUpdate(model, {
        kind: "subagent_state_update",
        subagent: { id: "a3df", state: "completed" },
      } as never);

      expect(model.items).toHaveLength(1);
      expect(model.items[0].subagent).toEqual({
        id: "a3df",
        name: "List directory files",
        task: "List the files.",
        state: "completed",
      });

      const html = renderToString(
        <AgentSubagent subagent={model.items[0].subagent!} />,
      );
      expect(html).toContain("List directory files");
      expect(html).toContain("List the files.");
      expect(html).toContain(">completed<");
    });

    it("keeps running until told otherwise", () => {
      const html = renderToString(<AgentSubagent subagent={{ id: "x" }} />);
      expect(html).toContain('data-state="running"');
      expect(html).toContain("Subagent");
    });
  });

  describe("AgentAsyncTask", () => {
    it("folds spawn, progress and state into one row that says what it cost", () => {
      // Three kinds, three partial messages, one thing. Progress arrives many
      // times; drawing each one gave a turn full of the same task counting up.
      let model = createInitialTranscriptModel();
      model = applySessionUpdate(model, {
        kind: "async_task_spawned",
        asyncTask: { id: "t1", name: "Run the test suite", taskType: "bash", canStop: true },
      } as never);
      model = applySessionUpdate(model, {
        kind: "async_task_progress",
        asyncTask: { id: "t1", lastToolName: "Bash", usage: { totalTokens: 10, toolUses: 3, durationMs: 72_000 } },
      } as never);
      model = applySessionUpdate(model, {
        kind: "async_task_progress",
        asyncTask: { id: "t1", lastToolName: "Read", usage: { totalTokens: 12, toolUses: 4, durationMs: 80_000 } },
      } as never);
      expect(model.items).toHaveLength(1);
      expect(model.items[0].asyncTask?.name).toBe("Run the test suite");
      expect(model.items[0].asyncTask?.canStop).toBe(true);

      const running = renderToString(<AgentAsyncTask variant="row" task={model.items[0].asyncTask!} />);
      expect(running).toContain("Run the test suite");
      expect(running).toContain("Read"); // what it is doing now
      expect(running).toContain("4 tools · 1m 20s");
      expect(running).toContain('data-state="running"');

      model = applySessionUpdate(model, {
        kind: "async_task_state_update",
        asyncTask: { id: "t1", state: "completed", summary: "212 passed" },
      } as never);
      const done = renderToString(<AgentAsyncTask task={model.items[0].asyncTask!} />);
      expect(done).toContain('data-state="completed"');
      expect(done).toContain("212 passed"); // once done, what it found
      expect(done).not.toContain(">Read<");
    });

    it("comes back as one row after a reload, and so does a subagent", () => {
      // Stored one row per event; a reload used to file every one of them
      // under "unmodelled", so the card on screen a moment ago rendered as
      // nothing after a restart.
      const model = initTranscript([
        { ID: "1", Seq: 1, Kind: "async_task_spawned", Payload: { asyncTask: { id: "t1", name: "Lint" } } },
        { ID: "2", Seq: 2, Kind: "async_task_state_update", Payload: { asyncTask: { id: "t1", state: "failed" } } },
        { ID: "3", Seq: 3, Kind: "subagent_spawned", Payload: { subagent: { id: "s1", name: "Explore", task: "Map the repo" } } },
        { ID: "4", Seq: 4, Kind: "subagent_state_update", Payload: { subagent: { id: "s1", state: "completed" } } },
      ]);
      expect(model.items).toHaveLength(2);
      expect(model.items[0].asyncTask).toMatchObject({ id: "t1", name: "Lint", state: "failed" });
      expect(model.items[1].subagent).toEqual({ id: "s1", name: "Explore", task: "Map the repo", state: "completed" });
    });
  });
});
