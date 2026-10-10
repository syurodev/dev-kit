import * as React from "react";
import { Check, Ellipsis } from "lucide-react";

import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { TrayChip, TrayCompactProvider } from "./ComposerTray";

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

/** Compact context controls sit above the input surface. Tasks and Subagents stay visible,
 * including zero counts; only secondary actions live in the overflow menu.
 * Groups wrap in narrow panes rather than clipping controls. */
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
  const direct = menu.filter((entry) => entry.kind === "item" && (entry.key === "tasks" || entry.key === "subagents"));
  const overflow = menu.filter((entry) => !direct.includes(entry));
  const label = following ? "More about this session — following" : "More about this session";
  return (
    <TrayCompactProvider compact={compact}>
      <div
        data-testid="composer-tray-strip"
        className={cn("flex min-w-0 max-w-full flex-wrap items-center gap-1", className)}
      >
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          {where}
          {chips}
          {direct.map((entry) => entry.kind === "item" ? (
            <TrayChip key={entry.key} testId={`tray-${entry.key}`} icon={entry.icon}
              count={Number(entry.detail ?? 0)} title={`${entry.label}: ${entry.detail ?? 0}`} active={entry.active}
              priority onClick={entry.onSelect} />
          ) : null)}
        </div>
        {overflow.length > 0 ? (
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
              {overflow.map((entry) =>
                entry.kind === "toggle" ? (
                  <DropdownMenuItem
                    key={entry.key}
                    data-testid={entry.testId}
                    role="menuitemcheckbox"
                    aria-checked={entry.checked}
                    icon={entry.icon}
                    detail={entry.checked ? <Check aria-hidden="true" className="size-3.5" /> : undefined}
                    onSelect={(event) => { event.preventDefault(); entry.onToggle(); }}
                  >
                    {entry.label}
                  </DropdownMenuItem>
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
