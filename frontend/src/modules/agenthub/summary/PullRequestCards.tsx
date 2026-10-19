import * as React from "react";
import { ChevronRight, CircleAlert, GitMerge, GitPullRequest, GitPullRequestClosed, GitPullRequestDraft, X, type LucideIcon } from "lucide-react";

import { openExternal } from "@/bridge/browser";
import { cn } from "@/lib/utils";
import { openAddToken } from "@/modules/accounts/addTokenRequest";
import type { PullRequestCard, PullRequestNotice, Tone } from "./pullRequestModel";
import type { PullRequestsPanel } from "./usePullRequests";

const STATE_ICON: Record<PullRequestCard["state"], { Icon: LucideIcon; tone: string; word: string }> = {
  open: { Icon: GitPullRequest, tone: "text-status-success", word: "open" },
  draft: { Icon: GitPullRequestDraft, tone: "text-text-tertiary", word: "draft" },
  merged: { Icon: GitMerge, tone: "text-status-recovery", word: "merged" },
  closed: { Icon: GitPullRequestClosed, tone: "text-status-danger", word: "closed" },
};

const TONE_TEXT: Record<Tone, string> = {
  success: "text-status-success",
  danger: "text-status-danger",
  warning: "text-status-warning",
  running: "text-text-secondary",
  muted: "text-text-tertiary",
};

const LINE = "flex min-w-0 items-center gap-2 pl-9.5 pr-3.5 text-caption-11 text-text-tertiary";
const FOCUS = "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-border-focus/50";

/**
 * One card per pull (or merge) request: number and title (opens the page),
 * size, checks progress, and the lines that need attention — conflicts, a
 * review verdict, open threads, the checks that failed. Stateless.
 */
export function PullRequestCards({ panel, several }: { panel: PullRequestsPanel; several: boolean }) {
  return (
    <>
      {panel.cards.map((card) => (
        <PullRequestCardView key={card.key} card={card} showRepo={several} onDetach={panel.detach} />
      ))}
      {panel.notices.map((notice) => (
        <NoticeRow key={notice.key} notice={notice} />
      ))}
      {panel.attach && (panel.cards.length > 0 || panel.notices.length > 0) ? <AttachRow attach={panel.attach} /> : null}
    </>
  );
}

