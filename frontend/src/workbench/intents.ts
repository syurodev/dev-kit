/**
 * Intents: how one module asks another to show something, without importing
 * its views or its actions (docs/dev-kit/cross-module.mdx § Primitive 2).
 *
 * The target module registers a handler under a name; the caller dispatches
 * the name with an EntityRef and a placement. A name nobody registered is
 * `unavailable` — the caller hides the menu item rather than offering a
 * button that does nothing. Intents are UI navigation only: any write they
 * lead to still goes through the owning module's bridge.
 */

/** "What, in which module" — small, serializable, owned by the module named. */
export type EntityRef = {
  module: string;
  /** "endpoint" | "node" | "file" | "planNode" | … — the owning module's word. */
  kind: string;
  /** Stable id the owning module issues: a node id, a project-relative path. */
  id: string;
  /** Shown while the owner has not answered (offline, not indexed yet). */
  label: string;
  /** The project root, when the entity belongs to one. */
  project?: string;
  /** A line inside a file ref. */
  line?: number;
};

/**
 * Where the target opens its document: in the focused group, or in a group
 * beside it so what you came from stays on screen (layout.mdx § Document group).
 */
export type Placement = "replace" | "beside";

export type IntentOptions = { placement?: Placement };

type Handler = (ref: EntityRef, options: Required<IntentOptions>) => void | Promise<void>;

const handlers = new Map<string, { handler: Handler; placement: Placement }>();

/**
 * Registers a handler. `placement` is the intent's default — Try in API Client
 * opens beside, Reveal in Code Graph replaces — and a caller may override it.
 * Returns the unregister function; registering a name twice replaces it.
 */
export function registerIntent(name: string, handler: Handler, placement: Placement = "replace"): () => void {
  const entry = { handler, placement };
  handlers.set(name, entry);
  return () => {
    if (handlers.get(name) === entry) handlers.delete(name);
  };
}

export function hasIntent(name: string): boolean {
  return handlers.has(name);
}

export async function dispatchIntent(
  name: string,
  ref: EntityRef,
  options: IntentOptions = {},
): Promise<"ok" | "unavailable"> {
  const entry = handlers.get(name);
  if (!entry) return "unavailable";
  await entry.handler(ref, { placement: options.placement ?? entry.placement });
  return "ok";
}
