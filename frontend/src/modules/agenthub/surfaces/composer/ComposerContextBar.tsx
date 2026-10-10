import * as React from "react";
import { Ellipsis } from "lucide-react";

import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { TrayCompactProvider } from "./ComposerTray";

/**
 * A row of the bar's "…" menu: what the bar keeps out of sight until asked.
 * `item` opens something (a panel); `toggle` is a setting with a state the menu
 * has to show — Follow says "Following" while it is on.
 */
export type ContextBarMenuEntry =
  | {
      kind: "item";
      key: string;
      testId?: string;
      icon: React.ReactNode;
      label: string;
      /** A count, or a word, at the row's end. */
      detail?: React.ReactNode;
      /** Nothing to open (no subagent is running): the row stays, quietly, so the feature is still where you look for it. */
      disabled?: boolean;
      /** Its panel is the one showing. */
      active?: boolean;
      onSelect: () => void;
    }
  | {
      kind: "toggle";
      key: string;
      testId?: string;
      icon: React.ReactNode;
      label: string;
      checked: boolean;
      onToggle: () => void;
    };

/**
 * The composer cluster's context bar — the session's working context, one row:
 * where it is, what needs the person, and a "…" for the rest. Not a bar that
 * holds every feature: Tasks, Subagents, Commands and Follow are rarely the
 * thing you reached for, so they live in the menu and come out as a chip only
 * when something under them wants attention (a permission, a running
 * subagent).
 *
 * `where` and `chips` are the content; they sit in a group that clips rather
 * than wraps or scrolls, so a narrow bar loses the least important label
 * (chips drop their text under a container query, the workspace name
 * truncates) and never a control: the menu is outside the clipping group and
 * never shrinks. The query asks the BAR's width (`@container`), so the same
 * bar is right in the 720px column and in the 320px rail.
 */
export function ComposerContextBar({
  compact,
  where,
  chips,
  menu,
  following = false,
  className,
}: {
  /** Icon-only chips — the rail's default. */
  compact: boolean;
  where: React.ReactNode;
  chips?: React.ReactNode;
  menu: ContextBarMenuEntry[];
  /** Shown as a dot on the menu button, so "on" is visible without opening it. */
  following?: boolean;
  className?: string;
}) {
  const [open, setOpen] = React.useState(false);
  const label = following ? "More about this session — following" : "More about this session";
  return (
    <TrayCompactProvider compact={compact}>
      <div
        data-testid="composer-tray-strip"
        className={cn("@container flex h-8 min-w-0 shrink-0 items-center gap-1 px-3", className)}
      >
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
          {where}
          {chips}
        </div>
        {menu.length > 0 ? (
          <DropdownMenu open={open} onOpenChange={setOpen}>
            <DropdownMenuTrigger asChild>
              <ActionTooltip label={label} side="top">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label={label}
                  data-testid="composer-tray-menu"
                  data-following={following || undefined}
                  className="relative shrink-0 text-text-tertiary hover:text-text-primary"
                >
                  <Ellipsis aria-hidden="true" />
                  {following ? (
                    <span
                      aria-hidden="true"
                      className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-status-info"
                    />
                  ) : null}
                </Button>
              </ActionTooltip>
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" align="end" className="w-60" data-testid="composer-tray-menu-content">
              <DropdownMenuLabel>This session</DropdownMenuLabel>
              {menu.map((entry) =>
                entry.kind === "toggle" ? (
                  <DropdownMenuCheckboxItem
                    key={entry.key}
                    data-testid={entry.testId}
                    checked={entry.checked}
                    onCheckedChange={entry.onToggle}
                    // Stay open: it is a switch, and the row says the new state.
                    onSelect={(event) => event.preventDefault()}
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <span aria-hidden="true" className="flex shrink-0 items-center [&_svg]:size-3.5">
                        {entry.icon}
                      </span>
                      <span className="min-w-0 flex-1 truncate">{entry.checked ? "Following" : entry.label}</span>
                    </span>
                  </DropdownMenuCheckboxItem>
                ) : (
                  <DropdownMenuItem
                    key={entry.key}
                    data-testid={entry.testId}
                    icon={entry.icon}
                    disabled={entry.disabled}
                    detail={entry.detail}
                    onSelect={entry.onSelect}
                  >
                    {entry.label}
                  </DropdownMenuItem>
                ),
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>
    </TrayCompactProvider>
  );
}
