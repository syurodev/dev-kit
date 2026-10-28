import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { IDLE_MAX_MINUTES, IDLE_MIN_MINUTES, minutesText, parseMinutes } from "./idleRelease";

export type IdleReleaseValue = { enabled: boolean; childMinutes: number; topMinutes: number };

/**
 * Settings → Agents → Models & subagents → "Idle sessions". Presentational: the
 * container reads and saves through the UI-only bridge methods.
 *
 * Every open agent session holds a process (and whatever the agent started inside
 * it), which is most of what DevKit uses in memory. A session that has been idle
 * for the chosen time gives that back; it stays in the rail and reopens when you
 * send the next prompt.
 */
export function IdleReleaseEditor({
  stored,
  saving,
  error,
  onSave,
}: {
  stored: IdleReleaseValue | null;
  saving: boolean;
  error: string;
  onSave: (next: IdleReleaseValue) => Promise<boolean>;
}) {
  const [enabled, setEnabled] = useState(stored?.enabled ?? true);
  const [child, setChild] = useState(String(stored?.childMinutes ?? 10));
  const [top, setTop] = useState(String(stored?.topMinutes ?? 30));
  const [problem, setProblem] = useState({ child: "", top: "" });
  const [saved, setSaved] = useState(false);

  // A save elsewhere (or the first read) moves the stored value under the fields.
  useEffect(() => {
    if (!stored) return;
    setEnabled(stored.enabled);
    setChild(String(stored.childMinutes));
    setTop(String(stored.topMinutes));
    setProblem({ child: "", top: "" });
  }, [stored]);

  const parsedChild = parseMinutes(child);
  const parsedTop = parseMinutes(top);
  const dirty =
    stored === null ||
    enabled !== stored.enabled ||
    !parsedChild.ok ||
    !parsedTop.ok ||
    parsedChild.value !== stored.childMinutes ||
    parsedTop.value !== stored.topMinutes;

  async function save() {
    if (!parsedChild.ok || !parsedTop.ok) {
      setProblem({ child: parsedChild.ok ? "" : parsedChild.problem, top: parsedTop.ok ? "" : parsedTop.problem });
      return;
    }
    setProblem({ child: "", top: "" });
    setSaved(false);
    if (await onSave({ enabled, childMinutes: parsedChild.value, topMinutes: parsedTop.value })) setSaved(true);
  }

  return (
    <section className="flex flex-col gap-3" aria-label="Idle sessions">
      <h3 className="text-body-14-medium text-text-primary">Idle sessions</h3>
      <p className="text-body-12 text-text-tertiary">
        Each open agent session keeps a process running, and so does everything the agent started in it (build
        daemons, language servers). Sessions started by another session usually finish one task and are not used again.
        An idle session gives that memory back after the time below. It stays in the list and its conversation is
        untouched; it reopens, in a few seconds, when you or another agent sends it something.
      </p>
      <form
        className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <label className="flex items-center gap-2.5">
          <span className="min-w-0 flex-1 text-body-13 text-text-primary">Release the memory of idle sessions</span>
          <Switch
            aria-label="Release the memory of idle sessions"
            checked={enabled}
            disabled={saving}
            onCheckedChange={(on) => {
              setEnabled(on);
              setSaved(false);
            }}
          />
        </label>
        <div className="flex items-start gap-3">
          <div className="w-52">
            <Input
              label="Sessions started by another session (minutes)"
              type="number"
              inputMode="numeric"
              min={IDLE_MIN_MINUTES}
              max={IDLE_MAX_MINUTES}
              step={1}
              value={child}
              disabled={!enabled}
              error={problem.child || undefined}
              onChange={(e) => {
                setChild(e.target.value);
                setProblem((p) => ({ ...p, child: "" }));
                setSaved(false);
              }}
            />
          </div>
          <div className="w-52">
            <Input
              label="Your own sessions (minutes)"
              type="number"
              inputMode="numeric"
              min={IDLE_MIN_MINUTES}
              max={IDLE_MAX_MINUTES}
              step={1}
              value={top}
              disabled={!enabled}
              error={problem.top || undefined}
              onChange={(e) => {
                setTop(e.target.value);
                setProblem((p) => ({ ...p, top: "" }));
                setSaved(false);
              }}
            />
          </div>
        </div>
        <p className="text-body-12 text-text-secondary">
          {enabled
            ? `Sub-sessions after ${parsedChild.ok ? minutesText(parsedChild.value) : "…"} idle, your own after ${
                parsedTop.ok ? minutesText(parsedTop.value) : "…"
              }.`
            : "Off: every session keeps its process until it is closed."}
        </p>
        <p className="text-body-12 text-text-tertiary">
          A session is never released while it is working, waiting for you, or has a sub-session that is still working,
          or a message on its way to it. Anything an agent was running in it stops; its files and conversation stay.
        </p>
        <div>
          <Button type="submit" size="lg" disabled={saving || !dirty}>{saving ? "Saving…" : "Save"}</Button>
        </div>
        {saved ? <p className="text-body-12 text-text-secondary" role="status">Saved.</p> : null}
        {error ? <p className="text-body-12 text-status-danger" role="alert">{error}</p> : null}
      </form>
    </section>
  );
}
