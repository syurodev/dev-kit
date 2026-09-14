import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("./sonner.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");

test("live toast uses the app's resolved theme, not a disconnected provider", () => {
  expect(source).toContain('from "@/theme/ThemeProvider"');
  expect(source).not.toContain("next-themes");
  expect(source).toContain("theme={resolvedTheme}");
});

test("live toast owns appearance and supplies the structure unstyled Sonner removes", () => {
  expect(source).toContain("unstyled: true");
  expect(source).toContain("app-toast flex");
  expect(source).toContain('content: "flex min-w-0 flex-1 flex-col gap-0"');
  expect(source).toContain('offset="var(--space-section)"');
  // Sonner's unlayered dark description rule still runs in unstyled mode.
  expect(css).toContain(".toaster[data-sonner-toaster] .app-toast [data-description]");
  expect(css).toContain("@apply text-body-12 text-text-tertiary;");
  expect(css).toContain(".toaster[data-sonner-toaster][data-sonner-theme] .app-toast [data-close-button]");
});
