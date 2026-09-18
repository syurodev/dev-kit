import { Fragment, Profiler, useCallback, useRef, useState, type ReactNode } from "react";
import { X, Wrench } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import { recordCommit } from "@/lib/perfLog";
import { shellSquircleRef } from "@/lib/shellSquircle";
import { cn } from "@/lib/utils";
import type { ModuleManifest } from "@/modules/registry";
import { GROUP_MIN_WIDTH, MAX_GROUPS, groupOfTab, tabsOfGroup, type DocGroup } from "@/stores/documentGroups";
import { moduleOfTab, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { GAP_WIDTH, GROUP_STRIP_WIDTH } from "./panelLayout";
import { useLayoutPressure } from "./useLayoutPressure";
import { ModuleEmptyState } from "./ModuleEmptyState";
import { TabStrip } from "./tabs/TabStrip";
import { useAdoptTab } from "./tabHosts";

/**
 * The document area: one card per document group, side by side
 * (docs/dev-kit/layout.mdx § Document group). With one group this is exactly
 * the content card the shell always had — the tab strip over the views.
 *
 * The views themselves are not rendered here. WorkbenchViews renders every
 * mounted tab once, into its own host (tabHosts.ts); a card only adopts the
 * host of the tab it shows.
 */
export function DocumentGroups({
  availableModules,
  hasMetadataWarning,
  keyboardBrowsing,
  onExitBrowsing,
  onConfigureTabs,
}: {
  availableModules: ModuleManifest[];
  hasMetadataWarning: boolean;
  keyboardBrowsing: boolean;
  onExitBrowsing: () => void;
  onConfigureTabs: () => void;
}) {
  const { groups, focusedGroupId, activeModuleId } = useWorkbenchTabsStore(
    useShallow((s) => ({ groups: s.groups, focusedGroupId: s.focusedGroupId, activeModuleId: s.activeModuleId })),
  );
  const setGroupWeights = useWorkbenchTabsStore((s) => s.setGroupWeights);

  // Before the first focusModule there is no group yet: draw the one there
  // will be, so the first frame is already the shell's shape.
  const shown: DocGroup[] =
    groups.length > 0 ? groups : [{ id: "", moduleId: activeModuleId, activeTabId: "", weight: 1 }];
  const multi = shown.length > 1;
  // Shares, normalised: closing a group leaves the others' weights summing to
  // less than one, and flex-grow under one hands out only that fraction of the
  // row — the groups would shrink instead of filling it.
  const fallbackModule = availableModules[0];

  const rowRef = useRef<HTMLDivElement>(null);
  // Last to give way on a narrow window: every group but the focused one folds
  // to a strip. Clicking a strip focuses it, which unfolds it and folds another.
  const { foldGroups } = useLayoutPressure();
  const isFolded = (g: DocGroup) => multi && foldGroups && g.id !== focusedGroupId;

  // A tab dragged out of its strip: onto another group of its module it
  // moves there; onto the right edge of its own group it splits off into a
  // new group. Anywhere else the strip reorders as before.
  const moveTabToGroup = useWorkbenchTabsStore((s) => s.moveTabToGroup);
  const splitTab = useWorkbenchTabsStore((s) => s.splitTab);
  const marked = useRef<HTMLElement | null>(null);
  const tabDropTarget = useCallback(
    (tabId: string, point: { x: number; y: number }): { kind: "move" | "split"; el: HTMLElement; groupId: string } | null => {
      const el = document.elementsFromPoint(point.x, point.y).find((e): e is HTMLElement => e instanceof HTMLElement && e.dataset.docGroup !== undefined);
      if (!el) return null;
      const state = useWorkbenchTabsStore.getState();
      const target = state.groups.find((g) => g.id === el.dataset.docGroup);
      const tab = state.modules[moduleOfTab(tabId)]?.openTabs.find((t) => t.id === tabId);
      if (!target || !tab) return null;
      const home = groupOfTab(state.groups, tab);
      if (target.moduleId !== tab.moduleId) return null;
      if (home && target.id !== home.id) return { kind: "move", el, groupId: target.id };
      const r = el.getBoundingClientRect();
      const canSplit = state.groups.length < MAX_GROUPS && home !== null && tabsOfGroup(state, home).length > 1;
      if (canSplit && point.x > r.right - Math.max(64, r.width * 0.3) && point.y > r.top + 48) {
        return { kind: "split", el, groupId: target.id };
      }
      return null;
    },
    [],
  );
  const onTabDragOver = useCallback(
    (tabId: string, point: { x: number; y: number } | null) => {
      const hit = point ? tabDropTarget(tabId, point) : null;
      if (marked.current && marked.current !== hit?.el) delete marked.current.dataset.tabDrop;
      if (hit) hit.el.dataset.tabDrop = hit.kind;
      marked.current = hit?.el ?? null;
    },
    [tabDropTarget],
  );
  const onTabDrop = useCallback(
    (tabId: string, point: { x: number; y: number }) => {
      const hit = tabDropTarget(tabId, point);
      if (!hit) return false;
      if (hit.kind === "move") moveTabToGroup(tabId, hit.groupId);
      else splitTab(tabId);
      return true;
    },
    [tabDropTarget, moveTabToGroup, splitTab],
  );
  // Among the groups still open: a folded strip's share must go to them.
  const totalWeight = shown.filter((g) => !isFolded(g)).reduce((sum, g) => sum + g.weight, 0) || 1;

  return (
    <div ref={rowRef} className="flex min-h-0 min-w-0 flex-1">
      {shown.map((group, i) => (
        <Fragment key={group.id || "initial"}>
          {i > 0 && (isFolded(shown[i - 1]) || isFolded(group)) ? (
            <div className="shrink-0" style={{ width: GAP_WIDTH }} aria-hidden />
          ) : i > 0 ? (
            <GroupSash
              left={shown[i - 1]}
              right={group}
              rowRef={rowRef}
              onWeights={setGroupWeights}
            />
          ) : null}
          <GroupCard
            group={group}
            folded={isFolded(group)}
            share={group.weight / totalWeight}
            module={availableModules.find((m) => m.id === group.moduleId) ?? fallbackModule}
            focused={group.id === focusedGroupId || shown.length === 1}
            multi={multi}
            canSplit={shown.length < MAX_GROUPS}
            siblings={shown.filter((g) => g !== group && g.moduleId === group.moduleId)}
            showWarning={hasMetadataWarning && i === 0}
            keyboardBrowsing={keyboardBrowsing && (group.id === focusedGroupId || shown.length === 1)}
            onExitBrowsing={onExitBrowsing}
            onConfigureTabs={onConfigureTabs}
            onTabDragOver={onTabDragOver}
            onTabDrop={onTabDrop}
          />
        </Fragment>
      ))}
    </div>
  );
}

function GroupCard({
  group,
  folded,
  share,
  module,
  focused,
  multi,
  canSplit,
  siblings,
  showWarning,
  keyboardBrowsing,
  onExitBrowsing,
  onConfigureTabs,
  onTabDragOver,
  onTabDrop,
}: {
  group: DocGroup;
  folded: boolean;
  share: number;
  module: ModuleManifest | undefined;
  focused: boolean;
  multi: boolean;
  canSplit: boolean;
  siblings: DocGroup[];
  showWarning: boolean;
  keyboardBrowsing: boolean;
  onExitBrowsing: () => void;
  onConfigureTabs: () => void;
  onTabDragOver: (tabId: string, point: { x: number; y: number } | null) => void;
  onTabDrop: (tabId: string, point: { x: number; y: number }) => boolean;
}) {
  // Selected with shallow equality over the tab objects: the store keeps a
  // tab's identity until it changes, so another group's edits do not reach here.
  const tabs = useWorkbenchTabsStore(useShallow((s) => tabsOfGroup(s, group)));
  const setActiveTab = useWorkbenchTabsStore((s) => s.setActiveTab);
  const closeTab = useWorkbenchTabsStore((s) => s.closeTab);
  const reorderTab = useWorkbenchTabsStore((s) => s.reorderTab);
  const pinTab = useWorkbenchTabsStore((s) => s.pinTab);
  const togglePinTab = useWorkbenchTabsStore((s) => s.togglePinTab);
  const focusGroup = useWorkbenchTabsStore((s) => s.focusGroup);
  const splitTab = useWorkbenchTabsStore((s) => s.splitTab);
  const moveTabToGroup = useWorkbenchTabsStore((s) => s.moveTabToGroup);
  const closeGroup = useWorkbenchTabsStore((s) => s.closeGroup);

  const slotRef = useRef<HTMLDivElement>(null);
  useAdoptTab(slotRef, group.activeTabId);

  // Focus follows a click or keyboard focus into the group, never hover —
  // the sidebar must not change under a passing pointer.
  const focus = useCallback(() => {
    if (group.id) focusGroup(group.id);
  }, [focusGroup, group.id]);

  const Icon = module?.icon ?? Wrench;
  const moveTargets = siblings.map((g, i) => ({
    id: g.id,
    label: siblings.length > 1 ? `Move to Group ${i + 1}` : "Move to Other Group",
  }));

  return (
    <div
      ref={shellSquircleRef}
      data-doc-group={group.id}
      data-focused={multi && focused ? "true" : undefined}
      onPointerDownCapture={focus}
      onFocusCapture={focus}
      className="doc-group relative flex min-h-0 min-w-0 flex-col overflow-hidden shell-surface"
      style={
        folded
          ? { flex: `0 0 ${GROUP_STRIP_WIDTH}px` }
          : { flex: `${share} 1 0%`, minWidth: multi ? GROUP_MIN_WIDTH : undefined }
      }
    >
      {folded ? (
        <button
          type="button"
          aria-label={`Show ${module?.title ?? "group"}: ${tabs.find((t) => t.id === group.activeTabId)?.title ?? ""}`}
          className="flex min-h-0 flex-1 flex-col items-center gap-2 py-3 text-text-secondary hover:bg-state-soft hover:text-text-primary"
          onClick={focus}
        >
          <Icon className="size-4 shrink-0" aria-hidden />
          <span className="min-h-0 truncate text-label-12 [writing-mode:vertical-rl]">
            {tabs.find((t) => t.id === group.activeTabId)?.title ?? module?.title}
          </span>
        </button>
      ) : null}
      {/* The strip belongs to the module on screen and shows only its tabs.
          Drawn only for modules that opted in (ModuleManifest.tabs): a module
          with a single view has nothing to switch between. With more than one
          group, a module without a strip still gets a bar — its name, and the
          way to close the group. */}
      {folded ? null : module?.tabs ? (
        <Profiler id="tabs" onRender={recordCommit}>
          <div className="flex min-w-0 shrink-0 items-center">
            <div className="min-w-0 flex-1">
              <TabStrip
                tabs={tabs}
                activeTabId={group.activeTabId}
                onSelect={setActiveTab}
                onClose={closeTab}
                onReorder={reorderTab}
                onPin={pinTab}
                onTogglePin={togglePinTab}
                onConfigureTabs={onConfigureTabs}
                onNewTab={module.newTab}
                onSplitRight={canSplit && tabs.length > 1 ? splitTab : undefined}
                moveTargets={moveTargets}
                onMoveToGroup={moveTabToGroup}
                onTabDragOver={onTabDragOver}
                onTabDrop={onTabDrop}
                keyboardBrowsing={keyboardBrowsing}
                onExitBrowsing={onExitBrowsing}
              />
            </div>
            {multi ? <CloseGroupButton onClose={() => closeGroup(group.id)} /> : null}
          </div>
        </Profiler>
      ) : multi ? (
        <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border-subtle pr-1 pl-3 text-text-secondary">
          <Icon className="size-4 shrink-0" aria-hidden />
          <span className="min-w-0 flex-1 truncate text-label-12">{module?.title}</span>
          <CloseGroupButton onClose={() => closeGroup(group.id)} />
        </div>
      ) : null}
      <section
        className={cn(
          // Folded, the view stays adopted but out of sight — as a tab not
          // on screen does — so unfolding shows it as it was.
          folded ? "hidden" : "flex min-h-0 flex-1 flex-col",
          module?.padded
            ? // Half the gutter at the top when a tab strip is above, because
              // the strip already ends in 8px of its own. Without a strip the
              // section touches the card's edge and owes the full gutter.
              module.tabs
              ? "overflow-auto px-4 pb-4 pt-2"
              : "overflow-auto p-4"
            : "overflow-hidden p-0",
        )}
      >
        {showWarning ? (
          <Alert variant="error" className="m-4 mb-0 w-auto">
            <AlertDescription>Module metadata is unavailable or out of sync.</AlertDescription>
          </Alert>
        ) : null}
        {/* Nothing open is not a problem to report: say what to do next and
            give the control for it (writing.md). */}
        {tabs.length === 0 ? <ModuleEmptyState module={module} /> : null}
        <div ref={slotRef} className="contents" />
      </section>
    </div>
  );
}

function CloseGroupButton({ onClose }: { onClose: () => void }): ReactNode {
  return (
    <button
      type="button"
      aria-label="Close group"
      title="Close group"
      className="mr-1 flex size-7 shrink-0 items-center justify-center rounded-md text-text-tertiary hover:bg-state-soft hover:text-text-primary"
      onClick={onClose}
    >
      <X className="size-3.5" />
    </button>
  );
}

/**
 * Between two groups: shares their combined width out again. Weights, not
 * pixels, so the split survives the window changing size.
 */
function GroupSash({
  left,
  right,
  rowRef,
  onWeights,
}: {
  left: DocGroup;
  right: DocGroup;
  rowRef: React.RefObject<HTMLDivElement | null>;
  onWeights: (weights: Record<string, number>) => void;
}) {
  const [resizing, setResizing] = useState(false);

  const onPointerDown = (e: React.PointerEvent) => {
    const row = rowRef.current;
    const l = row?.querySelector<HTMLElement>(`[data-doc-group="${left.id}"]`);
    const r = row?.querySelector<HTMLElement>(`[data-doc-group="${right.id}"]`);
    if (!l || !r || !left.id || !right.id) return;
    e.preventDefault();
    const startX = e.clientX;
    const lw = l.getBoundingClientRect().width;
    const rw = r.getBoundingClientRect().width;
    const total = lw + rw;
    const weight = left.weight + right.weight;
    setResizing(true);
    const move = (ev: PointerEvent) => {
      const next = Math.min(Math.max(lw + ev.clientX - startX, GROUP_MIN_WIDTH), total - GROUP_MIN_WIDTH);
      onWeights({ [left.id]: (next / total) * weight, [right.id]: ((total - next) / total) * weight });
    };
    const up = () => {
      setResizing(false);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  };

  return (
    <ShellResizeHandle
      orientation="vertical"
      aria-label="Resize groups"
      onPointerDown={onPointerDown}
      isResizing={resizing}
      style={{ width: GAP_WIDTH }}
    />
  );
}

