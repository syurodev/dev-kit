/**
 * The element a hidden-but-mounted view should render: the one it built the
 * last time it was visible.
 *
 * A view kept alive while hidden (the Agent companion) still sits under its
 * store subscriptions; returning the same element makes React bail out of the
 * whole subtree under it, so a hidden composer does not re-render, and reset
 * its editor, on every keystroke typed somewhere else. Shown again, the fresh
 * element goes through and the subtree catches up in one render. Not
 * <Activity>: its display:none would drop the scroll positions the hidden
 * view is kept alive to keep.
 */
export function frozenWhileHidden<T>(box: { current: T | null }, visible: boolean, next: T): T {
  if (visible || box.current === null) box.current = next;
  return box.current;
}
