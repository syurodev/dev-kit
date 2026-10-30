import "@/testing/happyDom";
import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from "bun:test";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { RootErrorBoundary } from "./RootErrorBoundary";

/**
 * A real client render: renderToString runs no error boundary, and "the window
 * went blank" is exactly what React does to a root with none.
 */
// The default report goes through the Wails bridge; no test but the helper's own should reach it.
const noReport = () => {};

describe("RootErrorBoundary", () => {
  let container: HTMLDivElement;
  let root: Root;
  let consoleError: ReturnType<typeof spyOn>;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    // React reports a caught render error on the console, and so does the boundary.
    consoleError = spyOn(console, "error").mockImplementation(mock(() => {}));
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    consoleError.mockRestore();
  });

  function Boom(): never {
    throw new Error("secret-token-in-a-message");
  }

  test("draws the app while nothing throws", async () => {
    await act(async () => root.render(<RootErrorBoundary reload={() => {}} report={noReport}><p>the app</p></RootErrorBoundary>));
    expect(container.textContent).toBe("the app");
  });

  test("a throw anywhere below draws the fallback, not an empty root", async () => {
    await act(async () => root.render(<RootErrorBoundary reload={() => {}} report={noReport}><div><Boom /></div></RootErrorBoundary>));
    expect(container.textContent).toContain("Something went wrong");
    expect(container.querySelector('[data-testid="root-error-fallback"]')).not.toBeNull();
    expect(container.querySelector("button")?.textContent).toBe("Reload");
  });

  test("shows neither the message nor a stack, only the console gets them", async () => {
    await act(async () => root.render(<RootErrorBoundary reload={() => {}} report={noReport}><Boom /></RootErrorBoundary>));
    expect(container.textContent).not.toContain("secret-token");
    expect(container.textContent).not.toContain("    at ");
    const logged = consoleError.mock.calls.map((call) => call.map(String).join(" ")).join("\n");
    expect(logged).toContain("[app] failed to render");
  });

  test("reports the throw once, with the boundary's label, the message, a stack and the component stack", async () => {
    const reports: Array<{ label: string; message: string; hasStack: boolean; componentStack: string | undefined }> = [];
    const report = (label: string, error: unknown, componentStack?: string | null) => {
      const e = error as Error;
      reports.push({ label, message: e.message, hasStack: typeof e.stack === "string" && e.stack.length > 0, componentStack: componentStack ?? undefined });
    };
    await act(async () => root.render(<RootErrorBoundary reload={() => {}} report={report}><div><Boom /></div></RootErrorBoundary>));
    expect(reports.length).toBe(1);
    expect(reports[0].label).toBe("app");
    expect(reports[0].message).toBe("secret-token-in-a-message");
    expect(reports[0].hasStack).toBe(true);
    expect(reports[0].componentStack).toContain("Boom");
  });

  test("reports nothing while nothing throws", async () => {
    const report = mock(() => {});
    await act(async () => root.render(<RootErrorBoundary reload={() => {}} report={report}><p>the app</p></RootErrorBoundary>));
    expect(report).not.toHaveBeenCalled();
  });

  test("a report that throws leaves the fallback on screen", async () => {
    const report = () => {
      throw new Error("the log is gone");
    };
    await act(async () => root.render(<RootErrorBoundary reload={() => {}} report={report}><Boom /></RootErrorBoundary>));
    expect(container.querySelector('[data-testid="root-error-fallback"]')).not.toBeNull();
  });

  test("Reload calls the reload it was given, once", async () => {
    const reload = mock(() => {});
    await act(async () => root.render(<RootErrorBoundary reload={reload} report={noReport}><Boom /></RootErrorBoundary>));
    const button = container.querySelector("button") as HTMLButtonElement;
    await act(async () => button.click());
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
