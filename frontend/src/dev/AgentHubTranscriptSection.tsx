import { useState } from "react";
import type { AgentSession } from "@/bridge/agenthub";
import { AgentHubContent } from "@/modules/agenthub/surfaces/AgentHubContent";
import { AgentSidebarTree } from "@/modules/agenthub/surfaces/AgentSidebarTree";
import { AgentPermissionRequest } from "@/modules/agenthub/surfaces/AgentPermissionRequest";
import { PermissionPreview } from "@/modules/agenthub/surfaces/PermissionPreview";
import { cn } from "@/lib/utils";
import { AgentSessionError } from "@/modules/agenthub/surfaces/AgentSessionError";
import { AgentSessionFailure } from "@/modules/agenthub/surfaces/AgentSessionFailure";
import { AgentToolDiff } from "@/modules/agenthub/transcript/AgentToolDiff";
import type { TranscriptItem } from "@/modules/agenthub/types";
import { draftConfigOptions, initialModelConfig } from "@/modules/agenthub/modelSettings";
import type { ConfigOption } from "@/modules/agenthub/types";

const modelCatalog: ConfigOption[] = [{ id: "model", category: "model", name: "Model", currentValue: "sol", defaultValue: "sol", options: [
  { value: "sol", name: "GPT-6.1 Sol", defaultEffort: "medium", supportedEfforts: ["low", "medium", "high", "xhigh", "max"].map((value) => ({ value })) },
  { value: "astra", name: "GPT-6 Astra", defaultEffort: "high", supportedEfforts: ["low", "medium", "high", "xhigh", "max", "ultra"].map((value) => ({ value })) },
  { value: "luna", name: "GPT-6 Luna", defaultEffort: "low", supportedEfforts: ["low", "medium", "high"].map((value) => ({ value })) },
] }];


const LONG_SEND_MESSAGE_INPUT = {
  sessionId: "8d2fcfa7-8e6a-4f5f-a8d6-460d798e85cc",
  message:
    "Bạn là session con của 8d2fcfa7-8e6a-4f5f-a8d6-460d798e85cc. Nhiệm vụ dễ, CHỈ ĐỌC / PHÂN TÍCH, không sửa file, không commit/push, không mở thêm agent.\n" +
    "User muốn từ Đề xuất mua ở medical-examination mở nhanh POS điền sẵn khách, thuốc, số lượng và ghi chú.\n" +
    "Hãy tìm điểm vào, các màn hình liên quan và nêu những file cần đổi.",
};

const PERMISSION_OPTIONS = [
  { optionId: "allow_once", name: "Allow", kind: "allow_once" },
  { optionId: "allow_always", name: "Allow for this session", kind: "allow_always" },
  { optionId: "reject_once", name: "Deny", kind: "reject_once" },
];

const mcpParams = (tool: string, rawInput: Record<string, unknown>) => ({
  toolCall: {
    toolCallId: `mcp-approval-${tool}`,
    title: `devkit.${tool}`,
    kind: "other",
    status: "pending",
    content: [{ type: "content", content: { type: "text", text: `Allow the devkit MCP server to run tool "${tool}"?` } }],
    rawInput,
  },
  options: PERMISSION_OPTIONS,
});

