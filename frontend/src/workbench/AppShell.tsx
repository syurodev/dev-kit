import { Profiler, type ReactNode } from "react";
import { recordCommit } from "@/lib/perfLog";

import { Titlebar } from "@/components/ui/titlebar";
import { SidebarSurface } from "@/components/ui/sidebar-shell";
import { SplitPanelProvider } from "@/components/ui/split-panel";
import { PaneSlotsRow } from "@/components/ui/pane-slots";
import { AgentCompanionSlot } from "./AgentCompanionSlot";

/**
 * The App Shell — Figma `107:137`, whose every variant has exactly three
 * children: a titlebar instance at 0,0 1440x40, a content-row at 8,40, and a
 * dock at 666,879.
 *
 * The file exists to keep that boundary findable: with the layout buried inside
 * Workbench there was no single place to compare against the node, and the
 * window gutter, the content card and the traffic-light alignment fell into a
 * gap nothing owned.
 *
 * What is layout lives here. What is state stays in Workbench and arrives
 * through the slots below.
 *
 * Geometry, with the code that owns each number:
 *
 *   titlebar        40 tall                  components/ui/titlebar.tsx
 *   window gutter   4 on l/r/b, flush on top  WINDOW_GUTTER, panelLayout.ts
 *   sidebar         280 + 4 sash             sidebar-shell.tsx / panelWidths
 *   content-surface fills, opaque, radius    .shell-surface, index.css
 *                   at least 12px; follows larger window radii (--shell-radius)
 *   columns A, B    320 + 4 sash each        pane-slots.tsx / panelWidths
 *   bottom          under content only       pane-slots.tsx
 *
 * Right of the sidebar is the slot layout (docs/dev-kit/layout.mdx): the
 * document, a bottom row under it, and up to two columns — panes move between
 * those slots by dragging their header or from its Move to… menu.
 *
 * Figma still draws a dock at 666,879. It is gone: modules switch from the
 * title bar (ModuleSwitcher.tsx).
 */

export interface AppShellProps {
  /** Fullscreen drops the traffic-light reservation. Figma 732:2170 `Mode`. */
  fullscreen?: boolean;
  /** Drives the three module-owned slots: sidebar, bottom, and the companion. */
  activeModuleId: string | null;
  /** Contents of the title bar. The bar itself is not the caller's to draw. */
  titlebar: ReactNode;
  /**
   * Palettes, modals, hosts and subscribers. They render inside the frame so
   * they can position against it, but they take part in no layout.
   */
  overlays?: ReactNode;
  /** Goes inside content-surface — in practice the tab strip and the views. */
  children: ReactNode;
}

export function AppShell({
  fullscreen = false,
  activeModuleId,
  titlebar,
  overlays,
  children,
}: AppShellProps) {
  return (
    <SplitPanelProvider>
      {/* The shell owns a neutral dark/light backdrop independently of the
          family palette. Module surfaces, controls and editors retain their
          existing theme tokens, including in translucent native windows. */}
      <div className="app-shell relative flex h-svh w-full flex-col overflow-hidden">
        <Titlebar fullscreen={fullscreen}>{titlebar}</Titlebar>

        {/* content-row, inset WINDOW_GUTTER from the window on the three sides
            that have an edge, and flush under the title bar. One band, drawn
            here rather than by each surface, so the shell reads as a set of
            cards on the app background and every one of them is inset by the
            same amount whether or not its neighbours are open.

            The row owns it deliberately. When the split panel drew its own
            inset instead, the column still measured that width while closed —
            padding survives width: 0 on a border-box — and left a dead band
            down the window edge with no panel behind it. A margin on the row
            has nothing to collapse.

            This used to be flush at 0, so macOS would round the surfaces with
            the window's own squircle mask. That is a real advantage and it is
            given up on purpose: it only ever applied to one platform, and it
            cost the shell its structure — flush surfaces are the walls of the
            window, and these are cards. */}
        <div className="mx-1 mb-1 flex min-h-0 flex-1">
          {/* A perf zone (lib/perfTrace). Inert in production builds. */}
          <Profiler id="rail" onRender={recordCommit}>
            <SidebarSurface activeModuleId={activeModuleId} />
          </Profiler>

          {/* Publishes the conversation into the companion state; the slot
              layout draws it as the Agent pane. */}
          <AgentCompanionSlot />
          <PaneSlotsRow activeModuleId={activeModuleId}>{children}</PaneSlotsRow>
        </div>

        {overlays}
      </div>
    </SplitPanelProvider>
  );
}
