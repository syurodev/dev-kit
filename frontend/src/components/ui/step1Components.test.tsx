import { describe, expect, it, mock } from "bun:test";
import { renderToString } from "react-dom/server";
import { IconButton } from "./icon-button";
import { Tag } from "./tag";
import { TagInput } from "./tag-input";

describe("Step 1 Shared Components Reconciliation", () => {
  describe("Icon Button (Figma 27:85)", () => {

    it("renders tone='primary' with action primary lime styling", () => {
      const html = renderToString(
        <IconButton tone="primary" aria-label="Confirm">
          <span data-testid="icon">+</span >
        </IconButton>,
      );

      expect(html).toContain('data-tone="primary"');
      expect(html).toContain("bg-action-primary");
      expect(html).toContain("text-action-primary-foreground");
      expect(html).toContain("size-(--control-height-md)");
      expect(html).toContain("rounded-md");
    });


    it("handles disabled state", () => {
      const html = renderToString(<IconButton disabled aria-label="Disabled" />);
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
      expect(html).toContain("bg-white/6");
      expect(html).toContain("border-status-info/8");
      expect(html).toContain("text-text-secondary");
        expect(html).toContain("Architecture");
    });

    it("renders dot indicator when dotColor is provided", () => {
      const html = renderToString(<Tag dotColor="#c6ff34">Active</Tag>);
      expect(html).toContain('data-slot="tag-dot"');
      expect(html).toContain("background-color:#c6ff34");
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
      expect(html).toContain("border-status-info/16");
      expect(html).toContain("bg-white/6");
        expect(html).toContain("focus-within:border-status-info");
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
