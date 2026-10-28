import * as React from "react";
import { useEffect, useRef, useState } from "react";

import { ErrorBoundary } from "@/components/ui/error-boundary";
import { parseDraft } from "../../composerDraft";

/** How long a fresh editor has to survive before a later throw counts as a new problem, not the same one. */
export const COMPOSER_EDITOR_HEALTHY_AFTER_MS = 2000;

/**
 * Marks the editor sound once it has stayed mounted for `afterMs`. A timer, not
 * "it mounted": an error thrown from one of the editor's own effects arrives
 * after this component's effect has run, and counting that as healthy would send
 * a persistent error round the retry for ever. When the boundary tears the
 * editor down the cleanup clears the timer.
 */
function EditorHealthy({ afterMs, onHealthy }: { afterMs: number; onHealthy: () => void }) {
  const latest = useRef(onHealthy);
  latest.current = onHealthy;
  useEffect(() => {
    const timer = setTimeout(() => latest.current(), afterMs);
    return () => clearTimeout(timer);
  }, [afterMs]);
  return null;
}

type ComposerEditorBoundaryProps = {
  /**
   * Which editor the children hold. The owner builds a new editor when this
   * changes (`usePlateEditor(options, [epoch])`) and the boundary gives the
   * children another go on the same change.
   */
  epoch: number;
  /** The draft, as the owner holds it. A fresh editor is built from this, never from the failed one's state. */
  draft: string;
  /** Asks the owner for a fresh editor: it raises `epoch`. */
  onRemount: () => void;
  /** Clears the draft and asks for a fresh editor. */
  onResetPrompt: () => void;
  /** Overridable for tests. */
  healthyAfterMs?: number;
  children?: React.ReactNode;
};

/**
 * Keeps a render error inside the prompt editor (the Plate/Slate field, whose
 * DOM the browser and an input method edit as well) to the field. The first
 * throw remounts a fresh editor built from the draft the owner holds, so the
 * text survives and the rest of the window never notices. A throw that follows
 * straight after — a persistent error — stops retrying: the field becomes a
 * plain read-only message with the draft's text and a "Reset prompt" button,
 * so the person is never stuck in a loop and never loses the text without
 * asking to.
 */
export function ComposerEditorBoundary({
  epoch,
  draft,
  onRemount,
  onResetPrompt,
  healthyAfterMs = COMPOSER_EDITOR_HEALTHY_AFTER_MS,
  children,
}: ComposerEditorBoundaryProps) {
  const [persistent, setPersistent] = useState(false);
  // An error since the editor was last known sound. The first one retries.
  const retried = useRef(false);

  function handleError() {
    if (retried.current) {
      setPersistent(true);
      return;
    }
    retried.current = true;
    onRemount();
  }

  function resetPrompt() {
    retried.current = false;
    setPersistent(false);
    onResetPrompt();
  }

  return (
    <ErrorBoundary
      label="agent prompt editor"
      resetKey={epoch}
      onError={handleError}
      fallback={persistent ? <PersistentFailure draft={draft} onResetPrompt={resetPrompt} /> : null}
    >
      {children}
      <EditorHealthy afterMs={healthyAfterMs} onHealthy={() => (retried.current = false)} />
    </ErrorBoundary>
  );
}

function PersistentFailure({ draft, onResetPrompt }: { draft: string; onResetPrompt: () => void }) {
  const text = parseDraft(draft).text;
  return (
    <div role="alert" data-testid="composer-editor-failed" className="flex min-w-0 flex-col items-start gap-1.5 px-2 pt-1 text-body-14">
      <p className="text-text-secondary">The prompt editor stopped working.</p>
      {text ? (
        <p
          data-testid="composer-editor-failed-draft"
          className="max-h-24 w-full overflow-y-auto whitespace-pre-wrap break-words text-text-primary select-text"
        >
          {text}
        </p>
      ) : null}
      <button
        type="button"
        onClick={onResetPrompt}
        className="wails-no-drag h-(--control-height-sm) cursor-pointer rounded-md border border-border bg-transparent px-3 text-label-12 text-text-primary outline-none hover:bg-state-soft focus-visible:border-border-focus"
      >
        Reset prompt
      </button>
    </div>
  );
}
