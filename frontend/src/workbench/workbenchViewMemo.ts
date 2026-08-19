import type { WorkbenchTab } from "../stores/workbenchTabs";

/**
 * Reordering tabs changes only their strip position. The mounted content tree
 * is keyed by tab id and doesn't consume that order, so comparing the member
 * objects by identity lets React skip the expensive editor subtree on a pure
 * reorder while still invalidating for add/remove or real tab-data changes.
 */
export function haveSameTabMembers(
  previous: readonly WorkbenchTab[],
  next: readonly WorkbenchTab[],
): boolean {
  if (previous === next) {
    return true;
  }
  if (previous.length !== next.length) {
    return false;
  }

  const previousById = new Map(previous.map((tab) => [tab.id, tab]));
  return next.every((tab) => previousById.get(tab.id) === tab);
}
