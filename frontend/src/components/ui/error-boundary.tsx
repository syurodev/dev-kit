import * as React from "react";

type ErrorBoundaryProps = {
  /** Drawn in place of the children once one of them has thrown. */
  fallback: React.ReactNode;
  /** Names what failed in the console line: "db.query result". */
  label: string;
  /**
   * The subject the children draw. A new one (compared by identity) gives
   * them another go; the same one keeps the fallback, so a view that throws
   * on every render does not retry on each of its parent's.
   */
  resetKey?: unknown;
  children?: React.ReactNode;
};

type ErrorBoundaryState = { failed: boolean; resetKey: unknown };

/**
 * Keeps a throw — a view's bug, or a lazy chunk that no longer exists after
 * an update — to the part of the page that threw, drawing `fallback` there
 * instead of unmounting the whole root. React has no hook for this, so it is
 * a class.
 */
export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { failed: false, resetKey: this.props.resetKey };

  static getDerivedStateFromError(): Partial<ErrorBoundaryState> {
    return { failed: true };
  }

  static getDerivedStateFromProps(props: ErrorBoundaryProps, state: ErrorBoundaryState): Partial<ErrorBoundaryState> | null {
    return Object.is(props.resetKey, state.resetKey) ? null : { failed: false, resetKey: props.resetKey };
  }

  componentDidCatch(error: unknown, info: React.ErrorInfo): void {
    console.error(`[${this.props.label}] failed to render; showing its fallback.`, error, info.componentStack);
  }

  render(): React.ReactNode {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}
