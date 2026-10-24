import { useEffect, useMemo, useState } from "react";
import type { AgentSession, PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import { AgentHubContent } from "@/modules/agenthub/surfaces/AgentHubContent";
import { AgentRail } from "@/modules/agenthub/surfaces/AgentRail";
import { TrayStatusOverride, type TrayStatus } from "@/modules/agenthub/surfaces/composer/useTrayStatus";
import { appendChips, fileChip, imageChip, refChip } from "@/modules/agenthub/composerDraft";
import { seedAttachmentInfo } from "@/modules/agenthub/surfaces/composer/useAttachmentInfo";
import { useComposerImagesStore } from "@/stores/composerImages";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import type { SessionFailure, TranscriptItem } from "@/modules/agenthub/types";

/**
 * The real conversation (AgentHubContent) and rail (AgentRail) with the
 * composer cluster in every state it has: idle, streaming, a permission, a pile
 * of questions, a failure, a long queue, attachments, a long prompt, a draft —
 * in a box whose width and height can be set, so a narrow rail and a short pane
 * are one click. No bridge: the repositories, workspace and boards the context
 * bar shows come from TrayStatusOverride.
 * `?gallery#agenthub-composer-layout`.
 */

const SESSION: AgentSession = {
  id: "sess-layout",
  agentType: "claude",
  projectPath: "/work/billing/api",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: "Fix the retry test",
  acpSessionId: "acp-layout",
};

const at = (n: number) => new Date(Date.UTC(2026, 9, 1, 10, 0, n)).toISOString();

/** `turns` prompt/answer pairs with long enough answers that the transcript scrolls. */
function transcript(turns: number): TranscriptItem[] {
  const out: TranscriptItem[] = [];
  for (let i = 0; i < turns; i += 1) {
    out.push({ id: `u${i}`, kind: "user_message_chunk", text: `Question ${i + 1}: what does the retry policy do on a 503?`, createdAt: at(i * 10) });
    out.push({
      id: `a${i}`,
      kind: "agent_message_chunk",
      text: `Answer ${i + 1}. The policy retries idempotent requests up to three times with exponential backoff, starting at 200ms and capping at 2s. Non-idempotent requests are never retried unless they carry an idempotency key.\n\nLine two of answer ${i + 1} says a little more, so the turn is taller than a single line and the transcript has something to scroll.`,
      createdAt: at(i * 10 + 1),
    });
  }
  return out;
}

const STATUS: TrayStatus = {
  workspace: { id: "ws", name: "billing-platform", folders: 8, paths: ["/work/billing/api", "/work/billing/web"] },
  branch: "feature/retry-policy",
  repos: [
    { name: "api", path: "/work/billing/api", branch: "feature/retry-policy", detached: false, ahead: 2, behind: 0, changed: 3, conflicted: 0, error: "", own: true },
    { name: "web", path: "/work/billing/web", branch: "main", detached: false, ahead: 0, behind: 1, changed: 0, conflicted: 2, error: "", own: false },
    ...Array.from({ length: 6 }, (_, i) => ({
      name: `service-${i}`,
      path: `/work/billing/service-${i}`,
      branch: "main",
      detached: false,
      ahead: 0,
      behind: 0,
      changed: i < 4 ? i + 1 : 0,
      conflicted: 0,
      error: "",
      own: false,
    })),
  ],
  hasBoards: true,
  tasks: [],
  others: [],
};

const PERMISSION: PendingPermission = {
  id: "perm-1",
  sessionId: SESSION.id,
  caller: "agent:sess-layout",
  createdAt: new Date().toISOString(),
  expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
  title: "Fix the retry test",
  depth: 0,
  params: {
    toolCall: {
      title: "`./gradlew :billing-api:test --tests '*RetryPolicyTest' --info --stacktrace --rerun-tasks -Pprofile=ci -Dorg.gradle.jvmargs=-Xmx2g && ./gradlew :billing-api:integrationTest --info && ./scripts/publish-report.sh --upload --verbose --retry 5 --timeout 600`",
      rawInput: {
        command:
          "./gradlew :billing-api:test --tests '*RetryPolicyTest' --info --stacktrace --rerun-tasks -Pprofile=ci -Dorg.gradle.jvmargs=-Xmx2g && ./gradlew :billing-api:integrationTest --info && ./scripts/publish-report.sh --upload --verbose --retry 5 --timeout 600\n" +
          Array.from({ length: 12 }, (_, i) => `echo "step ${i + 1}: a long line of a long script that keeps going to make the command tall"`).join("\n"),
      },
    },
    options: [
      { optionId: "allow_once", name: "Allow once", kind: "allow_once" },
      { optionId: "allow_always", name: "Allow always", kind: "allow_always" },
      { optionId: "reject_once", name: "Reject", kind: "reject_once" },
    ],
  },
};

// Codex asking before an MCP tool runs, with a long message as its argument:
// the card folds it to three lines and "Show full request" opens all of it.
const PERMISSION_MESSAGE: PendingPermission = {
  ...PERMISSION,
  id: "perm-message",
  params: {
    toolCall: {
      toolCallId: "mcp-approval-9",
      title: "devkit.agenthub.sendMessage",
      kind: "other",
      status: "pending",
      content: [{ type: "content", content: { type: "text", text: 'Allow the devkit MCP server to run tool "agenthub.sendMessage"?' } }],
      rawInput: {
        sessionId: "8d2fcfa7-8e6a-4f5f-a8d6-460d798e85cc",
        message:
          "Bạn là session con của 8d2fcfa7-8e6a-4f5f-a8d6-460d798e85cc. Nhiệm vụ dễ, CHỈ ĐỌC / PHÂN TÍCH, không sửa file, không commit/push, không mở thêm agent.\n" +
          "User muốn từ Đề xuất mua ở medical-examination mở nhanh POS điền sẵn khách, thuốc, số lượng và ghi chú.\n" +
          Array.from({ length: 8 }, (_, i) => `${i + 1}. Tìm điểm vào, màn hình liên quan và nêu những file cần đổi cho bước ${i + 1}, kèm lý do và rủi ro.`).join("\n"),
      },
    },
    options: [
      { optionId: "allow_once", name: "Allow", kind: "allow_once" },
      { optionId: "allow_always", name: "Allow for this session", kind: "allow_always" },
      { optionId: "reject_once", name: "Deny", kind: "reject_once" },
    ],
  },
};

const choice = (n: number) => ({
  [`question_${n}`]: {
    type: "string",
    title: `Q${n}`,
    description: `Which retry strategy should question ${n} use, given that the downstream service rate-limits at ${n * 100} requests a second and the SLO is 99.9%?`,
    oneOf: [
      { const: "Alpha", title: "Exponential backoff with jitter", description: "The usual answer, and the one the client library implements" },
      { const: "Beta", title: "Fixed delay" },
      { const: "Gamma", title: "Circuit breaker only", description: "No retries at all; fail fast and let the caller decide" },
    ],
  },
  [`question_${n}_custom`]: {
    type: "string",
    title: "Other",
    _meta: { _askUserQuestionCustomAnswer: { questionId: `question_${n}`, isCustomAnswer: true } },
  },
});

function question(id: string, count: number): PendingElicitation {
  return {
    id,
    sessionId: SESSION.id,
    toolCallId: "toolu_1",
    message: "Please answer the following questions.",
    params: {
      mode: "form",
      message: "Please answer the following questions.",
      requestedSchema: { type: "object", properties: Object.assign({}, ...Array.from({ length: count }, (_, n) => choice(n))) },
    },
    caller: "agent:sess-layout",
    title: "Fix the retry test",
    depth: 0,
    createdAt: new Date().toISOString(),
  };
}

const FAILURE: SessionFailure = {
  id: "f1",
  revision: 1,
  category: "access",
  severity: "error",
  title: "Usage limit reached.",
  details: "You have used all your messages for now. The limit resets at 5pm.",
  reason: "rate_limited",
  actions: ["retry"],
};

/** A picture for the image chip: an inline SVG, so the gallery needs no file. */
const PICTURE = {
  id: "gallery-picture",
  name: "checkout-error.png",
  mimeType: "image/png",
  data: "",
  previewUrl: `data:image/svg+xml;utf8,${encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180"><rect width="320" height="180" fill="#27272a"/><text x="160" y="96" fill="#a1a1aa" font-size="20" text-anchor="middle" font-family="sans-serif">screenshot</text></svg>',
  )}`,
  size: 48_213,
};

const LONG_PDF = "/home/ptvu3/Workspace/fsap/digital-connector-platform/SAP-Final Architecture — Galaxy G Super App Platform (consolidated from ADR-0001…0054)-191026-031020.pdf";
const TOO_BIG = "/home/ptvu3/Workspace/fsap/exports/full-database-dump-2026-10-19.sql";

/**
 * A draft with every kind of chip, the way a person would have written it: the
 * long-named PDF from the redesign's screenshot, a file Go would refuse, an
 * image, a reference and a dozen source files. What Go would say about the
 * files is seeded, since the gallery has no bridge.
 */
function attachmentsDraft(): string {
  useComposerImagesStore.getState().add(PICTURE);
  seedAttachmentInfo({ path: LONG_PDF, name: LONG_PDF.split("/").pop()!, size: 2_203_648, kind: "pdf", problem: "" });
  seedAttachmentInfo({ path: TOO_BIG, name: "full-database-dump-2026-10-19.sql", size: 41_943_040, kind: "text", problem: "Over the 8 MiB limit for an attachment." });
  const sources = Array.from({ length: 12 }, (_, i) => `/work/src/main/java/com/billing/retry/Retry${i}Policy.java`);
  for (const path of sources) seedAttachmentInfo({ path, name: path.split("/").pop()!, size: 3_072 + fakeSize(path), kind: "text", problem: "" });
  let draft = "Compare ";
  draft = appendChips(draft, [fileChip(LONG_PDF)]);
  draft += "with the retry code, and why does this fail? ";
  draft = appendChips(draft, [imageChip(PICTURE), fileChip(TOO_BIG), refChip({ module: "codegraph", kind: "endpoint", id: "POST /v2/partner", label: "POST /v2/partner" })]);
  draft += "\nThe policies: ";
  return appendChips(draft, sources.map((path) => fileChip(path)));
}

function fakeSize(path: string) {
  return path.length * 7;
}

const QUEUE: QueuedAgentPrompt[] = Array.from({ length: 8 }, (_, i) => ({
  id: `q${i}`,
  text: `Queued message ${i + 1}: also check the integration tests and the docs for the retry policy, and tell me what changed`,
  attachments: [],
  images: [],
  refs: [],
}));

const COMMANDS = [
  { name: "compact", description: "Clear history but keep a summary" },
  { name: "review", description: "Review the pending changes" },
  { name: "init", description: "" },
];

type Scenario = "idle" | "streaming" | "permission" | "permissionmessage" | "permission-other" | "questions" | "failure" | "error" | "queue" | "attachments" | "longprompt" | "draft";
const SCENARIOS: Scenario[] = ["idle", "streaming", "permission", "permissionmessage", "permission-other", "questions", "failure", "error", "queue", "attachments", "longprompt", "draft"];
const LONG_PROMPT = Array.from({ length: 30 }, (_, i) => `line ${i + 1} of a long prompt`).join("\n");

export function AgentComposerLayoutSection() {
  const [scenario, setScenario] = useState<Scenario>("idle");
  const [width, setWidth] = useState(760);
  const [height, setHeight] = useState(640);
  const [turns, setTurns] = useState(14);
  const [prompt, setPrompt] = useState("");
  // The attachments scenario starts from a draft full of chips and lets go of it
  // the moment the field is edited — emptying it must leave it empty.
  const [edited, setEdited] = useState(false);
  const [project, setProject] = useState("/work/billing/api");
  const [mode, setMode] = useState("default");
  const [model, setModel] = useState("sonnet");

  const session = useMemo<AgentSession>(
    () => ({ ...SESSION, status: scenario === "error" ? "error" : scenario === "streaming" ? "running" : scenario === "permission" || scenario === "permissionmessage" || scenario === "permission-other" ? "awaiting_permission" : "idle" }),
    [scenario],
  );
  const items = useMemo(() => transcript(turns), [turns]);

  // The queues are the store's, as in the app: the tray counts "1 of 3" off them.
  useEffect(() => {
    const permissions =
      scenario === "permission"
        ? [PERMISSION, { ...PERMISSION, id: "perm-2" }, { ...PERMISSION, id: "perm-3" }]
        : scenario === "permission-other"
          ? [PERMISSION, { ...PERMISSION, id: "perm-other", sessionId: "sess-other", title: "Other session" }]
        : scenario === "permissionmessage"
          ? [PERMISSION_MESSAGE]
          : [];
    const questions = scenario === "questions" ? [question("q-1", 4), question("q-2", 2)] : [];
    useAgentSessionsStore.setState({ pendingQueue: permissions, elicitationQueue: questions });
    return () => useAgentSessionsStore.setState({ pendingQueue: [], elicitationQueue: [] });
  }, [scenario]);

  const common = {
    items,
    usage: { used: 581_900, size: 1_000_000, amount: 0.91 },
    commands: COMMANDS,
    promptValue:
      scenario === "longprompt" && prompt === ""
        ? LONG_PROMPT
        : scenario === "attachments" && !edited
          ? attachmentsDraft()
          : prompt,
    onPromptChange: (value: string) => {
      setEdited(true);
      setPrompt(value);
    },
    onSend: () => {},
    onCancel: () => {},
    streaming: scenario === "streaming",
    mode,
    availableModes: [
      { id: "default", name: "Ask", description: "Ask before editing files or running commands" },
      { id: "acceptEdits", name: "Auto", description: "Edit files without asking" },
      { id: "plan", name: "Plan", description: "Read-only until you approve a plan" },
    ],
    onSetMode: setMode,
    configOptions: [
      {
        id: "model",
        name: "Model",
        category: "model",
        currentValue: model,
        defaultValue: "sonnet",
        options: [
          { value: "sonnet", name: "Sonnet 4.5" },
          { value: "opus", name: "Opus 4.1" },
        ],
      },
    ],
    onSetConfigOption: (_: string, value: string) => setModel(value),
    onRefreshConfig: () => {},
    queuedPrompts: scenario === "queue" ? QUEUE : [],
    queuePaused: scenario === "queue",
    queuePausedReason: scenario === "queue" ? "Claude is not signed in" : undefined,
    onResumeQueued: () => {},
    onCancelQueued: () => {},
    onEditQueued: () => {},
    onBrowseContext: async () => [],
    onOpenFile: () => {},
    onCopy: () => {},
    goal: { objective: "a green build on CI", status: "active" as const },
    onSetGoal: () => {},
    onCompact: () => {},
  };

  const pendingPermission = scenario === "permission" || scenario === "permission-other" ? PERMISSION : scenario === "permissionmessage" ? PERMISSION_MESSAGE : null;
  const pendingElicitation = scenario === "questions" ? question("q-1", 4) : null;

  return (
    <section id="agenthub-composer-layout" className="flex flex-col gap-4 p-6">
      <h2 className="text-title-16 text-text-primary">Agent Hub — composer cluster</h2>
      <div className="flex flex-wrap items-center gap-2 text-caption-11 text-text-secondary">
        {SCENARIOS.map((name) => (
          <button
            key={name}
            type="button"
            data-testid={`layout-scenario-${name}`}
            aria-pressed={scenario === name}
            onClick={() => {
              setScenario(name);
              setEdited(false);
              setPrompt("");
            }}
            className="rounded-md border border-border-subtle px-2 py-1 aria-pressed:bg-state-soft aria-pressed:text-text-primary"
          >
            {name === "permission-other" ? "own + other" : name}
          </button>
        ))}
        <span className="mx-2">|</span>
        {[320, 480, 760, 1000].map((w) => (
          <button key={w} type="button" data-testid={`layout-width-${w}`} onClick={() => setWidth(w)} className="rounded-md border border-border-subtle px-2 py-1">
            w{w}
          </button>
        ))}
        {[260, 360, 520, 720].map((h) => (
          <button key={h} type="button" data-testid={`layout-height-${h}`} onClick={() => setHeight(h)} className="rounded-md border border-border-subtle px-2 py-1">
            h{h}
          </button>
        ))}
        <button type="button" data-testid="layout-append" onClick={() => setTurns((n) => n + 1)} className="rounded-md border border-border-subtle px-2 py-1">
          append turn
        </button>
        {/* 250 turns is 500 rows: past TRANSCRIPT_VIRTUALIZE_ITEMS, so the virtualised stream. */}
        {[14, 250].map((n) => (
          <button key={n} type="button" data-testid={`layout-turns-${n}`} onClick={() => setTurns(n)} className="rounded-md border border-border-subtle px-2 py-1">
            {n} turns
          </button>
        ))}
        <button
          type="button"
          data-testid="layout-more-permission"
          onClick={() =>
            useAgentSessionsStore.setState((state) => ({
              pendingQueue: [...state.pendingQueue, { ...PERMISSION, id: `perm-${state.pendingQueue.length + 1}` }],
            }))
          }
          className="rounded-md border border-border-subtle px-2 py-1"
        >
          another permission
        </button>
      </div>
      <TrayStatusOverride.Provider value={STATUS}>
        <div className="flex flex-wrap items-start gap-6">
          <div
            data-testid="layout-hub"
            style={{ width, height }}
            className="relative overflow-hidden rounded-control border border-border-subtle bg-surface-base"
          >
            {scenario === "permission-other" ? (
              <div className="absolute right-3 top-3 z-10 rounded-full border border-border-subtle bg-surface-overlay px-3 py-1.5 text-caption-11-medium text-text-secondary shadow-sm" data-testid="layout-other-permission-pill">
                1 request waiting in Other session
              </div>
            ) : null}
            <AgentHubContent
              {...common}
              session={scenario === "draft" ? null : session}
              draft={scenario === "draft" ? { projectPath: project } : null}
              projects={["/work/billing/api", "/work/billing/web"]}
              onProjectChange={setProject}
              onChooseProject={() => setProject("/work/picked")}
              agents={["claude", "codex"]}
              agent="claude"
              onAgentChange={() => {}}
              failure={scenario === "failure" ? FAILURE : null}
              onFailureAction={() => {}}
              errorCause="crashed"
              errorMessage={scenario === "error" ? "The agent process exited with code 1" : undefined}
              onRetrySession={() => {}}
              pendingPermission={pendingPermission}
              onResolvePermission={() => {}}
              pendingElicitation={pendingElicitation}
              onRespondElicitation={() => {}}
            />
          </div>
          <div data-testid="layout-rail" style={{ width: 340, height }} className="overflow-hidden rounded-control border border-border-subtle bg-surface-base">
            <AgentRail
              {...common}
              activeSession={session}
              failure={null}
              pendingPermission={pendingPermission}
              onResolvePermission={() => {}}
              pendingElicitation={pendingElicitation}
              onRespondElicitation={() => {}}
            />
          </div>
        </div>
      </TrayStatusOverride.Provider>
    </section>
  );
}
