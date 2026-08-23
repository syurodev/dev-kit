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
      {/* The Tabs root is a COLUMN by design (list above, content below), so it
          wraps the strip only. Putting the + button inside it pushed the button
          onto a second row and centred everything. Row layout is this div's
          job, not the primitive's. */}
      <div className="flex shrink-0 items-center gap-1 border-b border-border-subtle px-2 py-1">
        <Tabs value={value} onValueChange={setActive} className="min-w-0 flex-1 gap-0">
          {/* Default (segmented) rather than line: this card is bg-card, which
              resolves to surface/base, while the track is bg-muted =
              surface/raised. Two different colours, so the track reads. On the
              old popover those were the same colour and it would not have. */}
          <TabsList className="max-w-full justify-start overflow-x-auto [&::-webkit-scrollbar]:hidden">
            {showModule && (
              <TabsTrigger value={MODULE_KEY} className="flex-none px-2 text-xs">
                Panel
              </TabsTrigger>
            )}
            {tabs.map((t) => (
              <TabsTrigger key={t.key} value={t.key} className="flex-none gap-1 px-2 pr-1 text-xs">
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
                  className="rounded p-0.5 opacity-40 hover:bg-foreground/10 hover:opacity-100"
                >
                  {/* Sized by class: TabsTrigger forces size-4 on any svg that
                      has no size class of its own. */}
                  <X className="size-3" />
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Button
          size="icon"
          variant="ghost"
          className="size-6 shrink-0"
          aria-label="New terminal"
          onClick={openTab}
        >
          <Plus className="size-3.5" />
        </Button>
      </div>

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
