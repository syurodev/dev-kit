import { useEffect } from "react";
import { AppShell } from "@/workbench/AppShell";
import { useSplitPanelActions } from "@/components/ui/split-panel";
import { useBottomSlotStore } from "@/stores/bottomSlot";

/**
 * DEV — the real AppShell without the vault gate, so its geometry and the
 * slot layout's drag can be exercised in a browser.
 * `?shell-harness`. Fake content; no bridge calls of its own.
 * `?shell-harness&workbench` renders the real Workbench instead (App.tsx).
 */
function OpenSlots() {
  const { openPanel, openCompanion } = useSplitPanelActions();
  const setBottom = useBottomSlotStore((s) => s.setOpen);
  useEffect(() => {
    openPanel(<div className="p-4 text-body-13 text-text-secondary">Module panel content</div>, { width: 420, onClose: () => undefined });
    openCompanion(<div className="p-4 text-body-13 text-text-secondary">Agent conversation</div>, { onClose: () => undefined });
    setBottom(true);
  }, [openPanel, openCompanion, setBottom]);
  return null;
}

export default function ShellHarness() {

  return (
    <AppShell activeModuleId="codegraph" titlebar={<span className="text-body-13 text-text-secondary">Shell harness</span>}>
      <OpenSlots />
      <div className="flex min-w-0 flex-1 items-center justify-center overflow-hidden text-body-13 text-text-secondary shell-surface">Document content</div>
    </AppShell>
  );
}
