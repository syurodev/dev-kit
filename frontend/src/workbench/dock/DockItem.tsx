import { useState } from "react";
import { AnimatePresence, m } from "framer-motion";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { DockEntry } from "./types";

/**
 * Hover state is local to each item, not lifted to a store — only the one
 * item being hovered needs to know it, and keeping it local means hovering
 * one icon never re-renders its siblings.
 */
export function DockItem({ item }: { item: DockEntry }) {
  const [panelOpen, setPanelOpen] = useState(false);

  const button = (
    <button
      type="button"
      disabled={item.unavailable}
      aria-disabled={item.unavailable}
      aria-label={item.title}
      onClick={item.onClick}
      onMouseEnter={() => item.panel && setPanelOpen(true)}
      onMouseLeave={() => item.panel && setPanelOpen(false)}
      className={cn(
        "relative flex size-8 shrink-0 items-center justify-center rounded-xl text-muted-foreground transition-colors",
        item.isActive
          ? "bg-accent text-foreground"
          : "hover:bg-accent/60 hover:text-foreground",
        item.unavailable && "pointer-events-none opacity-40",
      )}
    >
      <item.icon className="size-4" />
      {item.badge ? (
        <span
          className={cn(
            "absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] font-semibold",
            item.urgent
              ? "bg-(--status-permission-soft) text-(--status-permission)"
              : "bg-muted text-muted-foreground",
          )}
        >
          {item.badge}
        </span>
      ) : null}
    </button>
  );

  if (item.panel) {
    return (
      <div className="relative">
        {button}
        <AnimatePresence>
          {panelOpen ? (
            <m.div
              initial={{ opacity: 0, y: 6, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 6, scale: 0.96 }}
              transition={{ duration: 0.12 }}
              className="absolute bottom-full left-1/2 z-10 mb-3 -translate-x-1/2 overflow-hidden rounded-2xl border border-foreground/10 bg-popover"
            >
              {item.panel()}
            </m.div>
          ) : null}
        </AnimatePresence>
      </div>
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent side="top">{item.title}</TooltipContent>
    </Tooltip>
  );
}
