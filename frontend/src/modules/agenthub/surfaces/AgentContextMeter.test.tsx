import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { AgentContextMeter, shortCount } from "./AgentContextMeter";

describe("AgentContextMeter", () => {
  it("is a ring that says how full the window is, in words for a screen reader", () => {
    const html = renderToString(<AgentContextMeter usage={{ used: 24503, size: 1_000_000, amount: 0.21 }} />);
    expect(html).toContain('data-testid="composer-context"');
    expect(html).toContain('aria-label="Context: 24.5k of 1M used (2%)"');
    expect(html).toContain("<svg");
    // The numbers live in the popover, not spelled out on the composer.
    expect(html).not.toContain("1,000,000");
  });

  it("changes colour as the window fills", () => {
    const at = (used: number) => renderToString(<AgentContextMeter usage={{ used, size: 1_000_000, amount: 0 }} />);
    expect(at(500_000)).toContain('data-level="info"');
    expect(at(850_000)).toContain('data-level="warning"');
    expect(at(960_000)).toContain('data-level="danger"');
  });

  it("draws nothing before the agent has reported a window", () => {
    expect(renderToString(<AgentContextMeter usage={null} />)).toBe("");
    expect(renderToString(<AgentContextMeter usage={{ used: 0, size: 0, amount: 0 }} />)).toBe("");
  });

  it("abbreviates counts the way the gauge reads them", () => {
    expect(shortCount(29_480)).toBe("29.5k");
    expect(shortCount(1_000_000)).toBe("1M");
    expect(shortCount(812)).toBe("812");
  });
});
