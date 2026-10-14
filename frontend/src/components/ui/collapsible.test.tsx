import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "./collapsible";

const contentClass = (html: string) => html.match(/data-slot="collapsible-content"[^>]*class="([^"]*)"/)?.[1] ?? "";

function render(open: boolean, content: React.ReactNode) {
  return renderToString(
    <Collapsible open={open}>
      <CollapsibleTrigger>Toggle</CollapsibleTrigger>
      {content}
    </Collapsible>,
  );
}

describe("CollapsibleContent", () => {
  it("animates its height by default: open over --motion-slow, shut over --motion-base, one easing", () => {
    const cls = contentClass(render(true, <CollapsibleContent>rows</CollapsibleContent>)).split(" ");
    expect(cls).toContain("overflow-hidden");
    expect(cls).toContain("data-open:animate-collapsible-down");
    expect(cls).toContain("data-closed:animate-collapsible-up");
    expect(cls).toContain("data-open:duration-(--motion-slow)");
    expect(cls).toContain("data-closed:duration-(--motion-base)");
    expect(cls).toContain("ease-(--ease-out-strong)");
  });

  it("snaps under Reduce Motion, in both states", () => {
    // Variant-for-variant with the animation: a bare motion-reduce:animate-none
    // sorts before data-open:animate-* in Tailwind's output and loses to it.
    const cls = contentClass(render(true, <CollapsibleContent>rows</CollapsibleContent>)).split(" ");
    expect(cls).toContain("data-open:motion-reduce:animate-none");
    expect(cls).toContain("data-closed:motion-reduce:animate-none");
  });

  it("animated={false} keeps the shadcn element as it was", () => {
    const cls = contentClass(render(true, <CollapsibleContent animated={false}>rows</CollapsibleContent>));
    expect(cls).not.toContain("animate-collapsible");
    expect(cls).not.toContain("overflow-hidden");
    // The prop is the component's, not an attribute on the DOM.
    expect(render(true, <CollapsibleContent animated={false}>rows</CollapsibleContent>)).not.toContain("animated");
  });

  it("a caller's classes win (overflow-auto for a scrolling body)", () => {
    const cls = contentClass(render(true, <CollapsibleContent className="overflow-auto">rows</CollapsibleContent>));
    expect(cls).toContain("overflow-auto");
    expect(cls).not.toContain("overflow-hidden");
  });

  it("draws nothing while closed, so a shut disclosure costs no DOM", () => {
    const html = render(false, <CollapsibleContent>rows</CollapsibleContent>);
    expect(html).not.toContain("rows");
    expect(html).toContain('aria-expanded="false"');
  });

  it("fades as it moves: the height keyframes carry opacity too, on the motion tokens by default", () => {
    // tw-animate's collapsible-down/up only move height; index.css adds the fade
    // to the same animation so a single class drives both.
    const css = readFileSync(join(import.meta.dir, "../../index.css"), "utf8");
    const down = css.match(/--animate-collapsible-down:([^;]*);/)?.[1] ?? "";
    const up = css.match(/--animate-collapsible-up:([^;]*);/)?.[1] ?? "";
    expect(down).toContain("collapsible-down");
    expect(down).toContain("collapsible-fade-in");
    expect(down).toContain("var(--motion-slow)");
    expect(up).toContain("collapsible-up");
    expect(up).toContain("collapsible-fade-out");
    expect(up).toContain("var(--motion-base)");
    expect(css).toMatch(/@keyframes collapsible-fade-in\s*\{\s*from\s*\{\s*opacity:\s*0/);
    expect(css).toMatch(/@keyframes collapsible-fade-out\s*\{\s*to\s*\{\s*opacity:\s*0/);
  });
});
