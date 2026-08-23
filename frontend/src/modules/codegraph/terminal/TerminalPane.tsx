import { useEffect, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import {
  closeTerminal,
  onTerminalOutput,
  resizeTerminal,
  startCodeGraphTerminal,
  writeTerminal,
} from "@/bridge/terminal";
import { errorMessage } from "@/bridge/errors";

/**
 * A shell rooted at the open project, in the bottom slot.
 *
 * The whole lifecycle lives in one effect with an empty dep array on purpose:
 * the xterm instance, the PTY session and the output subscription have exactly
 * the same lifetime. Splitting them across effects is how you end up with two
 * shells, or a write landing after dispose. `disposed` guards the async gap
 * between mount and the session id coming back — without it, unmounting during
 * startup leaks a PTY nobody is reading.
 */
export function TerminalPane() {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let sessionId = "";
    let offOutput: (() => void) | undefined;

    const term = new Terminal({
      fontSize: 12,
      fontFamily: "var(--font-mono), monospace",
      convertEol: true,
      cursorBlink: true,
      theme: { background: "#00000000" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    void (async () => {
      try {
        const id = await startCodeGraphTerminal(term.rows, term.cols);
        if (disposed) {
          void closeTerminal(id);
          return;
        }
        sessionId = id;
        offOutput = onTerminalOutput(id, (chunk) => term.write(chunk));
        term.onData((data) => void writeTerminal(id, data));
      } catch (e) {
        if (!disposed) setError(errorMessage(e));
      }
    })();

    const observer = new ResizeObserver(() => {
      fit.fit();
      if (sessionId) void resizeTerminal(sessionId, term.rows, term.cols);
    });
    observer.observe(host);

    return () => {
      disposed = true;
      observer.disconnect();
      offOutput?.();
      if (sessionId) void closeTerminal(sessionId);
      // Last: nothing may write into a disposed terminal.
      term.dispose();
    };
  }, []);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {error && <p className="shrink-0 px-3 py-1.5 text-xs text-destructive">{error}</p>}
      <div ref={hostRef} className="min-h-0 flex-1 overflow-hidden p-2" />
    </div>
  );
}
