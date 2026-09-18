import { useCallback, useEffect, useMemo } from "react";
import { LayoutTemplate, RotateCcw, Save, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useSplitPanel } from "@/components/ui/split-panel";
import { modKey } from "@/lib/shortcut";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { placementFor, usePaneSlotsStore } from "@/stores/paneSlots";
import { useSavedLayoutsStore } from "@/stores/savedLayouts";
import { useSidebarOpenStore } from "@/stores/sidebarShell";
import {
  BUILT_IN_PRESETS,
  planPreset,
  planReset,
  presetKeyFromEvent,
  type LayoutPlan,
  type LayoutState,
} from "./slots/layoutPresets";

const presetShortcut = (key: string) => (modKey() === "⌘" ? `⌃⇧${key}` : `Ctrl+Shift+${key}`);

/**
 * The Layouts menu in the title bar, and ⌃⇧1–⌃⇧4 (docs/dev-kit/layout.mdx §
 * Preset). Rendered inside the shell, where the split panel lives, because a
 * preset may close the focused module's pane.
 */
export function LayoutMenu({ moduleId }: { moduleId: string | null }) {
  const { panels } = useSplitPanel();
  const agentOpen = useAgentSessionsStore((s) => s.chatOpen);
  const setAgentOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const terminalOpen = useBottomSlotStore((s) => s.open);
  const setTerminalOpen = useBottomSlotStore((s) => s.setOpen);
  const sidebarOpen = useSidebarOpenStore((s) => s.open);
  const setSidebarOpen = useSidebarOpenStore((s) => s.setOpen);
  const agent = usePaneSlotsStore((s) => s.agent);
  const terminal = usePaneSlotsStore((s) => s.terminal);
  const contextByModule = usePaneSlotsStore((s) => s.contextByModule);
  const applyPlacement = usePaneSlotsStore((s) => s.applyPlacement);
  const saved = useSavedLayoutsStore((s) => (moduleId ? s.byModule[moduleId] : undefined));
  const save = useSavedLayoutsStore((s) => s.save);
  const forget = useSavedLayoutsStore((s) => s.forget);

  const now = useMemo<LayoutState>(
    () => ({
      placement: placementFor({ agent, terminal, contextByModule }, moduleId ? [moduleId] : []),
      agentOpen,
      terminalOpen,
    }),
    [agent, terminal, contextByModule, moduleId, agentOpen, terminalOpen],
  );

  const apply = useCallback(
    (plan: LayoutPlan) => {
      applyPlacement(plan.placement);
      setAgentOpen(plan.agentOpen);
      setTerminalOpen(plan.terminalOpen);
      if (plan.closeContext && moduleId) panels[moduleId]?.onClose();
    },
    [applyPlacement, setAgentOpen, setTerminalOpen, panels, moduleId],
  );

  const applyPreset = useCallback(
    (id: string) => {
      if (moduleId) apply(planPreset(id, moduleId, now));
    },
    [apply, moduleId, now],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const key = presetKeyFromEvent(e);
      const preset = key && BUILT_IN_PRESETS.find((p) => p.key === key);
      if (!preset) return;
      e.preventDefault();
      applyPreset(preset.id);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [applyPreset]);

  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button variant="titlebar" size="icon-sm" aria-label="Layouts" disabled={!moduleId}>
              <LayoutTemplate className="size-4" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">Layouts</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel>Layouts</DropdownMenuLabel>
        {BUILT_IN_PRESETS.map((p) => (
          <DropdownMenuItem key={p.id} shortcut={presetShortcut(p.key)} onSelect={() => applyPreset(p.id)}>
            {p.name}
          </DropdownMenuItem>
        ))}
        {saved && saved.length > 0 ? (
          <>
            <DropdownMenuSeparator />
            {saved.map((l) => (
              <DropdownMenuItem key={l.id} onSelect={() => apply({ ...l.state, closeContext: false })}>
                {l.name}
              </DropdownMenuItem>
            ))}
            <DropdownMenuItem icon={<Trash2 />} onSelect={() => moduleId && forget(moduleId)}>
              Forget Saved Layouts
            </DropdownMenuItem>
          </>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuCheckboxItem checked={sidebarOpen} onCheckedChange={(v) => setSidebarOpen(Boolean(v))}>
          Sidebar
        </DropdownMenuCheckboxItem>
        <DropdownMenuCheckboxItem checked={agentOpen} onCheckedChange={(v) => setAgentOpen(Boolean(v))}>
          Agent
        </DropdownMenuCheckboxItem>
        <DropdownMenuCheckboxItem checked={terminalOpen} onCheckedChange={(v) => setTerminalOpen(Boolean(v))}>
          Terminal
        </DropdownMenuCheckboxItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<Save />} onSelect={() => moduleId && save(moduleId, now)}>
          Save Current Layout
        </DropdownMenuItem>
        <DropdownMenuItem icon={<RotateCcw />} onSelect={() => moduleId && apply(planReset(moduleId, now))}>
          Reset to Default
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
