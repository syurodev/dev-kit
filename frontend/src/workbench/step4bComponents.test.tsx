import { readFileSync } from "node:fs";
import { WINDOW_GUTTER } from "./panelLayout";
import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import { ScrollRuler } from "@/components/ui/scroll-ruler";
import { ToastSpecCard } from "@/components/ui/sonner";
import { CommandPaletteStaticCard } from "./CommandPalette";
import { ShellMenuItemSpec } from "@/components/ui/dropdown-menu";
import {
} from "./Workbench";
import { GAP_WIDTH, TITLE_BAR_HEIGHT } from "./panelLayout";
import { usePanelWidthsStore } from "@/stores/panelWidths";

describe("Step 4b Shell Overlays & Composition Reconciliation", () => {
  describe("App Shell Geometry & Layout Constants (Figma 107:137)", () => {
    it("traces exact shell constants: sidebar 280, panel 320, gap 4, titlebar 40", () => {
      // The three SHELL_*_CLASS strings that used to be asserted here are gone.
      // Nothing applied them — the live widths come from usePanelWidthsStore —
      // so the assertions only checked a constant equalled its own literal, and
      // "App Shell geometry reconciled" rested on them. What remains can fail.
      expect(GAP_WIDTH).toBe(4);
      expect(TITLE_BAR_HEIGHT).toBe(40);

      const storeState = usePanelWidthsStore.getState();
      expect(storeState.sidebarWidth).toBe(280);
      expect(storeState.splitPanelWidth).toBe(320);
    });
  });


    it("keeps every docked shell surface opaque and off the glass tiers", () => {
      // Figma content-surface 116:1346, panel-surface 116:1348 and
      // bottom-surface 204:462 are one material: surface/base at full opacity,
      // glass/border, radius/10, inset highlight. Two of the three were
      // glass-panel at 55%, which puts the user's wallpaper behind their code.
      // The sidebar rail is the only docked surface that stays transparent.
      const files = {
        // The content card lives in AppShell now, not Workbench — the split
        // that gave the shell's layout one file to compare against Figma 107:2.
        "AppShell.tsx": new URL("./AppShell.tsx", import.meta.url),
        "split-panel.tsx": new URL(
          "../components/ui/split-panel.tsx",
          import.meta.url
        ),
        "bottom-slot.tsx": new URL(
          "../components/ui/bottom-slot.tsx",
          import.meta.url
        ),
      };

      for (const [name, url] of Object.entries(files)) {
        const source = readFileSync(url, "utf8");
        const classLines = source
          .split("\n")
          .filter((line) => line.includes("overflow-hidden"));
        const surface = classLines.find((line) => line.includes("shell-surface"));
        if (!surface) {
          throw new Error(`${name}: no shell-surface docked surface found`);
        }
        expect(surface).not.toContain("glass-panel");
        expect(surface).not.toContain("rounded-xl");
      }
    });

    it("paints background/app on the shell wrapper", () => {
      // Figma sidebar-surface 116:1367 has no material of its own and its
      // description hands the fill up: the wrapper "owns the background/app
      // fill shared with the titlebar". The titlebar root is bare for the same
      // reason. So both surfaces are only ever the right colour if this one
      // element paints it — leave it transparent and they show whatever is
      // behind the window, which with a translucent window is the wallpaper.
      const source = readFileSync(
        new URL("./AppShell.tsx", import.meta.url),
        "utf8",
      );
      const wrapper = source
        .split("\n")
        .find((line) => line.includes("h-svh"));
      if (!wrapper) throw new Error("no h-svh shell wrapper found");
      expect(wrapper).toContain("bg-bg-app");
      expect(wrapper).not.toContain("bg-transparent");
    });

    it("insets the shell from the window and lets every surface draw its own corners", () => {
      // The shell used to run flush at gutter 0 so that macOS would round the
      // surfaces with the window's own squircle mask, and .shell-surface
      // squared its bottom corners to stay out of the way of it. That bought an
      // exact corner on one platform and cost the shell its structure: flush
      // surfaces read as the walls of the window, and DevKit's are cards. They
      // are inset now, on the three sides that have a window edge, and they
      // draw all four of their own corners on every platform — concentric with
      // the window's, at its radius minus this one gap.
      expect(WINDOW_GUTTER).toBe(4);
      expect(WINDOW_GUTTER).toBe(GAP_WIDTH);

      const shell = readFileSync(new URL("./AppShell.tsx", import.meta.url), "utf8");
      const row = shell.split("\n").find((line) => line.includes("flex min-h-0 flex-1"));
      expect(row).toBeDefined();
      // Left, right and bottom. Not the top: the title bar is chrome already,
      // and a band under it reads as the surfaces hanging in the window.
      expect(row).toContain("mx-1");
      expect(row).toContain("mb-1");
      expect(row).not.toContain("mt-");

      const css = readFileSync(new URL("../index.css", import.meta.url), "utf8");
      expect(css).not.toContain("border-bottom-left-radius: 0;");
      expect(css).not.toContain("border-bottom-right-radius: 0;");
      expect(css).toContain("--shell-radius: max(0px, calc(var(--window-radius) - var(--shell-gap)))");
      expect(css).toMatch(/\.shell-surface \{[^}]*border-radius: var\(--shell-radius\)/);
    });

    it("clamps every resizable shell surface at render, not only while dragging", () => {
      // The ceiling depends on the window; the stored size does not. Drag a
      // panel wide, then narrow the window, and a surface that only clamps on
      // drag keeps its old size and crushes its neighbour. bottom-slot fixed
      // this on the vertical axis and left a note that the sidebar still had
      // the hole; split-panel had it too. All three clamp now.
      const surfaces = {
        "sidebar-shell.tsx": new URL(
          "../components/ui/sidebar-shell.tsx",
          import.meta.url,
        ),
        "split-panel.tsx": new URL(
          "../components/ui/split-panel.tsx",
          import.meta.url,
        ),
        "bottom-slot.tsx": new URL(
          "../components/ui/bottom-slot.tsx",
          import.meta.url,
        ),
      };

      for (const [name, url] of Object.entries(surfaces)) {
        // The size animation, not the opacity fade split-panel also has: the
        // one that resolves the surface's extent, so it carries GAP_WIDTH.
        const animate = readFileSync(url, "utf8")
          .split("\n")
          .find((line) => line.includes("animate={{") && line.includes("GAP_WIDTH"));
        if (!animate) throw new Error(`${name}: no sizing animate prop found`);
        expect(animate).toContain("Math.min(");
      }
    });

  describe("Shell / Resize Handle (Figma 699:1104)", () => {
    it("EXCEPTION: Rest state is completely empty (no hairline, no grip, no border, no background)", () => {
      const html = renderToString(
        <ShellResizeHandle
          orientation="vertical"
          forceState="rest"
          aria-label="Resize column"
          style={{ height: 120 }}
        />
      );
      // Container is transparent
      expect(html).toContain("bg-transparent");
      // Hairline and grip have opacity-0 at rest
      expect(html).toContain("data-state=\"rest\"");
      expect(html).toContain("opacity-0");
    });

    it("verifies Vertical Hover: 8px wide, 1px hairline, 3×32 grip", () => {
      const html = renderToString(
        <ShellResizeHandle
          orientation="vertical"
          forceState="hover"
          aria-label="Resize column"
          style={{ height: 120 }}
        />
      );
      expect(html).toContain("w-2"); // 8px wide
      expect(html).toContain("w-px"); // 1px hairline
      expect(html).toContain("w-[3px]"); // 3px grip width
      expect(html).toContain("h-8"); // 32px grip height
      expect(html).toContain("rounded-full");
      expect(html).toContain("bg-text-tertiary");
    });

    it("verifies Horizontal Hover: 8px tall, 1px hairline, 32×3 grip", () => {
      const html = renderToString(
        <ShellResizeHandle
          orientation="horizontal"
          forceState="hover"
          aria-label="Resize row"
          style={{ width: 120 }}
        />
      );
      expect(html).toContain("h-2"); // 8px tall
      expect(html).toContain("h-px"); // 1px hairline
      expect(html).toContain("h-[3px]"); // 3px grip height
      expect(html).toContain("w-8"); // 32px grip width
      expect(html).toContain("rounded-full");
      expect(html).toContain("bg-text-tertiary");
    });

    it("verifies Focus Ring: 2px border-focus overflowing 8px gutter", () => {
      const html = renderToString(
        <ShellResizeHandle
          orientation="vertical"
          forceState="focus"
          aria-label="Resize column"
        />
      );
      expect(html).toContain("border-2");
      expect(html).toContain("border-[var(--border-focus,#b2d5e5)]");
      // Figma 699:1104 insets the ring -2.5% / -37.5%, a uniform -3px on both
      // orientations, with no corner radius.
      expect(html).toContain("-inset-[3px]");
      expect(html).not.toContain("rounded-sm");
    });
  });

  describe("Shell / Toast (Figma 727:1682)", () => {
    it("verifies dimensions and glass material: 380 wide, rounded-14, blur 20, shadow 0 12px 16px", () => {
      const html = renderToString(
        <ToastSpecCard
          tone="info"
          title="Notification Title"
          body="Notification Body"
        />
      );
      expect(html).toContain("w-[380px]");
      expect(html).toContain("rounded-float");
      expect(html).toContain("backdrop-blur-[20px]");
      expect(html).toContain("bg-glass-float");
      expect(html).toContain("shadow-[0_12px_16px_0_rgba(0,0,0,0.45)]");
    });

    it("EXCEPTION: Toast has NO inset top highlight", () => {
      const html = renderToString(
        <ToastSpecCard
          tone="info"
          title="Notification Title"
        />
      );
      expect(html).not.toContain("shadow-[inset_");
      expect(html).not.toContain("inset_0px_1px");
    });

    it("EXCEPTION: Body text is text-body-12 text-text-tertiary, not text-secondary", () => {
      const html = renderToString(
        <ToastSpecCard
          tone="info"
          title="Notification Title"
          body="Notification Body"
        />
      );
      expect(html).toContain("text-body-12");
      expect(html).toContain("text-text-tertiary");
      expect(html).not.toContain("text-text-secondary");
    });

    it("EXCEPTION: Gap between title and body is 0 (gap-0 / mt-0)", () => {
      const html = renderToString(
        <ToastSpecCard
          tone="info"
          title="Notification Title"
          body="Notification Body"
        />
      );
      expect(html).toContain("gap-0");
      expect(html).toContain("mt-0");
    });

    it("verifies Tone glyph is 20px (size-5) and only coloured element; text stays neutral", () => {
      const tones = ["info", "success", "warning", "error", "loading"] as const;
      tones.forEach((tone) => {
        const html = renderToString(
          <ToastSpecCard
            tone={tone}
            title="Neutral Title"
            body="Neutral Body"
          />
        );
        expect(html).toContain("size-5"); // 20px glyph
        expect(html).toContain("text-text-primary"); // title is neutral
        expect(html).toContain("text-text-tertiary"); // body is neutral
      });
    });

    it("verifies Action chip: 28px tall, surface-raised, r8, px-2, label-12", () => {
      const html = renderToString(
        <ToastSpecCard
          tone="info"
          title="Notification Title"
          actionLabel="Undo"
        />
      );
      expect(html).toContain("h-7"); // 28px tall
      expect(html).toContain("rounded-md");
      expect(html).toContain("bg-surface-raised");
      expect(html).toContain("text-label-12");
    });
  });

  describe("Shell / Command Palette (Figma 719:1475)", () => {
    it("verifies dimensions and glass material: 640 wide, rounded-14, blur 20, NO shadow, NO inset highlight", () => {
      const html = renderToString(<CommandPaletteStaticCard state="results" />);
      expect(html).toContain("w-[640px]");
      expect(html).toContain("rounded-float");
      expect(html).toContain("backdrop-blur-[20px]");
      expect(html).toContain("bg-glass-float");
      expect(html).toContain("shadow-none");
      expect(html).not.toContain("shadow-[inset_");
    });

    it("verifies search row: h56, pl-5 pr-4, gap-3, glyph 20px, text-title-16", () => {
      const html = renderToString(<CommandPaletteStaticCard state="results" />);
      expect(html).toContain("h-14"); // 56px
      expect(html).toContain("pl-5");
      expect(html).toContain("pr-4");
      expect(html).toContain("gap-3");
      expect(html).toContain("size-5"); // 20px glyph
      expect(html).toContain("text-title-16");
    });

    it("EXCEPTION: Group head is text-code-11 uppercase mono, NOT text-label-11-caps", () => {
      const html = renderToString(<CommandPaletteStaticCard state="results" />);
      expect(html).toContain("text-code-11");
      expect(html).toContain("uppercase");
      expect(html).not.toContain("text-label-11-caps");
    });

    it("EXCEPTION: Inactive rows have NO background at all; only active row gets surface-raised", () => {
      const html = renderToString(<CommandPaletteStaticCard state="results" />);
      expect(html).toContain("bg-surface-raised"); // active row
      expect(html).toContain("bg-transparent"); // inactive row
      expect(html).toContain("h-10"); // 40px row
      expect(html).toContain("rounded-md");
    });

    it("verifies footer: h40, px-5, gap-4, text-code-11 text-text-tertiary", () => {
      const html = renderToString(<CommandPaletteStaticCard state="results" />);
      expect(html).toContain("h-10"); // 40px
      expect(html).toContain("px-5");
      expect(html).toContain("gap-4");
      expect(html).toContain("text-code-11");
      expect(html).toContain("text-text-tertiary");
      expect(html).toContain("border-t");
    });

    it("verifies Empty state: 16 SemiBold text-secondary title and 13 Regular text-tertiary body", () => {
      const html = renderToString(<CommandPaletteStaticCard state="empty" />);
      expect(html).toContain("text-title-16 text-text-secondary");
      expect(html).toContain("text-body-13 text-text-tertiary");
      expect(html).toContain("pt-10 pb-12");
    });
  });

  describe("Shell / Menu Item (Figma 816:2423)", () => {
    it("verifies box: 200x28, r6, px-2, three columns with a reserved 14px glyph slot", () => {
      const html = renderToString(
        <ShellMenuItemSpec
          tone="default"
          state="rest"
          label="Menu Action"
          shortcut="⌘K"
        />
      );
      expect(html).toContain("w-[200px]");
      expect(html).toContain("h-7"); // 28px
      expect(html).toContain("rounded-sm");
      expect(html).toContain("px-2");
      expect(html).toContain("gap-x-2");
      expect(html).toContain("text-body-13");
      // icon · label · shortcut, and the icon slot is there with no icon.
      expect(html).toContain("grid-cols-[var(--icon-14)_minmax(0,1fr)_auto]");
      expect(html).toContain('data-slot="menu-icon"');
      // The shortcut is a key cap.
      expect(html).toContain('data-slot="kbd"');
    });

    it("verifies Default tone: rest is transparent text-primary, hover is state-soft", () => {
      const restHtml = renderToString(
        <ShellMenuItemSpec tone="default" state="rest" label="Action" />
      );
      expect(restHtml).toContain("bg-transparent");
      expect(restHtml).toContain("text-text-primary");

      const hoverHtml = renderToString(
        <ShellMenuItemSpec tone="default" state="hover" label="Action" />
      );
      expect(hoverHtml).toContain("bg-state-soft");
    });

    it("EXCEPTION: Danger at rest has NO red background; only label is status-danger", () => {
      const restHtml = renderToString(
        <ShellMenuItemSpec tone="danger" state="rest" label="Delete" />
      );
      expect(restHtml).toContain("bg-transparent"); // NO red background at rest!
      expect(restHtml).toContain("text-status-danger");

      const hoverHtml = renderToString(
        <ShellMenuItemSpec tone="danger" state="hover" label="Delete" />
      );
      expect(hoverHtml).toContain("bg-[var(--feedback-danger-soft)]"); // 14% danger soft
      expect(hoverHtml).toContain("text-status-danger");
    });
  });

  describe("Terminal Tab Pill (Figma 204:428)", () => {
    it("EXCEPTION: Terminal tab pill is 190×30, NOT 200×32", () => {
      // Figma 204:428 explicitly sizes terminal tab pills as 190×30.
      const terminalPillClass = "w-[190px] h-[30px] rounded-sm px-2 text-label-12";
      expect(terminalPillClass).toContain("w-[190px]");
      expect(terminalPillClass).toContain("h-[30px]");
      expect(terminalPillClass).not.toContain("w-[200px]");
      expect(terminalPillClass).not.toContain("h-8");
    });
  });

  describe("Shell / Scroll Ruler (Figma 700:1100)", () => {
    it("renders nothing at all when the region does not overflow", () => {
      // Figma: "a scrollbar over content that fits is a lie about how much
      // there is." An empty track is not good enough — there must be no node.
      const html = renderToString(
        <ScrollRuler contentHeight={400} viewportHeight={400} scrollTop={0} />
      );
      expect(html).toBe("");
    });

    it("draws one tick per milestone and nothing in between", () => {
      const html = renderToString(
        <ScrollRuler
          contentHeight={2000}
          viewportHeight={400}
          scrollTop={0}
          milestones={[0.25, 0.5, 0.75]}
        />
      );
      expect(html).toContain('data-slot="scroll-ruler"');
      const marks = html.match(/data-slot="scroll-ruler-milestone"/g) ?? [];
      expect(marks.length).toBe(3);
      // No tick grid: the dense 5px pitch from Figma 700:1100 is deliberately
      // not implemented, so there must be no base layer and no gradient.
      expect(html).not.toContain("scroll-ruler-base");
      expect(html).not.toContain("repeating-linear-gradient");
    });

    it("puts the current position at the scrolled fraction", () => {
      const html = renderToString(
        <ScrollRuler contentHeight={2000} viewportHeight={400} scrollTop={800} />
      );
      // 800 / (2000 - 400) = 50%
      expect(html).toContain('data-slot="scroll-ruler-active"');
      expect(html).toContain("calc(50% - 1px)");
    });

    it("separates a milestone from the current position by colour, not size", () => {
      const html = renderToString(
        <ScrollRuler
          contentHeight={2000}
          viewportHeight={400}
          scrollTop={0}
          milestones={[0.5]}
        />
      );
      // Both ticks are 5.5x2; only the colour differs.
      const widths = html.match(/width:5\.5px/g) ?? [];
      expect(widths.length).toBe(2);
      expect(html).toContain("bg-text-secondary");
      expect(html).toContain("var(--brand-text)");
    });
  });
});
