import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/lib/query";
import { LiveActivityView } from "./LiveActivityView";
import { QueryHistoryView } from "./QueryHistoryView";
import { SavedQueriesView } from "./SavedQueriesView";

/**
 * Before the first fetch answers there is no list to be empty. Rendering the
 * "nothing here" copy in that window is the flash seen on every tab switch.
 */
describe("Database tab views, first paint", () => {
  it.each([
    ["activity", <LiveActivityView />, "No active queries running"],
    ["history", <QueryHistoryView />, "No queries recorded"],
    ["saved", <SavedQueriesView />, "No saved queries yet"],
  ])(
    "%s shows loading or connection guidance, not the empty copy",
    (_name, view, emptyCopy) => {
      const html = renderToString(
        <QueryClientProvider client={queryClient}>{view}</QueryClientProvider>,
      );
      if (_name === "activity") expect(html).toContain('aria-label="Loading"');
      else expect(html).toContain("Choose a connection");
      expect(html).not.toContain(emptyCopy);
    },
  );
});
