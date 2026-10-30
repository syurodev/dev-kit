import { AppService } from "../../bindings/devkit/internal/bridge";

/**
 * A one-way channel from the window to DevKit's own log. The production webview
 * has no inspector, so an error that unmounts the root is gone when the person
 * presses Reload; Go writes one bounded line per report to its log and to
 * client-errors.log (Go caps and rate-limits again, this only bounds the
 * crossing). It records a label, a message and a stack: never props, state,
 * the DOM or a store.
 */

/** At most this many reports leave one page load. */
export const MAX_CLIENT_ERROR_REPORTS = 20;

const MAX_MESSAGE = 1000;
const MAX_STACK = 4000;
const MAX_COMPONENT_STACK = 2000;

/** The bridge call, injectable so a test sees what would be sent. */
export type ClientErrorSender = (label: string, message: string, stack: string, componentStack: string) => Promise<void>;

/** Never throws and returns nothing: a diagnostic must not become a second failure. */
export type ClientErrorReporter = (label: string, error: unknown, componentStack?: string | null) => void;

const sendToGo: ClientErrorSender = (label, message, stack, componentStack) =>
  AppService.ReportClientError(label, message, stack, componentStack);

/** What of a thrown value is safe to send: an Error's message and stack, a string, or only its type. */
function describeError(error: unknown): { message: string; stack: string } {
  try {
    if (error instanceof Error) {
      return { message: String(error.message), stack: typeof error.stack === "string" ? error.stack : "" };
    }
    // Not String(error) for an object: that would run its own toString on data the window holds.
    if (typeof error === "object" && error !== null) return { message: Object.prototype.toString.call(error), stack: "" };
    return { message: String(error), stack: "" };
  } catch {
    return { message: "[unprintable error]", stack: "" };
  }
}

/**
 * A reporter with its own page-load state: each distinct message+stack is sent
 * once, and no more than MAX_CLIENT_ERROR_REPORTS in all.
 */
export function createClientErrorReporter(send: ClientErrorSender): ClientErrorReporter {
  const seen = new Set<string>();
  let sent = 0;
  return (label, error, componentStack) => {
    try {
      if (sent >= MAX_CLIENT_ERROR_REPORTS) return;
      const { message, stack } = describeError(error);
      const key = `${message}\n${stack}`;
      if (seen.has(key)) return;
      seen.add(key);
      sent += 1;
      const result = send(
        label,
        message.slice(0, MAX_MESSAGE),
        stack.slice(0, MAX_STACK),
        (componentStack ?? "").slice(0, MAX_COMPONENT_STACK),
      );
      // A rejection here is a bridge that is down; nobody is left to tell.
      void Promise.resolve(result).catch(() => {});
    } catch {
      // Same: reporting must never be the thing that throws.
    }
  };
}

/** The reporter the app uses: it goes through AppService.ReportClientError. */
export const reportClientError: ClientErrorReporter = createClientErrorReporter(sendToGo);

/** Browser notifications that are not failures, and a cross-origin error with no detail to record. */
function isNoise(message: string, hasError: boolean): boolean {
  const text = message.trim().replace(/^Uncaught\s+/, "");
  if (text.startsWith("ResizeObserver loop")) return true;
  return !hasError && /^Script error\.?$/.test(text);
}

const installed = new WeakSet<object>();

/**
 * Reports the window's own `error` and `unhandledrejection` events. It only
 * listens: nothing is cancelled and the page behaves as before. Once per
 * window, so a second call (a hot reload) adds no second listener.
 */
export function installWindowErrorReporting(target: Window, report: ClientErrorReporter = reportClientError): void {
  if (installed.has(target)) return;
  installed.add(target);

  target.addEventListener("error", (event) => {
    try {
      const { error, message, filename, lineno, colno } = event as ErrorEvent;
      const text = typeof message === "string" ? message : "";
      if (isNoise(text, error != null)) return;
      if (error != null) {
        report("window.error", error);
        return;
      }
      const where = filename ? ` (${filename}:${lineno ?? 0}:${colno ?? 0})` : "";
      report("window.error", `${text}${where}`);
    } catch {
      // A listener that throws would itself become a window error.
    }
  });

  target.addEventListener("unhandledrejection", (event) => {
    try {
      const reason = (event as PromiseRejectionEvent).reason as unknown;
      if (isNoise(describeError(reason).message, true)) return;
      report("window.unhandledrejection", reason);
    } catch {
      // As above.
    }
  });
}
