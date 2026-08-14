import { FOLDER_COLORS, type FolderColor } from "./folderColors";

export function noteAccentColor(color: string): string | undefined {
  if (!FOLDER_COLORS.includes(color as FolderColor)) {
    return undefined;
  }
  return `var(--note-folder-${color})`;
}
