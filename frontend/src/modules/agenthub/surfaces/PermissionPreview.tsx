import * as React from "react";
import { Check, ChevronDown, Copy } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { previewPermissionText } from "./permissionPreviewText";

/**
 * The command or arguments a permission request would run, in a mono well.
 * Folded it is the text as given, three lines at most — the cut is CSS, never
 * an ellipsis written into the text. When there is more to read (the text is
 * long, has line breaks, or the well measures taller than its three lines) a
 * row under it offers "Show full request": the well opens in place to the whole
 * of it — JSON indented, a message's line breaks as real ones, long lines
 * wrapped, scrolling past a ceiling that follows the conversation's height — and
 * Copy puts the whole text on the clipboard. It only displays the input the
 * request already carries; nothing here edits it or changes what Allow means.
 */
export function PermissionPreview({
  text,
  message,
  testId,
  defaultOpen = false,
  className,
}: {
  text: string;
  /** The question the agent put (Codex's "Allow the … MCP server to run tool …?"), opened with the text. */
  message?: string;
  /** "permission-command" or "permission-args", the ids the card already published. */
  testId: string;
  /** Starts opened (the gallery and the markup tests); the card starts folded. */
  defaultOpen?: boolean;
  className?: string;
}) {
  const preview = React.useMemo(() => previewPermissionText(text), [text]);
  const [open, setOpen] = React.useState(defaultOpen);
  const [copied, setCopied] = React.useState(false);
  const [clipped, setClipped] = React.useState(false);
  const bodyRef = React.useRef<HTMLPreElement>(null);
  const id = React.useId();

  // A well narrower than the text (a rail) can cut what a short string does not:
  // ask the layout rather than guess from the length. Only while folded.
  React.useLayoutEffect(() => {
    const body = bodyRef.current;
    if (!body || open) return;
    const measure = () => setClipped(body.scrollHeight > body.clientHeight + 1);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(body);
    return () => observer.disconnect();
  }, [open, text]);

  const expandable = preview.long || clipped || open || Boolean(message && message.length > 200);
  const expanded = open && expandable;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(preview.copy);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // No clipboard in this webview: the open view is selectable.
    }
  };

  return (
    <div data-testid="permission-preview" data-expanded={expanded} className={cn("flex min-w-0 flex-col gap-2", className)}>
      {message ? (
        <p data-testid="permission-message" className={cn("text-body-13 text-text-secondary", !expanded && "line-clamp-3")}>
          {message}
        </p>
      ) : null}
      <div className="min-w-0 overflow-hidden rounded-md bg-surface-well">
        {/* The padding is on this wrapper, not the clamped <pre>: a line clamp on
            an element with vertical padding shows the next line in the padding. */}
        <div className="py-1.5">
          <pre
            ref={bodyRef}
            id={id}
            data-testid={testId}
            {...(expanded ? { tabIndex: 0, role: "region", "aria-label": "Full request" } : {})}
            className={cn(
              "min-w-0 px-2.5 text-code-12 whitespace-pre-wrap [overflow-wrap:anywhere] text-text-primary select-text outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-status-info",
              expanded ? "permission-preview-cap overflow-y-auto overscroll-contain" : "line-clamp-3",
            )}
          >
            {expanded ? preview.expanded : preview.collapsed}
          </pre>
        </div>
        {expandable ? (
          <div className="flex items-center justify-between gap-2 border-t border-border-subtle px-1 py-0.5">
            <Button
              type="button"
              variant="ghost"
              size="xs"
              data-testid="permission-preview-toggle"
              aria-expanded={expanded}
              aria-controls={id}
              onClick={() => setOpen((value) => !value)}
              className="text-text-secondary hover:text-text-primary"
            >
              <ChevronDown aria-hidden="true" className={cn("transition-transform", expanded && "rotate-180")} />
              {expanded ? "Show less" : "Show full request"}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              data-testid="permission-preview-copy"
              onClick={() => void copy()}
              className="text-text-secondary hover:text-text-primary"
            >
              {copied ? <Check aria-hidden="true" className="text-status-success" /> : <Copy aria-hidden="true" />}
              <span aria-live="polite">{copied ? "Copied" : "Copy"}</span>
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
