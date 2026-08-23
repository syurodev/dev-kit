import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TerminalPane } from "@/modules/codegraph/terminal/TerminalPane";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { cn } from "@/lib/utils";

/**
 * The bottom slot's terminals, owned by the shell.
 *
 * Every pane stays mounted and inactive ones are merely hidden. Unmounting on
 * tab switch would kill the shell and whatever is running in it, which is the
 * one thing tabs exist to avoid. xterm re-fits itself when a hidden pane comes
 * back, because its size changes from zero and the ResizeObserver fires.
 *
 * TabsContent is deliberately not used: Radix unmounts inactive content, which
 * is exactly the behaviour above rules out. The tab strip still comes from the
 * shared component so it matches every other tab row in the app.
 */
export function TerminalWorkspace({ moduleContent }: { moduleContent?: React.ReactNode }) {
  const tabs = useTerminalTabsStore((s) => s.tabs);
  const activeKey = useTerminalTabsStore((s) => s.activeKey);
  const openTab = useTerminalTabsStore((s) => s.openTab);
  const closeTab = useTerminalTabsStore((s) => s.closeTab);
  const setActive = useTerminalTabsStore((s) => s.setActive);

  const MODULE_KEY = "__module";
  const showModule = moduleContent != null;
  const value = activeKey || (showModule ? MODULE_KEY : "");

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Tabs
        value={value}
        onValueChange={setActive}
        className="flex shrink-0 items-center gap-2 border-b border-border-subtle px-2 py-1.5"
      >
        <TabsList variant="line" className="min-w-0 flex-1 justify-start overflow-x-auto">
          {showModule && <TabsTrigger value={MODULE_KEY}>Panel</TabsTrigger>}
          {tabs.map((t) => (
            <TabsTrigger key={t.key} value={t.key} className="group/tab gap-1 pr-1">
              {t.title}
              <span
                role="button"
                tabIndex={-1}
                aria-label={`Close ${t.title}`}
                onPointerDown={(e) => {
                  // pointerdown, not click: the trigger activates on pointer
                  // down, so a click handler would first switch to the tab it
                  // is about to close.
                  e.preventDefault();
                  e.stopPropagation();
                  closeTab(t.key);
                }}
                className="rounded p-0.5 opacity-50 hover:bg-foreground/10 hover:opacity-100"
              >
                <X size={11} />
              </span>
            </TabsTrigger>
          ))}
        </TabsList>
        <Button
          size="icon"
          variant="ghost"
          className="size-6 shrink-0"
          aria-label="New terminal"
          onClick={openTab}
        >
          <Plus size={14} />
        </Button>
      </Tabs>

      <div className="relative min-h-0 flex-1">
        {showModule && (
          <div className={cn("absolute inset-0", value !== MODULE_KEY && "hidden")}>
            {moduleContent}
          </div>
        )}
        {tabs.map((t) => (
          <div key={t.key} className={cn("absolute inset-0", value !== t.key && "hidden")}>
            <TerminalPane />
          </div>
        ))}
        {tabs.length === 0 && !showModule && (
          <div className="flex h-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
            <p>No terminal open.</p>
            <Button size="sm" variant="secondary" className="h-7 px-2 text-xs" onClick={openTab}>
              New terminal
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
