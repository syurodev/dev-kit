import * as React from "react";
import { Ellipsis, MessageSquareShare } from "lucide-react";

import type { AgentSession } from "@/bridge/agenthub";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { Subagent } from "../types";
import { vendorInfo } from "../vendors";
import { sessionTitle } from "../documents";
import { SessionMenuItems, useSessionActions, type MenuKit } from "./actions";
import { shortAge } from "@/lib/time";
import { attentionKind, cardSummary, peerUnreadLabel, secondLine, type Attention } from "./model";
import { SessionAvatar, type AvatarState } from "./SessionAvatar";

const CONTEXT_KIT: MenuKit = {
  Item: ContextMenuItem as MenuKit["Item"],
  Separator: ContextMenuSeparator as MenuKit["Separator"],
  Label: ContextMenuLabel as MenuKit["Label"],
};
const DROPDOWN_KIT: MenuKit = {
  Item: DropdownMenuItem as MenuKit["Item"],
  Separator: DropdownMenuSeparator as MenuKit["Separator"],
  Label: DropdownMenuLabel as MenuKit["Label"],
};

export type AgentSessionCardProps = {
  session: AgentSession;
  /** The session the focused group shows. */
  selected: boolean;
  attention?: Attention;
  /** What the session waits on the person for, in its own words (a permission's title, a question). */
  ask: string | null;
  /** The folder it works in, when its workspace has more than one; null otherwise. */
  folder: string | null;
  pinned: boolean;
  /** Said in the tooltip and the description only; the row draws no avatars for them. */
  subagents: readonly Subagent[];
  /** The parent's title, for a child session: where the lineage is said, since it is not drawn. */
  parentTitle?: string;
  /**
   * Messages other sessions sent it that the person has not looked at. A small
   * mark beside the title, nothing more: the session is not waiting on the
   * person, so it neither joins Needs you nor takes the waiting dot.
   */
  peerUnread?: number;
  /** The minute clock, so a card's age is the only thing a tick changes. */
  now: number;
};

/**
 * One session: a face, a title and where it got to, in a fixed 50px row.
 *
 * The face (SessionAvatar) carries the vendor and the state — an arc while it
 * runs, a dot when it waits on the person or failed. Beside it, the title and
 * its age, then one line: what it is waiting for (warning), else the last thing
 * it said (danger when it failed), behind the folder when the workspace has
 * several. Nothing else is drawn: pinned, child-of and subagents are said in
 * the tooltip and the description.
 *
 * Structure: the card is a `div`, never a button. The title is the open button;
 * its ::after stretches over the card so the whole card opens the session, and
 * the "…" menu sits above it (z-20) as a sibling, so no button holds another.
 *
 * Waiting: the dot on the face is the standing signal. A session that starts
 * waiting moves into Needs you, and that list's enter motion is the "this just
 * happened"; the row plays nothing of its own.
 *
 * The face's dot rings itself in the row's ground (--avatar-ground), which the
 * stylesheet sets from the row's state: at rest, hovered or focused, selected.
 */