const PERMISSION_STATES: { name: string; width: string; size: "rail" | "wide"; params: unknown }[] = [
  { name: "short", width: "w-96 max-w-full", size: "wide", params: mcpParams("db.query", { profileId: "0123456789abcdef01234567", sql: "select 1" }) },
  { name: "long message", width: "w-180 max-w-full", size: "wide", params: mcpParams("agenthub.sendMessage", LONG_SEND_MESSAGE_INPUT) },
  {
    name: "JSON arguments",
    width: "w-120 max-w-full",
    size: "wide",
    params: mcpParams("api.saveRequest", {
      collectionId: "c-1",
      request: { method: "POST", url: "https://api.example.test/v1/refunds", headers: [{ name: "Content-Type", value: "application/json" }], body: { orderId: "o-1042", amount: 1999, reason: "duplicate charge" } },
    }),
  },
  {
    name: "long command",
    width: "w-120 max-w-full",
    size: "wide",
    params: { toolCall: { title: "`./gradlew test`", rawInput: { command: "./gradlew :billing-api:test --tests '*RetryPolicyTest' --info --stacktrace && ./scripts/publish-report.sh --upload --verbose\necho done" } }, options: PERMISSION_OPTIONS },
  },
  { name: "narrow rail", width: "w-66", size: "rail", params: mcpParams("agenthub.sendMessage", LONG_SEND_MESSAGE_INPUT) },
];

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
  // The session moved to another account before the next prompt (see Accounts & limits below).
  {
    id: "an1",
    kind: "devkit_account_notice",
    accountNotice: { kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" },
    createdAt: at(8),
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
    id: "tc2b",
    kind: "tool_call",
    toolCallId: "c2b",
    toolCall: {
      id: "c2b",
      kind: "read",
      title: "Read ArbitrationPolicy.java",
      status: "completed",
      locations: [{ path: "/Users/syuro/Workspace/PERSONAL/dev-kit/dev-kit-backend/src/main/java/com/synx/devkit/replication/domain/service/ArbitrationPolicy.java", line: 1 }],
      content: [
        {
          type: "content",
          content: {
            type: "text",
            // The shape Claude's Read sends: a fence, every line numbered with a tab.
            text: [
              "```",
              "1\tpackage com.synx.devkit.replication.domain.service;",
              "2\t",
              "3\timport java.util.Optional;",
              "4\t",
              "5\t/** Stateless version arbitration shared by unit tests and the push use case. */",
              "6\tpublic final class ArbitrationPolicy {",
              "7\t    public ArbitrationDecision decide(Optional<EntityHead> head, ReplicationOperation incoming) {",
              '8\t        if (head.isEmpty()) return ArbitrationDecision.accept("first write", 1);',
              "9\t        return head.get().version() >= incoming.version() ? ArbitrationDecision.reject() : ArbitrationDecision.accept();",
              "10\t    }",
              "11\t}",
              "```",
            ].join("\n"),
          },
        },
      ],
    },
    createdAt: at(247),
    updatedAt: at(247),
  },
  {
    id: "tc2c",
    kind: "tool_call",
    toolCallId: "c2c",
    toolCall: {
      id: "c2c",
      kind: "other",
      title: "mcp__devkit__codegraph_searchSymbol",
      status: "completed",
      content: [
        {
          type: "content",
          content: {
            type: "text",
            text: JSON.stringify({
              result: [
                { nodeId: "com.synx.devkit.bootstrap.configuration.ApplicationConfiguration.arbitrationPolicy", kind: "method", file: "ApplicationConfiguration.java", line: 113 },
                { nodeId: "com.synx.devkit.replication.domain.service.ArbitrationPolicy", kind: "class", file: "ArbitrationPolicy.java", line: 15 },
              ],
            }),
          },
        },
      ],
    },
    createdAt: at(247),
    updatedAt: at(248),
  },
  {
    id: "a2",
    kind: "agent_message_chunk",
    text:
      "## Cấu trúc\n\nĐây là một project **Gradle** với các module chính:\n\n" +
      "- `gateway` — API gateway\n- `keycloak` — cấu hình xác thực\n- `deploy/server` — script triển khai\n\n" +
      "```bash\n./gradlew build\n```\n\n" +
      "| Process | Vai trò | Modules |\n| --- | --- | ---: |\n" +
      "| root (`src/`) | Sync API — Hexagonal Architecture | 5 |\n" +
      "| `gateway/` | Spring Cloud Gateway MVC — validate Keycloak token, strip client identity header, ký internal JWT RS256 (TTL ~45s), publish JWKS | 1 |",
    createdAt: at(248),
    updatedAt: at(250),
  },
  { id: "u2b", kind: "user_message_chunk", text: "Vẽ luồng nạp plugin lúc runtime", createdAt: at(252) },
  {
    id: "a2b",
    kind: "agent_message_chunk",
    text:
      "## Hôm nay code đang chạy thế nào\n\n" +
      "```mermaid\nsequenceDiagram\n  participant Dev as Dev (bundleToRuntime)\n  participant RT as runtime\n  participant DB as bảng plugins\n  participant S3\n" +
      "  Dev->>RT: POST /v1/cells {programId, versionId, uri: s3://bất kỳ}\n  RT->>DB: lưu (không auth, không digest)\n" +
      "  Note over RT: ...sau đó, có request đến...\n  RT->>S3: tải JAR (chỉ kiểm Content-Type)\n  RT->>RT: PF4J load JAR → commence()\n```\n\n" +
      "Một diagram mà Mermaid từ chối thì vẫn là khối code:\n\n" +
      "```mermaid\nsequenceDiagram\n  Dev->>\n```\n\n" +
      "| Thành phần | Nơi |\n| --- | --- |\n" +
      "| `DispatchFunction<I,O>`: commence(in), domainId() | `core/.../DispatchFunction.java` |\n" +
      "| `RouterId(domain, program)` | `core/…/RouterId.java` |",
    createdAt: at(253),
    updatedAt: at(255),
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
  // The draft's composer is live, so typing into it shows the field growing.
  const [draftPrompt, setDraftPrompt] = useState("");
  const [draftProject, setDraftProject] = useState(session.projectPath);
  const [activeId, setActiveId] = useState(session.id);
  const [modelSelection, setModelSelection] = useState<Record<string, string>>({});
  const modelOptions = draftConfigOptions(modelCatalog, modelSelection);
  return (
    <section id="agenthub-transcript" className="flex flex-col gap-3">
      <h2 className="text-title-16 text-text-primary">Agent Hub — conversation</h2>
      <div className="h-180 overflow-hidden rounded-control border border-border-subtle bg-surface-base">
        <AgentHubContent
          session={session}
          items={items}
          usage={{
            used: 581_900,
            size: 1_000_000,
            amount: 0.91,
            rateLimit: { status: "allowed_warning", resetsAt: Math.round(Date.now() / 1000) + 3 * 3600, utilization: 82 },
          }}
          goal={{ objective: "a green build on CI", status: "active" }}
          onSetGoal={() => {}}
          commands={[
            { name: "compact", description: "Clear history but keep a summary" },
            {
              name: "deep-research",
              description:
                "Deep research harness — fan-out web searches, fetch sources, adversarially verify claims, synthesise a cited report. This runs a converter that bundles the real composer code from your own codebase, so what you see is what ships, not a description of it.",
            },
            { name: "dataviz", description: "Use whenever you are about to create ANY chart, graph, plot, dashboard or data visualization, in ANY output medium." },
            { name: "init", description: "" },
          ]}
          onCompact={() => {}}
          streaming
          promptValue=""
          onPromptChange={() => {}}
          onSend={() => {}}
          onCancel={() => {}}
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
          draft={{ projectPath: draftProject }}
          projects={[session.projectPath, "/Users/dev/billing-service", "/Users/dev/work/api"]}
          onProjectChange={setDraftProject}
          onChooseProject={() => setDraftProject("/Users/dev/picked-folder")}
          agents={["codex"]}
          agent="codex"
          onAgentChange={() => {}}
          configOptions={modelOptions}
          onSetConfigOption={(id, value) => setModelSelection((previous) => initialModelConfig(draftConfigOptions(modelCatalog, { ...previous, [id]: value })))}
          onRefreshConfig={() => {}}
          promptValue={draftPrompt}
          onPromptChange={setDraftPrompt}
          onSend={() => {}}
          onCancel={() => {}}
        />
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — what the vendor reports</h2>
      <div data-testid="gallery-agenthub-failure" className="flex w-full flex-col gap-3">
        <AgentSessionFailure
          failure={{
            id: "f1",
            revision: 1,
            category: "access",
            severity: "error",
            title: "Sign in to continue using Claude.",
            details: "The session's credentials expired.",
            reason: "auth_required",
            actions: ["login"],
          }}
          onAction={() => {}}
          onDismiss={() => {}}
        />
        <AgentSessionFailure
          failure={{
            id: "f2",
            revision: 1,
            category: "limit",
            severity: "warning",
            title: "Claude is temporarily rate limited.",
            details: "The five-hour window is 82% used.",
            actions: ["retry", "new_session"],
          }}
          onAction={() => {}}
        />
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — a file a tool changed</h2>
      <div data-testid="gallery-agenthub-tool-diff" className="w-180 max-w-full">
        <AgentToolDiff
          path="src/main/java/com/acme/billing/RefundPolicy.java"
          oldText={"public final class RefundPolicy {\n    private static final int WINDOW_DAYS = 30;\n\n    public boolean refundable(Order order) {\n        return order.ageInDays() <= WINDOW_DAYS;\n    }\n}\n"}
          newText={"public final class RefundPolicy {\n    private static final int WINDOW_DAYS = 45;\n\n    public boolean refundable(Order order) {\n        if (order.isGift()) {\n            return true;\n        }\n        return order.ageInDays() <= WINDOW_DAYS;\n    }\n}\n"}
        />
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — session error</h2>
      <div data-testid="gallery-agenthub-error" className="flex w-full flex-col gap-4">
        <AgentSessionError
          cause="crashed"
          agentName="Dev-kit-backend"
          reason="agenthub.processFailed — the agent exited after session/cancel timed out."
          onRetry={() => {}}
        />
        <AgentSessionError
          cause="notInstalled"
          agentName="Claude"
          reason="agent_not_installed — Claude Agent is not installed. Install it in Settings → Agents."
          onOpenSettings={() => {}}
          onClose={() => {}}
        />
        <div className="w-62">
          <AgentSessionError cause="crashed" agentName="docs-sweep" onRetry={() => {}} />
        </div>
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — permission to edit a file</h2>
      <div data-testid="gallery-agenthub-permission-edit" className="w-180 max-w-full">
        <AgentPermissionRequest
          size="wide"
          session={session}
          onResolve={() => {}}
          pending={{
            id: "perm-edit",
            sessionId: session.id,
            caller: `agent:${session.id}`,
            title: "",
            depth: 0,
            createdAt: "",
            expiresAt: "",
            params: {
              toolCall: {
                title: "Edit src/config.ts",
                content: [
                  {
                    type: "diff",
                    path: `${session.projectPath}/src/config.ts`,
                    oldText: "export const timeout = 30;\nexport const retries = 2;",
                    newText: "export const timeout = 60;\nexport const retries = 3;\nexport const backoff = \"exponential\";",
                  },
                ],
              },
              options: [
                { optionId: "allow_once", name: "Allow once", kind: "allow_once" },
                { optionId: "allow_always", name: "Allow always", kind: "allow_always" },
              ],
            },
          }}
        />
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — permission request, long input</h2>
      <div className="flex flex-wrap items-start gap-4" data-testid="gallery-agenthub-permission-states">
        {PERMISSION_STATES.map((state) => (
          <figure key={state.name} className={cn("m-0 flex flex-col gap-1", state.width)}>
            <figcaption className="text-caption-11 text-text-tertiary">{state.name}</figcaption>
            <div className="overflow-hidden rounded-control border border-border-subtle bg-surface-raised">
              <AgentPermissionRequest
                panel
                size={state.size}
                session={session}
                onResolve={() => {}}
                onMinimize={() => {}}
                pending={{ id: `perm-${state.name}`, sessionId: session.id, caller: `agent:${session.id}`, title: "", depth: 0, createdAt: "", expiresAt: "", params: state.params }}
              />
            </div>
          </figure>
        ))}
        <figure className="m-0 flex w-96 max-w-full flex-col gap-1">
          <figcaption className="text-caption-11 text-text-tertiary">expanded (static, defaultOpen)</figcaption>
          <PermissionPreview defaultOpen testId="permission-args" text={JSON.stringify(LONG_SEND_MESSAGE_INPUT)} />
        </figure>
      </div>
      <h2 className="text-title-16 text-text-primary">Agent Hub — swarm rail</h2>
      <div data-testid="gallery-agenthub-rail" className="h-80 w-66 overflow-hidden rounded-control border border-border-subtle bg-(--background-app) px-2">
        <AgentSidebarTree
          sessions={[
            session,
            { ...session, id: "sess-2", title: "Refund worker", status: "running" },
            { ...session, id: "sess-3", title: "Fix flaky test", status: "closed", parentSessionId: session.id, depth: 1 },
            { ...session, id: "sess-4", title: "Scratch idea", projectPath: "/cfg/DevKit/agent-scratch/20260918-1" },
          ]}
          scratchRoot="/cfg/DevKit/agent-scratch"
          activeSessionId={activeId}
          onSelectSession={setActiveId}
          onNewSession={() => {}}
        />
      </div>
    </section>
  );
}
