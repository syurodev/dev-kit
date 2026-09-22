import { lazy, memo, Profiler, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { recordCommit, WORKBENCH_PROFILER } from "@/lib/perfLog";
import { ModuleSkeleton } from "./ModuleSkeleton";
import { Events, Window } from "@wailsio/runtime";
import { MessageSquare, PanelBottom, PanelLeft, Search, Settings as SettingsIcon } from "lucide-react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { useMenuCommands } from "./menuCommands";
import { ModuleSwitcher } from "./ModuleSwitcher";
import { buildSwitcherItems, slotModuleIds, slotTarget } from "./switcherItems";
import { editsText, inRichTextEditor, isPaletteShortcut, moduleIndexFromEvent, peekIndexFromEvent } from "./moduleShortcuts";
import { shortcut } from "@/lib/shortcut";
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
import { CommandPalette } from "./CommandPalette";
import { SystemStatusMenu } from "./SystemStatusMenu";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { DocumentGroups } from "./DocumentGroups";
import { LayoutMenu } from "./LayoutMenu";
import { setParking, syncTabHosts, tabHost } from "./tabHosts";
import { TabSwitcher } from "./TabSwitcher";
import { InteractionHost } from "./InteractionHost";
import { onSettingsRequested, requestSettings } from "./settingsRequest";
import {
  compareModuleParity,
  CURRENT_MODULE_API_VERSION,
  getModule,
  listRegisteredModules,
  registerModule,
} from "../modules/registry";
import { toolsModule } from "../modules/tools/manifest";
import { notesModule } from "../modules/notes/manifest";
import { databaseModule } from "../modules/database/manifest";
import { sshModule } from "../modules/ssh/manifest";
import { kafkaModule } from "../modules/kafka/manifest";
import { agenthubModule } from "../modules/agenthub/manifest";
import { syncModule } from "../modules/sync/manifest";
import { settingsModule } from "../modules/settings/manifest";
import { codegraphModule } from "../modules/codegraph/manifest";
import { accountsModule } from "../modules/accounts/manifest";
import { tasksModule } from "../modules/tasks/manifest";
import { gitModule } from "../modules/git/manifest";
import { apiclientModule } from "../modules/apiclient/manifest";

import type { SettingsTab } from "@/modules/settings/SettingsView";

const SettingsModal = lazy(() =>
  import("../modules/settings/SettingsView").then((module) => ({
    default: module.SettingsModal,
  })),
);
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
import { ModuleVisibilityProvider } from "@/components/ui/module-visibility";
import { useModuleActionsStore } from "../stores/moduleActions";
import { useModuleGroupsStore } from "../stores/moduleGroups";
import { useSwitcherPeekStore } from "../stores/switcherPeek";
import {
  allOpenTabs,
  moduleTabs,
  useWorkbenchTabsStore,
  type WorkbenchTab,
} from "../stores/workbenchTabs";
import { useSidebarOpenStore } from "../stores/sidebarShell";
import { useBottomSlotStore } from "../stores/bottomSlot";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { AppShell } from "./AppShell";
import { cn } from "@/lib/utils";
import { haveSameTabMembers } from "./workbenchViewMemo";
import { focusedGroup } from "../stores/documentGroups";
import { openHomeWhenAsked } from "./showModule";

const LazyAgentPermissionToastHost = lazy(() =>
  import("./AgentPermissionToastHost").then((m) => ({
    default: m.AgentPermissionToastHost,
  })),
);

// Settings absorbs Sync as a tab (SettingsView) — both stay registered
// modules (backend still reports "sync", so removing its registration
// would trip the metadata-parity check), they just don't get their own
// switcher button anymore.
const HEADER_ONLY_MODULE_IDS = new Set(["settings", "sync"]);

// Bounds how many modules' tab content stays mounted in WorkbenchViews:
// the active module plus up to MAX_MOUNTED_MODULES - 1 more that were
// active within the last MODULE_GRACE_MS. See useMountedModuleIds below.
// 45 minutes, up from 2: a developer stays in one module for a long stretch,
// and two minutes in a console meant coming back to Database rebuilt every tab
// it had open at once. MAX_MOUNTED_MODULES still bounds the memory.
const MODULE_GRACE_MS = 45 * 60 * 1000;
const MAX_MOUNTED_MODULES = 3;


registerModule(toolsModule);
registerModule(notesModule);
registerModule(databaseModule);
registerModule(sshModule);
registerModule(kafkaModule);
registerModule(agenthubModule);
registerModule(syncModule);
registerModule(settingsModule);
registerModule(codegraphModule);
registerModule(accountsModule);
registerModule(tasksModule);
registerModule(gitModule);
registerModule(apiclientModule);

type WorkbenchViewsProps = {
  /** Every module's tabs — this layer keeps recently-left modules mounted. */
  openTabs: WorkbenchTab[];
  /** The module you are working in: the focused document group's. */
  activeModuleId: string;
  /** Modules on screen in some group, joined by "|" so the memo can compare. */
  visibleModules: string;
  /** The tab each group shows, joined by "|". */
  shownTabs: string;
};

function areWorkbenchViewsPropsEqual(
  previous: WorkbenchViewsProps,
  next: WorkbenchViewsProps,
): boolean {
  return (
    previous.activeModuleId === next.activeModuleId &&
    previous.visibleModules === next.visibleModules &&
    previous.shownTabs === next.shownTabs &&
    haveSameTabMembers(previous.openTabs, next.openTabs)
  );
}

/**
 * MRU-tracked set of non-exempt module ids allowed to stay mounted: the
 * active module always qualifies; up to MAX_MOUNTED_MODULES - 1 more
 * recently-active modules stay mounted for MODULE_GRACE_MS after being
 * left. Exempt ids (HEADER_ONLY_MODULE_IDS) are never added here — the
 * caller unions them in separately and they're always mounted regardless
 * of this hook's output.
 */
function useMountedModuleIds(
  activeModuleId: string,
  exemptIds: ReadonlySet<string>,
): Set<string> {
  const [recent, setRecent] = useState<string[]>([]);
  const prevActive = useRef("");
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());

  useEffect(() => {
    if (!activeModuleId) {
      return;
    }
    const leaving = prevActive.current;
    prevActive.current = activeModuleId;

    if (leaving && leaving !== activeModuleId && !exemptIds.has(leaving)) {
      const timer = setTimeout(() => {
        timers.current.delete(leaving);
        setRecent((prev) => prev.filter((id) => id !== leaving));
      }, MODULE_GRACE_MS);
      timers.current.set(leaving, timer);
    }

    if (exemptIds.has(activeModuleId)) {
      return;
    }

    const ownTimer = timers.current.get(activeModuleId);
    if (ownTimer) {
      clearTimeout(ownTimer);
      timers.current.delete(activeModuleId);
    }

    setRecent((prev) => {
      const next = [activeModuleId, ...prev.filter((id) => id !== activeModuleId)];
      for (const evictedId of next.slice(MAX_MOUNTED_MODULES)) {
        const evictedTimer = timers.current.get(evictedId);
        if (evictedTimer) {
          clearTimeout(evictedTimer);
          timers.current.delete(evictedId);
        }
      }
      return next.slice(0, MAX_MOUNTED_MODULES);
    });
  }, [activeModuleId, exemptIds]);

  useEffect(() => {
    const timerMap = timers.current;
    return () => {
      for (const timer of timerMap.values()) {
        clearTimeout(timer);
      }
    };
  }, []);

  return new Set(recent);
}

