import { Globe, Zap, Database, Box, type LucideIcon } from "lucide-react";

/** Matches codegraph.Node.layer as produced by the Java analyzer. */
export type Layer = "controller" | "service" | "repository" | "unknown";

export type LayerVisual = {
  /** CSS custom property name, so every surface reads one source of truth. */
  cssVar: string;
  icon: LucideIcon;
  label: string;
};

export const LAYERS: Record<Layer, LayerVisual> = {
  controller: { cssVar: "--status-info", icon: Globe, label: "CONTROLLER" },
  service: { cssVar: "--status-success", icon: Zap, label: "SERVICE" },
  repository: { cssVar: "--status-warning", icon: Database, label: "REPOSITORY" },
  unknown: { cssVar: "--text-tertiary", icon: Box, label: "UNKNOWN" },
};

export function layerOf(raw: string): Layer {
  return raw in LAYERS ? (raw as Layer) : "unknown";
}

/**
 * Figma `method-chip/*` (482:880–482:900): a solid status colour with the
 * on-accent text. Reads the same in the sidebar rows and the canvas header,
 * which is the point of a chip.
 */
export function methodChipClass(method: string): string {
  switch (method.toUpperCase()) {
    case "GET":
      return "bg-status-info text-action-primary-foreground";
    case "POST":
      return "bg-status-success text-action-primary-foreground";
    case "DELETE":
      return "bg-status-danger text-action-primary-foreground";
    case "PUT":
    case "PATCH":
      return "bg-status-warning text-action-primary-foreground";
    default:
      return "bg-surface-raised text-text-primary";
  }
}

/**
 * Figma `icon-badge` (162:47, 331:444): a 30px ring on surface/raised with a
 * subtle stroke, the layer icon inside. Shared by the node cards and the
 * inspector header so a node looks like itself in both places.
 */
export const ICON_BADGE =
  "flex size-7.5 shrink-0 items-center justify-center rounded-full border border-border-subtle bg-surface-raised";

/** Read off Connector (167:79). */
export const EDGE = {
  /** Matches the `a14 14 0 0 0` arc in the Figma Branch path. */
  elbowRadius: 14,
  strokeWidth: 1.5,
  dashArray: "5 5",
} as const;
