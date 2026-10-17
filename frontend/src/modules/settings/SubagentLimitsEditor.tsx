import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { limitMeaning, limitText, parseLimit, SUBAGENT_LIMIT_MAX } from "./subagentLimits";

/**
 * Settings → Agents → Models & subagents → "Subagent limits". Presentational:
 * the container reads and saves through the UI-only bridge methods.
 *
 * Codex only. Its cap counts every subagent alive at once, nested ones included,
 * so one number also bounds how deep a tree can grow; the depth setting Codex
 * documents is ignored by its app-server (measured on 0.163.0), which is said
 * here rather than offering a control that does nothing.
 */
export function SubagentLimitsEditor({
  stored,
  saving,
  error,
  onSave,
}: {
  /** The saved Codex cap; null while unset. */
  stored: number | null;
  saving: boolean;
  error: string;
  onSave: (next: number | null) => Promise<boolean>;
}) {
  const [text, setText] = useState(limitText(stored));
  const [problem, setProblem] = useState("");
  const [saved, setSaved] = useState(false);

  // A save elsewhere (or the first read) moves the stored value under the field.
  useEffect(() => {
    setText(limitText(stored));
    setProblem("");
  }, [stored]);

  const parsed = parseLimit(text);
  const dirty = parsed.ok ? parsed.value !== stored : true;

  async function save() {
    const next = parseLimit(text);
    if (!next.ok) {
      setProblem(next.problem);
      return;
    }
    setProblem("");
    setSaved(false);
    if (await onSave(next.value)) setSaved(true);
  }

  return (
    <section className="flex flex-col gap-3" aria-label="Subagent limits">
      <div className="flex items-center gap-2">
        <h3 className="text-body-14-medium text-text-primary">Subagent limits</h3>
        <Badge variant="outline">Codex only</Badge>
      </div>
      <p className="text-body-12 text-text-tertiary">
        Cap how many subagents Codex may run at the same time. A subagent can start subagents of its own, and they all
        draw from this one count, so a small number also keeps the tree shallow: with 1, a subagent cannot start
        another. Every subagent is a full model session — more of them, and deeper nesting, multiply the model calls
        and the cost of a task.
      </p>
      <form
        className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <div className="flex items-end gap-3">
          <div className="w-44">
            <Input
              label="Subagents at once"
              type="number"
              inputMode="numeric"
              min={0}
              max={SUBAGENT_LIMIT_MAX}
              step={1}
              value={text}
              placeholder="Codex's default"
              error={problem || undefined}
              onChange={(e) => {
                setText(e.target.value);
                setProblem("");
                setSaved(false);
              }}
            />
          </div>
          <Button type="submit" size="lg" disabled={saving || !dirty}>{saving ? "Saving…" : "Save"}</Button>
          {stored !== null ? (
            <Button
              type="button"
              variant="ghost"
              size="lg"
              disabled={saving}
              onClick={() => {
                setSaved(false);
                void onSave(null).then((ok) => ok && setSaved(true));
              }}
            >
              Use Codex's default
            </Button>
          ) : null}
        </div>
        <p className="text-body-12 text-text-secondary">{limitMeaning(parsed.ok ? parsed.value : stored)}</p>
        <p className="text-body-12 text-text-tertiary">
          Applies to Codex sessions you start from now on. An open session keeps the limit it started with until you open
          its Environment and choose Reload session. Leave it empty and DevKit passes Codex nothing.
        </p>
        {saved ? <p className="text-body-12 text-text-secondary" role="status">Saved.</p> : null}
        {error ? <p className="text-body-12 text-status-danger" role="alert">{error}</p> : null}
      </form>
      <p className="text-body-12 text-text-tertiary">
        Codex also documents a nesting-depth setting (<code>agents.max_depth</code>); its agent server ignores it in
        version 0.163, so DevKit does not offer one. The count above is what bounds nesting.
      </p>
      <p className="text-body-12 text-text-tertiary">
        Claude: a subagent cannot start another subagent, and DevKit sets no limit on how many a session runs.
      </p>
    </section>
  );
}