function PullRequestCardView({
  card,
  showRepo,
  onDetach,
}: {
  card: PullRequestCard;
  showRepo: boolean;
  onDetach?: (card: PullRequestCard) => Promise<void>;
}) {
  const { Icon, tone, word } = STATE_ICON[card.state];
  const label = `${card.short} #${card.number}`;
  return (
    <div
      data-testid="summary-pr-card"
      data-state={card.state}
      data-pr={card.key}
      className="group/pr flex flex-col gap-0.5 pb-1.5 first:pt-0.5"
    >
      <div className="flex min-w-0 items-center">
        <button
          type="button"
          title={`${label} · ${card.title}\n${card.head} → ${card.base}\nOpen on ${card.platform || "the host"}`}
          onClick={() => void openExternal(card.url)}
          className={cn(
            "flex h-7.5 min-w-0 flex-1 cursor-pointer items-center gap-2.5 px-3.5 text-left text-body-13 text-text-primary transition-control hover:bg-state-soft",
            FOCUS,
          )}
        >
          <Icon aria-hidden="true" className={cn("size-(--icon-14) shrink-0", tone)} />
          <span className="shrink-0 tabular-nums text-text-secondary">{`#${card.number}`}</span>
          <span className="min-w-0 truncate">{card.title}</span>
          <span className="sr-only">{`${card.short} ${word}`}</span>
        </button>
        {card.source === "attached" && onDetach ? (
          <button
            type="button"
            aria-label={`Unlink ${label}`}
            title="Unlink from this session (closes nothing on the host)"
            data-testid="summary-pr-detach"
            onClick={() => void onDetach(card)}
            className={cn("mr-2 shrink-0 cursor-pointer rounded p-1 text-text-tertiary opacity-0 transition-control hover:text-text-primary group-hover/pr:opacity-100 focus-visible:opacity-100", FOCUS)}
          >
            <X aria-hidden="true" className="size-(--icon-12)" />
          </button>
        ) : null}
      </div>

      <div className={LINE} data-testid="summary-pr-meta">
        {showRepo ? <span className="min-w-0 shrink truncate" title={card.repo}>{card.repo}</span> : null}
        <span className="flex shrink-0 gap-1 tabular-nums" title={`${card.files} file${card.files === 1 ? "" : "s"}`}>
          <span className="text-status-success">{`+${card.added.toLocaleString("en-US")}`}</span>
          <span className="text-status-danger">{`−${card.removed.toLocaleString("en-US")}`}</span>
        </span>
        {card.checks ? (
          <span
            data-testid="summary-pr-checks"
            data-tone={card.checks.tone}
            title={card.checks.title}
            className={cn("ml-auto flex shrink-0 items-center gap-1 tabular-nums", TONE_TEXT[card.checks.tone])}
          >
            <span
              aria-hidden="true"
              className={cn("size-1.5 rounded-full bg-current", card.checks.tone === "running" && "animate-pulse motion-reduce:animate-none")}
            />
            {card.checks.label}
          </span>
        ) : null}
      </div>

      {card.merge || card.review || card.unresolved > 0 ? (
        <div className={cn(LINE, "flex-wrap gap-y-0.5")} data-testid="summary-pr-attention">
          {card.merge ? <span className={TONE_TEXT[card.merge.tone]}>{card.merge.text}</span> : null}
          {card.review ? <span className={TONE_TEXT[card.review.tone]}>{card.review.text}</span> : null}
          {card.unresolved > 0 ? (
            <span className="text-status-warning">{`${card.unresolved} unresolved thread${card.unresolved === 1 ? "" : "s"}`}</span>
          ) : null}
        </div>
      ) : null}

      {card.failing.map((check) => (
        <button
          key={check.name}
          type="button"
          data-testid="summary-pr-failing"
          disabled={!check.url}
          title={[check.name, check.summary, check.url && "Open the check"].filter(Boolean).join("\n")}
          onClick={() => check.url && void openExternal(check.url)}
          className={cn(
            "flex min-w-0 items-center gap-2 py-0.5 pl-9.5 pr-3.5 text-left text-caption-11 transition-control",
            check.url ? "cursor-pointer hover:bg-state-soft" : "cursor-default",
            FOCUS,
          )}
        >
          <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-status-danger" />
          <span className="shrink-0 text-text-secondary">{check.name}</span>
          {check.summary ? <span className="min-w-0 truncate text-text-tertiary">{check.summary}</span> : null}
        </button>
      ))}
      {card.failingMore > 0 ? (
        <div className={LINE} data-testid="summary-pr-failing-more">{`+${card.failingMore} more failing`}</div>
      ) : null}
      {card.notes.map((note) => (
        <div key={note} className={LINE} data-testid="summary-pr-note" title={note}>
          <span className="min-w-0 truncate">{note}</span>
        </div>
      ))}
    </div>
  );
}

/**
 * Why repositories have no card: stated, not silent, and once per reason. One
 * repository reads as its own sentence; several that fail the same way read as
 * the sentence they share, with the repositories behind a disclosure and the
 * one action that fixes all of them beside it.
 */
