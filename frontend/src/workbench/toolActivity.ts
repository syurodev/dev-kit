/**
 * ToolActivity: what a DevKit tool that CHANGES something does, said the way a
 * person would — "Creating board Backend", "Committed a1b2c3d" — and which
 * thing it acted on. The transcript draws these as cards that stay on screen
 * while the rest of a run's steps fold away: a board the agent made is news,
 * a file it read is not. Read-only tools register nothing here.
 *
 * Same shape and same names as ./toolResultViews.ts, and registered beside a
 * tool's result view in the owning module's manifest (rule 6 of
 * docs/dev-kit/tool-result-view.mdx: the module that owns the tool says what
 * it means). The two answer different questions: the result view draws what
 * came back; this says what is happening, before anything has come back, and
 * what it is about — the target is the one declaration the card's Open, and
 * later the follow panel, read.
 */

import type { EntityRef } from "./intents";
import { createToolRegistry, type ToolResultInput } from "./toolResultViews";

/** Where a call stands, as the card draws it. */
export type ToolActivityState = "running" | "done" | "failed";

/** What a call acts on. `ref` is there once the owner has issued an id. */
export type ToolActivityTarget = {
  /** What the person calls it: a board's name, a task's title, a SHA. */
  label: string;
  /** A second, quieter part: a task's key, a branch. */
  detail?: string;
  /** Opens it (dispatchIntent with ref.module + ".reveal"). */
  ref?: EntityRef;
};

export type ToolActivity = {
  /** The owning module, for its icon. */
  module: string;
  /** "Creating board" — the call is under way. */
  running: string;
  /** "Created board" — it came back. */
  done: string;
  /** "Couldn't create board". */
  failed: string;
  /**
   * The target, read from rawInput while running (no id yet: a label at
   * best) and from rawOutput once done. Pure and cheap: it runs on every
   * render of the card. Null is "nothing to name" and the card says the verb.
   */
  target: (input: ToolResultInput & { state: ToolActivityState }) => ToolActivityTarget | null;
  /**
   * Warms the result card's chunk while the call runs, so the card that
   * replaces the running one is ready when the answer lands.
   */
  preload?: () => Promise<unknown>;
};

const activities = createToolRegistry<ToolActivity>();

/** Registers what tool `name` (the Go `Name:`) does. Registering twice replaces it. */
export function registerToolActivity(name: string, activity: ToolActivity): () => void {
  return activities.register(name, activity);
}

/** The activity a call's tool declared, if it is one that changes something. */
export function toolActivityOf(callName: string | undefined): ToolActivity | undefined {
  return activities.lookup(callName);
}

/** rawInput/rawOutput as an object, whether the transport parsed it or not. */
export function toolRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

/** The string field `key` of a JSON object, or "". For target() readers. */
export function stringField(value: unknown, key: string): string {
  const field = toolRecord(value)?.[key];
  return typeof field === "string" ? field : "";
}

/** The first line of a text, clipped — a commit message, a comment. */
export function firstLine(text: string, max = 80): string {
  const line = text.trim().split("\n", 1)[0] ?? "";
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}
