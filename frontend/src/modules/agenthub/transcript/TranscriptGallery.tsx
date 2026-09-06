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

export function TranscriptGallery() {
  const sampleDiffOld = `package main

import "fmt"

func run() {
\tfmt.Println("Starting session...")
}
`;

  const sampleDiffNew = `package main

import (
\t"fmt"
\t"time"
)

func run() {
\tfmt.Println("Starting session with telemetry...")
\ttime.Sleep(100 * time.Millisecond)
}
`;

  return (
    <div
      className="flex flex-col gap-6 p-6 max-w-[760px] mx-auto overflow-y-auto"
      data-testid="transcript-gallery"
    >
      <div className="flex items-center justify-between border-b border-border-subtle pb-3">
        <div>
          <h3 className="text-title-16 text-text-primary">
            Agent Hub — Transcript Leaves Showcase
          </h3>
          <p className="text-caption-11 text-text-tertiary">
            Figma Page 17 &quot;Agent Hub&quot; — 11 Transcript Components
          </p>
        </div>
        <div className="flex items-center gap-1.5 text-caption-11">
          <span className="text-status-success font-mono">11 / 11 Mounted</span>
        </div>
      </div>

      {/* 1. Agent / Message (926:126) */}
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <span className="text-label-11-caps text-text-tertiary">
            1. Agent / Message (926:126) — Align Start (Agent) & Align End (User)
          </span>
        </div>
        <div className="flex flex-col gap-3 p-3 rounded-md bg-surface-base border border-border-subtle">
          <AgentMessage
            align="start"
            author="Claude 3.7 Sonnet"
            timestamp="10:42 AM"
            text="I've explored the workspace and located the database models. I am ready to implement the migration."
          />
          <AgentMessage
            align="end"
            author="You"
            timestamp="10:43 AM"
            text="Proceed with the migration and run the parity test."
          />
        </div>
      </section>

      {/* 2. Agent / Code Block (1023:2688) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          2. Agent / Code Block (1023:2688) — Default & Collapsed
        </span>
        <div className="flex flex-col gap-3">
          <AgentCodeBlock
            path="frontend/src/bridge/agenthub.ts"
            language="typescript"
            code={`export async function agentHubTranscript(sessionId: string): Promise<TranscriptEntry[]> {
  return callBridge<TranscriptEntry[]>("AgentHubTranscript", sessionId);
}`}
          />
          <AgentCodeBlock
            path="internal/agenthub/schema.sql"
            language="sql"
            defaultCollapsed={true}
            code={`CREATE TABLE IF NOT EXISTS agent_transcript (
  session_id TEXT NOT NULL,
  line_index INTEGER NOT NULL,
  kind TEXT NOT NULL,
  message_id TEXT,
  payload JSON NOT NULL,
  PRIMARY KEY (session_id, line_index)
);`}
          />
        </div>
      </section>

      {/* 3. Agent / Thinking (1029:2742) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          3. Agent / Thinking (1029:2742) — Collapsed & Expanded
        </span>
        <div className="flex flex-col gap-3">
          <AgentThinking
            text="Looking at internal/agenthub/update.go. We need to preserve unknown update kinds in Raw rather than erroring, and coalesce message chunks."
            duration="1.8s"
            defaultExpanded={false}
          />
          <AgentThinking
            text="The user requested verifying parity between frontend reducer and Go recorder. The golden test fixture matches SQLite rows against emitted session updates."
            duration="3.4s"
            defaultExpanded={true}
          />
        </div>
      </section>

      {/* 4. Agent / Tool Call (947:2214) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          4. Agent / Tool Call (947:2214) — States: Done, Running, Awaiting, Failed
        </span>
        <div className="flex flex-col gap-3">
          <AgentToolCall
            call={{
              id: "call-edit-1",
              kind: "edit",
              title: "Edit frontend/src/bridge/agenthub.ts",
              status: "completed",
              locations: [
                { path: "frontend/src/bridge/agenthub.ts", line: 128 },
                { path: "frontend/src/modules/agenthub/types.ts", line: 45 },
              ],
              content: [
                {
                  type: "diff",
                  path: "frontend/src/bridge/agenthub.ts",
                  oldText: `export async function agentHubTranscript() {}`,
                  newText: `export async function agentHubTranscript(id: string): Promise<TranscriptEntry[]> {\n  return callBridge("AgentHubTranscript", id);\n}`,
                },
              ],
            }}
          />

          <AgentToolCall
            call={{
              id: "call-read-1",
              kind: "read",
              title: "Read base-doc/content/projects/dev-kit/agent-hub.mdx",
              status: "completed",
            }}
          />

          <AgentToolCall
            call={{
              id: "call-run-1",
              kind: "execute",
              title: "bun test src/modules/agenthub/transcriptGolden.test.ts",
              status: "in_progress",
            }}
          />

          <AgentToolCall
            call={{
              id: "call-perm-1",
              kind: "execute",
              title: "git push origin feat/agent-hub-ui",
              status: "awaiting_permission",
            }}
          />
        </div>
      </section>

      {/* 5. Agent / Tool Diff (1029:2667) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          5. Agent / Tool Diff (1029:2667) — Line-by-line Diff
        </span>
        <AgentToolDiff
          path="cmd/daemon/main.go"
          oldText={sampleDiffOld}
          newText={sampleDiffNew}
        />
      </section>

      {/* 6. Agent / Tool Terminal (1029:2721) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          6. Agent / Tool Terminal (1029:2721) — Exited (0) & Running
        </span>
        <div className="flex flex-col gap-3">
          <AgentToolTerminal
            terminalId="bash #1 (bun test)"
            status="exited"
            exitCode={0}
            output={`bun test v1.4.0
src/modules/agenthub/transcriptGolden.test.ts:
  ✓ golden fixture parity with the Go recorder [1.82ms]
184 pass, 0 fail`}
          />
          <AgentToolTerminal
            terminalId="bash #2 (docker compose up)"
            status="running"
            output={`[+] Running 2/2
 ✔ Network devkit_default Created
 ⠋ Container devkit-db-1 Starting...`}
          />
        </div>
      </section>

      {/* 7. Agent / File Chip (1029:2722) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          7. Agent / File Chip (1029:2722) — With and Without Line
        </span>
        <div className="flex flex-wrap gap-2 p-3 rounded-md bg-surface-base border border-border-subtle">
          <AgentFileChip path="src/modules/agenthub/transcript.ts" line={104} />
          <AgentFileChip path="internal/agenthub/update.go" line={56} />
          <AgentFileChip path="README.md" />
          <AgentFileChip path="base-doc/content/projects/dev-kit/agent-hub.mdx" line={350} />
        </div>
      </section>

      {/* 8. Agent / Plan (947:2239) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          8. Agent / Plan (947:2239) — Execution Plan
        </span>
        <AgentPlan
          entries={[
            { content: "Read agent-hub.mdx specifications and contract", status: "completed", priority: "high" },
            { content: "Implement 11 transcript leaf components in frontend/src/modules/agenthub/transcript/", status: "completed", priority: "high" },
            { content: "Run golden fixture tests and build validation", status: "in_progress", priority: "medium" },
            { content: "Mount gallery in AgentHubView and verify visually", status: "pending", priority: "low" },
          ]}
        />
      </section>

      {/* 9. Agent / Invoke Result (948:2223) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          9. Agent / Invoke Result (948:2223) — Complete Result
        </span>
        <AgentInvokeResult
          summary="Successfully generated all 11 transcript leaf components. All unit tests passed and bundle performance budget verified."
          duration="3.2s"
          cost={{ amount: 0.0084 }}
          filesChanged={[
            "frontend/src/modules/agenthub/transcript/AgentMessage.tsx",
            "frontend/src/modules/agenthub/transcript/AgentCodeBlock.tsx",
            "frontend/src/modules/agenthub/transcript/AgentThinking.tsx",
            "frontend/src/modules/agenthub/transcript/AgentToolCall.tsx",
            "frontend/src/modules/agenthub/transcript/AgentToolDiff.tsx",
            "frontend/src/modules/agenthub/transcript/AgentToolTerminal.tsx",
            "frontend/src/modules/agenthub/transcript/AgentFileChip.tsx",
            "frontend/src/modules/agenthub/transcript/AgentPlan.tsx",
            "frontend/src/modules/agenthub/transcript/AgentInvokeResult.tsx",
            "frontend/src/modules/agenthub/transcript/AgentRateLimit.tsx",
            "frontend/src/modules/agenthub/transcript/AgentImage.tsx",
          ]}
        />
      </section>

      {/* 10. Agent / Rate Limit (1044:3195) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          10. Agent / Rate Limit (1044:3195) — Warning & Exhausted
        </span>
        <div className="flex flex-col gap-3">
          <AgentRateLimit
            level="warning"
            remaining={5}
            resetTime="4m 20s"
          />
          <AgentRateLimit
            level="exhausted"
            resetTime="14m 10s"
          />
        </div>
      </section>

      {/* 11. Agent / Image (1061:3228) */}
      <section className="flex flex-col gap-3">
        <span className="text-label-11-caps text-text-tertiary">
          11. Agent / Image (1061:3228) — 16:9 Frame & Missing Fallback
        </span>
        <div className="flex flex-wrap gap-4">
          <AgentImage
            src="data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='800' height='450' viewBox='0 0 800 450'><rect width='100%' height='100%' fill='%231d1d1d'/><circle cx='400' cy='225' r='80' fill='%23c6ff34'/><text x='400' y='235' font-family='sans-serif' font-size='20' font-weight='600' fill='%23020202' text-anchor='middle'>Agent Hub</text></svg>"
            name="architecture-preview.svg"
            mimeType="image/svg+xml"
            dimensions="800x450"
            size="4.2 KB"
          />
          <AgentImage
            name="pending-diagram.png"
            mimeType="image/png"
            size="0 KB"
          />
        </div>
      </section>
    </div>
  );
}
