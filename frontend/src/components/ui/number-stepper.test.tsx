import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { NumberStepper } from "./number-stepper";

function button(html: string, label: string) {
  return Array.from(html.matchAll(/<button\b[^>]*>/g), match => match[0])
    .find(tag => tag.includes(`aria-label="${label}"`)) ?? "";
}
const disabled = (tag: string) => /\sdisabled(?:=""|(?=\s|>))/.test(tag);

test("the stepper exposes its current value and bounds to keyboard users", () => {
  const html = renderToString(<NumberStepper value={14} min={10} max={24} label="Font size" onChange={() => {}} />);
  expect(html).toContain('role="spinbutton"');
  expect(html).toContain('aria-valuenow="14"');
  expect(html).toContain('aria-valuemin="10"');
  expect(html).toContain('aria-valuemax="24"');
  expect(html).toContain('tabindex="0"');
  expect(disabled(button(html, "Font size: smaller"))).toBe(false);
  expect(disabled(button(html, "Font size: larger"))).toBe(false);
});

test("only the unavailable direction is disabled at each bound", () => {
  const low = renderToString(<NumberStepper value={10} min={10} max={24} label="Size" onChange={() => {}} />);
  const high = renderToString(<NumberStepper value={24} min={10} max={24} label="Size" onChange={() => {}} />);
  expect(disabled(button(low, "Size: smaller"))).toBe(true);
  expect(disabled(button(low, "Size: larger"))).toBe(false);
  expect(disabled(button(high, "Size: smaller"))).toBe(false);
  expect(disabled(button(high, "Size: larger"))).toBe(true);
});

test("a disabled stepper removes all three controls from keyboard interaction", () => {
  const html = renderToString(<NumberStepper value={14} label="Size" disabled onChange={() => {}} />);
  expect(html).toContain('aria-disabled="true"');
  expect(html).toContain('tabindex="-1"');
  expect(disabled(button(html, "Size: smaller"))).toBe(true);
  expect(disabled(button(html, "Size: larger"))).toBe(true);
});
