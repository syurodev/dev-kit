import { create } from "zustand";
import { executeCommand } from "@/bridge/commands";
import { errorMessage } from "@/bridge/errors";

export const HASH_ALGORITHMS = ["sha256", "sha512", "md5"] as const;
export type HashAlgorithm = (typeof HASH_ALGORITHMS)[number];
export const HASH_LABELS: Record<HashAlgorithm, string> = { sha256: "SHA-256", sha512: "SHA-512", md5: "MD5" };

type HashResult = { Algorithm: string; Output: string };

type ToolsState = {
  /** The registry command id, or "" when nothing is picked. */
  selected: string;
  algorithm: HashAlgorithm;
  input: string;
  output: string;
  error: string;
  running: boolean;
  select: (id: string) => void;
  setAlgorithm: (a: HashAlgorithm) => void;
  setInput: (s: string) => void;
  runHash: () => Promise<void>;
};

/**
 * Tools has no state worth keeping — every command is a pure function — but
 * the rail is registered from the view and has to tell the pane which tool
 * was picked, and a store is the smallest thing that lets two subtrees agree.
 */
export const useToolsStore = create<ToolsState>((set, get) => ({
  selected: "tools.hash",
  algorithm: "sha256",
  input: "",
  output: "",
  error: "",
  running: false,
  select: (id) => set({ selected: id }),
  setAlgorithm: (algorithm) => set({ algorithm }),
  setInput: (input) => set({ input }),
  runHash: async () => {
    const { algorithm, input } = get();
    set({ running: true, error: "" });
    try {
      const result = await executeCommand<HashResult>({ id: "tools.hash", payload: { algorithm, input } });
      set({ output: result.Output });
    } catch (e) {
      set({ output: "", error: errorMessage(e) });
    } finally {
      set({ running: false });
    }
  },
}));
