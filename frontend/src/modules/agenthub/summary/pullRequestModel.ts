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

/**
 * Why some repositories have no card — one entry per *reason*, not per
 * repository: fifteen folders that all lack a GitLab account are one notice
 * that names them, because the person's next step (add one token) is the same.
 */
export type PullRequestNotice = {
  key: string;
  status: string;
  /** One repository's own sentence; for several, the sentence they share. */
  message: string;
  tone: Tone;
  /** The repositories it applies to, in the order they were asked, each with its own sentence. */
  folders: { folder: string; name: string; message: string }[];
  /** What the person can do about it, when it is theirs to do: Accounts is where it is fixed. */
  action: { label: string; target: "accounts" } | null;
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

/** The one word an action on a reason says. A rate limit or a host outage passes by itself. */
const NOTICE_ACTIONS: Record<string, string> = {
  no_account: "Add token",
  ambiguous_account: "Choose account",
  unauthorized: "Fix token",
  forbidden: "Fix token",
};

/**
 * A repository's sentence with the repository taken out of it, so two
 * repositories refused for the same reason read as the same sentence:
 * "No GitLab account for this remote (a/b on gitlab.com). Add a token in
 * Accounts." and the one for `a/c` both become "No GitLab account for this
 * remote. Add a token in Accounts.".
 */
export function sharedMessage(message: string): string {
  return message.replace(/\s*\([^()]*\)/g, "").replace(/\s+/g, " ").trim();
}

export function pullRequestsView(items: readonly PullRequestItem[] | null | undefined): PullRequestsView {
  const cards: PullRequestCard[] = [];
  const groups = new Map<string, PullRequestNotice>();
  for (const item of items ?? []) {
    if (item.status === "found" && item.changeRequest) {
      cards.push(cardOf(item.changeRequest, item.folder, item.source === "attached" ? "attached" : "branch"));
      continue;
    }
    const tone = NOTICE_STATUSES[item.status];
    if (!tone || !item.message) continue;
    const key = `${item.status}:${sharedMessage(item.message)}`;
    const entry = { folder: item.folder, name: folderName(item.folder), message: item.message };
    const group = groups.get(key);
    if (!group) {
      const label = NOTICE_ACTIONS[item.status];
      groups.set(key, {
        key,
        status: item.status,
        message: item.message,
        tone,
        folders: [entry],
        action: label ? { label, target: "accounts" } : null,
      });
    } else if (!group.folders.some((f) => f.folder === item.folder)) {
      group.folders.push(entry);
      group.message = sharedMessage(item.message);
    }
  }
  cards.sort((a, b) => rank(a) - rank(b) || b.number - a.number);
  return { cards, notices: [...groups.values()].sort((a, b) => noticeRank(a) - noticeRank(b)) };
}

/** What the person can fix first; an outage that heals itself last. Stable otherwise. */
function noticeRank(notice: PullRequestNotice): number {
  return notice.tone === "danger" ? 0 : notice.tone === "warning" ? 1 : 2;
}

/** The folded section's line: "2 open · 6 need an account". */
export function pullRequestsHint(view: PullRequestsView): string | undefined {
  const parts: string[] = [];
  const live = view.cards.filter((card) => card.state === "open" || card.state === "draft").length;
  if (live) parts.push(`${live} open`);
  // Finished ones only when nothing is open: the line is short, and a merged request is no news beside an open one.
  const done = view.cards.length - live;
  if (done && !live) parts.push(`${done} finished`);
  for (const notice of view.notices) parts.push(noticeCount(notice));
  return parts.length ? parts.join(" · ") : undefined;
}

/** [one repository, several]: the sentence is a count followed by this. */
const NOTICE_WORDS: Record<string, readonly [string, string]> = {
  no_account: ["needs an account", "need an account"],
  ambiguous_account: ["has several accounts to choose from", "have several accounts to choose from"],
  unauthorized: ["has a refused token", "have a refused token"],
  forbidden: ["has a refused token", "have a refused token"],
  not_found: ["was not found", "were not found"],
  rate_limited: ["is rate limited", "are rate limited"],
  unavailable: ["is unavailable", "are unavailable"],
};

function noticeCount(notice: PullRequestNotice): string {
  const n = notice.folders.length;
  const words = NOTICE_WORDS[notice.status] ?? ["needs attention", "need attention"];
  return `${n} ${n === 1 ? words[0] : words[1]}`;
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
