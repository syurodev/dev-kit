import * as React from "react";
import { useBottomContentStore } from "@/stores/bottomContent";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";

/**
 * Registers `content` as the calling module's bottom-slot panel for as long as
 * the component stays mounted. Registration is independent of whether the slot
 * is currently shown — that is a global toggle (useBottomSlotStore), so a
 * module registers unconditionally and the shell decides visibility.
 *
 * Reuses SidebarModuleIdContext rather than a second identical context: both
 * slots ask "which module is this subtree", and WorkbenchViews already provides
 * it once per module subtree.
 *
 * Deliberately reads only the store's setter: see stores/bottomContent.ts for
 * why subscribing to the content itself here would deadlock the render loop.
 */
export function useBottomContent(content: React.ReactNode | null) {
  const moduleId = React.useContext(SidebarModuleIdContext);
  const setContent = useBottomContentStore((s) => s.setContent);

  React.useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}