function AgentSessionCardView({
  session,
  selected,
  attention,
  ask,
  folder,
  pinned,
  subagents,
  parentTitle,
  peerUnread,
  now,
}: AgentSessionCardProps) {
  const actions = useSessionActions();
  const vendor = vendorInfo(session.agentType);
  const title = sessionTitle(session);
  const kind = attentionKind(session, attention);
  const waiting = kind !== null;
  const age = shortAge(session.updatedAt, now);
  const summary = cardSummary({ vendorName: vendor.name, session, attention, subagents, pinned, age, parentTitle, peerUnread });
  const line = secondLine({ session, ask, folder });
  const avatarState: AvatarState = waiting
    ? "waiting"
    : session.status === "error"
      ? "error"
      : session.status === "running"
        ? "running"
        : null;

  const closed = session.status === "closed";
  const strong = selected || waiting || session.status === "running";
  const summaryId = React.useId();
  const [menuOpen, setMenuOpen] = React.useState(false);

  const card = (
    <div
      role="listitem"
      data-slot="agent-session-card"
      data-testid="agent-session-card"
      data-session-id={session.id}
      data-status={session.status || "idle"}
      data-selected={selected || undefined}
      data-waiting={waiting || undefined}
      className={cn(
        "group/card relative flex h-12.5 items-center gap-2.75 rounded-lg px-2 transition-colors duration-(--motion-fast)",
        selected ? "bg-surface-raised" : "hover:bg-state-soft focus-within:bg-state-soft",
      )}
    >
      <SessionAvatar vendor={session.agentType} state={avatarState} />

      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                data-sidebar-nav=""
                data-testid="agent-card-open"
                aria-current={selected ? "true" : undefined}
                aria-describedby={summaryId}
                onClick={() => actions.open(session)}
                // Double-click renames, as in a file tree.
                onDoubleClick={actions.rename ? () => actions.rename?.(session) : undefined}
                className={cn(
                  "block min-w-0 flex-1 cursor-pointer text-left outline-none",
                  // The stretched hit area doubles as the focus ring, so the ring is
                  // the whole card rather than a line of text.
                  "after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-2 focus-visible:after:ring-border-focus/50",
                )}
              >
                <span
                  data-testid="agent-card-title"
                  className={cn(
                    "block truncate text-body-13",
                    strong ? "text-text-primary" : closed ? "text-text-tertiary" : "text-text-secondary",
                  )}
                >
                  {title}
                </span>
              </button>
            </TooltipTrigger>
            <TooltipContent
              side="right"
              align="start"
              className="h-auto max-w-72 flex-col items-start gap-0.5 py-1.5 whitespace-normal"
            >
              <span className="block max-w-full text-label-12 break-words">{title}</span>
              <span data-testid="agent-card-summary" className="block text-caption-11 text-text-tertiary">
                {summary.join(" · ")}
              </span>
            </TooltipContent>
          </Tooltip>
          {peerUnread ? (
            <span
              data-testid="agent-card-peer-unread"
              role="img"
              aria-label={peerUnreadLabel(peerUnread)}
              className="relative z-10 flex shrink-0 items-center gap-0.5 text-caption-11 text-text-secondary tabular-nums"
            >
              <MessageSquareShare aria-hidden="true" className="size-3" />
              {peerUnread > 9 ? "9+" : peerUnread}
            </span>
          ) : null}
          {age ? (
            <span
              data-testid="agent-card-age"
              aria-hidden="true"
              // Gives its place to the "…" menu on hover and focus.
              className="relative z-10 shrink-0 text-caption-11 text-text-tertiary tabular-nums transition-opacity duration-(--motion-fast) group-hover/card:opacity-0 group-focus-within/card:opacity-0"
            >
              {age}
            </span>
          ) : null}
        </div>
        <span
          data-testid="agent-card-line"
          className={cn(
            "mt-0.5 block truncate text-body-12",
            line.tone === "warning"
              ? "text-status-warning"
              : line.tone === "danger"
                ? "text-status-danger"
                : "text-text-tertiary",
          )}
        >
          {line.folder ? <span className="opacity-70">{line.text ? `${line.folder} · ` : line.folder}</span> : null}
          {/* A blank line still takes its height, so every row is the same two lines. */}
          {line.text || (line.folder ? null : "\u00a0")}
        </span>
      </div>

      <span id={summaryId} data-testid="agent-card-description" className="sr-only">
        {summary.join(". ")}
      </span>

      {/* Appears on hover and on keyboard focus anywhere in the card, and stays
          while its menu is open. It sits where the age was. */}
      <div
        className={cn(
          "absolute top-1.5 right-1.5 z-20 opacity-0 transition-opacity duration-(--motion-fast)",
          "group-hover/card:opacity-100 group-focus-within/card:opacity-100",
          menuOpen && "opacity-100",
        )}
      >
        <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              data-testid="agent-card-menu"
              aria-label={`Actions for ${title}`}
              title="Actions"
              className="bg-surface-raised text-text-secondary"
            >
              <Ellipsis aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            align="end"
            className="w-56"
            onCloseAutoFocus={() => actions.release((apply) => apply())}
          >
            <SessionMenuItems kit={DROPDOWN_KIT} session={session} pinned={pinned} subagents={subagents} />
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{card}</ContextMenuTrigger>
      <ContextMenuContent className="w-56" onCloseAutoFocus={() => actions.release((apply) => apply())}>
        <ContextMenuLabel className="truncate">{title}</ContextMenuLabel>
        <ContextMenuSeparator />
        <SessionMenuItems kit={CONTEXT_KIT} session={session} pinned={pinned} subagents={subagents} />
      </ContextMenuContent>
    </ContextMenu>
  );
}

export const AgentSessionCard = React.memo(AgentSessionCardView);
