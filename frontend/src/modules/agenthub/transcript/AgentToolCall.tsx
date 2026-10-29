import { Shimmer } from "@/components/ui/shimmer";
import * as React from "react";
import { BookOpen, Bot, ChevronRight, FileEdit, Globe, Search, Shrink, Terminal, Wrench } from "lucide-react";

import { cn } from "@/lib/utils";
import { AnimatedCollapse } from "@/components/ui/animated-collapse";
import { ErrorBoundary } from "@/components/ui/error-boundary";
import { resultCallOf, useFullToolCall } from "./toolCallDetail";
import type { ToolCall } from "../types";
import { commandDescription, isCompaction, stepLabel, toolCommand } from "../turns";
import { AgentFileChip } from "./AgentFileChip";
import { AgentToolDiff } from "./AgentToolDiff";
import { AgentToolTerminal } from "./AgentToolTerminal";
import { AgentCodeBlock } from "./AgentCodeBlock";
import { languageForPath, normalizeLanguage } from "@/components/code/languages";
import { parseListing } from "./syntax";
import { failureLine, settleToolState, useSettleFacts, type SettledToolState } from "./toolSettle";
import { hasToolResultView, renderToolResult } from "@/workbench/toolResultViews";
import { isDevkitTool, toolNameOf, toolPayloadOf, toolResultInputOf } from "@/workbench/toolPayload";

export type AgentToolCallProps = {
  call: ToolCall;
  /**
   * Whether the output shows before anyone asks. Off by default: a directory
   * listing or a test log opened in place turned one tool call into a screen of
   * text, and a turn makes dozens of them. The row says what ran and how it
   * went; the output is one click away for the call you actually care about.
   */
  defaultExpanded?: boolean;
  /**
   * `card` is Figma's standalone tool call. `row` is one step inside a turn's
   * work: an icon, a verb and what it acted on — "Read hook.go" — on the page's
   * own ground. Forty cards in a list read as forty separate things; forty rows
   * read as one piece of work.
   */
  variant?: "card" | "row";
  /**
   * Whose transcript this call lives in — the page read truncated oversized
   * raw fields to a preview, and this is the handle for fetching the whole
   * row on demand.
   */
  sessionId?: string;
  /** The transcript item this row draws, as `data-step-id` — how a run's "+N more" line finds it. */
  stepId?: string;
  /**
   * The call's result view already stands as a card after the work fold
   * (StepGroup): inside the fold the row is a plain row, its own output and
   * "Show full output" — a second copy of the card would hide both.
   */
  resultOutside?: boolean;
  /** The subagent the call belongs to, when it is a subagent's: it is settled only by its turn ending (toolSettle.tsx). */
  origin?: string;
  onOpenFile?: (path: string, line?: number) => void;
  className?: string;
};

/**
 * Agent / Tool Call — Figma `947:2214`, states `947:2191` Running,
 * `947:2205` Done, `947:2213` Failed.
 *
 * Figma's note: "every session/update tool_call carries a status, so the
 * transcript shows the whole arc — queued, running, blocked on permission,
 * finished, failed. Failed keeps its reason inline; a red dot alone tells you
 * nothing to act on."
 *
 * One row: dot, kind icon, tool name, then the right-hand slot carrying
 * whatever the status has to say — "running…", the result, "exit 1". That slot
 * is the status, so there is no uppercase DONE/FAILED badge: it would say the
 * same thing twice and leave no room for the result.
 *
 * Failed is the only state that changes the ground (feedback/danger-soft) and
 * the only one that adds a second line — the reason.
 *
 * Dots, from the exported SVGs: running --status-running (text/primary), done
 * text/tertiary, failed status/danger. Done is deliberately grey: a
 * finished tool call is not good news, it is just finished.
 */