/**
 * One tab's view, memoised on the tab object.
 *
 * WorkbenchViews re-renders on every switch, and it used to create each tab's
 * view element inline, so every mounted tab of every mounted module re-rendered
 * with it — measured in the app at 50–90ms of "views" per module switch, for
 * tabs that stayed hidden. React Compiler did not cache elements built inside
 * the map. The store keeps a tab's object identity until that tab changes, so
 * a switch now re-renders only the wrappers whose class flips, not the views.
 */
const TabBody = memo(function TabBody({ tab }: { tab: WorkbenchTab }) {
  const view = getModule(tab.moduleId)?.views.find((candidate) => candidate.id === tab.viewId);
  if (!view) {
    return null;
  }
  const TabView = view.component;
  return (
    // Provider outside Suspense so the skeleton can register a rail under
    // this module's id while the chunk loads.
    <SidebarModuleIdContext.Provider value={tab.moduleId}>
      <Suspense fallback={<ModuleSkeleton />}>
        {/* module-enter mounts with the chunk, so its @starting-style fires at
            the skeleton→content swap and nowhere else. It is the flex box the
            `contents` wrapper gave the module before. */}
        <div className="module-enter">
          <TabView params={tab.params} />
        </div>
      </Suspense>
    </SidebarModuleIdContext.Provider>
  );
});

