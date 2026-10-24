import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AgentSandboxSetting } from "./AgentSandboxSetting";
import type { AgentSandboxSettings } from "../../../bindings/devkit/internal/preferences/models";

function render(settings?: AgentSandboxSettings) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (settings) client.setQueryData(["preferences", "agent-sandbox"], settings);
  return renderToStaticMarkup(<QueryClientProvider client={client}><AgentSandboxSetting /></QueryClientProvider>);
}

function toolchainRow(html: string) {
  return html.slice(html.indexOf("Allow developer toolchains"), html.indexOf("Read-only access to macOS"));
}

describe("AgentSandboxSetting developer toolchains", () => {
  test("old/unset settings enable the toolchain switch", () => {
    const html = render({ revision: 0 });
    expect(toolchainRow(html)).toContain('aria-checked="true"');
    expect(html).toContain("Read-only access to macOS Command Line Tools and Xcode");
  });
  test("a stored opt-out stays off", () => {
    expect(toolchainRow(render({ revision: 4, allowDeveloperToolchains: false }))).toContain('aria-checked="false"');
  });
  test("the switch stays disabled until settings load", () => {
    expect(toolchainRow(render())).toContain("disabled");
  });
});
