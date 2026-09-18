import { useCallback, useEffect, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import {
  attachTerminal,
  closeTerminal,
  resizeTerminal,
  startCodeGraphTerminal,
  writeTerminal,
  type TerminalStart,
} from "@/bridge/terminal";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";
import { monoFontStack, useAppearanceStore } from "@/stores/appearance";
import { terminalTheme } from "@/theme/terminalTheme";
import { onThemeChange } from "@/theme/themeColors";

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
// Font prefs come from Settings > Appearance. monoFontStack returns a literal
// font list — `var(--font-mono)` cannot be handed to xterm: it applies the
// family to a DOM node (where var() resolves) but MEASURES cell width through
// a canvas, where var() does not resolve at all. The result is glyphs from one
// font sitting in cells sized for another.

export function TerminalPane({
  start = startCodeGraphTerminal,
  onExit,
}: {
  /** What this tab runs; a shell at the open project by default. */
  start?: TerminalStart;
  /** The process's exit code, once — -1 when the tab closed before it exited. */
  onExit?: (code: number) => void;
} = {}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [error, setError] = useState("");
  // Bumping this re-runs the whole effect, which is the only correct way to
  // retry: a failed start leaves no session, no subscription and a terminal
  // that must be rebuilt, not patched.
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  // The effect is keyed on [attempt] only — re-running it would kill the
  // shell — so it reads the callbacks through refs. Captured directly, onExit
  // would be whichever function the FIRST render passed, and a parent that
  // re-rendered with a new handler (new tab id, new closure over state) would
  // never hear the exit.
  const onExitRef = useRef(onExit);
  const startRef = useRef(start);
  useEffect(() => {
    onExitRef.current = onExit;
    startRef.current = start;
  });

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let exited = false;
    let sessionId = "";
    let detach: (() => void) | undefined;

    setError("");
    const prefs = useAppearanceStore.getState();
    const term = new Terminal({
      fontSize: prefs.terminalSize,
      fontFamily: monoFontStack(prefs.terminalFont),
      convertEol: true,
      cursorBlink: true,
      // The bottom slot is glass now, so the terminal must not paint its own
      // background — the pane shows the panel's tint and the vibrancy behind
      // it. allowTransparency is the switch that makes xterm honour an alpha
      // background; without it the renderer clamps alpha to 1 and the pane
      // reads as an opaque hole cut into the glass (the bug this replaces).
      allowTransparency: true,
      theme: { background: "rgba(0, 0, 0, 0)", ...(terminalTheme() ?? {}) },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    // xterm sizes its cells ONCE, from whatever font was available at that
    // moment. Geist Mono arrives as a webfont, so the first measurement can
    // land on the fallback and every column stays wrong afterwards. Re-measure
    // when the font is really there — assigning a different value first, since
    // xterm ignores a write that does not change the option.
    void document.fonts.ready.then(() => {
      if (disposed) return;
      term.options.fontFamily = "monospace";
      term.options.fontFamily = monoFontStack(useAppearanceStore.getState().terminalFont);
      fit.fit();
      if (sessionId) void resizeTerminal(sessionId, term.rows, term.cols);
    });

    // A canvas cannot read a CSS variable, so unlike every other surface the
    // terminal has to be told when the palette moves. Watching the attributes
    // rather than subscribing to React state keeps this inside the effect that
    // already owns the terminal's lifetime, and it catches an OS-driven mode
    // flip too, which arrives as an attribute change and nothing else.
    const offTheme = onThemeChange(() => {
      const palette = terminalTheme();
      if (palette) {
        term.options.theme = { background: "rgba(0, 0, 0, 0)", ...palette };
      }
    });

    // Live font/size changes from Settings > Appearance. Same lifetime as the
    // terminal itself, so the subscription lives in this effect too.
    const offAppearance = useAppearanceStore.subscribe((s) => {
      term.options.fontSize = s.terminalSize;
      term.options.fontFamily = monoFontStack(s.terminalFont);
      fit.fit();
      if (sessionId) void resizeTerminal(sessionId, term.rows, term.cols);
    });

    void (async () => {
      try {
        const attached = await attachTerminal(
          () => startRef.current(term.rows, term.cols),
          (chunk) => {
            if (!disposed) term.write(chunk);
          },
          (code) => {
            exited = true;
            if (disposed) return;
            // Said in the pane as well: otherwise a finished command's tab just
            // stops, and looks the same as one still waiting for input.
            term.write(`\r\n[Process exited with code ${code}]\r\n`);
            onExitRef.current?.(code);
          },
        );
        const id = attached.sessionId;
        if (disposed) {
          attached.detach();
          void closeTerminal(id).catch(() => {});
          return;
        }
        sessionId = id;
        detach = attached.detach;
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
      offAppearance();
      offTheme();
      detach?.();
      if (sessionId) {
        // Already gone when the process exited on its own; nothing to close.
        void closeTerminal(sessionId).catch(() => {});
        if (!exited) onExitRef.current?.(-1);
      }
      // Last: nothing may write into a disposed terminal.
      term.dispose();
    };
  }, [attempt]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {error && (
        <div className="flex shrink-0 items-center gap-2 px-3 py-1.5">
          <p className="min-w-0 flex-1 truncate text-body-12 text-status-danger">{error}</p>
          <Button size="sm" variant="secondary" className="h-6 shrink-0 px-2 text-label-12" onClick={retry}>
            Retry
          </Button>
        </div>
      )}
      <div ref={hostRef} className="terminal-glass min-h-0 flex-1 overflow-hidden p-2" />
    </div>
  );
}
