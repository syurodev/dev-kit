import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

/**
 * Font/size preferences for code surfaces (CodeMirror) and the terminal
 * (xterm). Options mirror the Figma Settings/Appearance dropdown; fonts not
 * installed on the machine fall back through the stack below.
 */
export const monoFontOptions = [
  "Geist Mono",
  "JetBrains Mono",
  "SF Mono",
  "Fira Code",
  "IBM Plex Mono",
  "Menlo",
] as const;
export type MonoFont = (typeof monoFontOptions)[number];

// Label -> CSS family. Geist Mono ships bundled as a variable webfont under
// the "Geist Mono Variable" family name; the rest are system-installed.
const cssFamily: Record<MonoFont, string> = {
  "Geist Mono": "'Geist Mono Variable'",
  "JetBrains Mono": "'JetBrains Mono'",
  "SF Mono": "'SF Mono'",
  "Fira Code": "'Fira Code'",
  "IBM Plex Mono": "'IBM Plex Mono'",
  Menlo: "Menlo",
};

/**
 * A literal font list (no var()) — xterm measures cell width through a
 * canvas where CSS custom properties do not resolve (see TerminalPane).
 */
export function monoFontStack(font: MonoFont): string {
  const head = cssFamily[font];
  return head === cssFamily["Geist Mono"]
    ? `${head}, monospace`
    : `${head}, 'Geist Mono Variable', monospace`;
}

type AppearanceState = {
  codeFont: MonoFont;
  codeSize: number;
  terminalFont: MonoFont;
  terminalSize: number;
  setAppearance: (
    patch: Partial<Pick<AppearanceState, "codeFont" | "codeSize" | "terminalFont" | "terminalSize">>,
  ) => void;
};

export const useAppearanceStore = create<AppearanceState>()(
  persist(
    (set) => ({
      codeFont: "Geist Mono",
      codeSize: 13,
      terminalFont: "Geist Mono",
      terminalSize: 12,
      setAppearance: (patch) => set(patch),
    }),
    {
      name: "appearance",
      storage: uiStateStorage,
    },
  ),
);

// Mirror the code-font prefs into CSS custom properties on <html>, so every
// CodeMirror instance follows live without remounting (its theme reads the
// vars). xterm cannot use vars — TerminalPane subscribes to the store instead.
//
// The face also becomes the app's monospace (--font-mono, index.css). Only
// CodeMirror used to follow it: the result grid, a tree's column types, key
// caps and every other text-code-* surface stayed on Geist Mono whatever the
// setting said. The size stays CodeMirror's alone — those surfaces keep their
// type scale.
function applyCssVars(state: AppearanceState) {
  const root = document.documentElement;
  const stack = monoFontStack(state.codeFont);
  root.style.setProperty("--code-font-family", stack);
  root.style.setProperty("--font-mono-choice", stack);
  root.style.setProperty("--code-font-size", `${state.codeSize}px`);
}
if (typeof document !== "undefined") {
  applyCssVars(useAppearanceStore.getState());
  useAppearanceStore.subscribe(applyCssVars);
}
