import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";
import { indentSize, type IndentSize, type IndentStyle } from "./editing";

type Preferences = {
  size: IndentSize;
  style: IndentStyle;
  showIndentGuides: boolean;
  setShowIndentGuides: (show: boolean) => void;
  setSize: (size: IndentSize) => void;
  setStyle: (style: IndentStyle) => void;
};

export const useEditorPreferences = create<Preferences>()(persist((set) => ({
  size: 2,
  style: "spaces",
  showIndentGuides: true,
  setShowIndentGuides: (showIndentGuides) => set({ showIndentGuides }),
  setSize: (size) => set({ size: indentSize(size) }),
  setStyle: (style) => set({ style: style === "tabs" ? "tabs" : "spaces" }),
}), {
  name: "code-editor-indentation",
  storage: uiStateStorage,
  partialize: ({ size, style, showIndentGuides }) => ({ size, style, showIndentGuides }),
  merge: (saved, current) => {
    const value = saved as Partial<Preferences> | undefined;
    return { ...current, showIndentGuides: typeof value?.showIndentGuides === "boolean" ? value.showIndentGuides : true, size: indentSize(value?.size), style: value?.style === "tabs" ? "tabs" : "spaces" };
  },
}));
