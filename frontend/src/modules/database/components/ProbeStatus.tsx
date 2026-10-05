import { useEffect, useState } from "react";
import { PendingStatus } from "@/components/ui/feedback";

/** The clock owns its ticks; it never re-renders the form or the result table. */
export function ProbeStatus({ startedAt }: { startedAt: number }) {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const tick = () => setSeconds((Date.now() - startedAt) / 1000);
    tick();
    const timer = setInterval(tick, 250);
    return () => clearInterval(timer);
  }, [startedAt]);
  return (
    <PendingStatus>
      Testing connection…{" "}
      <span className="tabular-nums">{seconds.toFixed(1)}s</span>
    </PendingStatus>
  );
}
