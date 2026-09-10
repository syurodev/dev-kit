/**
 * Stable empty values for query defaults.
 *
 * `const { data: notes = [] } = useQuery(...)` allocates a NEW array on every
 * render where `data` is undefined — every render until the query resolves, and
 * every render after a reset. Anything memoised on it recomputes, and anything
 * writing it into a store makes the store churn: that is how NotesDashboard
 * re-rendered itself into "Maximum update depth exceeded".
 *
 * A module-level constant is TanStack Query's own recommendation: one identity
 * for the whole app, so `undefined` reads as the same empty value every time.
 * Frozen so a caller that mutates it fails loudly here.
 */
export const EMPTY_ARRAY: never[] = Object.freeze([]) as never[];

/** The object counterpart; see EMPTY_ARRAY. */
export const EMPTY_OBJECT: Record<string, never> = Object.freeze({});