function kindIcon(call: ToolCall) {
  if (isCompaction(call)) return Shrink;
  const k = call.kind?.toLowerCase() ?? "";
  if (k.includes("read") || k.includes("view") || k.includes("cat")) return BookOpen;
  if (k.includes("edit") || k.includes("write") || k.includes("patch")) return FileEdit;
  if (k.includes("exec") || k.includes("bash") || k.includes("run") || k.includes("command"))
    return Terminal;
  if (k.includes("search") || k.includes("grep") || k.includes("glob") || k.includes("find"))
    return Search;
  if (k.includes("fetch")) return Globe;
  // claude-agent-acp gives its Task/Agent tool the `think` kind: a subagent.
  // (Compaction shares the kind and is caught above by its name.)
  if (k.includes("think")) return Bot;
  return Wrench;
}

type ToolState = SettledToolState;

function toToolState(status?: string): ToolState {
  switch (status?.toLowerCase()) {
    case "pending":
      return "pending";
    case "in_progress":
    case "running":
      return "running";
    case "awaiting_permission":
    case "awaiting":
      return "awaiting";
    case "failed":
    case "error":
      return "failed";
    default:
      return "done";
  }
}

/**
 * The three states an activity card draws (workbench/toolActivity.ts): queued,
 * running and awaiting permission are all "under way" to someone watching.
 */
export function toolActivityState(status?: string): "running" | "done" | "failed" {
  const state = toToolState(status);
  return state === "done" ? "done" : state === "failed" ? "failed" : "running";
}

const DOT: Record<ToolState, string> = {
  pending: "border border-text-tertiary",
  unfinished: "border border-border",
  running: "bg-(--status-running)",
  awaiting: "bg-status-warning",
  done: "bg-text-tertiary",
  failed: "bg-status-danger",
};

/** What the right-hand slot says when the call has no result of its own. */
const FALLBACK: Record<ToolState, string> = {
  pending: "queued",
  running: "running…",
  awaiting: "awaiting permission",
  unfinished: "didn't finish",
  done: "",
  failed: "failed",
};

/**
 * Whether rawOutput can stand in the header's status slot.
 *
 * A JSON document never can, however short: an MCP tool answers with one, and
 * `{"result":[{"id":"caad65a3…"}]}` is both unreadable at 11px and unbounded —
 * it is the payload, not a summary of it. Length alone was the wrong test; a
 * 67-character document passed it.
 */
function isOneLineStatus(raw: string): boolean {
  const text = raw.trim();
  if (!text || text.includes("\n") || text.length > 60) return false;
  return !text.startsWith("{") && !text.startsWith("[");
}

export { toolCallFromDetail } from "./toolCallDetail";

/** rawOutput as text: vendor JSON of any shape, strings kept as they are. */
export function toolOutputText(raw: unknown): string {
  if (raw === undefined || raw === null) return "";
  return typeof raw === "string" ? raw : JSON.stringify(raw, null, 2);
}

export type ToolOutputView = { code: string; language: string; wrap: boolean; startLine?: number };

/**
 * Tool output as it should read.
 *
 * - A JSON document, indented and wrapped. MCP tools answer with one minified
 *   line of it; a preview cut mid-document does not parse and stays as text.
 * - A listing (Claude's Read: a fenced file with every line numbered), as the
 *   code alone with its numbers in a gutter, in the file's language.
 * - Anything else as it came, wrapped.
 */
export function formatToolOutput(text: string, path?: string): ToolOutputView {
  const trimmed = text.trim();
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try {
      return { code: JSON.stringify(JSON.parse(trimmed), null, 2), language: "json", wrap: true };
    } catch {
      // Not JSON after all, or a truncated preview of it.
    }
  }
  const listing = parseListing(text);
  if (listing) {
    const language = normalizeLanguage(listing.info) ?? languageForPath(path) ?? "text";
    return listing.startLine === undefined
      ? { code: listing.code, language, wrap: true }
      : { code: listing.code, language, wrap: false, startLine: listing.startLine };
  }
  return { code: text, language: "text", wrap: true };
}

/**
 * Its own component so the formatting runs when the disclosure is open, not
 * for every closed row on every render: full output can be megabytes.
 */
