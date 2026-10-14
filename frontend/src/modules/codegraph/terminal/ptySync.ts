/**
 * Tells a PTY its size only when xterm's grid changed.
 *
 * Every fit used to send one, including the fits that changed nothing; a
 * resize is an IPC round trip and a SIGWINCH that makes a full-screen program
 * redraw. A resize the bridge refused is forgotten, so the next fit sends it
 * again rather than believing the PTY already has it.
 */
export function ptySizeSync(
  grid: () => { id: string; rows: number; cols: number } | null,
  resize: (id: string, rows: number, cols: number) => Promise<void>,
): (() => void) & { started: (rows: number, cols: number) => void } {
  let sentRows = 0;
  let sentCols = 0;
  const sync = () => {
    const g = grid();
    if (!g || (g.rows === sentRows && g.cols === sentCols)) return;
    sentRows = g.rows;
    sentCols = g.cols;
    resize(g.id, g.rows, g.cols).catch(() => {
      // Only if nothing newer was sent meanwhile.
      if (sentRows === g.rows && sentCols === g.cols) {
        sentRows = 0;
        sentCols = 0;
      }
    });
  };
  /** The size the PTY was started with: already known to it. */
  sync.started = (rows: number, cols: number) => {
    sentRows = rows;
    sentCols = cols;
  };
  return sync;
}
