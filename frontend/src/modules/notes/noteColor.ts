import type { CSSProperties } from "react";
import { FOLDER_COLORS, type FolderColor } from "./folderColors";

export function notePanelStyle(color: string): CSSProperties {
  if (!FOLDER_COLORS.includes(color as FolderColor)) {
    return {};
  }
  const base = `var(--note-folder-${color})`;
  return {
    background: `color-mix(in oklch, ${base} 30%, transparent)`,
    borderColor: `color-mix(in oklch, ${base} 45%, transparent)`,
  };
}