function NoticeRow({ notice }: { notice: PullRequestNotice }) {
  const [open, setOpen] = React.useState(false);
  const many = notice.folders.length > 1;
  const listId = React.useId();
  return (
    <div
      data-testid="summary-pr-notice"
      data-status={notice.status}
      data-tone={notice.tone}
      data-count={notice.folders.length}
      role="status"
      className="flex flex-col gap-1 px-3.5 py-1.5 text-caption-11 text-text-secondary"
    >
      <div className="flex items-start gap-2.5">
        <CircleAlert aria-hidden="true" className={cn("mt-px size-(--icon-14) shrink-0", TONE_TEXT[notice.tone])} />
        <span className="min-w-0 wrap-break-word" title={notice.folders.map((f) => `${f.name}: ${f.message}`).join("\n")}>
          {notice.message}
        </span>
      </div>
      <div className="flex min-w-0 items-center gap-1 pl-6.5">
        {notice.action ? (
          <button
            type="button"
            data-testid="summary-pr-notice-action"
            onClick={() => openAddToken(notice.provider)}
            className={cn(
              "h-6 shrink-0 cursor-pointer rounded-sm px-2 text-caption-11 text-text-primary ring-1 ring-border-subtle transition-control hover:bg-state-soft",
              FOCUS,
            )}
          >
            {notice.action.label}
          </button>
        ) : null}
        {many ? (
          <button
            type="button"
            data-testid="summary-pr-notice-toggle"
            aria-expanded={open}
            aria-controls={listId}
            onClick={() => setOpen((value) => !value)}
            className={cn(
              "flex h-6 min-w-0 cursor-pointer items-center gap-0.5 rounded-sm px-1.5 text-caption-11 text-text-tertiary transition-control hover:text-text-secondary",
              FOCUS,
            )}
          >
            <ChevronRight aria-hidden="true" className={cn("size-(--icon-12) shrink-0 transition-transform motion-reduce:transition-none", open && "rotate-90")} />
            <span className="truncate">{`${notice.folders.length} repositories`}</span>
          </button>
        ) : (
          <span className="min-w-0 truncate px-1.5 text-text-tertiary" title={notice.folders[0]?.folder}>
            {notice.folders[0]?.name}
          </span>
        )}
      </div>
      {many && open ? (
        <ul id={listId} data-testid="summary-pr-notice-folders" className="flex flex-col pl-6.5">
          {notice.folders.map((folder) => (
            <li key={folder.folder} title={`${folder.folder}\n${folder.message}`} className="min-w-0 truncate py-0.5 pl-1.5 text-text-tertiary">
              {folder.name}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** A request found by hand: a fork's, or one opened where DevKit did not see it. */
function AttachRow({ attach }: { attach: (url: string) => Promise<void> }) {
  const [open, setOpen] = React.useState(false);
  const [url, setUrl] = React.useState("");
  const [error, setError] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  if (!open) {
    return (
      <button
        type="button"
        data-testid="summary-pr-attach"
        onClick={() => setOpen(true)}
        className={cn("flex h-7 w-full cursor-pointer items-center px-3.5 text-left text-caption-11 text-text-tertiary transition-control hover:text-text-secondary", FOCUS)}
      >
        Link another by address…
      </button>
    );
  }
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!url.trim() || busy) return;
    setBusy(true);
    setError("");
    try {
      await attach(url);
      setUrl("");
      setOpen(false);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-1 px-3.5 py-1.5" data-testid="summary-pr-attach-form">
      <input
        autoFocus
        value={url}
        onChange={(event) => setUrl(event.target.value)}
        onKeyDown={(event) => event.key === "Escape" && setOpen(false)}
        placeholder="https://github.com/owner/repo/pull/12"
        aria-label="Pull request address"
        aria-invalid={error ? true : undefined}
        disabled={busy}
        className="h-7 w-full min-w-0 rounded-md bg-surface-well px-2 text-caption-11 text-text-primary outline-none ring-1 ring-border-subtle placeholder:text-text-tertiary focus-visible:ring-2 focus-visible:ring-border-focus/50"
      />
      {error ? (
        <p role="alert" className="text-caption-11 wrap-break-word text-status-danger">
          {error}
        </p>
      ) : null}
    </form>
  );
}
