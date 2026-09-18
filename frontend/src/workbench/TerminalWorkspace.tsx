import { Suspense, lazy } from "react";
import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { cn } from "@/lib/utils";

// xterm plus its stylesheet is 324 KiB — a third of the shell's initial JS for
// something nobody sees until they open a terminal. Splitting it here rather
// than at the workspace keeps the tab strip in the shell chunk, so the strip
// and the "New terminal" button still paint in the first frame.
const TerminalPane = lazy(() =>
  import("@/modules/codegraph/terminal/TerminalPane").then((m) => ({
    default: m.TerminalPane,
  })),
);

const MODULE_KEY = "__module";

function useTerminalValue(showModule: boolean) {
  const activeKey = useTerminalTabsStore((s) => s.activeKey);
  return activeKey || (showModule ? MODULE_KEY : "");
}

/**
 * The terminals' tab strip, drawn in the Terminal pane's own header
 * (components/ui/pane-slots.tsx) rather than as a second band under it: a
 * pane header and a tab strip stacked read as two title bars.
 */
export function TerminalTabs({ showModule }: { showModule: boolean }) {
  const tabs = useTerminalTabsStore((s) => s.tabs);
  const openTab = useTerminalTabsStore((s) => s.openTab);
  const closeTab = useTerminalTabsStore((s) => s.closeTab);
  const setActive = useTerminalTabsStore((s) => s.setActive);
  const value = useTerminalValue(showModule);

  return (
    <div className="flex min-w-0 flex-1 items-center gap-1">
      {/* Nothing open: the header still says what the pane is. */}
      {tabs.length === 0 && !showModule ? (
        <span className="min-w-0 flex-1 truncate text-label-12">Terminal</span>
      ) : null}
      <Tabs
        value={value}
        onValueChange={setActive}
        className={cn("min-w-0 gap-0", (tabs.length > 0 || showModule) && "flex-1")}
      >
        <TabsList className="h-6 max-w-full justify-start gap-2 bg-transparent p-0 overflow-x-auto [&::-webkit-scrollbar]:hidden">
          {showModule && (
            <TabsTrigger
              value={MODULE_KEY}
              className="w-[190px] h-6 flex-none rounded-sm px-2 text-label-12 data-[state=active]:bg-surface-raised data-[state=active]:text-text-primary data-[state=inactive]:bg-state-soft data-[state=inactive]:text-text-secondary"
            >
              Panel
            </TabsTrigger>
          )}
          {tabs.map((t) => (
            <TabsTrigger
              key={t.key}
              value={t.key}
              className="w-[190px] h-6 flex-none gap-1 rounded-sm px-2 pr-1.5 text-label-12 data-[state=active]:bg-surface-raised data-[state=active]:text-text-primary data-[state=inactive]:bg-state-soft data-[state=inactive]:text-text-secondary"
            >
              <span className="truncate flex-1 text-left">{t.title}</span>
              <span
                role="button"
                tabIndex={-1}
                aria-label={`Close ${t.title}`}
                onPointerDown={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  closeTab(t.key);
                }}
                className="rounded p-0.5 opacity-60 hover:bg-foreground/10 hover:opacity-100"
              >
                <X className="size-3" />
              </span>
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <div className="flex items-center gap-1">
        <Button
          size="icon-xs"
          variant="titlebar"
          aria-label="New terminal"
          onClick={() => openTab()}
        >
          <Plus className="size-[13px]" />
        </Button>
      </div>
    </div>
  );
}

/**
 * The bottom slot's terminals, owned by the shell.
 *
 * Every pane stays mounted and inactive ones are merely hidden. Unmounting on
 * tab switch would kill the shell and whatever is running in it, which is the
 * one thing tabs exist to avoid. xterm re-fits itself when a hidden pane comes
 * back, because its size changes from zero and the ResizeObserver fires.
 *
 * Not TabsContent: Radix unmounts inactive content, which is what the paragraph
 * above rules out. The strip is TerminalTabs, in the pane header.
 */
export function TerminalWorkspace({ moduleContent }: { moduleContent?: React.ReactNode }) {
  const tabs = useTerminalTabsStore((s) => s.tabs);
  const openTab = useTerminalTabsStore((s) => s.openTab);
  const showModule = moduleContent != null;
  const value = useTerminalValue(showModule);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="relative min-h-0 flex-1">
        {showModule && (
          <div className={cn("absolute inset-0", value !== MODULE_KEY && "hidden")}>
            {moduleContent}
          </div>
        )}
        {tabs.map((t) => (
          <div key={t.key} className={cn("absolute inset-0", value !== t.key && "hidden")}>
            <Suspense fallback={null}>
              <TerminalPane start={t.start} onExit={t.onExit} />
            </Suspense>
          </div>
        ))}
        {tabs.length === 0 && !showModule && (
          <div className="flex h-full flex-col items-center justify-center gap-2 text-body-13 text-text-tertiary">
            <p>No terminal open.</p>
            <Button size="sm" variant="secondary" className="h-7 px-2 text-label-12" onClick={() => openTab()}>
              New terminal
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
