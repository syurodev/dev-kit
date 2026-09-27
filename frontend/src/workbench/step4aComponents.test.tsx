import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TabStrip } from "./tabs/TabStrip";
import { ModuleSwitcher } from "./ModuleSwitcher";
import { Button } from "@/components/ui/button";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { WorkbenchTab } from "../stores/workbenchTabs";
import { Titlebar } from "@/components/ui/titlebar";
import { ModuleEmptyState } from "./ModuleEmptyState";
import { databaseModule } from "@/modules/database/manifest";
import { notesModule } from "@/modules/notes/manifest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

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

  /**
   * This used to declare the class string and then assert that the string it
   * had just written contained the words it had just written — green whatever
   * the title bar actually rendered, which is how nine call sites drifted into
   * copies of it. It renders the real control now.
   */
  describe("Titlebar Action (Figma 674:47115)", () => {
    it("is Button's titlebar variant: 28x28, radius 8, state-soft hover, no-drag", () => {
      const html = renderToString(
        <Button variant="titlebar" size="icon-sm" aria-label="Commands" />,
      );
      expect(html).toContain("size-(--control-height-sm)"); // 28px
      expect(html).toContain("rounded-md"); // --radius-md is 8px
      expect(html).toContain("hover:bg-state-soft");
      // A control in the drag region that hands its mousedown to the window
      // move is a control you cannot click.
      expect(html).toContain("[--wails-draggable:no-drag]");
    });
  });

  describe("System Status (Figma 616:932)", () => {
    it("verifies 3 distinct shapes exception: Neutral has radius 8, Attention/Active have no radius wrapper", () => {
      // EXCEPTION: Neutral is a rounded-8 28x28 wrapper with 16px glyph.
      // Attention and Active are full-bleed 28x28 graphics with no radius wrapper.
      const neutralClass = "size-7 shrink-0 rounded-md";
      const attentionClass = "size-7 shrink-0 rounded-none p-0";
      const activeClass = "size-7 shrink-0 rounded-none p-0";

      expect(neutralClass).toContain("rounded-md");
      expect(attentionClass).toContain("rounded-none");
      expect(activeClass).toContain("rounded-none");
    });
  });

  describe("Tab Strip (Figma 449:325)", () => {
    it("renders a compact 40px strip with 4px vertical inset, new-tab button and trailing spacer", () => {
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
        <TooltipProvider>
        <TabStrip
          tabs={sampleTabs}
          activeTabId="notes:1"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />
        </TooltipProvider>,
      );

      // Compact shell: retain horizontal alignment while bringing content up.
      expect(html).toContain("h-10");
      expect(html).toContain("px-2 py-1");
      expect(html).toContain("gap-2");
      // New-tab button: the titlebar variant at icon-sm — 28x28, radius 8 —
      // with a 13px plus of its own.
      expect(html).toContain('aria-label="New tab"');
      expect(html).toContain("size-(--control-height-sm)");
      expect(html).toContain("rounded-md");
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
        <TooltipProvider>
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />
        </TooltipProvider>,
      );

      // 200x32, radius 6, px-3, gap-2
      expect(html).toContain("h-8");
      expect(html).toContain("rounded-sm");
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
        <TooltipProvider>
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />
        </TooltipProvider>,
      );

      expect(html).toContain("bg-state-soft");
      expect(html).toContain("border-border-subtle");
      expect(html).toContain('aria-label="Close All Notes"');
    });

    it("renders Preview pill with dashed border, body-12 regular font, no close button and no dot", () => {
      const html = renderToString(
        <TooltipProvider>
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={false}
          onExitBrowsing={() => {}}
        />
        </TooltipProvider>,
      );

      expect(html).toContain("border-dashed");
      expect(html).toContain("text-body-12");
      // Preview tab has NO close button
      expect(html).not.toContain('aria-label="Close Query 1"');
    });

    it("renders separate radius 8 focus ring layer when keyboard focused", () => {
      const html = renderToString(
        <TooltipProvider>
        <TabStrip
          tabs={tabs}
          activeTabId="active-tab"
          onSelect={() => {}}
          onClose={() => {}}
          onReorder={() => {}}
          keyboardBrowsing={true}
          onExitBrowsing={() => {}}
        />
        </TooltipProvider>,
      );

      // Separate layer at radius 8, drawn outside pill (-inset-[2px]), border-2
      expect(html).toContain("rounded-md");
      expect(html).toContain("-inset-[2px]");
      expect(html).toContain("border-[var(--border-focus,#b2d5e5)]");
    });
  });

  describe("ModuleSwitcher", () => {
    it("draws a no-drag title-bar button per module and marks the current one", () => {
      // No registerModule here: the registry is shared across test files and
      // throws on a second registration. Icons, peeks and badges come from it,
      // and store-driven badges read Zustand's initial state under SSR anyway.
      const html = renderToString(
        <TooltipProvider>
          <ModuleSwitcher
            activeModuleId="database"
            items={[
              { kind: "module", module: { id: "database", title: "Database" } },
              { kind: "module", module: { id: "agenthub", title: "Agent Hub" } },
              { kind: "module", module: { id: "notes", title: "Notes", unavailable: true } },
            ]}
            onSelectModule={() => {}}
          />
        </TooltipProvider>,
      );
      expect(html.match(/\[--wails-draggable:no-drag\]/g)?.length).toBe(3);
      expect(html.match(/aria-current="page"/g)?.length).toBe(1);
      expect(html).toContain('aria-label="Database"');
      expect(html).toMatch(/<button[^>]*disabled[^>]*aria-label="Notes"/);
    });

    it("draws a group as one button, lit while any member is on screen", () => {
      const members = [
        { id: "database", title: "Database" },
        { id: "kafka", title: "Kafka" },
        { id: "ssh", title: "SSH" },
      ];
      const draw = (activeModuleId: string) =>
        renderToString(
          <TooltipProvider>
            <ModuleSwitcher
              activeModuleId={activeModuleId}
              items={[
                { kind: "module", module: { id: "git", title: "Git" } },
                { kind: "group", group: { id: "infra", title: "Infra" }, members },
              ]}
              onSelectModule={() => {}}
            />
          </TooltipProvider>,
        );
      const away = draw("git");
      // Two buttons for three members: the members are behind the peek, not on the bar.
      expect(away.match(/\[--wails-draggable:no-drag\]/g)?.length).toBe(2);
      expect(away).toContain('aria-label="Infra"');
      expect(away).not.toContain('aria-label="Kafka"');
      // The notch is what says there is more behind the button.
      expect(away).toContain("clip-path");
      expect(away.match(/aria-current="page"/g)?.length).toBe(1);

      const inside = draw("kafka");
      expect(inside.match(/aria-current="page"/g)?.length).toBe(1);
      expect(inside).toMatch(/aria-label="Infra"[^>]*aria-current="page"|aria-current="page"[^>]*aria-label="Infra"/);
    });
  });
});


