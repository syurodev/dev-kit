import * as React from "react";
import { Check, Copy, Terminal } from "lucide-react";
import { copyText } from "@/bridge/clipboard";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { commandDisplay, type CommandDisplay, type RunCommandResult } from "./parse";

export type CommandResultViewProps = {
  result: RunCommandResult;
  /** commandDisplay(result.output, all). */
  display: CommandDisplay;
  /** Every line rather than the first SHOWN_LINES. */
  all: boolean;
  /** Copy just succeeded: the action says so for a moment. */
  copied: boolean;
  onToggleAll(): void;
  /** Called with the raw output — escapes, carriage returns and every line. */
  onCopy(output: string): void;
};

/**
 * `ssh.runCommand` — the command and what it printed, the way a terminal left
 * it. No status: the tool returns the output only, so whether the command
 * succeeded is not something the card knows. Stateless; CommandResultCard
 * holds the state.
 */
export function CommandResultView({ result, display, all, copied, onToggleAll, onCopy }: CommandResultViewProps) {
  const { count } = display;
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Terminal}
        title="Command"
        meta={`${count} ${count === 1 ? "line" : "lines"}`}
        action={
          <FramedCardAction opens={false} onClick={() => onCopy(result.output)}>
            {copied ? <Check className="text-status-success" aria-hidden="true" /> : <Copy aria-hidden="true" />}
            {copied ? "Copied" : "Copy"}
          </FramedCardAction>
        }
      />
      <FramedCardPreview>
        {result.command ? (
          <div className="min-w-0 text-code-12 whitespace-pre-wrap break-all text-text-primary select-text">
            <span className="text-text-tertiary select-none">$ </span>
            {result.command}
          </div>
        ) : null}
        {count === 0 ? (
          <span className="text-caption-11 text-text-tertiary">No output</span>
        ) : (
          <pre className="max-h-120 overflow-y-auto whitespace-pre-wrap break-all text-code-12 text-text-secondary select-text">
            {display.shown}
          </pre>
        )}
        {display.hidden > 0 || all ? (
          <button
            type="button"
            onClick={onToggleAll}
            className="-mx-(--space-micro) self-start rounded-sm px-(--space-micro) text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none"
          >
            {all ? "Show less" : `Show all · ${display.hidden} more ${display.hidden === 1 ? "line" : "lines"}`}
          </button>
        ) : null}
      </FramedCardPreview>
    </FramedCard>
  );
}

/** CommandResultView with its state: Show all, and Copy that flashes "Copied". */
export function CommandResultCard({ result }: { result: RunCommandResult }) {
  const [all, setAll] = React.useState(false);
  const [copied, setCopied] = React.useState(false);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  React.useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );
  const onCopy = React.useCallback(async (output: string) => {
    try {
      await copyText(output);
      setCopied(true);
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        timer.current = null;
        setCopied(false);
      }, 1500);
    } catch {
      // No clipboard: the output is still selectable.
    }
  }, []);
  const onToggleAll = React.useCallback(() => setAll((current) => !current), []);
  // The display work (escapes, \r, line split) is per output and fold state,
  // not per "Copied" flash.
  const display = React.useMemo(() => commandDisplay(result.output, all), [result.output, all]);
  return (
    <CommandResultView
      result={result}
      display={display}
      all={all}
      copied={copied}
      onToggleAll={onToggleAll}
      onCopy={(output) => void onCopy(output)}
    />
  );
}
