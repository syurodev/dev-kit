import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { Workflow } from "lucide-react";

import {
  FramedCard,
  FramedCardAction,
  FramedCardHeader,
  FramedCardMore,
  FramedCardPreview,
  FramedCardRow,
} from "./framed-card";

// docs/dev-kit/framed-card.mdx is the contract; these hold the parts of it a
// class change could quietly break.
describe("FramedCard", () => {
  const html = renderToString(
    <FramedCard>
      <FramedCardHeader icon={Workflow} title="API flow" meta="4 steps" action={<FramedCardAction>Reveal</FramedCardAction>} />
      <FramedCardPreview flush>
        <FramedCardRow label="POST /refunds" trailing="refund.controller.ts:42" onClick={() => {}} />
        <FramedCardMore count={2} />
      </FramedCardPreview>
    </FramedCard>,
  );

  it("nests the radii: the card is the preview's radius plus the 4px frame", () => {
    expect(html).toMatch(/data-slot="framed-card"[^>]*class="[^"]*rounded-float[^"]*p-\(--space-micro\)/);
    expect(html).toMatch(/data-slot="framed-card-preview"[^>]*class="[^"]*rounded-control/);
  });

  it("puts the preview on the page's surface inside the card's raised one", () => {
    expect(html).toMatch(/data-slot="framed-card"[^>]*class="[^"]*bg-surface-raised/);
    expect(html).toMatch(/data-slot="framed-card-preview"[^>]*class="[^"]*bg-surface-base/);
  });

  it("lines the header and a flush row up on the same inline padding", () => {
    expect(html).toMatch(/data-slot="framed-card-header"[^>]*class="[^"]*px-\(--space-inline\)/);
    expect(html).toMatch(/data-slot="framed-card-row"[^>]*class="[^"]*px-\(--space-inline\)/);
  });

  it("draws the header's parts and one action at its end", () => {
    expect(html).toContain("API flow");
    expect(html).toContain('data-slot="framed-card-meta"');
    expect(html).toContain('data-slot="framed-card-action"');
    // React's server render splits "+", the count and " more" with comments.
    expect(html.replace(/<!-- -->/g, "")).toContain("+2 more");
  });

  it("draws no more-line for nothing hidden, and no action when none is given", () => {
    const bare = renderToString(
      <FramedCard>
        <FramedCardHeader title="Symbols" />
        <FramedCardPreview>
          <FramedCardMore count={0} />
        </FramedCardPreview>
      </FramedCard>,
    );
    expect(bare).not.toContain("framed-card-action");
    expect(bare).not.toContain("framed-card-more");
    expect(bare).not.toContain("framed-card-meta");
  });
});