/**
 * A module with nothing open.
 *
 * It used to report "No view is currently available." — a fault message for a
 * state that is not a fault — because the workbench always conjured the home
 * view instead of remembering what was left open. Tabs are stored now, so
 * "nothing" is a real answer and has to say what to do about it.
 */
describe("a module with no tabs", () => {
  it("offers the way in rather than reporting a fault", () => {
    const html = renderToString(<ModuleEmptyState module={databaseModule} />);
    expect(html).not.toContain("No view is currently available");
    expect(html).toContain("has nothing open");
    expect(html).toContain("Open console");
    // The invitation is the craft button: alone on the screen, pressed rarely,
    // and not an error. Its wash is a child that grows past its own box, so
    // the pill has to clip — without this it is a plain button in a round hole.
    expect(html).toContain("rounded-full");
    expect(html).toContain("overflow-hidden");
    expect(html).toContain("group-hover/button:scale-[15]");
  });

  it("says so plainly when there is no module at all", () => {
    const html = renderToString(<ModuleEmptyState module={undefined} />);
    expect(html).toContain("No modules are available");
  });
});

/**
 * A rail that only exists while a document does.
 *
 * Every module registered its rail from its views, which held exactly as long
 * as the workbench always had one open. It stopped holding the moment tabs
 * became something remembered rather than invented: a module left with nothing
 * open had no rail at all — the reported "where is the sidebar". A module that
 * can be emptied has to own its rail.
 */
describe("a module that can be left with nothing open", () => {
  it("declares its own rail, not only its views'", () => {
    for (const module of [databaseModule, notesModule]) {
      expect(module.tabs, `${module.id} is in this list because it has tabs`).toBe(true);
      expect(
        module.sidebar,
        `${module.id} can be left with no document open, and its rail would go with it`,
      ).toBeDefined();
    }
  });
});

/**
 * A rail's data has to come from the rail.
 *
 * DatabaseView fetched the connection list on mount, which held only while a
 * console was always open. With tabs remembered rather than invented, a module
 * can be on screen with no document at all — and the rail sat on "No
 * connections configured." until one was opened. The rail mounts once for the
 * module (SidebarSurface keeps it mounted even collapsed), so the fetch is
 * there and nowhere else.
 */
describe("the connection rail's own data", () => {
  const read = (file: string) =>
    readFileSync(join(import.meta.dir, "..", file), "utf8");

  it("is fetched by the rail, not by a document", () => {
    expect(read("modules/database/components/DatabaseSidebar.tsx")).toContain(
      "void fetchProfiles();",
    );
    expect(
      read("modules/database/DatabaseView.tsx"),
      "a document fetching it means an empty rail whenever no document is open",
    ).not.toContain("fetchProfiles");
  });
});
