import { useRef, useState } from "react";
import { Pencil } from "lucide-react";
import type { Task } from "@/bridge/tasks";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import { Textarea } from "@/components/ui/textarea";
import { saveText } from "../taskActions";

/**
 * The title, edited where it is read. The draft is this component's alone —
 * a keystroke here renders this input and nothing else (frontend-performance
 * §1) — and the field shows the task's own title whenever it is not being
 * edited, so a rename that arrives from an agent lands without an effect
 * copying it in.
 */
export function TaskTitle({ task }: { task: Task }) {
  const [draft, setDraft] = useState<string | null>(null);
  // Escape has to win over the blur it causes: the blur handler still sees
  // the draft in its closure, so the cancel is recorded where it can read it.
  const cancelled = useRef(false);
  const editing = draft !== null;

  const commit = () => {
    if (draft === null) return;
    const title = draft.trim();
    setDraft(null);
    if (cancelled.current) {
      cancelled.current = false;
      return;
    }
    if (title && title !== task.title) void saveText(task, title, task.description ?? "");
  };

  return (
    <input
      value={editing ? draft : task.title}
      onFocus={() => setDraft(task.title)}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          event.currentTarget.blur();
        } else if (event.key === "Escape") {
          cancelled.current = true;
          event.currentTarget.blur();
        }
      }}
      aria-label="Task title"
      maxLength={200}
      className="-mx-1.5 w-full rounded-md border border-transparent bg-transparent px-1.5 py-0.5 text-title-16 text-text-primary transition-colors duration-(--motion-fast) outline-none hover:border-border-subtle focus:border-border-focus"
    />
  );
}

/**
 * The description: read as text, edited in a textarea. Kept to plain text on
 * purpose — the markdown renderer lives in Agent Hub's chunk, and pulling it
 * into this one to draw a paragraph is a cost every task tab would pay.
 */
export function TaskDescription({ task }: { task: Task }) {
  const [draft, setDraft] = useState<string | null>(null);
  const description = task.description ?? "";

  const save = () => {
    if (draft === null) return;
    const next = draft.replace(/\s+$/, "");
    setDraft(null);
    if (next !== description) void saveText(task, task.title, next);
  };

  if (draft !== null) {
    return (
      <div className="flex flex-col gap-2">
        <Textarea
          autoFocus
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              save();
            } else if (event.key === "Escape") {
              event.preventDefault();
              setDraft(null);
            }
          }}
          aria-label="Description"
          placeholder="What is this task about? What does done look like?"
          className="min-h-32 text-body-13"
        />
        <div className="flex items-center gap-2">
          <Button size="sm" onClick={save}>
            Save
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
            Cancel
          </Button>
          <span className="ml-auto flex items-center gap-1 text-caption-11 text-text-tertiary">
            <Kbd>⌘↵</Kbd> save · <Kbd>esc</Kbd> cancel
          </span>
        </div>
      </div>
    );
  }

  if (!description) {
    return (
      <button
        type="button"
        onClick={() => setDraft("")}
        className="flex h-(--control-height-md) w-full items-center gap-2 rounded-md border border-dashed border-border-subtle px-3 text-body-13 text-text-tertiary transition-colors duration-(--motion-fast) hover:border-status-info hover:text-text-secondary"
      >
        <Pencil className="size-(--icon-14)" aria-hidden />
        Add a description
      </button>
    );
  }

  return (
    <div className="group/description relative">
      <p className="text-body-13 whitespace-pre-wrap text-text-primary">{description}</p>
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label="Edit description"
        onClick={() => setDraft(description)}
        className="absolute top-0 right-0 opacity-0 transition-opacity duration-(--motion-fast) group-hover/description:opacity-100 focus-visible:opacity-100"
      >
        <Pencil aria-hidden />
      </Button>
    </div>
  );
}
