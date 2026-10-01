import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useApiClientStore } from "./apiclientStore";
import { MODULE } from "./documents";

/**
 * The palette's "Import cURL or Postman…". The dialog lives in the rail, so
 * the module is brought on screen first — the rail is what mounts it.
 */
export function openImport() {
  useWorkbenchTabsStore.getState().focusModule(MODULE);
  useApiClientStore.getState().setImportKind("curl");
}
