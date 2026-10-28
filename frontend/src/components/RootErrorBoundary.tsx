import * as React from "react";

import { ErrorBoundary } from "@/components/ui/error-boundary";

/**
 * Reloads the window. Injectable so a test can see the call without leaving
 * the page.
 */
export type ReloadWindow = () => void;

const reloadWindow: ReloadWindow = () => window.location.reload();

/**
 * What the window shows when something threw outside every nearer boundary.
 * Without it React unmounts the root and the window is an empty page while the
 * process lives on. Deliberately small and self-contained — plain elements,
 * chrome tokens only — because it runs after the rest of the UI has failed: no
 * providers, no shared Button, no stack, no data. The console line the boundary
 * writes is where the cause goes.
 */
export function RootErrorFallback({ onReload }: { onReload: ReloadWindow }) {
  return (
    <div
      role="alert"
      data-testid="root-error-fallback"
      className="flex h-screen w-screen flex-col items-center justify-center gap-3 bg-bg-app px-4 text-center"
    >
      <p className="text-body-14-medium text-text-primary">Something went wrong</p>
      <button
        type="button"
        onClick={onReload}
        className="wails-no-drag h-(--control-height-md) cursor-pointer rounded-md border border-border bg-transparent px-4 text-body-14-medium text-text-primary outline-none hover:bg-state-soft focus-visible:border-border-focus"
      >
        Reload
      </button>
    </div>
  );
}

/** The outermost boundary: the whole app below it, one panel in its place when it throws. */
export function RootErrorBoundary({ children, reload = reloadWindow }: { children?: React.ReactNode; reload?: ReloadWindow }) {
  return (
    <ErrorBoundary label="app" fallback={<RootErrorFallback onReload={reload} />}>
      {children}
    </ErrorBoundary>
  );
}
