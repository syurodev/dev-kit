import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import { indentSize, type IndentSize, type IndentStyle } from "./editing";

type Preferences = {
  size: IndentSize;
  style: IndentStyle;
  setSize: (size: IndentSize) => void;
  setStyle: (style: IndentStyle) => void;
};

export const useEditorPreferences = create<Preferences>()(persist((set) => ({
  size: 2,
  style: "spaces",
  setSize: (size) => set({ size: indentSize(size) }),
  setStyle: (style) => set({ style: style === "tabs" ? "tabs" : "spaces" }),
}), {
  name: "code-editor-indentation",
  storage: uiStateStorage,
  partialize: ({ size, style }) => ({ size, style }),
  merge: (saved, current) => {
    const value = saved as Partial<Preferences> | undefined;
    return { ...current, size: indentSize(value?.size), style: value?.style === "tabs" ? "tabs" : "spaces" };
  },
}));
