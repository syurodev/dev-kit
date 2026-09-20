import { Events } from "@wailsio/runtime";
import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

/**
 * The folder the app is working in, and the modules pointed somewhere else.
 * `pins` holds only the modules that are NOT following: a module absent from it
 * uses `current`.
 */
export type WorkFolderState = {
  current: string;
  pins: Record<string, string>;
};

/** The frontend's module ids, which are also the keys a pin is filed under. */
export type WorkFolderModule = "git" | "agenthub" | "tasks";

const CHANGED_EVENT = "workfolder:changed";

export function workFolderState(): Promise<WorkFolderState> {
  return callBridge(async () => {
    const state = await AppService.WorkFolderState();
    return { current: state.current, pins: { ...(state.pins ?? {}) } } as WorkFolderState;
  });
}

/** Points one module at a folder without moving anything else. */
export function pinWorkFolder(module: WorkFolderModule, folder: string): Promise<void> {
  return callBridge(async () => {
    await AppService.WorkFolderPin(module, folder);
  });
}

/** Sends a module back to following the current folder. */
export function followWorkFolder(module: WorkFolderModule): Promise<void> {
  return callBridge(async () => {
    await AppService.WorkFolderFollow(module);
  });
}

/** Fires after any change to the current folder or a pin. Returns an unsubscribe. */
export function onWorkFolderChanged(handler: (state: WorkFolderState) => void): () => void {
  return Events.On(CHANGED_EVENT, (ev) => {
    handler({
      current: String(ev.data?.current ?? ""),
      pins: { ...((ev.data?.pins as Record<string, string> | null) ?? {}) },
    });
  });
}
