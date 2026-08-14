export const FOLDER_COLORS = [
  "red", "orange", "yellow", "green", "teal", "blue", "violet", "pink", "gray",
] as const;
export type FolderColor = (typeof FOLDER_COLORS)[number];

export function folderDotVar(color: string): string {
  return FOLDER_COLORS.includes(color as FolderColor)
    ? `var(--note-folder-${color})`
    : "var(--note-folder-gray)";
}
