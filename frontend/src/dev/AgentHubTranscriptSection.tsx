import type { AgentSession } from "@/bridge/agenthub";
import { AgentHubContent } from "@/modules/agenthub/surfaces/AgentHubContent";
import type { TranscriptItem } from "@/modules/agenthub/types";

/**
 * The Agent Hub conversation with a finished turn and a live one, through the
 * real AgentHubContent — header, folded work, prose answer, orb — without a
 * bridge or an agent. `?gallery#agenthub-transcript`.
 */
const session: AgentSession = {
  id: "sess-gallery",
  agentType: "claude",
  projectPath: "/Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-backend",
  status: "running",
  parentSessionId: null,
  depth: 0,
  title: "Dev-kit-backend",
  acpSessionId: "acp-gallery",
};

const at = (second: number) => new Date(Date.UTC(2026, 8, 14, 10, 0, second)).toISOString();

const items: TranscriptItem[] = [
  { id: "u1", kind: "user_message_chunk", text: "xin chào", createdAt: at(0) },
  {
    id: "a1",
    kind: "agent_message_chunk",
    text: "Xin chào!\n\nMình đang ở thư mục `dev-kit-backend`. Bạn cần mình giúp gì hôm nay?",
    createdAt: at(2),
    updatedAt: at(3),
  },
  { id: "u2", kind: "user_message_chunk", text: "Khám phá cấu trúc repo này", createdAt: at(10) },
  { id: "th1", kind: "agent_thought_chunk", text: "Start with the directory tree, then the build files.", createdAt: at(11) },
  {
    id: "n1",
    kind: "agent_message_chunk",
    text: "I'll start by exploring the repository structure and key config files.",
    createdAt: at(12),
  },
  {
    id: "tc1",
    kind: "tool_call",
    toolCallId: "c1",
    toolCall: {
      id: "c1",
      kind: "execute",
      title: "find . -type d -name node_modules -prune -o -type d -name .git -prune -o -type d -print | head -200",
      status: "completed",
      content: [{ type: "content", content: { type: "text", text: "./deploy\n./docs\n./gradle\n./src\n./gateway" } }],
    },
    createdAt: at(13),
    updatedAt: at(20),
  },
  {
    id: "tc2",
    kind: "tool_call",
    toolCallId: "c2",
    toolCall: { id: "c2", kind: "read", title: "Read build.gradle.kts", status: "completed", rawOutput: "118 lines" },
    createdAt: at(21),
    updatedAt: at(247),
  },
  {
    id: "a2",
    kind: "agent_message_chunk",
    text:
      "## Cấu trúc\n\nĐây là một project **Gradle** với các module chính:\n\n" +
      "- `gateway` — API gateway\n- `keycloak` — cấu hình xác thực\n- `deploy/server` — script triển khai\n\n" +
      "```bash\n./gradlew build\n```",
    createdAt: at(248),
    updatedAt: at(250),
  },
  { id: "u3", kind: "user_message_chunk", text: "Chạy test của module gateway", createdAt: at(300) },
  {
    id: "tc3",
    kind: "tool_call",
    toolCallId: "c3",
    toolCall: { id: "c3", kind: "execute", title: "./gradlew :gateway:test", status: "in_progress" },
    createdAt: at(302),
  },
];

export function AgentHubTranscriptSection() {
  return (
    <section id="agenthub-transcript" className="flex flex-col gap-3">
      <h2 className="text-title-16 text-text-primary">Agent Hub — conversation</h2>
      <div className="h-180 overflow-hidden rounded-control border border-border-subtle bg-surface-base">
        <AgentHubContent
          session={session}
          items={items}
          usage={{ used: 581_900, size: 1_000_000, amount: 0.91 }}
          commands={[{ name: "compact", description: "Clear history but keep a summary" }]}
          onCompact={() => {}}
          streaming
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
          onCloseSession={() => {}}
          onCopy={() => {}}
        />
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — starting a session</h2>
      <div
        data-testid="gallery-agenthub-draft"
        className="h-120 overflow-hidden rounded-control border border-border-subtle bg-surface-base"
      >
        <AgentHubContent
          session={null}
          draft={{ projectPath: session.projectPath }}
          agents={["claude", "mock"]}
          agent="claude"
          onAgentChange={() => {}}
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
        />
      </div>
    </section>
  );
}