function ToolOutput({ text, path, testId }: { text: string; path?: string; testId?: string }) {
  const view = React.useMemo(() => formatToolOutput(text, path), [text, path]);
  return (
    <div data-testid={testId} className="w-full min-w-0">
      {/* A numbered listing is a file the tool read: its card is named for the
          file. Anything else (JSON, a log) is not that file, whatever path the
          call touched. */}
      <AgentCodeBlock
        code={view.code}
        language={view.language}
        wrap={view.wrap}
        startLine={view.startLine}
        path={view.startLine !== undefined ? path : undefined}
        label="Output"
      />
    </div>
  );
}

export function AgentToolCall({
  call,
  defaultExpanded = false,
  variant = "card",
  sessionId,
  stepId,
  resultOutside = false,
  origin,
  onOpenFile,
  className,
}: AgentToolCallProps) {
  const KindIcon = kindIcon(call);
  // A call the stored transcript still has pending or in progress, in a turn
  // that is over or in a session nothing works on, will never finish (toolSettle.tsx).
  const state = settleToolState(toToolState(call.status), useSettleFacts(origin, sessionId));
  const title = call.title || call.kind || call.id || "Tool execution";
  const [expanded, setExpanded] = React.useState(defaultExpanded);
  // The whole call behind a truncated preview, fetched on ask; once loaded it
  // replaces the preview's content too.
  const { fullCall, loading: loadingFull, canLoad: canLoadFull, load: loadFull } = useFullToolCall(call, sessionId);
  const fullOutput = fullCall ? toolOutputText(fullCall.rawOutput) : null;

  const locations = call.locations ?? [];
  // The command a row labelled by its purpose ran — and for any shell call, the
  // whole of it: a title is one truncated line, and heredocs are most of what
  // agents run.
  const command = call.kind?.toLowerCase() === "execute" ? toolCommand(call) : "";
  const description = commandDescription(call);
  // While a Bash call runs, Claude's content is its description again — the
  // row's label already says it.
  const content = (fullCall?.content ?? call.content ?? []).filter(
    (item) => !(description && item.type === "content" && item.content?.text?.trim() === description),
  );
  // ToolResultView (docs/dev-kit/tool-result-view.mdx): the module that owns
  // this tool gets first say over how its result looks, in place of the
  // JSON below. Reads from fullCall when "Show full output" has loaded it,
  // so a card that bailed on a truncated preview (rawOutput cut mid-document
  // by ToolRawPreviewLimit) upgrades once the whole row is in. Only once the
  // call is actually done — a running call has no rawOutput yet to render.
  // toolResultInputOf reads the result wherever the vendor put it (Claude's
  // content text, Codex's MCP result in rawOutput) and names a Codex call by
  // its title; an error gives no input, so the failed row stays as it is.
  // The full row answers for the output; the call on screen for who and how.
  // Read only for a tool with a view: a turn has dozens of done rows, and the
  // answer is up to a preview's 8 KiB of JSON — or the whole row once loaded.
  const name = toolNameOf(call);
  const resultCall = React.useMemo(() => resultCallOf(call, fullCall), [call, fullCall]);
  const toolResultNode = React.useMemo(() => {
    if (resultOutside || state !== "done" || !hasToolResultView(name)) return null;
    const input = toolResultInputOf(resultCall);
    return input ? renderToolResult(input) : null;
  }, [resultOutside, state, name, resultCall]);
  // Truncated output also makes the row openable: the preview is the summary,
  // the full text is what the disclosure holds.
  const hasBody =
    Boolean(command) || locations.length > 0 || content.length > 0 || canLoadFull || Boolean(toolResultNode);
  // rawOutput is `unknown` off the wire — an object stringifies to
  // "[object Object]", which is worse than saying nothing.
  const raw = typeof call.rawOutput === "string" ? call.rawOutput : "";
  // Figma's right-hand slot holds a status or a one-line result ("running…",
  // "exit 1", "POST /refunds → RefundController.create"). An MCP tool returns
  // a whole JSON document there; putting it in a nowrap, no-shrink span made
  // the row wider than the module and gave the transcript a horizontal
  // scrollbar. Long or multi-line output belongs in the block below, which
  // scrolls inside itself.
  const summary = isOneLineStatus(raw) ? raw : "";
  const result = state === "failed" ? FALLBACK.failed : summary || FALLBACK[state];
  // Why it failed: the text a vendor put in rawOutput, or — for a DevKit tool —
  // the error wherever its vendor put it (Claude's fenced AppError in content,
  // Codex's `{error}`). A bare "failed" says nothing the slot does not.
  const reason = React.useMemo(() => {
    if (state !== "failed") return "";
    if (raw || !isDevkitTool(call)) return raw;
    const read = toolPayloadOf(resultCall);
    return read && "error" in read && read.error !== FALLBACK.failed ? read.error : "";
  }, [state, raw, call, resultCall]);

  // The whole row is the toggle when there is output to show, and a plain row
  // when there is not — a button that opens nothing is a lie about what the
  // row holds.
  const head = (className: string, children: React.ReactNode) =>
    React.createElement(
      hasBody ? "button" : "div",
      {
        ...(hasBody
          ? {
              type: "button",
              onClick: () => setExpanded((open) => !open),
              "aria-expanded": expanded,
              "data-testid": "tool-call-toggle",
            }
          : {}),
        className: cn(className, hasBody && "cursor-pointer"),
      },
      children,
    );

  const chevron = hasBody ? (
    <ChevronRight
      aria-hidden="true"
      className={cn(
        "size-3.5 shrink-0 text-text-tertiary transition-transform duration-(--motion-fast)",
        expanded && "rotate-90",
      )}
    />
  ) : null;

  // The call's own output: what a tool without a view shows, and what a view
  // that failed to draw falls back to.
  const genericBody = (
    <>
      {content.map((item, idx) => {
        if (item.type === "diff") {
          return (
            <AgentToolDiff
              key={idx}
              path={item.path}
              oldText={item.oldText}
              newText={item.newText}
              truncated={item.truncated}
            />
          );
        }
        if (item.type === "terminal") {
          return <AgentToolTerminal key={idx} terminalId={item.terminalId} />;
        }
        if (item.content?.text) {
          return <ToolOutput key={idx} text={item.content.text} path={locations[0]?.path} />;
        }
        return null;
      })}

      {fullOutput ? (
        <ToolOutput text={fullOutput} path={locations[0]?.path} testId="tool-call-full-output" />
      ) : null}
    </>
  );

  const body = (
    <>
      {/* Figma: failed keeps its reason inline. A red dot alone is not actionable. */}
      {reason ? (
        <p
          data-testid="tool-call-reason"
          className={cn(
            "w-full max-w-full break-all whitespace-pre-wrap text-text-secondary",
            variant === "row" ? "pt-2 text-code-12" : "text-caption-11",
          )}
        >
          {reason}
        </p>
      ) : null}

      <AnimatedCollapse
        open={expanded}
        className={cn("flex w-full min-w-0 flex-col gap-2", variant === "row" && "pt-2")}
      >
      {command ? (
        <div data-testid="tool-call-command" className="w-full min-w-0">
          <AgentCodeBlock code={command} language="bash" wrap />
        </div>
      ) : null}

      {locations.length > 0 ? (
        <div className="flex w-full flex-wrap items-center gap-1">
          {locations.map((loc, idx) => (
            <AgentFileChip key={`${loc.path}-${loc.line ?? idx}`} path={loc.path} line={loc.line} onClick={onOpenFile ? () => onOpenFile(loc.path, loc.line) : undefined} />
          ))}
        </div>
      ) : null}

      {toolResultNode ? (
        // The card may load lazily (registerToolResultView); this boundary
        // keeps a suspending card to itself rather than the whole transcript,
        // and the error boundary a throwing one (or a chunk gone after an
        // update): the call's own output is drawn instead.
        <div data-testid="tool-call-result-view">
          <ErrorBoundary label={`${name || "tool"} result view`} fallback={genericBody} resetKey={call}>
            <React.Suspense fallback={null}>{toolResultNode}</React.Suspense>
          </ErrorBoundary>
        </div>
      ) : (
        genericBody
      )}
      {fullCall === null && canLoadFull ? (
        <button
          type="button"
          onClick={loadFull}
          disabled={loadingFull}
          data-testid="tool-call-load-full"
          className={cn(
            "self-start text-caption-11 text-text-tertiary underline decoration-border-subtle",
            "cursor-pointer hover:text-text-secondary focus-visible:ring-ring focus-visible:ring-1 focus-visible:outline-none",
            "disabled:cursor-default disabled:opacity-70",
          )}
        >
          <Shimmer active={loadingFull}>{loadingFull ? "Loading full output…" : "Show full output"}</Shimmer>
        </button>
      ) : null}
      </AnimatedCollapse>
    </>
  );

  if (variant === "row") {
    const { verb, object } = stepLabel(call);
    const pending = state === "running" || state === "pending" || state === "awaiting" || state === "unfinished";
    // What a failed call said, for a row that has no error line of its own below it.
    const said = state === "failed" && !reason ? failureLine({ rawOutput: fullCall?.rawOutput ?? call.rawOutput, content }) : "";
    return (
      <div
        data-testid="agent-tool-call"
        data-variant="row"
        data-step-id={stepId}
        data-result={resultOutside ? "outside" : undefined}
        data-status={call.status ?? "completed"}
        data-state={state}
        className={cn("flex w-full min-w-0 flex-col", className)}
      >
        {head(
          "flex w-full min-w-0 items-center gap-2 text-left text-body-13",
          <>
            <KindIcon className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
            <span className="flex min-w-0 items-baseline gap-1">
              {/* A verb holds its width beside what it acted on; alone, it is a
                  whole sentence (a command's description) and has to be able
                  to truncate like one. */}
              <span
                className={cn(
                  object ? "shrink-0" : "min-w-0 truncate",
                  state === "failed" ? "text-status-danger" : "text-text-secondary",
                )}
              >
                {verb}
              </span>
              {object ? (
                // A command is code; a file name and a query are words.
                <span
                  className={cn(
                    "min-w-0 truncate text-text-tertiary",
                    call.kind?.toLowerCase() === "execute" && "text-code-12",
                  )}
                >
                  {object}
                </span>
              ) : null}
              {said ? (
                // Last to give way: the verb and what it acted on matter more.
                <span data-testid="tool-call-said" className="min-w-0 shrink-3 truncate text-caption-11 text-text-tertiary">
                  {`· ${said}`}
                </span>
              ) : null}
            </span>
            {pending ? <Shimmer active={state === "running"} className="shrink-0 text-text-tertiary">{FALLBACK[state]}</Shimmer> : null}
            {chevron}
          </>,
        )}
        {/* Under the words, not under the icon: 14px icon + 8px gap. A measure
            taken from the row, like the tree's indent, not a spacing step. */}
        {/* Mounted whenever there is output, open or not: unmounting it on
            close would take the output away before it could animate shut. */}
        {reason || hasBody ? <div className="flex w-full min-w-0 flex-col pl-5.5">{body}</div> : null}
      </div>
    );
  }

  return (
    <div
      data-testid="agent-tool-call"
      data-status={call.status ?? "completed"}
      data-state={state}
      className={cn(
        "flex w-full max-w-full flex-col gap-2 rounded-control p-3",
        state === "failed" ? "bg-(--feedback-danger-soft)" : "bg-(--state-soft)",
        className,
      )}
    >
      {head(
        "flex w-full min-w-0 items-center gap-2 text-left",
        <>
          <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", DOT[state])} />
          <KindIcon className="size-3 shrink-0 text-icon-secondary" aria-hidden="true" />
          {/* Truncates rather than holding its width: a shell command is a title
              too, and at nowrap/shrink-0 a long one pushed the row past the
              column. The full command is in the output once opened. */}
          <Shimmer active={state === "running"} className="min-w-0 truncate text-body-12 text-text-secondary">{title}</Shimmer>
          <span className="min-w-0 flex-1" />
          {result ? (
            <span
              data-testid="tool-call-result"
              className={cn(
                "min-w-0 max-w-1/2 shrink truncate text-caption-11 whitespace-nowrap",
                state === "failed"
                  ? "text-status-danger"
                  : state === "done" || state === "unfinished"
                    ? "text-text-tertiary"
                    : "text-text-secondary",
              )}
            >
              {result}
            </span>
          ) : null}
          {chevron}
        </>,
      )}
      {body}
    </div>
  );
}
