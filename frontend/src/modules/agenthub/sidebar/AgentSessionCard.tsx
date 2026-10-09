import * as React from "react";
import { CircleAlert, CornerDownRight, Ellipsis, LoaderCircle, MessageCircleQuestionMark, Pin, ShieldAlert } from "lucide-react";

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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { Subagent } from "../types";
import { VendorIcon } from "../surfaces/VendorIcon";
import { vendorInfo } from "../vendors";
import { sessionTitle } from "../documents";
import { SessionMenuItems, useSessionActions, type MenuKit } from "./actions";
import { AgentSubagentAvatars } from "./AgentSubagentAvatars";
import { useRisingEdge } from "./hooks";
import {
  attentionKind,
  cardIndicator,
  cardSummary,
  indicatorLabel,
  shortAge,
  type Attention,
  type CardIndicator,
} from "./model";

const CONTEXT_KIT: MenuKit = {
  Item: ContextMenuItem as MenuKit["Item"],
  Separator: ContextMenuSeparator as MenuKit["Separator"],
};
const DROPDOWN_KIT: MenuKit = {
  Item: DropdownMenuItem as MenuKit["Item"],
  Separator: DropdownMenuSeparator as MenuKit["Separator"],
};

export type AgentSessionCardProps = {
  session: AgentSession;
  /** The session the focused group shows. */
  selected: boolean;
  attention?: Attention;
  pinned: boolean;
  subagents: readonly Subagent[];
  /** The parent's title, for a child session: where the lineage is said, since it is not drawn. */
  parentTitle?: string;
  /** The minute clock, so a card's age is the only thing a tick changes. */
  now: number;
  /** A narrow rail drops the time and one avatar. */
  narrow: boolean;
  /**
   * Mounted into a group that was already drawn (pulled out of the stack, or
   * new): it fades in once. Read at mount only; later renders cannot replay it.
   */
  enter?: boolean;
};

const AVATARS_WIDE = 3;
const AVATARS_NARROW = 2;

/** One glyph per state worth a glance; idle and closed have none. */
function IndicatorIcon({ indicator, pulse }: { indicator: Exclude<CardIndicator, null>; pulse: boolean }) {
  const common = "size-3.5";
  switch (indicator) {
    case "permission":
      return <ShieldAlert aria-hidden="true" className={cn(common, "text-status-warning", pulse && "agent-attention-pulse")} />;
    case "question":
      return (
        <MessageCircleQuestionMark
          aria-hidden="true"
          className={cn(common, "text-status-warning", pulse && "agent-attention-pulse")}
        />
      );
    case "error":
      return <CircleAlert aria-hidden="true" className={cn(common, "text-status-danger")} />;
    case "running":
      // Small and slow. With reduced motion it is a still ring: the tooltip and
      // the card's text still say "Running".
      return (
        <LoaderCircle
          aria-hidden="true"
          className={cn(common, "text-status-info animate-spin motion-reduce:animate-none")}
          style={{ animationDuration: "1.6s" }}
        />
      );
  }
}

/**
 * A non-interactive glyph with a name for the pointer. A reader does not get it
 * from here: it is hidden, and the card's description (the open button's
 * aria-describedby) says the same thing once, in a sentence.
 */
