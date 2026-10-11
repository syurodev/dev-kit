import { useEffect, useRef } from "react";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { activeConsoleId } from "./documents";
import { browseKeyOfTab } from "./useConsoleLifetime";

/**
 * What the menu bar can ask of the document in front of you (#51).
 *
 * Explain needs the console's selection and caret; Export, Show DDL and Open in
 * console need the table tab's rows, filter and key. Those live in the
 * components, not the store, so each document registers its own handlers under
 * its key and the menu runs whichever belongs to the active tab. A document
 * that has no such action — Explain on a table — simply does not register one.
 */
export type DocumentCommand = "run" | "explain" | "explainAnalyze" | "export" | "reload" | "showDDL" | "openInConsole";
type Handlers = Partial<Record<DocumentCommand, () => void>>;

const byDocument = new Map<string, { current: Handlers }>();

export const consoleDocument = (consoleId: string) => `console:${consoleId}`;
export const tableDocument = (browseKey: string) => `table:${browseKey}`;

/** Registers a document's handlers for as long as it is mounted. Read through a
 * ref, so a new closure each render does not re-register. */
export function useDocumentCommands(documentKey: string, handlers: Handlers): void {
  const ref = useRef(handlers);
  ref.current = handlers;
  useEffect(() => {
    if (!documentKey) return;
    byDocument.set(documentKey, ref);
    return () => {
      if (byDocument.get(documentKey) === ref) byDocument.delete(documentKey);
    };
  }, [documentKey]);
}

function activeDocumentKey(): string {
  const state = useWorkbenchTabsStore.getState();
  const tabs = moduleTabs(state.modules, "database");
  const active = tabs.openTabs.find((tab) => tab.id === tabs.activeTabId);
  if (active?.viewId === "database.console") return consoleDocument(activeConsoleId());
  if (active?.viewId === "database.table") {
    const key = browseKeyOfTab("database.table", active.params ?? {});
    return key ? tableDocument(key) : "";
  }
  return "";
}

export function runDocumentCommand(command: DocumentCommand): void {
  byDocument.get(activeDocumentKey())?.current[command]?.();
}
