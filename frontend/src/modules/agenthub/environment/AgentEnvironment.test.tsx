import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { EnvironmentReportView } from "./AgentEnvironment";
import { ENVIRONMENT_PREVIEW } from "@/dev/AgentEnvironmentPreview";

test("inventory distinguishes discovery, runtime loading and authentication", () => {
  const html = renderToString(<EnvironmentReportView report={ENVIRONMENT_PREVIEW} />);
  expect(html).toContain("Found or configured does not verify execution");
  expect(html).toContain("Loaded");
  expect(html).toContain("Sign-in needed");
  expect(html).toContain("This tool is optional");
  expect(html).toContain("Add a valid name and description");
  expect(html).toContain("after reopening the session");
});

test("next-session report never claims that an agent has started", () => {
  const html = renderToString(<EnvironmentReportView report={{ ...ENVIRONMENT_PREVIEW, scope: "next", entries: [] }} />);
  expect(html).toContain("No agent was started");
  expect(html).toContain("No skills found in the inspected sources");
  expect(html).not.toContain("this session’s launch");
});

test("untrusted names and paths are rendered as text", () => {
  const html = renderToString(<EnvironmentReportView report={{ ...ENVIRONMENT_PREVIEW, entries: [{
    name: "<script>alert(1)</script>", source: "<img src=x onerror=alert(1)>", kind: "skills", status: "made-up", detail: "<b>text</b>",
  }] }} />);
  expect(html).not.toContain("<script>");
  expect(html).not.toContain("<img src=x");
  expect(html).toContain("&lt;script&gt;");
  expect(html).toContain("Unverified");
});
