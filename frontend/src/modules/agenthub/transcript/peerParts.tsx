import * as React from "react";
import { Terminal } from "lucide-react";

import { cn } from "@/lib/utils";
import { VendorIcon } from "../surfaces/VendorIcon";

/**
 * The pieces a message between sessions is drawn from, shared by the receiving
 * session's block (PeerMessage) and the sender's tool result (toolViews/message)
 * so the two read as one thing: the same face and title for a session, the
 * same chip for what became of a message.
 */

/** A session's small face: the vendor's mark on a disc, or a terminal prompt for the person's CLI. */
export function PeerAvatar({ vendor, terminal = false, className }: { vendor?: string; terminal?: boolean; className?: string }) {
  return (
    <span
      data-slot="peer-avatar"
      aria-hidden="true"
      className={cn(
        "inline-flex size-4.5 shrink-0 items-center justify-center rounded-full bg-surface-base text-text-secondary ring-1 ring-border-subtle ring-inset",
        className,
      )}
    >
      {terminal ? <Terminal className="size-2.5" /> : vendor ? <VendorIcon vendor={vendor} className="size-2.5" /> : null}
    </span>
  );
}

/**
 * Who a message is from or to: a face and a title, optionally a button that
 * opens the session. The title truncates and the face does not, so a long
 * session name in the narrow rail never pushes the row wider.
 */
export function PeerParty({
  prefix,
  title,
  vendor,
  terminal,
  onOpen,
  openLabel = "Open this session",
  testId,
  suffix,
}: {
  /** "From" / "To": said before the name, in the quieter tone. */
  prefix: string;
  title: string;
  vendor?: string;
  terminal?: boolean;
  onOpen?: () => void;
  openLabel?: string;
  testId?: string;
  /** Said after the name, e.g. "(session gone)". */
  suffix?: string;
}) {
  const content = (
    <>
      <PeerAvatar vendor={vendor} terminal={terminal} />
      <span className="min-w-0 truncate">
        {prefix ? <span className="text-text-tertiary">{`${prefix} `}</span> : null}
        {title}
      </span>
    </>
  );
  const shared = "inline-flex min-w-0 max-w-full items-center gap-1.5 align-middle";
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      {onOpen ? (
        <button
          type="button"
          data-testid={testId}
          onClick={onOpen}
          title={`${prefix} ${title} · ${openLabel}`}
          aria-label={`${prefix} ${title}. ${openLabel}`}
          className={cn(shared, "cursor-pointer rounded-sm outline-none hover:text-text-primary focus-visible:ring-2 focus-visible:ring-border-focus/50")}
        >
          {content}
        </button>
      ) : (
        <span data-testid={testId} title={`${prefix} ${title}`} className={shared}>
          {content}
        </span>
      )}
      {suffix ? <span className="shrink-0 text-text-tertiary">{suffix}</span> : null}
    </span>
  );
}

const CHIP_DOT: Record<string, string> = {
  started: "bg-status-success",
  steered: "bg-status-success",
  queued: "bg-status-warning",
  refused: "bg-status-danger",
};

/**
 * A small status chip: a dot whose colour says how it went (it landed, it
 * waits, it did not), and one word. Colour is never the only signal.
 */
export function StatusChip({ tone, children, testId }: { tone?: "started" | "steered" | "queued" | "refused"; children: React.ReactNode; testId?: string }) {
  return (
    <span
      data-slot="status-chip"
      data-testid={testId}
      data-tone={tone}
      className="inline-flex h-4.5 shrink-0 items-center gap-1 rounded-full bg-state-soft px-1.5 text-caption-11 whitespace-nowrap text-text-secondary"
    >
      {tone ? <span aria-hidden="true" className={cn("size-1.5 rounded-full", CHIP_DOT[tone])} /> : null}
      {children}
    </span>
  );
}
