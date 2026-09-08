import type { ReactNode } from "react";

import { Titlebar } from "@/components/ui/titlebar";
import { SidebarSurface } from "@/components/ui/sidebar-shell";
import { BottomSlotSurface } from "@/components/ui/bottom-slot";
import { SplitPanelProvider, SplitPanelSlot } from "@/components/ui/split-panel";
import { AgentCompanionSlot } from "./AgentCompanionSlot";

/**
 * The App Shell — Figma `107:137`, whose every variant has exactly three
 * children: a titlebar instance at 0,0 1440x40, a content-row at 8,40, and a
 * dock at 666,879.
 *
 * This file exists because that boundary kept getting lost. The shell was ~40
 * lines of layout buried inside a 785-line Workbench that also owned tab state,
 * module mounting, the command palette and the settings modal, so no single
 * place could be compared against the Figma node. A reconcile pass split along
 * file sizes instead of that boundary and left a gap nothing owned — the window
 * gutter, the translucent content card and the traffic-light alignment all lived
 * in it.
 *
 * What is layout lives here. What is state stays in Workbench and arrives
 * through the slots below.
 *
 * Geometry, with the code that owns each number:
 *
 *   titlebar        40 tall                  components/ui/titlebar.tsx
 *   window gutter   0, flush (Figma draws 8)  WINDOW_GUTTER, panelLayout.ts
 *   sidebar         280 + 8 sash             sidebar-shell.tsx / panelWidths
 *   content-surface fills, opaque, radius 10 .shell-surface, index.css
 *   panel           320 + 8 sash             split-panel.tsx / panelWidths
 *   bottom          under content only       bottom-slot.tsx
 *   dock            108x11, centred          dock/Dock.tsx
 */

export interface AppShellProps {
  /** Fullscreen drops the traffic-light reservation. Figma 732:2170 `Mode`. */
  fullscreen?: boolean;
  /** Drives the three module-owned slots: sidebar, bottom, and the companion. */
  activeModuleId: string | null;
  /** Contents of the title bar. The bar itself is not the caller's to draw. */
  titlebar: ReactNode;
  /** The module switcher. Floats over content; it is not in the row. */
  dock: ReactNode;
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
  dock,
  overlays,
  children,
}: AppShellProps) {
  return (
    <SplitPanelProvider>
      {/* The shell wrapper owns background/app. That is Figma's own wording:
          sidebar-surface 116:1367 has no material at all and its description
          says the wrapper "owns the background/app fill shared with the
          titlebar", and the titlebar root is likewise bare — no fill, border,
          blur or shadow. Both read as chrome because they show this through.

          It used to be bg-transparent, deferring to the colour the native
          window paints. That holds only while the window is opaque. With
          DEVKIT_TRANSLUCENT_WINDOW=1 nothing painted background/app at all, so
          the two bare surfaces showed the desktop and every neighbouring
          colour got judged against someone's wallpaper. Painting it here makes
          the frontend say what Figma says, in either window mode. */}
      <div className="relative flex h-svh w-full flex-col overflow-hidden bg-bg-app">
        <Titlebar fullscreen={fullscreen}>{titlebar}</Titlebar>

        {/* content-row, flush to the window (WINDOW_GUTTER is 0; Figma draws 8).
            The 8px sashes between surfaces are unchanged — only the dead margin
            at the window edge is gone.

            Flush is what makes the corner right rather than approximately
            right: macOS masks the window's content to its own squircle, so a
            surface that reaches the edge is rounded BY the window instead of
            having to imitate it. An inset surface had to draw its own corner
            parallel to one CSS cannot express, and every radius we tried was
            wrong by a measurable amount. */}
        <div className="flex min-h-0 flex-1">
          <SidebarSurface activeModuleId={activeModuleId} />

          {/* The bottom slot stacks under the module content ONLY — the sidebar
              and the split panel keep their full height either side of it, so
              opening it never shortens them. min-w-0 because this column has no
              overflow-hidden of its own to zero its automatic minimum width,
              and without it the two side panels could not squeeze it. */}
          <div className="flex min-w-0 min-h-0 flex-1 flex-col">
            {/* One card holds the strip and the views, which is how Figma's
                content-surface is built: tab-strip at the top edge and
                content-slot below it, inside the same surface.

                It is opaque on purpose. Figma 116:1346 fills it with
                surface/base, and 01 Foundations calls a translucent content
                card a bug rather than a look — module content must not sit on
                the user's wallpaper. Same material as the side panel and the
                bottom slot; only the sidebar rail is transparent. */}
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden shell-surface">
              {children}
            </div>
            <BottomSlotSurface activeModuleId={activeModuleId} />
          </div>

          <AgentCompanionSlot />
          <SplitPanelSlot />
        </div>

        {dock}
        {overlays}
      </div>
    </SplitPanelProvider>
  );
}