function MetaIcon({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span aria-hidden="true" data-meta={label} className="relative z-10 inline-flex shrink-0 items-center">
          {children}
        </span>
      </TooltipTrigger>
      <TooltipContent side="top">{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * One session: a title and one line of metadata.
 *
 * The title is the point — body size, one line, truncated with the full name in
 * the tooltip. Beneath it, small: the vendor mark, a state glyph only when the
 * session is running, waiting on the person or failed, its subagents, and how
 * long ago it moved. Nothing is drawn for "idle" and nothing for "no
 * subagents": a row of identical empty slots says nothing.
 *
 * Structure: the card is a `div`, never a button. The title is the open button;
 * its ::after stretches over the card so the whole card opens the session, and
 * the controls that must work on their own — the avatars, the "…" menu — sit
 * above it (z-10) as siblings, so no button holds another.
 *
 * Waiting: a bar at the left edge and the glyph are the standing signal. The
 * background flash and the glyph's three beats play once, on the render where
 * waiting begins (useRisingEdge) — not on mount, not on selection.
 */
function AgentSessionCardView({ session, selected, attention, pinned, subagents, parentTitle, now, narrow, enter }: AgentSessionCardProps) {
  const [entering] = React.useState(Boolean(enter));
  const actions = useSessionActions();
  const vendor = vendorInfo(session.agentType);
  const title = sessionTitle(session);
  const indicator = cardIndicator(session, attention);
  const waiting = attentionKind(session, attention) !== null;
  const flash = useRisingEdge(waiting);
  const age = shortAge(session.updatedAt, now);
  const summary = cardSummary({ vendorName: vendor.name, session, attention, subagents, pinned, age, parentTitle });

  const closed = session.status === "closed";
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
        "group/card relative flex flex-col gap-1 rounded-md px-3 py-2 transition-colors duration-(--motion-fast)",
        selected
          ? // The ring is the shape cue; the heavier title below is the second one.
            "bg-surface-raised ring-1 ring-border ring-inset"
          : "hover:bg-state-soft focus-within:bg-state-soft",
        entering && "agent-card-enter",
        flash && "agent-attention-flash",
      )}
    >
      {waiting ? (
        <span
          aria-hidden="true"
          data-testid="agent-card-bar"
          className="absolute inset-y-2 left-0 w-0.75 rounded-sm bg-status-warning"
        />
      ) : null}

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
              "block min-w-0 cursor-pointer text-left outline-none",
              // The stretched hit area doubles as the focus ring, so the ring is
              // the whole card rather than a line of text.
              "after:absolute after:inset-0 after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-status-info",
            )}
          >
            <span
              data-testid="agent-card-title"
              className={cn(
                "block truncate",
                selected ? "text-body-13-medium text-text-primary" : "text-body-13",
                !selected && (closed ? "text-text-tertiary" : "text-text-secondary"),
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

      <span id={summaryId} data-testid="agent-card-description" className="sr-only">
        {summary.join(". ")}
      </span>

      <div
        data-testid="agent-card-meta"
        className="flex min-h-5 min-w-0 items-center gap-1.5 overflow-hidden px-0.5 text-caption-11 text-text-tertiary"
      >
        <MetaIcon label={vendor.name}>
          <VendorIcon vendor={session.agentType} working={session.status === "running"} className="size-3.5" />
        </MetaIcon>

        {indicator ? (
          <MetaIcon label={indicatorLabel(indicator, attention)}>
            <IndicatorIcon indicator={indicator} pulse={flash} />
          </MetaIcon>
        ) : null}

        {parentTitle ? (
          <MetaIcon label={`Child of ${parentTitle}`}>
            <CornerDownRight aria-hidden="true" className="size-3.5" />
          </MetaIcon>
        ) : null}

        {subagents.length ? (
          <AgentSubagentAvatars
            subagents={subagents}
            max={narrow ? AVATARS_NARROW : AVATARS_WIDE}
            onOpen={(agent) => actions.openSubagent(session, agent)}
          />
        ) : null}

        {/* Pushes what follows to the right edge; the time yields first when narrow. */}
        <span className="min-w-0 flex-1" />

        {pinned ? (
          <MetaIcon label="Pinned">
            <Pin aria-hidden="true" data-testid="agent-row-pin" className="size-3 fill-current" />
          </MetaIcon>
        ) : null}

        {age && !narrow ? (
          <span data-testid="agent-card-age" aria-hidden="true" className="relative z-10 shrink-0 tabular-nums">
            {age}
          </span>
        ) : null}
      </div>

      {/* Appears on hover and on keyboard focus anywhere in the card, and stays
          while its menu is open. It sits over the end of the title rather than
          taking room from it. */}
      <div
        className={cn(
          "absolute top-1 right-1 z-20 opacity-0 transition-opacity duration-(--motion-fast)",
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
            <SessionMenuItems kit={DROPDOWN_KIT} session={session} pinned={pinned} />
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
        <SessionMenuItems kit={CONTEXT_KIT} session={session} pinned={pinned} />
      </ContextMenuContent>
    </ContextMenu>
  );
}

export const AgentSessionCard = React.memo(AgentSessionCardView);
