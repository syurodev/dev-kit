import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileCode2, Plus, Trash2 } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { EmptyState } from "@/components/ui/empty-state";
import { SidebarRow, SidebarSection } from "@/components/ui/sidebar-shell";
import { ListSkeleton } from "@/components/ui/skeleton";
import { listConsoles, type StoredConsole } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";
import { consoleLabel, groupConsoles, whenOf } from "../consoleList";
import { useDatabaseStore } from "../databaseStore";
import { closeConsoleTab, openConsoleTab, openNewConsoleTab } from "../documents";

export const CONSOLE_FILES_QUERY = ["database", "consoles"] as const;

/**
 * Delete, asked twice, without a dialog.
 *
 * A console file has no trash behind it and often holds an hour nobody named
 * — and this control lives in a hover slot in a dense list, which is where
 * misclicks happen. A modal for a scratch file is heavier than the act, so
 * the button asks instead: the first press arms it, the second does it, and
 * walking away is the answer to having pressed it by accident.
 */
function DeleteConsoleButton({ label, onConfirm }: { label: string; onConfirm: () => void }) {
  const [armed, setArmed] = useState(false);

  useEffect(() => {
    if (!armed) return;
    const timer = setTimeout(() => setArmed(false), 3000);
    return () => clearTimeout(timer);
  }, [armed]);

  if (armed) {
    return (
      <Button
        variant="ghost"
        size="xs"
        aria-label={`Confirm deleting ${label}`}
        className="text-code-11 text-status-danger hover:bg-status-danger/10 hover:text-status-danger"
        onClick={onConfirm}
        onMouseLeave={() => setArmed(false)}
      >
        Delete?
      </Button>
    );
  }
  return (
    <ActionTooltip label={`Delete ${label}`} wrap>
      <Button
        variant="ghost"
        size="icon-xs"
        className="hidden group-hover:inline-flex"
        onClick={() => setArmed(true)}
      >
        <Trash2 className="size-3 text-text-tertiary hover:text-status-danger" />
      </Button>
    </ActionTooltip>
  );
}

/**
 * Every console on disk, under the connection it was written for.
 *
 * The reason this exists: closing a tab is not deleting a console any more,
 * and before this there was nothing that could open one again. A console
 * whose tab was closed — or whose tab died with a crash, or that arrived in
 * the folder the setting points at — had no way back into the window at all.
 *
 * It reads the FILES rather than the store's open consoles. The store only
 * knows the ones this window has touched, which is precisely the set that did
 * not need a list.
 */
export function ConsoleFilesPanel() {
  const client = useQueryClient();
  const { profiles, selectedProfileId, setSelectedProfileId, hydrateConsoles, deleteConsoleFile } =
    useDatabaseStore(
      useShallow((s) => ({
        profiles: s.profiles,
        selectedProfileId: s.selectedProfileId,
        setSelectedProfileId: s.setSelectedProfileId,
        hydrateConsoles: s.hydrateConsoles,
        deleteConsoleFile: s.deleteConsoleFile,
      })),
    );

  const { data, isLoading, error } = useQuery({
    queryKey: CONSOLE_FILES_QUERY,
    queryFn: listConsoles,
    // The files change on a debounce as the user types in another pane. A
    // list that is a second stale is fine; one that refetches per keystroke
    // is a file read per keystroke.
    staleTime: 2000,
    refetchOnWindowFocus: true,
  });

  const groups = groupConsoles(data ?? [], profiles);

  async function open(file: StoredConsole) {
    // Point the module at the connection the console was written for, the way
    // the tree's own "Open query console" does. Without it the console opens
    // against whatever was last selected, its binding chip says so, and the
    // first ⌘↵ goes somewhere the text was not written for.
    if (file.profileId && file.profileId !== selectedProfileId) {
      if (profiles.some((profile) => profile.id === file.profileId)) {
        setSelectedProfileId(file.profileId);
      }
    }
    // The same rule hydration uses, with a loader of exactly one file: fill
    // the slice only if nobody has typed in it. Reusing the action rather
    // than assigning the text keeps "never land on top of a query in
    // progress" in one place.
    await hydrateConsoles(async () => [file]);
    openConsoleTab(file.id, consoleLabel(file.sql).slice(0, 24) || "Console");
  }

  function remove(file: StoredConsole) {
    // The tab first: deleting a console deletes the document, and an editor
    // left open over a file that no longer exists writes the whole thing
    // back on the next keystroke. Closing releases its queued write, and the
    // delete that follows cancels anything still pending — in that order,
    // nothing resurrects.
    closeConsoleTab(file.id);
    deleteConsoleFile(file.id);
    client.setQueryData<StoredConsole[]>(CONSOLE_FILES_QUERY, (current) =>
      (current ?? []).filter((entry) => entry.id !== file.id),
    );
  }

  if (isLoading) {
    return <ListSkeleton className="px-2 pt-2" />;
  }

  // Only when there is nothing to show. A refetch that fails — the folder on
  // a disk that went away, a call made while the window was asleep — must not
  // blank a list that is still perfectly good; react-query keeps the last
  // data beside the error, and the last data is the better answer.
  if (error && !data) {
    return (
      <EmptyState size="rail" tone="danger" title="Could not load consoles" body={errorMessage(error)} />
    );
  }

  if (groups.length === 0) {
    return (
      <EmptyState
        size="rail"
        title="No consoles yet"
        body="One is written for you as soon as you type in a query console."
        action={{ label: "New console", icon: Plus, onClick: () => openNewConsoleTab() }}
      />
    );
  }

  return (
    <>
      {groups.map((group, index) => (
        <div key={group.profileId || "unlinked"}>
          <SidebarSection label={group.label} count={group.files.length} first={index === 0} />
          {group.files.map((file) => (
            <SidebarRow
              key={file.id}
              icon={<FileCode2 className="size-(--icon-12) shrink-0 text-text-tertiary" />}
              label={consoleLabel(file.sql) || "Empty console"}
              onClick={() => void open(file)}
              trailing={
                <>
                  {/* The age gives way to the delete on hover rather than
                      sitting beside it: two trailing items in a 240px rail
                      leave the label about six characters. */}
                  <span className="text-code-11 text-text-tertiary group-hover:hidden">
                    {whenOf(file.updatedAt)}
                  </span>
                  <DeleteConsoleButton
                    label={consoleLabel(file.sql) || "this console"}
                    onConfirm={() => remove(file)}
                  />
                </>
              }
            />
          ))}
        </div>
      ))}
    </>
  );
}
