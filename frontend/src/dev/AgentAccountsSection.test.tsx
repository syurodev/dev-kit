import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentAccountsSection } from "./AgentAccountsSection";

describe("the accounts gallery section", () => {
  const html = renderToStaticMarkup(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <AgentAccountsSection />
      </TooltipProvider>
    </QueryClientProvider>,
  );

  test("is the page the gallery links to and draws every state the plan lists", () => {
    expect(html).toContain('id="agent-accounts"');
    for (const state of [
      "Only the built-in account",
      "Three accounts in a saved order",
      "Every account at its limit",
      "A signed-out account offers its sign-in",
      "Add account: the form",
      "A change Go refused",
      "Switching on with one signed-in account",
      "The terms warning",
      "In the transcript",
    ]) {
      expect(html).toContain(state);
    }
  });

  test("uses the real editors: meters, both tones, the add form, both notices", () => {
    expect(html).toContain('role="meter"');
    expect(html).toContain('data-tone="warning"');
    expect(html).toContain('data-tone="danger"');
    expect(html).toContain('aria-label="Add a Claude account"');
    expect(html).toContain("Switched from System default to Work");
    expect(html).toContain("Could not switch from System default to Spare");
    expect(html).toContain("Codex");
  });
});
