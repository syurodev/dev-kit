import { useEffect } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  listFolders,
  listNotes,
  listTrashedNotes,
  onNotesChanged,
  type NoteFolder,
  type NoteSummary,
} from "../../bridge/notes";
import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { useNotesSidebarStore } from "./notesSidebarStore";

export type NoteCounts = { all: number; ungrouped: number; byFolder: Map<string, number> };

// One counts object per (notes, folders) pair. The rail's store is
// identity-guarded (setData), and more than one component loads the rail —
// the module's own sidebar and whichever view registers over it — so each
// computing its own object for the same lists would set the store back and
// forth for nothing.
const countsCache = new WeakMap<NoteSummary[], WeakMap<NoteFolder[], NoteCounts>>();

export function countNotes(notes: NoteSummary[], folders: NoteFolder[]): NoteCounts {
  let byFolders = countsCache.get(notes);
  const cached = byFolders?.get(folders);
  if (cached) return cached;
  const folderIds = new Set(folders.map((f) => f.id));
  let ungrouped = 0;
  const byFolder = new Map<string, number>();
  for (const n of notes) {
    if (!n.folderId || !folderIds.has(n.folderId)) {
      ungrouped += 1;
    } else {
      byFolder.set(n.folderId, (byFolder.get(n.folderId) ?? 0) + 1);
    }
  }
  const counts = { all: notes.length, ungrouped, byFolder };
  if (!byFolders) {
    byFolders = new WeakMap();
    countsCache.set(notes, byFolders);
  }
  byFolders.set(folders, counts);
  return counts;
}

let lastInvalidatedToken: number | null = null;

/**
 * Re-reads every notes query once per refresh bump, however many mounted
 * components watch the token. The first token seen is only recorded: the
 * queries are fetching for the first time anyway.
 */
export function invalidateNotesOnce(queryClient: QueryClient, token: number): void {
  if (lastInvalidatedToken === null) {
    lastInvalidatedToken = token;
    return;
  }
  if (token === lastInvalidatedToken) return;
  lastInvalidatedToken = token;
  void queryClient.invalidateQueries({ queryKey: ["notes"] });
}

/**
 * Loads what the rail lists — every live note, the folders, the trash count —
 * and hands it to the rail's store.
 *
 * The dashboard used to be the only loader, so a Notes module with no tab
 * open (or only an editor) showed an empty rail until "Open notes" was
 * pressed. The rail loads for itself now; the dashboard's unfiltered list
 * shares the same query key, so the two cost one request.
 *
 * Always the full list, never the dashboard's search results: typing in the
 * search box filtered the rail's folders and counts along with the cards.
 */
export function useNotesRailData(): void {
  const queryClient = useQueryClient();
  const refreshToken = useNotesRefreshStore((s) => s.token);
  const setData = useNotesSidebarStore((s) => s.setData);
  const setTrashCount = useNotesSidebarStore((s) => s.setTrashCount);

  const { data: notes = EMPTY_ARRAY } = useQuery({
    queryKey: ["notes", "list", ""],
    queryFn: () => listNotes(),
  });
  const { data: folders = EMPTY_ARRAY } = useQuery({
    queryKey: ["notes", "folders"],
    queryFn: () => listFolders(),
  });
  const { data: trashed = EMPTY_ARRAY } = useQuery({
    queryKey: ["notes", "trash"],
    queryFn: () => listTrashedNotes(),
  });

  useEffect(() => {
    invalidateNotesOnce(queryClient, refreshToken);
  }, [queryClient, refreshToken]);

  // A note or folder can change from outside this window entirely — an MCP
  // tool, another tab — so the rail's own save/delete calling bump() is not
  // enough. Same bump(), just a second trigger for it, for as long as the
  // rail is mounted.
  useEffect(() => {
    return onNotesChanged(() => {
      useNotesRefreshStore.getState().bump();
    });
  }, []);

  useEffect(() => {
    setData({ notes, folders, counts: countNotes(notes, folders) });
  }, [notes, folders, setData]);

  useEffect(() => {
    setTrashCount(trashed.length);
  }, [trashed.length, setTrashCount]);
}
