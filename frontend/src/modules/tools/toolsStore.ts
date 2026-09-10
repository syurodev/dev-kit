import { create } from "zustand";
import { executeCommand } from "@/bridge/commands";
import { errorMessage } from "@/bridge/errors";
import { TOOLS, toolById, type HashAlgorithm, type UUIDVersion } from "./toolCatalogue";

/** Every command answers with an Output field; only hash adds to it. */
type CommandResult = { Output: string };

type ToolsState = {
  /** The registry command id. Never empty: the rail always has a selection. */
  selected: string;
  algorithm: HashAlgorithm;
  uuidVersion: UUIDVersion;
  /** Input per tool, so switching tools does not discard what was typed. */
  inputs: Record<string, string>;
  /** Output per tool, for the same reason. */
  outputs: Record<string, string>;
  errors: Record<string, string>;
  running: boolean;
  select: (id: string) => void;
  setAlgorithm: (a: HashAlgorithm) => void;
  setUuidVersion: (v: UUIDVersion) => void;
  setInput: (value: string) => void;
  run: () => Promise<void>;
  clear: () => void;
};

/**
 * Tools has no state worth persisting — every command is a pure function — but
 * the rail is registered from the view and has to tell the pane which tool was
 * picked, and a store is the smallest thing that lets two subtrees agree.
 *
 * Input and output are keyed by tool rather than held as one field each. With a
 * single field, picking a different tool in the rail silently threw away what
 * was typed; with a map, the rail is navigation and nothing else.
 */
export const useToolsStore = create<ToolsState>((set, get) => ({
  selected: TOOLS[0].id,
  algorithm: "sha256",
  uuidVersion: "v4",
  inputs: {},
  outputs: {},
  errors: {},
  running: false,

  select: (selected) => set({ selected }),
  setAlgorithm: (algorithm) => set({ algorithm }),
  setUuidVersion: (uuidVersion) => set({ uuidVersion }),
  setInput: (value) =>
    set((state) => ({
      inputs: { ...state.inputs, [state.selected]: value },
      // A stale result under a changed input reads as the answer to the new
      // one. Clearing on edit is what keeps the output well honest.
      outputs: { ...state.outputs, [state.selected]: "" },
      errors: { ...state.errors, [state.selected]: "" },
    })),

  clear: () =>
    set((state) => ({
      inputs: { ...state.inputs, [state.selected]: "" },
      outputs: { ...state.outputs, [state.selected]: "" },
      errors: { ...state.errors, [state.selected]: "" },
    })),

  run: async () => {
    const { selected, algorithm, uuidVersion, inputs } = get();
    const tool = toolById(selected);
    if (!tool) return;

    // Only the fields this command reads. The bridge decodes the whole map
    // either way, but sending a version to a hash is a lie about the call.
    const payload: Record<string, string> =
      tool.shape === "generator"
        ? { version: uuidVersion }
        : tool.shape === "text-with-algorithm"
          ? { algorithm, input: inputs[selected] ?? "" }
          : { input: inputs[selected] ?? "" };

    set({ running: true, errors: { ...get().errors, [selected]: "" } });
    try {
      const result = await executeCommand<CommandResult>({ id: selected, payload });
      set((state) => ({
        outputs: { ...state.outputs, [selected]: result.Output },
        errors: { ...state.errors, [selected]: "" },
      }));
    } catch (e) {
      set((state) => ({
        outputs: { ...state.outputs, [selected]: "" },
        errors: { ...state.errors, [selected]: errorMessage(e) },
      }));
    } finally {
      set({ running: false });
    }
  },
}));
