import { Globe, Zap, Database, Box, type LucideIcon } from "lucide-react";

/** Matches codegraph.Node.layer, whichever analyzer produced it. */
export type Layer = "controller" | "service" | "repository" | "unknown";

export type LayerVisual = {
  /** CSS custom property name, so every surface reads one source of truth. */
  cssVar: string;
  icon: LucideIcon;
  label: string;
};

export const LAYERS: Record<Layer, LayerVisual> = {
  controller: { cssVar: "--hue-blue", icon: Globe, label: "CONTROLLER" },
  service: { cssVar: "--hue-green", icon: Zap, label: "SERVICE" },
  repository: { cssVar: "--hue-yellow", icon: Database, label: "REPOSITORY" },
  unknown: { cssVar: "--text-tertiary", icon: Box, label: "UNKNOWN" },
};

export function layerOf(raw: string): Layer {
  return raw in LAYERS ? (raw as Layer) : "unknown";
}

/**
 * Figma `method-chip/*` (482:880–482:900): a solid hue (--hue-*, the verb's
 * kind, not a status) with the on-accent text. Reads the same in the sidebar
 * rows and the canvas header, which is the point of a chip.
 */
export function verbChipClass(verb: string): string {
  switch (verb.toUpperCase()) {
    case "GET":
      return "bg-(--hue-blue) text-action-primary-foreground";
    case "POST":
      return "bg-(--hue-green) text-action-primary-foreground";
    case "DELETE":
      return "bg-(--hue-red) text-action-primary-foreground";
    case "PUT":
    case "PATCH":
      return "bg-(--hue-yellow) text-action-primary-foreground";
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

/**
 * What an extractor saw at a call ("Kafka produce", "DB read"), as a small
 * outlined chip in the caption face. One class for the card and the outline
 * row, so the same effect reads the same in both modes.
 */
export const EFFECT_CHIP = "shrink-0 rounded-sm border border-border-subtle px-1.5 text-caption-11 text-text-secondary";

/** Read off Connector (167:79). */
export const EDGE = {
  /** Matches the `a14 14 0 0 0` arc in the Figma Branch path. */
  elbowRadius: 14,
  strokeWidth: 1.5,
  dashArray: "5 5",
} as const;

/**
 * A document's header strip, run edge to edge the way the workbench's tab
 * strip above it is: 40px, the strip's 6px inset (the strip is 6px on every
 * side, so a header's first control lines up with the first tab), its
 * hairline as a bottom border. The flow document's two modes share it, so
 * switching between them never moves the header.
 */
export const FLOW_HEADER =
  "flex h-10 shrink-0 items-center justify-between gap-3 border-b border-border-subtle px-1.5";
