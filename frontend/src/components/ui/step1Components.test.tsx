import { describe, expect, it, mock } from "bun:test";
import { renderToString } from "react-dom/server";
import { Button } from "./button";
import { Tag } from "./tag";
import { TagInput } from "./tag-input";

describe("Step 1 Shared Components Reconciliation", () => {
  /**
   * Figma 27:85 was a component of its own until it turned out to be Button
   * twice over: its primary tone is Button's primary, and its neutral tone is
   * the same glass recipe as Button's secondary. A second component for a
   * button that already existed is a second place to fix everything.
   */
  describe("Icon Button (Figma 27:85) is Button at icon size", () => {
    it("renders the primary tone with action primary styling", () => {
      const html = renderToString(
        <Button variant="primary" size="icon" aria-label="Confirm">
          <span data-testid="icon">+</span>
        </Button>,
      );

      expect(html).toContain("bg-action-primary");
      expect(html).toContain("text-action-primary-foreground");
      expect(html).toContain("size-(--control-height-md)");
      expect(html).toContain("rounded-md");
    });

    it("renders the neutral tone on the glass recipe", () => {
      const html = renderToString(
        <Button variant="secondary" size="icon" aria-label="Neutral" />,
      );
      expect(html).toContain("bg-(--glass-panel-bg)");
      expect(html).toContain("border-(--glass-border)");
      expect(html).toContain("size-(--control-height-md)");
    });

    it("handles disabled state", () => {
      const html = renderToString(
        <Button variant="primary" size="icon" disabled aria-label="Disabled" />,
      );
      expect(html).toContain("disabled");
      expect(html).toContain("disabled:opacity-38");
    });
  });

  describe("Tag / Chip (Figma 378:2674)", () => {
    it("renders standard 20px tag with text-caption-11-medium and rounded-sm", () => {
      const html = renderToString(<Tag>Architecture</Tag>);

      expect(html).toContain('data-slot="tag"');
      expect(html).toContain("h-5");
      expect(html).toContain("rounded-sm");
      expect(html).toContain("text-caption-11-medium");
      expect(html).toContain("bg-surface-well");
      expect(html).toContain("border-border-subtle");
      expect(html).toContain("text-text-secondary");
        expect(html).toContain("Architecture");
    });

    it("renders dot indicator when dotColor is provided", () => {
      const html = renderToString(<Tag dotColor="#123456">Active</Tag>);
      expect(html).toContain('data-slot="tag-dot"');
      expect(html).toContain("background-color:#123456");
      expect(html).toContain("size-1.5");
      expect(html).toContain("rounded-full");
    });

    it("renders remove button when onRemove is provided", () => {
      const onRemove = mock(() => {});
      const html = renderToString(<Tag onRemove={onRemove}>Removable</Tag>);
      expect(html).toContain('data-slot="tag-remove"');
      expect(html).toContain('aria-label="Remove tag"');
    });
  });

  describe("Tag Input (Figma 378:2698)", () => {
    it("renders tag input container with initial tags and input field", () => {
      const html = renderToString(
        <TagInput
          value={["react", "tailwind"]}
          onChange={() => {}}
          placeholder="New tag..."
        />,
      );

      expect(html).toContain('data-slot="tag-input"');
      expect(html).toContain("min-h-(--control-height-md)");
      expect(html).toContain("rounded-md");
      expect(html).toContain("border-border-subtle");
      expect(html).toContain("bg-surface-well");
        expect(html).toContain("focus-within:border-border-focus");
      expect(html).toContain("react");
      expect(html).toContain("tailwind");
    });

    it("renders disabled state with pointer-events-none and opacity-38", () => {
      const html = renderToString(
        <TagInput
          value={["fixed"]}
          onChange={() => {}}
          disabled
        />,
      );

      expect(html).toContain('data-disabled="true"');
      expect(html).toContain("pointer-events-none");
      expect(html).toContain("opacity-38");
      expect(html).toContain("disabled");
    });

    it("passes tagDotColor to rendered tags", () => {
      const html = renderToString(
        <TagInput
          value={["colored"]}
          onChange={() => {}}
          tagDotColor={(tag) => (tag === "colored" ? "#99e1d9" : undefined)}
        />,
      );

      expect(html).toContain('data-slot="tag-dot"');
      expect(html).toContain("background-color:#99e1d9");
    });
  });
});
