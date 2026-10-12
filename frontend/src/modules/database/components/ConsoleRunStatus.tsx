import { useEffect, useState } from "react";
import type { ConsoleStatus } from "../consoleStatus";
import { formatDuration } from "../consoleStatus";

export function ConsoleRunStatus({
  outcome,
  running,
  startedAt,
}: {
  outcome: ConsoleStatus;
  running: boolean;
  startedAt: number | null;
}) {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!running || !startedAt) return;
    const tick = () => setElapsed(Date.now() - startedAt);
    tick();
    const timer = setInterval(tick, 100);
    return () => clearInterval(timer);
  }, [running, startedAt]);
  return (
    <span
      role="status"
      className="min-w-0 truncate text-code-11 text-text-secondary"
      title={outcome.kind === "error" ? outcome.detail : undefined}
    >
      {running
        ? `Running · ${formatDuration(elapsed)}`
        : outcome.kind === "idle"
          ? "Ready"
          : outcome.kind === "running"
            ? outcome.time
            : `${outcome.message} · ${outcome.time}`}
    </span>
  );
}
