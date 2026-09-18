import { useEffect } from "react";
import { AppShell } from "@/workbench/AppShell";
import { useSplitPanel } from "@/components/ui/split-panel";
import { useBottomSlotStore } from "@/stores/bottomSlot";

/**
 * DEV — the real AppShell without the vault gate, so its geometry (and the
 * dock shell trial behind devkit.dockShell) can be measured in a browser.
 * `?shell-harness`. Fake content; no bridge calls of its own.
 */
function OpenSlots() {
  const { openPanel } = useSplitPanel();
  const setBottom = useBottomSlotStore((s) => s.setOpen);
  useEffect(() => {
    openPanel(<div className="p-4 text-body-13 text-text-secondary">Module panel content</div>, { width: 420, onClose: () => undefined });
    setBottom(true);
  }, [openPanel, setBottom]);
  return null;
}

export default function ShellHarness() {
  return (
    <AppShell activeModuleId="codegraph" titlebar={<span className="text-body-13 text-text-secondary">Shell harness</span>}>
      <OpenSlots />
      <div className="flex h-full items-center justify-center text-body-13 text-text-secondary">Document content</div>
    </AppShell>
  );
}
