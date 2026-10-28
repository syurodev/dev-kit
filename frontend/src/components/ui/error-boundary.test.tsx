import { describe, expect, mock, spyOn, test } from "bun:test";
import * as React from "react";
import { ErrorBoundary } from "./error-boundary";

/**
 * renderToString runs no error boundary, so the boundary's three parts are
 * exercised directly: what an error turns its state into, what it draws in
 * each state, and when a new subject puts the children back.
 */
describe("ErrorBoundary", () => {
  const fallback = <p>fallback</p>;
  const child = <p>view</p>;

  const boundary = (props: Partial<React.ComponentProps<typeof ErrorBoundary>> = {}) =>
    new ErrorBoundary({ fallback, label: "test", children: child, ...props });

  test("draws its children until something below it throws, then its fallback", () => {
    const b = boundary();
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromProps(b.props, b.state) };
    expect(b.render()).toBe(child);
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromError() };
    expect(b.render()).toBe(fallback);
  });

  test("a fallback of nothing draws nothing", () => {
    const b = boundary({ fallback: null });
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromError() };
    expect(b.render()).toBeNull();
  });

  test("stays failed for the same subject, tries again for a new one", () => {
    const call = { id: "a" };
    const b = boundary({ resetKey: call });
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromProps(b.props, b.state) };
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromError() };
    // The parent re-renders with the same call: still the fallback, no retry loop.
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromProps({ ...b.props }, b.state) };
    expect(b.render()).toBe(fallback);
    // The call is replaced (an update landed): the view gets another go.
    const next = { ...b.props, resetKey: { id: "a" } };
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromProps(next, b.state) };
    expect(b.state.failed).toBe(false);
  });

  test("a function fallback is told the error and can put the children back", () => {
    const thrown = new Error("boom");
    const seen: Array<{ error: unknown }> = [];
    const b = boundary({
      fallback: ({ error }) => {
        seen.push({ error });
        return <p>fn fallback</p>;
      },
    });
    b.state = { ...b.state, ...ErrorBoundary.getDerivedStateFromError(thrown) };
    const drawn = b.render() as React.ReactElement;
    expect(seen).toEqual([{ error: thrown }]);
    expect(drawn).toEqual(<p>fn fallback</p>);
    // reset() is a setState; a real instance has React's updater, this one does not.
    const states: unknown[] = [];
    b.setState = ((next: unknown) => void states.push(next)) as never;
    (b as unknown as { reset: () => void }).reset();
    expect(states).toEqual([{ failed: false, error: undefined }]);
  });

  test("tells onError about the throw after the console line", () => {
    const consoleError = spyOn(console, "error").mockImplementation(mock(() => {}));
    try {
      const onError = mock(() => {});
      const b = boundary({ onError });
      const thrown = new Error("boom");
      b.componentDidCatch(thrown, { componentStack: "" });
      expect(onError).toHaveBeenCalledWith(thrown);
    } finally {
      consoleError.mockRestore();
    }
  });

  test("says what failed once, on the console", () => {
    const error = spyOn(console, "error").mockImplementation(mock(() => {}));
    try {
      const b = boundary({ label: "db.query result" });
      const thrown = new Error("boom");
      b.componentDidCatch(thrown, { componentStack: "\n    at Card" });
      expect(error).toHaveBeenCalledTimes(1);
      expect(String(error.mock.calls[0][0])).toContain("db.query result");
      expect(error.mock.calls[0]).toContain(thrown);
    } finally {
      error.mockRestore();
    }
  });
});
