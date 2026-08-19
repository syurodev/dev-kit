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

/** Read off Connector (167:79). */
export const EDGE = {
  /** Matches the `a14 14 0 0 0` arc in the Figma Branch path. */
  elbowRadius: 14,
  strokeWidth: 1.5,
  dashArray: "5 5",
} as const;
