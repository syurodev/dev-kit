import { showModuleHome } from "@/workbench/showModule";
import { useToolsStore } from "./toolsStore";

/**
 * Picks a tool and brings Tools on screen — what each of the manifest's eight
 * commands runs. They had no `run` at all, so the palette listed eight rows
 * that did nothing.
 *
 * Tools has no palette search of its own, on purpose: its tools ARE its
 * commands, so a search would list the same eight a second time under the
 * first.
 */
export function openTool(id: string): void {
  useToolsStore.getState().select(id);
  showModuleHome("tools");
}
