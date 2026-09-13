import { Bookmark, Copy, CopyPlus, FileCode, History, Terminal } from "lucide-react";
import { toast } from "sonner";
import type { TabAction } from "../registry";
import type { WorkbenchTab } from "@/stores/workbenchTabs";
import { shortcut } from "@/lib/shortcut";
import { copyText } from "../../bridge/clipboard";
import { consoleDirectory } from "../../bridge/database";
import { errorMessage } from "../../bridge/errors";
import { DEFAULT_CONSOLE_ID, EMPTY_CONSOLE, useDatabaseStore } from "./databaseStore";
import { newConsoleId, openConsoleTab, openHistoryTab, openNewConsoleTab } from "./documents";

/**
 * A console's file: `<folder>/<id>.sql`, joined with the separator the folder
 * already uses, so a Windows path is not handed back with a stray slash in it.
 */
export function consoleFilePath(directory: string, consoleId: string): string {
  const separator = directory.includes("\\") && !directory.includes("/") ? "\\" : "/";
  return `${directory.replace(/[\\/]+$/, "")}${separator}${consoleId}.sql`;
}

/**
 * What a Database tab adds to its right-click menu (ModuleManifest.tabActions).
 *
 * Read when the menu opens, so "Copy SQL" and "Save Query…" see the console as
 * it is now, not as it was when the tab was drawn.
 */
export function databaseTabActions(tab: WorkbenchTab): TabAction[] {
  const params = tab.params ?? {};

  if (tab.viewId === "database.table") {
    const schema = String(params.schema ?? "");
    const table = String(params.table ?? "");
    return [
      {
        type: "submenu",
        id: "copy",
        label: "Copy",
        icon: Copy,
        items: [
          { type: "item", id: "copy.name", label: "Table Name", run: () => void copyText(table) },
          {
            type: "item",
            id: "copy.qualified",
            label: "Qualified Name",
            run: () => void copyText(schema ? `${schema}.${table}` : table),
          },
        ],
      },
      {
        type: "item",
        id: "ddl",
        label: "Show DDL",
        icon: FileCode,
        run: () => useDatabaseStore.getState().openDDL(schema, table),
      },
      { type: "item", id: "console", label: "Open Query Console", icon: Terminal, run: openNewConsoleTab },
    ];
  }

  if (tab.viewId === "database.console") {
    const consoleId = typeof params.consoleId === "string" ? params.consoleId : DEFAULT_CONSOLE_ID;
    const sql = (useDatabaseStore.getState().consoles[consoleId] ?? EMPTY_CONSOLE).sql;
    const empty = !sql.trim();
    return [
      {
        type: "submenu",
        id: "copy",
        label: "Copy",
        icon: Copy,
        items: [
          {
            type: "item",
            id: "copy.path",
            label: "File Path",
            run: () => {
              consoleDirectory()
                .then((folder) => copyText(consoleFilePath(folder.path, consoleId)))
                .catch((error: unknown) => toast.error(`Could not read the console folder: ${errorMessage(error)}`));
            },
          },
          { type: "item", id: "copy.sql", label: "SQL", disabled: empty, run: () => void copyText(sql) },
        ],
      },
      {
        type: "item",
        id: "duplicate",
        label: "Duplicate",
        icon: CopyPlus,
        run: () => {
          // A new console with this one's text, in a tab of its own.
          const copyId = newConsoleId();
          useDatabaseStore.getState().setSql(copyId, sql);
          openConsoleTab(copyId, tab.title);
        },
      },
      { type: "separator", id: "separator" },
      {
        type: "item",
        id: "save",
        label: "Save Query…",
        icon: Bookmark,
        disabled: empty,
        run: () => useDatabaseStore.getState().openSaveQueryModal(consoleId),
      },
      {
        type: "item",
        id: "history",
        label: "Query History",
        icon: History,
        shortcut: shortcut("Y", { shift: true }),
        run: openHistoryTab,
      },
    ];
  }

  return [];
}
