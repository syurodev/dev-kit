import * as React from "react";

/** What a function fallback is told: the error, and a way to give the children another go. */
export type ErrorBoundaryFallbackArgs = { error: unknown; reset: () => void };

type ErrorBoundaryProps = {
  /**
   * Drawn in place of the children once one of them has thrown. A function is
   * called with the error and a `reset` that puts the children back.
   */
  fallback: React.ReactNode | ((args: ErrorBoundaryFallbackArgs) => React.ReactNode);
  /** Names what failed in the console line: "db.query result". */
  label: string;
  /**
   * The subject the children draw. A new one (compared by identity) gives
   * them another go; the same one keeps the fallback, so a view that throws
   * on every render does not retry on each of its parent's.
   */
  resetKey?: unknown;
  /**
   * Told after the console line; for a caller that has to react to the throw.
   * The component stack is the second argument, for a caller that records the
   * failure; a caller that takes one argument is unaffected.
   */
  onError?: (error: unknown, componentStack?: string) => void;
  children?: React.ReactNode;
};

type ErrorBoundaryState = { failed: boolean; error?: unknown; resetKey: unknown };

/**
 * Keeps a throw — a view's bug, or a lazy chunk that no longer exists after
 * an update — to the part of the page that threw, drawing `fallback` there
 * instead of unmounting the whole root. React has no hook for this, so it is
 * a class.
 */
export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { failed: false, resetKey: this.props.resetKey };

  static getDerivedStateFromError(error?: unknown): Partial<ErrorBoundaryState> {
    return { failed: true, error };
  }

  static getDerivedStateFromProps(props: ErrorBoundaryProps, state: ErrorBoundaryState): Partial<ErrorBoundaryState> | null {
    return Object.is(props.resetKey, state.resetKey) ? null : { failed: false, error: undefined, resetKey: props.resetKey };
  }

  componentDidCatch(error: unknown, info: React.ErrorInfo): void {
    console.error(`[${this.props.label}] failed to render; showing its fallback.`, error, info.componentStack);
    this.props.onError?.(error, info.componentStack ?? undefined);
  }

  private reset = (): void => {
    this.setState({ failed: false, error: undefined });
  };

  render(): React.ReactNode {
    if (!this.state.failed) return this.props.children;
    const { fallback } = this.props;
    return typeof fallback === "function" ? fallback({ error: this.state.error, reset: this.reset }) : fallback;
  }
}
