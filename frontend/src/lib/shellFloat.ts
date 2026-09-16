/**
 * Shell floats: cards the shell keeps above every module AND every modal —
 * today the agent permission toast. They sit at z-60, one step over the z-50
 * tier that dialogs, sheets, popovers and menus share, because a request that
 * fails closed in 60s must not hide behind whatever dialog happens to be open.
 *
 * Being on top is not enough to be usable. A modal Radix dialog treats a
 * pointerdown anywhere outside its content as "dismiss", so clicking Allow on
 * the toast would also close Settings. Dialog and Sheet content call
 * `ignoreShellFloatInteraction` from onInteractOutside to keep themselves open.
 */
export const SHELL_FLOAT_ATTR = "data-shell-float";

export function isInShellFloat(target: EventTarget | null): boolean {
  // Duck-typed rather than `instanceof Element`: a text node or a target from
  // another realm has no closest, and neither is inside a float.
  const closest = (target as Partial<Element> | null)?.closest;
  return typeof closest === "function" && closest.call(target, `[${SHELL_FLOAT_ATTR}]`) !== null;
}

export function ignoreShellFloatInteraction<E extends { target: EventTarget | null; preventDefault(): void }>(
  event: E,
  next?: (event: E) => void,
): void {
  if (isInShellFloat(event.target)) {
    event.preventDefault();
    return;
  }
  next?.(event);
}
