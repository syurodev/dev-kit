import { describe, expect, it } from "bun:test"
import { renderToString } from "react-dom/server"
import { Input } from "./input"
import { Checkbox } from "./checkbox"
import { Switch } from "./switch"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "./select"
import { Tabs, TabsList, TabsTrigger, TabsContent, TabsCount } from "./tabs"
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "./tooltip"
import { readFileSync } from "node:fs";
import { join } from "node:path";

describe("Step 2 Form Controls Reconciliation", () => {
  describe("Input (Figma 28:62)", () => {
    it("renders field with shared glass recipe and body-14 typography", () => {
      const html = renderToString(
        <Input placeholder="Enter username..." defaultValue="dev-kit" />,
      )

      expect(html).toContain('data-slot="input"')
      expect(html).toContain("rounded-md")
      expect(html).toContain("border-status-info/16")
      expect(html).toContain("bg-white/6")
      expect(html).toContain("text-body-14")
      expect(html).toContain("text-text-primary")
      expect(html).toContain("placeholder:text-text-tertiary")
    })

    it("renders password field with visibility action toggle button", () => {
      const html = renderToString(
        <Input type="password" placeholder="Password" defaultValue="secret123" />,
      )

      expect(html).toContain('type="password"')
      expect(html).toContain('data-slot="input-visibility-action"')
      expect(html).toContain('aria-label="Show password"')
    })

    it("renders label and helper text when provided", () => {
      const html = renderToString(
        <Input
          label="Email Address"
          helperText="We will never share your email"
        />,
      )

      expect(html).toContain('data-slot="input-container"')
      expect(html).toContain("text-label-12")
      expect(html).toContain("text-text-secondary")
      expect(html).toContain("Email Address")
      expect(html).toContain("text-caption-11")
      expect(html).toContain("text-text-tertiary")
      expect(html).toContain("We will never share your email")
    })

    it("renders error state with danger border and helper color", () => {
      const html = renderToString(
        <Input
          label="API Key"
          error="Invalid key format"
        />,
      )

      expect(html).toContain('aria-invalid="true"')
      expect(html).toContain("text-status-danger")
      expect(html).toContain("Invalid key format")
    })

    it("handles disabled state", () => {
      const html = renderToString(<Input disabled placeholder="Locked" />)
      expect(html).toContain("disabled")
      expect(html).toContain("disabled:text-text-tertiary")
    })
  })

  describe("Checkbox (Figma 28:78)", () => {
    it("renders 18x18 r=6 control with shared glass surface recipe", () => {
      const html = renderToString(<Checkbox id="test-check" />)

      expect(html).toContain('data-slot="checkbox"')
      expect(html).toContain("size-4.5")
      expect(html).toContain("rounded-sm")
      expect(html).toContain("bg-white/6")
      expect(html).toContain("border-status-info/16")
    })

    it("renders checked indicator and styling", () => {
      const html = renderToString(<Checkbox checked />)

      expect(html).toContain('data-state="checked"')
      expect(html).toContain("data-checked:bg-action-primary")
      expect(html).toContain('data-slot="checkbox-indicator"')
    })

    it("renders row wrapper with label when label prop is provided", () => {
      const html = renderToString(
        <Checkbox id="remember" label="Remember credentials" />,
      )

      expect(html).toContain('data-slot="checkbox-row"')
      expect(html).toContain("text-body-13")
      expect(html).toContain("text-text-primary")
      expect(html).toContain("Remember credentials")
    })
  })

  describe("Switch (Figma 570:59)", () => {
    it("renders 32x18 r=full track and 16x16 thumb", () => {
      const html = renderToString(<Switch id="dark-mode" />)

      expect(html).toContain('data-slot="switch"')
      expect(html).toContain("h-4.5")
      expect(html).toContain("w-8")
      expect(html).toContain("rounded-full")
      expect(html).toContain("bg-white/6")
      expect(html).toContain("border-transparent")
      expect(html).toContain('data-slot="switch-thumb"')
      expect(html).toContain("size-4")
      expect(html).toContain("rounded-full")
    })

    it("renders checked switch with action primary fill and dark thumb", () => {
      const html = renderToString(<Switch checked />)

      expect(html).toContain('data-state="checked"')
      expect(html).toContain("data-checked:bg-action-primary")
      expect(html).toContain("group-data-[state=checked]/switch:bg-palette-onyx")
    })
  })

  describe("Select (Figma 378:61, 378:70, 378:71)", () => {
    it("renders SelectTrigger with max-w-55, height-md and text-body-13", () => {
      const html = renderToString(
        <Select defaultValue="postgres">
          <SelectTrigger>
            <SelectValue placeholder="Select dialect" />
          </SelectTrigger>
        </Select>,
      )

      expect(html).toContain('data-slot="select-trigger"')
      expect(html).toContain("max-w-55")
      expect(html).toContain("h-(--control-height-md)")
      expect(html).toContain("rounded-md")
      expect(html).toContain("border-status-info/16")
      expect(html).toContain("bg-white/6")
      expect(html).toContain("text-body-13")
    })

    it("renders SelectContent and SelectItem with check indicator and body-13 text", () => {
      const html = renderToString(
        <Select defaultValue="postgres" open>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent portal={false}>
            <SelectItem value="postgres">PostgreSQL</SelectItem>
          </SelectContent>
        </Select>,
      )

      expect(html).toContain('data-slot="select-content"')
      expect(html).toContain('data-slot="select-item"')
      expect(html).toContain("h-8")
      expect(html).toContain("rounded-sm")
      expect(html).toContain("text-body-13")
      expect(html).toContain("PostgreSQL")
    })
  })

  describe("Tabs (Figma 318:385, 317:380)", () => {
    it("renders default TabsList with black/25 fill and rounded-md", () => {
      const html = renderToString(
        <Tabs defaultValue="overview">
          <TabsList variant="default">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="activity">
              Activity <TabsCount>12</TabsCount>
            </TabsTrigger>
          </TabsList>
          <TabsContent value="overview">Overview content</TabsContent>
        </Tabs>,
      )

      expect(html).toContain('data-slot="tabs-list"')
      expect(html).toContain("h-8")
      expect(html).toContain("rounded-md")
      // Figma's black/25 lives in --segment-track now, with a light-mode
      // value beside it: composited on a light surface the raw alpha made a
      // grey track with the chip six values away from it, so the selected
      // tab was invisible in light mode.
      expect(html).toContain("bg-(--segment-track)")
      expect(html).toContain('data-slot="tabs-trigger"')
      expect(html).toContain("h-6")
      expect(html).toContain("rounded-sm")
      // Figma 317:380: label and count are both 14 Medium, not 13.
      expect(html).toContain("text-body-14-medium")
      expect(html).toContain('data-slot="tabs-count"')
    })

    it("renders line variant TabsList with bottom rule active indicator", () => {
      const html = renderToString(
        <Tabs defaultValue="tab1">
          <TabsList variant="line">
            <TabsTrigger value="tab1">Tab 1</TabsTrigger>
          </TabsList>
        </Tabs>,
      )

      expect(html).toContain("bg-transparent")
      expect(html).toContain("rounded-none")
      // The bottom rule is the shared sliding indicator now, not an ::after.
      expect(html).toContain('data-slot="tabs-indicator"')
      expect(html).toContain("tabs-list:bg-text-primary")
    })
  })

  describe("Tooltip (Figma 28:79)", () => {
    it("renders TooltipContent with glass floating surface, rounded-md and label-12", () => {
      const html = renderToString(
        <TooltipProvider>
          <Tooltip open>
            <TooltipTrigger asChild>
              <button type="button">Hover me</button>
            </TooltipTrigger>
            <TooltipContent portal={false}>Inspect schema</TooltipContent>
          </Tooltip>
        </TooltipProvider>,
      )

      expect(html).toContain('data-slot="tooltip-content"')
      expect(html).toContain("h-8")
      expect(html).toContain("rounded-md")
      expect(html).toContain("bg-surface-glass-floating")
      expect(html).toContain("text-label-12")
      expect(html).toContain("Inspect schema")
    })
  })
})

/**
 * A menu whose items are wider than the menu clips them.
 *
 * DropdownMenuContent used to pin itself to the trigger's width — select
 * behaviour on an action menu — so a menu off an icon button came out at the
 * 128px floor while its items declared 200, and every label was cut. The floor
 * and the item minimum have to agree, in both menus.
 */
describe("menu widths", () => {
  // Comments stripped before matching. The comment above the fix names the
  // very binding being forbidden, and a plain substring search reads its own
  // explanation as the thing it is looking for — which it did.
  const read = (file: string) =>
    readFileSync(join(import.meta.dir, file), "utf8")
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .replace(/\/\/.*$/gm, "");

  it.each([
    ["dropdown-menu.tsx", "radix-dropdown-menu-trigger-width"],
    ["context-menu.tsx", "radix-context-menu-trigger-width"],
  ])("%s does not size an action menu to its trigger", (file, binding) => {
    expect(read(file), `${file} pins its content to ${binding}`).not.toContain(`w-(--${binding})`);
  });

  // The floor is deliberately NOT asserted against the item minimum: a floor
  // is not a cap, and with no width imposed the items grow the box themselves.
  // Pinning the two numbers together would have been a rule about nothing.
});
