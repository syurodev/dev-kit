import { create } from "zustand";
import {
  followWorkFolder,
  onWorkFolderChanged,
  pinWorkFolder,
  workFolderState,
  type WorkFolderModule,
  type WorkFolderState,
} from "@/bridge/workfolder";

/**
 * The folder the app is working in — Go's, mirrored. Go is the authority: Code
 * Graph opening a project sets it, and it survives a restart because the
 * restored project sets it again. Nothing here is persisted, and a pin lasts
 * until the app quits: a module comes back following the folder the person
 * last opened, which is the folder they will expect to find.
 */
type WorkFolderStore = WorkFolderState & {
  /** False until Go has been asked once, so "no folder" is not shown for "not loaded". */
  loaded: boolean;
};

export const useWorkFolderStore = create<WorkFolderStore>()(() => ({
  current: "",
  pins: {},
  loaded: false,
}));

/** The folder a module works in: its pin, else the current folder. */
export function effectiveFolder(state: WorkFolderState, module: WorkFolderModule): string {
  return state.pins[module] ?? state.current;
}

let started = false;

/**
 * Reads Go's state once and follows its changes. Idempotent, and safe from a
 * hook: the subscription goes up before the read, so a change landing between
 * the two is not lost — whichever answer arrives last is the newer one, and a
 * changed event carries the whole state.
 */
export function ensureWorkFolderSync() {
  if (started) return;
  started = true;
  onWorkFolderChanged((state) => useWorkFolderStore.setState({ ...state, loaded: true }));
  workFolderState()
    .then((state) => {
      // An event that arrived first is newer than this read; keep it.
      if (!useWorkFolderStore.getState().loaded) {
        useWorkFolderStore.setState({ ...state, loaded: true });
      }
    })
    .catch(() => {
      // Left unloaded: the next hook mount tries again.
      started = false;
    });
}

/**
 * Where a module works, and the two ways to change that: pin it to a folder of
 * its own, or send it back to following.
 */
export function useModuleFolder(module: WorkFolderModule) {
  ensureWorkFolderSync();
  const state = useWorkFolderStore();
  return {
    loaded: state.loaded,
    folder: effectiveFolder(state, module),
    /** The folder every unpinned module is in — what "follow" goes back to. */
    current: state.current,
    pinned: module in state.pins,
    pin: (folder: string) => pinWorkFolder(module, folder),
    follow: () => followWorkFolder(module),
  };
}

/** The current folder alone, for a surface that is not one of the modules and has no pin to honour. */
export function useCurrentFolder(): string {
  ensureWorkFolderSync();
  return useWorkFolderStore((s) => s.current);
}
