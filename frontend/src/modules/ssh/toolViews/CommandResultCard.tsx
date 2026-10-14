import * as React from "react";
import { Check, Copy, Terminal } from "lucide-react";
import { copyText } from "@/bridge/clipboard";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { clipLines, lineCount, SHOWN_LINES, type RunCommandResult } from "./parse";

/**
 * `ssh.runCommand` — the command and what it printed, the way a terminal shows
 * it. No status: the tool returns the output only, so whether the command
 * succeeded is not something the card knows.
 */
export function CommandResultCard({ result }: { result: RunCommandResult }) {
  const [all, setAll] = React.useState(false);
  const [copied, setCopied] = React.useState(false);
  const count = lineCount(result.output);
  const clip = clipLines(result.output, all ? Number.POSITIVE_INFINITY : SHOWN_LINES);

  const copy = async () => {
    try {
      await copyText(result.output);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // No clipboard: the output is still selectable.
    }
  };

  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Terminal}
        title="Command"
        meta={`${count} ${count === 1 ? "line" : "lines"}`}
        action={
          <FramedCardAction opens={false} onClick={() => void copy()}>
            {copied ? <Check className="text-status-success" aria-hidden="true" /> : <Copy aria-hidden="true" />}
            {copied ? "Copied" : "Copy"}
          </FramedCardAction>
        }
      />
      <FramedCardPreview>
        {result.command ? (
          <div className="min-w-0 text-code-12 break-all text-text-primary select-text">
            <span className="text-text-tertiary select-none">$ </span>
            {result.command}
          </div>
        ) : null}
        {count === 0 ? (
          <span className="text-caption-11 text-text-tertiary">No output</span>
        ) : (
          <pre className="max-h-120 overflow-y-auto whitespace-pre-wrap break-all text-code-12 text-text-secondary select-text">
            {clip.shown}
          </pre>
        )}
        {clip.hidden > 0 || all ? (
          <button
            type="button"
            onClick={() => setAll((current) => !current)}
            className="-mx-(--space-micro) self-start rounded-sm px-(--space-micro) text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
          >
            {all ? "Show less" : `Show all · ${clip.hidden} more ${clip.hidden === 1 ? "line" : "lines"}`}
          </button>
        ) : null}
      </FramedCardPreview>
    </FramedCard>
  );
}
