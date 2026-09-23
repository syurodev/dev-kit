import { FOLDER_COLORS, folderDotVar, type FolderColor } from "@/modules/notes/folderColors";

export { folderDotVar };

/**
 * Two-letter initials for a workspace's avatar, same rule an OS uses for a
 * folder/app initial: first letter of up to two words. "acme" → "AC",
 * "billing-service" → "BS" (hyphens count as word breaks), single word "x" →
 * "X".
 */
export function workspaceInitials(name: string): string {
  const words = name.trim().split(/[\s_-]+/).filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}

/**
 * A deterministic color from the same 9-hue palette workspace.mdx picked for
 * every "assign a color to a named thing" surface in the app (notes,
 * folders — see docs/dev-kit/workspace.mdx's Known ceiling on reusing
 * note/*). No color field on Workspace itself, so this hashes the id: stable
 * across renders and reloads, and two workspaces named the same still read
 * apart if their ids differ.
 */
export function workspaceColor(id: string): FolderColor {
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = (hash * 31 + id.charCodeAt(i)) | 0;
  }
  const index = Math.abs(hash) % FOLDER_COLORS.length;
  return FOLDER_COLORS[index];
}
