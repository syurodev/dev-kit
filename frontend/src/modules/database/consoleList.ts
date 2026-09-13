import type { ConnectionProfile, StoredConsole } from "../../bridge/database";

/**
 * What the rail says about a console file, as data.
 *
 * Pulled out of the panel for the same reason consoleStatus was pulled out of
 * the toolbar: all three of these are wording and ranking decisions, and a
 * component that makes them inline is a component nobody can test the wording
 * of.
 */

/**
 * A console's line in the rail: the first thing it actually does.
 *
 * Not the file name — `console-3-x8f2la` tells you nothing and is the same
 * kind of nothing for every console you have. Leading comments are skipped
 * because a console that opens with a note about what it is for still has to
 * be told apart from the other four, and the statement is what does that.
 */
export function consoleLabel(sql: string): string {
  for (const line of sql.split("\n")) {
    const text = line.trim();
    if (!text || text.startsWith("--")) continue;
    return text.replace(/\s+/g, " ").slice(0, 120);
  }
  return "";
}

/**
 * How long ago, short enough for a rail's trailing slot.
 *
 * Relative up to a week, then the date: "3d" is a useful answer and "34d" is
 * not — past a certain age nobody is counting days, they are looking for the
 * one from before the release.
 */
export function whenOf(iso: string, now: number = Date.now()): string {
  const at = new Date(iso).getTime();
  if (!iso || Number.isNaN(at)) return "";
  const seconds = Math.max(0, (now - at) / 1000);
  if (seconds < 60) return "now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  if (seconds < 604800) return `${Math.floor(seconds / 86400)}d`;
  return new Date(at).toLocaleDateString([], { day: "numeric", month: "short" });
}

export type ConsoleGroup = {
  /** "" for the consoles whose connection is gone or was never recorded. */
  profileId: string;
  label: string;
  files: StoredConsole[];
};

/** The label for consoles that belong to no connection this app still knows. */
export const UNLINKED_LABEL = "No connection";

/**
 * Consoles under the connection they were written for.
 *
 * In the connections' own order, so the rail's two lists read the same way
 * down the window. The unlinked group is last and always shown when it has
 * anything in it: those are the consoles from a deleted connection, from a
 * folder someone pointed the setting at, or from the crash that left the file
 * without its tab — the ones with no other way in.
 */
export function groupConsoles(
  files: readonly StoredConsole[],
  profiles: readonly ConnectionProfile[],
): ConsoleGroup[] {
  const groups = new Map<string, ConsoleGroup>();
  for (const profile of profiles) {
    groups.set(profile.id, { profileId: profile.id, label: profile.name, files: [] });
  }
  const unlinked: ConsoleGroup = { profileId: "", label: UNLINKED_LABEL, files: [] };

  for (const file of files) {
    (groups.get(file.profileId) ?? unlinked).files.push(file);
  }

  const ordered = [...groups.values()].filter((group) => group.files.length > 0);
  return unlinked.files.length > 0 ? [...ordered, unlinked] : ordered;
}