/**
 * Heavy module trees are deliberately isolated from tab-strip ordering.
 * Tab dragging changes only TabStrip-local state and commits to the store once
 * on drop, so this subtree stays completely outside the high-frequency path.
 *
 * Every mounted tab renders here once, portalled into its own host
 * (tabHosts.ts). The document groups adopt the hosts they show; the rest wait
 * in the parking box below, display: none — what `hidden` was for inactive
 * tabs before. So a tab moving between groups keeps its view mounted.
 */
const WorkbenchViews = memo(
  function WorkbenchViews({ openTabs, activeModuleId, visibleModules, shownTabs }: WorkbenchViewsProps) {
    const recentModuleIds = useMountedModuleIds(activeModuleId, HEADER_ONLY_MODULE_IDS);
    // Every module on screen is unioned in directly (not only through
    // recentModuleIds) because useMountedModuleIds only learns about the
    // active one via an effect, which runs one render after it changed —
    // without this, the just-activated tab would render null for that frame.
    const mountedModuleIds = new Set([
      ...HEADER_ONLY_MODULE_IDS,
      ...(activeModuleId ? [activeModuleId] : []),
      ...visibleModules.split("|").filter(Boolean),
      ...recentModuleIds,
    ]);
    const shown = new Set(shownTabs.split("|"));

    useLayoutEffect(() => {
      syncTabHosts(new Set(openTabs.map((tab) => tab.id)));
    });

    return (
      <div ref={setParking} hidden aria-hidden>
        {openTabs.map((tab) => {
          // Tabs outside the grace/cap window aren't mounted at all — see
          // useMountedModuleIds above. Their tab stays in openTabs (visible,
          // clickable) and remounts fresh when reactivated; background work
          // belonging to that module (e.g. Agent Hub sessions) lives outside
          // this render tree and is unaffected either way.
          if (!mountedModuleIds.has(tab.moduleId)) {
            return null;
          }
          return createPortal(
            <ModuleVisibilityProvider visible={shown.has(tab.id)}>
              <TabBody tab={tab} />
            </ModuleVisibilityProvider>,
            tabHost(tab.id),
            tab.id,
          );
        })}
      </div>
    );
  },
  areWorkbenchViewsPropsEqual,
);

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [backendModules, setBackendModules] = useState<ModuleDTO[] | null>(null);
  const [metadataError, setMetadataError] = useState(false);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(false);
  // "browsing": the real tab strip is forced to full width, ArrowLeft/
  // ArrowRight + Enter navigate it directly (TabStrip.tsx) — the first
  // ⌘⇧E press. "popup": the detail popup (TabSwitcher.tsx) — reached by
  // holding the key down or pressing it again while already browsing.
  const [switcher, setSwitcher] = useState<"closed" | "browsing" | "popup">("closed");
  const [settingsOpen, setSettingsOpen] = useState(false);
  // Which section Settings opens on. Only "Configure Tabs…" asks for one; it
  // is cleared on close so the title-bar button still opens where it always did.
  const [settingsTab, setSettingsTab] = useState<SettingsTab | undefined>(undefined);
  useEffect(
    () =>
      onSettingsRequested((tab) => {
        setSettingsTab(tab);
        setSettingsOpen(true);
      }),
    [],
  );
  const [isFullscreen, setIsFullscreen] = useState(false);
  const sidebarOpen = useSidebarOpenStore((s) => s.open);
  const setSidebarOpen = useSidebarOpenStore((s) => s.setOpen);
  const bottomOpen = useBottomSlotStore((s) => s.open);
  const setBottomOpen = useBottomSlotStore((s) => s.setOpen);
  const chatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);
  const pendingAgentHubCommand = useAgentSessionsStore((s) => s.pendingAgentHubCommand);

  const moduleActions = useModuleActionsStore((s) => s.actions);
  const activeModuleId = useWorkbenchTabsStore((state) => state.activeModuleId);
  const tabModules = useWorkbenchTabsStore((state) => state.modules);
  // The strip and the switcher see one module's tabs; only the views layer
  // sees everyone's, because it keeps recently-left modules mounted. Memoised
  // on the map's identity so a reorder inside one module still reaches
  // haveSameTabMembers and is recognised as a no-op for the heavy subtree.
  const groups = useWorkbenchTabsStore((state) => state.groups);
  const visibleModules = groups.map((g) => g.moduleId).join("|");
  const shownTabs = groups.map((g) => g.activeTabId).join("|");
  const allTabs = useMemo(() => allOpenTabs(tabModules), [tabModules]);
  const focusModuleInStore = useWorkbenchTabsStore((state) => state.focusModule);
  const openModuleBeside = useWorkbenchTabsStore((state) => state.openModuleBeside);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);

  useEffect(() => {
    // Accept either ⌘ or Ctrl rather than branching on navigator.platform:
    // that value is unreliable inside Wails' WKWebView (empty or
    // inconsistent depending on build/runtime config), which made this
    // check on macOS test event.ctrlKey — the wrong modifier for a real
    // ⌘⇧E press — so the shortcut silently never fired. Checking the
    // event's actual modifier state directly needs no platform guess at
    // all.
    function isTriggerModifier(event: KeyboardEvent): boolean {
      return (event.metaKey || event.ctrlKey) && event.shiftKey;
    }

    // A single, ordinary tap of a key isn't instantaneous — the press can
    // last past the OS's key-repeat delay on some systems, which fired a
    // second "browsing already active" keydown and escalated straight to
    // the popup on what the user experienced as one press. Requiring
    // browsing to have been active for at least this long before a further
    // "e" is allowed to escalate distinguishes an actual hold from that.
    const ESCALATE_HOLD_MS = 400;
    let browsingStartedAt = 0;

    function handleKeyDown(event: KeyboardEvent) {
      // ⌘1–⌘9 select the nth switcher module. Read through refs because this
      // listener is registered once (empty deps) and must never see a
      // stale module list — same reason stores/moduleActions.ts reads
      // onClick through a ref inside its long-lived registration.
      // ⌘⌥1–⌘⌥9: open that slot's peek, to pick from with the arrow keys. A slot
      // whose module has nothing to peek at ignores it and leaves the chord alone.
      const peekIndex = peekIndexFromEvent(event, {
        editsText: editsText(event.target),
        richText: inRichTextEditor(event.target),
      });
      if (peekIndex !== null) {
        const item = switcherItemsRef.current[peekIndex];
        const module = item?.kind === "module" ? getModule(item.module.id) : undefined;
        const peeks = item?.kind === "group" || Boolean(module?.peek || module?.tabs);
        if (peeks) {
          event.preventDefault();
          useSwitcherPeekStore.getState().toggle(peekIndex);
        }
        return;
      }
      const moduleIndex = moduleIndexFromEvent(event);
      if (moduleIndex !== null) {
        // A slot is a module, or a group — which opens the member used last, and
        // the next one when pressed again from inside it (switcherItems).
        const item = switcherItemsRef.current[moduleIndex];
        const lastUsed = item?.kind === "group" ? useModuleGroupsStore.getState().lastUsed[item.group.id] : undefined;
        const target = slotTarget(item, lastUsed, activeModuleIdRef.current);
        if (target) {
          event.preventDefault();
          focusModuleRef.current(target.id);
        }
        // preventDefault stays inside the branch above: an unusable index
        // (⌘8 with seven modules) has no default worth suppressing — the
        // menu Wails installs binds ⌘0 but no ⌘1–⌘9, and ⌘0 never gets
        // here — so leave it for whatever else might want it.
        return;
      }
      // ⌘K / Ctrl+K: the command palette. It was only ever a label on the
      // titlebar button's tooltip — nothing listened for it.
      if (isPaletteShortcut(event, editsText(event.target))) {
        event.preventDefault();
        setPaletteOpen((open) => !open);
        return;
      }
      // ⌘\ / Ctrl+\: the focused group's tab into a new group beside it.
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey && event.key === "\\") {
        const state = useWorkbenchTabsStore.getState();
        const group = focusedGroup(state);
        if (group?.activeTabId) {
          event.preventDefault();
          state.splitTab(group.activeTabId);
        }
        return;
      }
      // ⌘/ / Ctrl+/: the keyboard reference — Settings on its Keyboard section.
      // Not from a text field: CodeMirror binds ⌘/ to comment a line, and the
      // editor's own binding must win where the caret is.
      if (
        (event.metaKey || event.ctrlKey) &&
        !event.shiftKey &&
        !event.altKey &&
        event.key === "/" &&
        !editsText(event.target)
      ) {
        event.preventDefault();
        requestSettings("keyboard");
        return;
      }
      if ((event.metaKey || event.ctrlKey) && event.key === ",") {
        event.preventDefault();
        setSettingsOpen((current) => !current);
        return;
      }
      if (event.key.toLowerCase() !== "e" || !isTriggerModifier(event)) {
        return;
      }
      event.preventDefault();
      setSwitcher((current) => {
        if (current === "closed") {
          browsingStartedAt = Date.now();
          return "browsing";
        }
        if (current === "browsing" && Date.now() - browsingStartedAt >= ESCALATE_HOLD_MS) {
          // Whether this "e" is the OS's native key-repeat from holding
          // the key down, or a second discrete press (the fallback for
          // users with OS key-repeat disabled), both arrive here as
          // another keydown and take the same branch on purpose.
          return "popup";
        }
        return current;
      });
    }

    // Neither state cares about the chord being released once entered —
    // browsing (like the popup) stays put after a plain press-and-release,
    // since requiring the user to keep three keys held down while also
    // pressing arrow keys/Enter to actually use it would be the opposite
    // of the point. Exiting is explicit: Enter selects, Escape cancels
    // (see TabStrip.tsx's own keydown handling, and the popup's own
    // Escape/click-outside handling in TabSwitcher.tsx via Dialog).
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, []);

  useEffect(() => {
    let mounted = true;
    listModules()
      .then((metadata) => {
        if (mounted) {
          setBackendModules(metadata);
        }
      })
      .catch(() => {
        if (mounted) {
          setMetadataError(true);
        }
      })
      .finally(() => {
        if (mounted) {
          setMetadataLoading(false);
        }
      });

    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    let mounted = true;
    let receivedFullscreenEvent = false;

    const updateFullscreen = (fullscreen: boolean) => {
      receivedFullscreenEvent = true;
      setIsFullscreen(fullscreen);
    };

    // The macOS "will" events start the web transition alongside the native
    // fullscreen animation. Common events remain as the cross-platform
    // fallback and are harmless when macOS emits them again on completion.
    const offWillFullscreen = Events.On(Events.Types.Mac.WindowWillEnterFullScreen, () => {
      updateFullscreen(true);
    });
    const offWillUnFullscreen = Events.On(Events.Types.Mac.WindowWillExitFullScreen, () => {
      updateFullscreen(false);
    });
    const offFullscreen = Events.On(Events.Types.Common.WindowFullscreen, () => {
      updateFullscreen(true);
    });
    const offUnFullscreen = Events.On(Events.Types.Common.WindowUnFullscreen, () => {
      updateFullscreen(false);
    });

    void Window.IsFullscreen()
      .then((fullscreen) => {
        if (mounted && !receivedFullscreenEvent) {
          setIsFullscreen(fullscreen);
        }
      })
      .catch(() => {
        // Browser-only development has no native window to query.
      });

    return () => {
      mounted = false;
      offWillFullscreen();
      offWillUnFullscreen();
      offFullscreen();
      offUnFullscreen();
    };
  }, []);

  const parityIssues = useMemo(
    () => (backendModules ? compareModuleParity(modules, backendModules) : []),
    [backendModules, modules],
  );
  const unavailableModuleIds = useMemo(() => {
    if (metadataError || !backendModules) {
      return new Set<string>();
    }

    const unavailable = new Set(
      parityIssues
        .map((issue) => issue.moduleId)
        .filter((moduleId) => moduleId !== "<unknown>"),
    );
    for (const metadata of backendModules) {
      if (metadata.apiVersion !== CURRENT_MODULE_API_VERSION && metadata.id) {
        unavailable.add(metadata.id);
      }
    }
    return unavailable;
  }, [backendModules, metadataError, parityIssues]);
  const availableModules = useMemo(
    () => modules.filter((module) => !unavailableModuleIds.has(module.id)),
    [modules, unavailableModuleIds],
  );
  const activityModules = useMemo(
    () =>
      modules.map((module) => ({
        id: module.id,
        title: module.title,
        unavailable: unavailableModuleIds.has(module.id),
      })),
    [modules, unavailableModuleIds],
  );
  const switcherItems = useMemo(
    () =>
      buildSwitcherItems(
        activityModules.filter((module) => !HEADER_ONLY_MODULE_IDS.has(module.id)),
        (moduleId) => getModule(moduleId)?.group,
      ),
    [activityModules],
  );
  const activeModule =
    availableModules.find((module) => module.id === activeModuleId) ??
    availableModules[0];

  function focusModule(moduleId: string, opts?: { beside?: boolean }) {
    const module = getModule(moduleId);
    const view = module?.views[0];
    if (!module || !view) {
      return;
    }
    // Start the module request before the tab state update triggers render.
    // The same cached promise is consumed by React.lazy in WorkbenchViews.
    const preload = view.preload?.();
    void preload?.catch(() => undefined);
    // ⌥-click: beside the focused group rather than in place of it.
    if (opts?.beside) openModuleBeside(moduleId);
    else focusModuleInStore(moduleId);
    // Sticky (module-tabs.mdx): a module that already has a tab active comes
    // back to exactly that tab — its tabs are its own and were never touched
    // by whatever happened in other modules meanwhile. Tabs are stored now, so
    // that also holds across a relaunch.
    //
    // A module with NOTHING open opens nothing. It used to get its home view
    // as a preview tab, which was a guess standing in for the memory the
    // workbench did not have: visiting Database always produced a blank
    // console whether or not anyone wanted one. The empty state below offers
    // it instead, which is the same click without the assumption.
    //
    // Except for a module that says its home is what anyone arriving wants
    // (Git's Changes, which reads the folder the app has open).
    openHomeWhenAsked(moduleId);
  }

  // Mirrors for the mount-once keydown listener above, which must not close
  // over the first render's values. Declared here, after both definitions.
  const switcherItemsRef = useRef(switcherItems);
  switcherItemsRef.current = switcherItems;
  const activeModuleIdRef = useRef(activeModule?.id ?? "");
  activeModuleIdRef.current = activeModule?.id ?? "";
  const focusModuleRef = useRef(focusModule);
  focusModuleRef.current = focusModule;

  useEffect(() => {
    if (availableModules.length === 0) {
      return;
    }
    if (!activeModuleId) {
      focusModule(availableModules[0].id);
      return;
    }
    // Closing a module's last tab empties its partition. The module stays on
    // screen — with the empty state, not a view conjured back — rather than
    // the app jumping to whichever module happens to be first in the list.
  }, [availableModules, activeModuleId]);
  useMenuCommands();

  // Palette and native-menu Agent commands may be chosen while another module
  // is visible. Open the Agent Hub view first; AgentHubView consumes the
  // pending command after it is mounted, so "New agent session" can still
  // start its folder picker without moving that ownership into the shell.
  useEffect(() => {
    if (!pendingAgentHubCommand) return;
    const agentTabs = moduleTabs(tabModules, "agenthub").openTabs;
    if (activeModuleId !== "agenthub" || agentTabs.length === 0) {
      openTab({ moduleId: "agenthub", viewId: "agenthub.sessions", title: "Agent Hub" });
    }
  }, [activeModuleId, openTab, pendingAgentHubCommand, tabModules]);

  // What the palette calls a module's digit: its slot, and none for a member of
  // a group (whose slot opens whichever member was used last).
  const switcherModuleIds = useMemo(() => slotModuleIds(switcherItems), [switcherItems]);
  // A group's button opens the member used last, so what is on screen is what
  // it must remember.
  const activeGroupId = activeModule ? getModule(activeModule.id)?.group?.id : undefined;
  const activeModuleKey = activeModule?.id;
  const setLastUsed = useModuleGroupsStore((state) => state.setLastUsed);
  useEffect(() => {
    if (activeGroupId && activeModuleKey) setLastUsed(activeGroupId, activeModuleKey);
  }, [activeGroupId, activeModuleKey, setLastUsed]);
  // Stable: the palette's sections are memoized on it, and focusModule is a
  // new function every render.
  const focusModuleFromPalette = useCallback((moduleId: string) => focusModuleRef.current(moduleId), []);
  const hasMetadataWarning = metadataError || parityIssues.length > 0;

  return (
    // Feeds perfTrace's "ms React"; the zones inside it (titlebar, rail, tabs,
    // views) break that number down. Profiler is inert in production builds.
    <Profiler id={WORKBENCH_PROFILER} onRender={recordCommit}>
    <AppShell
      fullscreen={isFullscreen}
      activeModuleId={activeModule?.id ?? null}
      overlays={
        <>
          <AgentSessionsSubscriber />
          <CommandPalette
            open={paletteOpen}
            onClose={() => setPaletteOpen(false)}
            modules={availableModules}
            switcherModuleIds={switcherModuleIds}
            unavailableModuleIds={unavailableModuleIds}
            activeModuleId={activeModule?.id ?? null}
            onFocusModule={focusModuleFromPalette}
          />
          <Suspense fallback={null}>
            <SettingsModal
              open={settingsOpen}
              initialTab={settingsTab}
              onClose={() => {
                setSettingsOpen(false);
                setSettingsTab(undefined);
              }}
            />
          </Suspense>
          <TabSwitcher open={switcher === "popup"} onClose={() => setSwitcher("closed")} />
          <InteractionHost />
          <Suspense fallback={null}>
            <LazyAgentPermissionToastHost activeModuleId={activeModule?.id ?? null} />
          </Suspense>
        </>
      }
      titlebar={
        <Profiler id="titlebar" onRender={recordCommit}>
          {/* Sidebar toggle sits immediately after the traffic lights and
              deliberately OUTSIDE the three zones below — it is not a status
              item, a module action, or an app action, so it does not belong to
              any of them. pl-21 (84px) + mx-1 (4px) puts its left edge at
              x=88, matching sidebar-toggle's own x=88 in the App Shell
              component in Figma. The reservation used to be pl-20, which is
              80px and landed the control at 84 — the comment claimed 88 and
              the arithmetic did not. Traffic lights end at x=72, so 88 also
              reproduces Figma's 16px clearance. */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="titlebar"
                size="icon-sm"
                aria-label="Toggle sidebar"
                aria-pressed={sidebarOpen}
                className={cn(
                  sidebarOpen && "bg-accent text-foreground",
                  "mx-1",
                )}
                onClick={() => setSidebarOpen(!sidebarOpen)}
              >
                <PanelLeft className="size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
          </Tooltip>
          {/* Also outside the three zones, right after sidebar toggle — the
              multi-repo registry (workspace.mdx) is a window-wide context,
              not a module or app action. Breaks the icon-only rule the rest
              of the bar follows because a workspace's name is the point. */}
          <WorkspaceSwitcher />
          {/* Its own zone, centred on the WINDOW rather than between its
              neighbours: the two side clusters differ in width per module and
              per platform, and a switcher that drifted with them would not stay
              where the hand learned it is. Out of flow, so it cannot be pushed.
              Room is not a concern at the 1080px minimum window: ~216 in the
              middle leaves ~430 a side, and neither cluster reaches 320.
              paddingTop inherits the bar's optical offset so the icons sit on
              the same line as the ones in flow. */}
          <div
            className="absolute inset-y-0 left-1/2 flex -translate-x-1/2 items-center"
            style={{ paddingTop: "inherit" }}
          >
            <ModuleSwitcher
              activeModuleId={activeModule?.id ?? ""}
              items={switcherItems}
              onSelectModule={focusModule}
            />
          </div>
          {/* Two zones, not three. The workspace pill that used to hold the
              centre is gone: it named a workspace this app has no concept of,
              and carried an env chip for a value that belongs to a connection
              rather than to the window — the Database header already shows it
              beside the profile it describes. Module actions move left, where
              they sit next to the content they act on; everything the shell
              owns collects on the right. */}
          <div className="flex h-full min-w-0 flex-1 items-center gap-1">
            <div className="flex min-w-0 items-center gap-1">
              {moduleActions.map((action) => (
                <Tooltip key={action.id}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="titlebar"
                      size="icon-sm"
                      aria-label={action.label}
                      aria-pressed={action.isActive}
                      className={cn(
                        action.isActive && "bg-accent text-foreground",
                      )}
                      onClick={action.onClick}
                    >
                      <action.icon className="size-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">{action.label}</TooltipContent>
                </Tooltip>
              ))}
            </div>
            <div className="min-w-0 flex-1 select-none truncate text-caption-11 text-text-tertiary">
              {metadataLoading ? "Loading module metadata..." : null}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <LayoutMenu moduleId={activeModule?.id ?? null} />
              <SystemStatusMenu
                onResolveConflicts={() => openTab({ moduleId: "sync", viewId: "sync.status", title: "Sync" })}
                onOpenAgents={() => setChatOpen(true)}
              />
              <span className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden />
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Commands"
                    onClick={() => setPaletteOpen(true)}
                  >
                    <Search className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Commands <Kbd>{shortcut("K")}</Kbd>
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Settings"
                    onClick={() => setSettingsOpen(true)}
                  >
                    <SettingsIcon className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Settings <Kbd>⌘,</Kbd>
                </TooltipContent>
              </Tooltip>
              {/* Zone 3, after Settings — the rightmost item in the title bar.
                  The sidebar toggle is pinned at the far left, outside the
                  three zones, because Figma's App Shell puts it there against
                  the traffic lights; this one has no such constraint, so the
                  two layout toggles bracket the bar at its outer edges.

                  Always rendered and always enabled, even when the active
                  module contributes no panel: it toggles the surface, and the
                  surface explains its own emptiness. Disabling it would make
                  the control flicker in and out as the user changes module. */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Toggle bottom panel"
                    aria-pressed={bottomOpen}
                    className={cn(
                      bottomOpen && "bg-accent text-foreground",
                    )}
                    onClick={() => setBottomOpen(!bottomOpen)}
                  >
                    <PanelBottom className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Toggle bottom panel</TooltipContent>
              </Tooltip>

              {/* Always visible, like the bottom-panel toggle beside it: the
                  conversation belongs to the shell, so its control cannot
                  appear and disappear with the active module. */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Toggle agent conversation"
                    aria-pressed={chatOpen}
                    className={cn(
                      chatOpen && "bg-accent text-foreground",
                    )}
                    onClick={() => setChatOpen(!chatOpen)}
                  >
                    <MessageSquare className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Toggle agent conversation</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </Profiler>
      }
    >
      <DocumentGroups
        availableModules={availableModules}
        hasMetadataWarning={hasMetadataWarning}
        keyboardBrowsing={switcher === "browsing"}
        onExitBrowsing={() => setSwitcher("closed")}
        onConfigureTabs={() => {
          setSettingsTab("appearance");
          setSettingsOpen(true);
        }}
      />
      <Profiler id="views" onRender={recordCommit}>
      <WorkbenchViews
        openTabs={allTabs}
        activeModuleId={activeModule?.id ?? ""}
        visibleModules={visibleModules}
        shownTabs={shownTabs}
      />
      </Profiler>
    </AppShell>
    </Profiler>
  );
}
