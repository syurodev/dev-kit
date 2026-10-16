import type { ChangeRequest, PullRequestItem } from "@/bridge/pullRequests";

/**
 * The pull-request card, derived from what Go reports. Pure: no React, no
 * bridge. The words are the platform's own ("PR"/"MR", "checks"/"pipelines")
 * because Go sends them in `terms`; nothing here is GitHub-shaped, so a second
 * platform changes no line of this file.
 */

export type Tone = "success" | "danger" | "warning" | "running" | "muted";

export type PullRequestCard = {
  key: string;
  folder: string;
  /** "acme/widgets": which repository, for a workspace of several. */
  repo: string;
  platform: string;
  /** "PR" or "MR". */
  short: string;
  number: number;
  title: string;
  url: string;
  state: "open" | "draft" | "merged" | "closed";
  added: number;
  removed: number;
  files: number;
  /** "3/9" — passed of total; null while the host reports no checks or they are unreadable. */
  checks: { label: string; tone: Tone; failed: number; running: number; title: string } | null;
  /** A line about mergeability, only when it needs attention. */
  merge: { text: string; tone: Tone } | null;
  review: { text: string; tone: Tone } | null;
  /** Review threads still open; 0 hides the line. */
  unresolved: number;
  failing: { name: string; url: string; summary: string }[];
  failingMore: number;
  notes: string[];
  source: "branch" | "attached";
  head: string;
  base: string;
};

export type PullRequestNotice = {
  key: string;
  folder: string;
  /** The folder's name, for the line. */
  name: string;
  status: string;
  message: string;
  tone: Tone;
};

export type PullRequestsView = {
  cards: PullRequestCard[];
  notices: PullRequestNotice[];
};

export const EMPTY_PULL_REQUESTS: PullRequestsView = { cards: [], notices: [] };

/** Reasons a person can act on. The rest (no request yet, detached, default branch) are not news. */
const NOTICE_STATUSES: Record<string, Tone> = {
  no_account: "warning",
  ambiguous_account: "warning",
  unauthorized: "danger",
  forbidden: "danger",
  not_found: "warning",
  rate_limited: "warning",
  unavailable: "muted",
};

const MAX_FAILING = 3;

export function folderName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const cut = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return cut >= 0 ? trimmed.slice(cut + 1) : trimmed;
}

export function pullRequestsView(items: readonly PullRequestItem[] | null | undefined): PullRequestsView {
  const cards: PullRequestCard[] = [];
  const notices: PullRequestNotice[] = [];
  const seenNotice = new Set<string>();
  for (const item of items ?? []) {
    if (item.status === "found" && item.changeRequest) {
      cards.push(cardOf(item.changeRequest, item.folder, item.source === "attached" ? "attached" : "branch"));
      continue;
    }
    const tone = NOTICE_STATUSES[item.status];
    if (!tone || !item.message) continue;
    // Two folders in one repository fail the same way: say it once.
    const key = `${item.status}:${item.message}`;
    if (seenNotice.has(key)) continue;
    seenNotice.add(key);
    notices.push({ key: `${item.folder}:${item.status}`, folder: item.folder, name: folderName(item.folder), status: item.status, message: item.message, tone });
  }
  cards.sort((a, b) => rank(a) - rank(b) || b.number - a.number);
  return { cards, notices };
}

function rank(card: PullRequestCard): number {
  return card.state === "open" || card.state === "draft" ? 0 : 1;
}

function cardOf(cr: ChangeRequest, folder: string, source: "branch" | "attached"): PullRequestCard {
  const terminal = cr.state === "merged" || cr.state === "closed";
  const short = cr.terms?.short || "PR";
  const notes = [...(cr.notes ?? [])];
  const failing = (cr.checks?.failing ?? []).slice(0, MAX_FAILING).map((c) => ({ name: c.name, url: c.url ?? "", summary: c.summary ?? "" }));
  return {
    key: `${cr.repo.host}/${cr.repo.path}#${cr.number}`,
    folder,
    repo: cr.repo.path,
    platform: cr.terms?.platform ?? "",
    short,
    number: cr.number,
    title: cr.title,
    url: cr.url,
    state: cr.state === "merged" ? "merged" : cr.state === "closed" ? "closed" : cr.draft ? "draft" : "open",
    added: cr.additions,
    removed: cr.deletions,
    files: cr.changedFiles,
    checks: terminal ? null : checksOf(cr),
    merge: terminal ? null : mergeOf(cr),
    review: terminal ? null : reviewOf(cr),
    unresolved: !terminal && cr.unresolvedThreads > 0 ? cr.unresolvedThreads : 0,
    failing: terminal ? [] : failing,
    failingMore: terminal ? 0 : Math.max(0, (cr.checks?.failed ?? 0) - failing.length),
    notes,
    source,
    head: cr.head,
    base: cr.base,
  };
}

function checksOf(cr: ChangeRequest): PullRequestCard["checks"] {
  const c = cr.checks;
  if (!cr.checksKnown || !c || c.total === 0) return null;
  const word = cr.terms?.checks || "checks";
  const tone: Tone = c.failed > 0 ? "danger" : c.running > 0 ? "running" : "success";
  const parts = [`${c.passed} passed`];
  if (c.failed) parts.push(`${c.failed} failing`);
  if (c.running) parts.push(`${c.running} running`);
  return { label: `${c.passed}/${c.total}`, tone, failed: c.failed, running: c.running, title: `${word}: ${parts.join(", ")}` };
}

function mergeOf(cr: ChangeRequest): PullRequestCard["merge"] {
  switch (cr.mergeable) {
    case "conflicting":
      return { text: `Conflicts with ${cr.base}`, tone: "danger" };
    case "behind":
      return { text: `Behind ${cr.base}`, tone: "warning" };
    case "blocked":
      // A draft is blocked by being a draft; the title already says so.
      return cr.draft ? null : { text: "Blocked by a rule", tone: "warning" };
    default:
      return null;
  }
}

function reviewOf(cr: ChangeRequest): PullRequestCard["review"] {
  if (cr.reviewDecision === "approved") return { text: "Approved", tone: "success" };
  if (cr.reviewDecision === "changes_requested") return { text: "Changes requested", tone: "danger" };
  return null;
}

/** The corner of the summary: whether any card or notice is worth drawing. */
export function hasPullRequests(view: PullRequestsView | undefined): boolean {
  return Boolean(view && (view.cards.length > 0 || view.notices.length > 0));
}

/** Total +/− of the open requests: not summed with the files' (a request is not a file). */
export function pullRequestSignature(view: PullRequestsView): string {
  return view.cards.map((c) => `${c.key}:${c.state}:${c.checks?.label ?? ""}`).join("|");
}
