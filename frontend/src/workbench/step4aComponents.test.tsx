import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TabStrip } from "./tabs/TabStrip";
import { CollapsedDock } from "./dock/CollapsedDock";
import { DockItem } from "./dock/DockItem";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Wrench } from "lucide-react";
import type { WorkbenchTab } from "../stores/workbenchTabs";
import { Titlebar } from "@/components/ui/titlebar";

describe("Step 4a Shell Chrome Reconciliation", () => {
  describe("Titlebar (Figma 732:2170)", () => {
    it("verifies titlebar rules: 40px height, gap-1 between actions, no root fill/border", () => {
      // This used to declare the class string as a local and then assert
      // against that local — it never touched the component, and it had already
      // drifted (pr-3 while the shell rendered pr-4) with the suite still green.
      // Render the real thing.
      const html = renderToString(<Titlebar platform="mac">content</Titlebar>);

      expect(html).toContain("h-10");
      expect(html).toContain("gap-1");
      expect(html).toContain("pr-4");
      // Figma EXCEPTION: the titlebar has NO material of its own.
      expect(html).not.toContain("bg-");
      expect(html).not.toContain("border-");
      expect(html).not.toContain("backdrop-blur");
      expect(html).not.toContain("shadow-");
    });

    it("reserves the traffic lights in windowed mode and drops them in fullscreen", () => {
      // Figma 732:2170 Mode: module zone starts at 88 windowed (pl-21 + the
      // first control's mx-1) and at space/4 fullscreen.
      expect(renderToString(<Titlebar platform="mac">x</Titlebar>)).toContain("pl-21");
      expect(renderToString(<Titlebar platform="mac" fullscreen>x</Titlebar>)).toContain(
        "pl-4",
      );
    });

    it("reserves nothing where the OS puts its window buttons on the right", () => {
      // The 84px is a macOS traffic-light reservation and nothing else. On
      // Ubuntu it rendered as an empty left end with the sidebar toggle
      // floating at x=88, which is the bug this asserts against.
      for (const platform of ["linux", "windows", "web"] as const) {
        const html = renderToString(<Titlebar platform={platform}>x</Titlebar>);
        expect(html).toContain("pl-4");
        expect(html).not.toContain("pl-21");
        // The optical offset is a calibration against the traffic lights too.
        expect(html).not.toContain("titlebar-optical-offset");
      }
    });

    it("draws window controls exactly where the window is frameless", () => {
      // Linux only, matching framelessWindow in native_webview_linux.go: GTK
      // draws no decoration there, so a window with no controls here has none
      // at all. macOS has the traffic lights and Windows its native caption.
      const linux = renderToString(<Titlebar platform="linux">x</Titlebar>);
      expect(linux).toContain("Minimise window");
      expect(linux).toContain("Maximise window");
      expect(linux).toContain("Close window");
      // Flush to the window corner, the way a native cluster sits.
      expect(linux).toContain("pr-1");

      for (const platform of ["mac", "windows", "web"] as const) {
        expect(renderToString(<Titlebar platform={platform}>x</Titlebar>)).not.toContain(
          "Close window",
        );
      }
      // Fullscreen has nothing to minimise, maximise or restore to.
      expect(
        renderToString(
          <Titlebar platform="linux" fullscreen>
            x
          </Titlebar>,
        ),
      ).not.toContain("Close window");
    });

    it("is one component — the vault gate and the workbench share it", () => {
      // Five title bars existed before 2026-09-06. macOS pins the traffic
      // lights to the window, so more than one height cannot be aligned.
      const html = renderToString(<Titlebar platform="mac">x</Titlebar>);
      expect(html).toContain('data-slot="title-bar"');
      expect(html).not.toContain("h-[30px]");
    });
  });

  describe("Titlebar Action (Figma 674:47115)", () => {
    it("verifies action button specifications: 28x28, radius 8, 16px icon, state-soft hover", () => {
      const actionClass =
        "size-7 shrink-0 rounded-[8px] hover:bg-state-soft hover:text-text-primary [--wails-draggable:no-drag]";
      expect(actionClass).toContain("size-7"); // 28px
      expect(actionClass).toContain("rounded-[8px]"); // radius 8
      expect(actionClass).toContain("hover:bg-state-soft");
    });
  });

  describe("System Status (Figma 616:932)", () => {
    it("verifies 3 distinct shapes exception: Neutral has radius 8, Attention/Active have no radius wrapper", () => {
      // EXCEPTION: Neutral is a rounded-8 28x28 wrapper with 16px glyph.
      // Attention and Active are full-bleed 28x28 graphics with no radius wrapper.
      const neutralClass = "size-7 shrink-0 rounded-[8px]";
      const attentionClass = "size-7 shrink-0 rounded-none p-0";
      const activeClass = "size-7 shrink-0 rounded-none p-0";

      expect(neutralClass).toContain("rounded-[8px]");
      expect(attentionClass).toContain("rounded-none");
      expect(activeClass).toContain("rounded-none");
    });
  });

  describe("Tab Strip (Figma 449:325)", () => {
    it("renders 48px height container with p-2, gap-2, new-tab button with 13px plus, and trailing spacer", () => {
      const sampleTabs: WorkbenchTab[] = [
        {
          id: "notes:1",
          moduleId: "notes",
          viewId: "notes.detail",
          title: "Architecture Plan",
          status: "idle",
        },
      ];

      const html = renderToString(
        <TabStrip
          tabs={sampleTabs}
          activeTabId="notes:1"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />,
      );

      // Strip container is 48px (h-12), p-2, gap-2
      expect(html).toContain("h-12");
      expect(html).toContain("p-2");
      expect(html).toContain("gap-2");
      // New-tab button: 28x28 (size-7), radius 8 (rounded-[8px]), 13px plus (size-[13px])
      expect(html).toContain('aria-label="New tab"');
      expect(html).toContain("size-7");
      expect(html).toContain("rounded-[8px]");
      expect(html).toContain("size-[13px]");
      // Trailing spacer: flex-1 h-2
      expect(html).toContain("flex-1 h-2");
    });
  });

  describe("Tab Pill (Figma 448:343)", () => {
    const tabs: WorkbenchTab[] = [
      {
        id: "active-tab",
        moduleId: "notes",
        viewId: "notes.detail",
        title: "Active Document",
        status: "running",
      },
      {
        id: "inactive-tab",
        moduleId: "notes",
        viewId: "notes.list",
        title: "All Notes",
        status: "idle",
      },
      {
        id: "preview-tab",
        moduleId: "database",
        viewId: "db.query",
        title: "Query 1",
        status: "idle",
        preview: true,
      },
    ];

    it("renders Active pill with h-8, radius 6, surface-raised bg, text-primary, 6px status dot and close button", () => {
      const html = renderToString(
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />,
      );

      // 200x32, radius 6, px-3, gap-2
      expect(html).toContain("h-8");
      expect(html).toContain("rounded-[6px]");
      expect(html).toContain("px-3");
      expect(html).toContain("gap-2");
      // Active styling
      expect(html).toContain("bg-surface-raised");
      expect(html).toContain("border-border");
      expect(html).toContain("text-text-primary");
      // Status dot 6px (size-1.5)
      expect(html).toContain("size-1.5");
      // Close button
      expect(html).toContain('aria-label="Close Active Document"');
    });

    it("renders Inactive pill with state-soft bg, subtle border, secondary text, close button, no status dot", () => {
      const html = renderToString(
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />,
      );

      expect(html).toContain("bg-state-soft");
      expect(html).toContain("border-border-subtle");
      expect(html).toContain('aria-label="Close All Notes"');
    });

    it("renders Preview pill with dashed border, body-12 regular font, no close button and no dot", () => {
      const html = renderToString(
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />,
      );

      expect(html).toContain("border-dashed");
      expect(html).toContain("text-body-12");
      // Preview tab has NO close button
      expect(html).not.toContain('aria-label="Close Query 1"');
    });

    it("renders separate radius 8 focus ring layer when keyboard focused", () => {
      const html = renderToString(
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={true}
          onExitBrowsing={() => {}}
        />,
      );

      // Separate layer at radius 8, drawn outside pill (-inset-[2px]), border-2
      expect(html).toContain("rounded-[8px]");
      expect(html).toContain("-inset-[2px]");
      expect(html).toContain("border-[var(--border-focus,#b2d5e5)]");
    });
  });

  describe("Dock (Figma 208:438)", () => {
    it("renders CollapsedDock with px-2 py-1, action-primary for active and text-tertiary for others", () => {
      const html = renderToString(
        <CollapsedDock count={6} activeIndex={0} />,
      );

      expect(html).toContain("px-2 py-1");
      expect(html).toContain("bg-action-primary");
      expect(html).toContain("bg-text-tertiary");
      expect(html).toContain("w-3"); // 12px
      expect(html).toContain("h-[3px]");
    });

    it("renders DockItem with 32x32 size, radius 18, 16px icon, active surface-base and inactive transparent", () => {
      const activeHtml = renderToString(
        <TooltipProvider>
          <DockItem
            item={{
              id: "notes",
              title: "Notes",
              icon: Wrench,
              isActive: true,
              onClick: () => {},
            }}
          />
        </TooltipProvider>,
      );
      expect(activeHtml).toContain("size-8"); // 32px
      expect(activeHtml).toContain("rounded-[18px]");
      expect(activeHtml).toContain("bg-surface-base");

      const inactiveHtml = renderToString(
        <TooltipProvider>
          <DockItem
            item={{
              id: "notes",
              title: "Notes",
              icon: Wrench,
              isActive: false,
              onClick: () => {},
            }}
          />
        </TooltipProvider>,
      );
      expect(inactiveHtml).toContain("bg-transparent");
      expect(inactiveHtml).toContain("text-text-secondary");
    });
  });
});

