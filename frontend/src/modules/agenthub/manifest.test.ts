import { expect, test } from "bun:test";
import { agenthubModule } from "./manifest";

test("Agent Hub draws no tab strip: the sidebar is how you move between sessions", () => {
  expect(agenthubModule.tabs).toBe(false);
  // Its own "new" entry points (the title-bar menu, the nothing-open state) still work without a strip.
  expect(typeof agenthubModule.newTab).toBe("function");
  expect(agenthubModule.sidebar).toBeDefined();
  expect(agenthubModule.opensHome).toBe(true);
});
