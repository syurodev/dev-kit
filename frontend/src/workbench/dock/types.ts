import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

/**
 * One icon in the floating dock. `panel`, when present, opens on hover
 * instead of the plain title tooltip — click always runs `onClick`, hover
 * is a separate, optional peek (this is how Agent Hub shows its session
 * list without requiring a click to leave the module you're already on).
 */
export type DockEntry = {
  id: string;
  title: string;
  icon: LucideIcon;
  onClick: () => void;
  onPreload?: () => void;
  isActive?: boolean;
  unavailable?: boolean;
  badge?: string | null;
  urgent?: boolean;
  /** Display only — e.g. "⌘3". The handler lives in Workbench.tsx. */
  shortcut?: string | null;
  panel?: () => ReactNode;
};
