/**
 * Stable empty values for query defaults.
 *
 * `const { data: notes = [] } = useQuery(...)` allocates a NEW array on every
 * render where `data` is undefined — which is every render until the query
 * resolves, and every render after a reset. Anything memoised on that value
 * recomputes, and anything that writes it into a store makes the store churn:
 * NotesDashboard fed such an array into notesSidebarStore from an effect, read
 * it back through the rail's selectors in the same component, and re-rendered
 * itself into "Maximum update depth exceeded".
 *
 * A module-level constant is the fix TanStack Query itself recommends: one
 * identity for the whole app, so `undefined` reads as the same empty value
 * every time. Frozen so a caller that mutates it fails loudly here rather than
 * silently corrupting every other consumer.
 */
export const EMPTY_ARRAY: never[] = Object.freeze([]) as never[];

/** The object counterpart; see EMPTY_ARRAY. */
export const EMPTY_OBJECT: Record<string, never> = Object.freeze({});
