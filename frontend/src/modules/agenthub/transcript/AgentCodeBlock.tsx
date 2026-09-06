import * as React from "react";
import { Check, ChevronDown, ChevronRight, Copy, FileCode } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentCodeBlockProps = {
  /**
   * Code block within transcript or tool result.
   * Figma node: 1023:2688 (Agent / Code Block)
   * Variants: Default / Collapsed
   */
  code: string;
  path?: string;
  language?: string;
  collapsed?: boolean;
  defaultCollapsed?: boolean;
  onToggleCollapse?: () => void;
  className?: string;
};

export function AgentCodeBlock({
  code,
  path,
  language,
  collapsed: controlledCollapsed,
  defaultCollapsed = false,
  onToggleCollapse,
  className,
}: AgentCodeBlockProps) {
  const [internalCollapsed, setInternalCollapsed] = React.useState(defaultCollapsed);
  const [copied, setCopied] = React.useState(false);

  const isCollapsed = controlledCollapsed ?? internalCollapsed;

  const toggle = () => {
    if (onToggleCollapse) {
      onToggleCollapse();
    } else {
      setInternalCollapsed((prev) => !prev);
    }
  };

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Ignore clipboard write failure
    }
  };

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-full rounded-md border border-border-subtle bg-surface-base overflow-hidden",
        className,
      )}
      data-testid="agent-code-block"
      data-state={isCollapsed ? "collapsed" : "default"}
    >
      <div className="flex h-(--control-height-sm) shrink-0 items-center justify-between gap-2 border-b border-border-subtle bg-surface-raised/50 px-2.5">
        <button
          type="button"
          onClick={toggle}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left hover:opacity-80 transition-opacity"
        >
          {isCollapsed ? (
            <ChevronRight className="size-(--icon-12) text-text-tertiary shrink-0" />
          ) : (
            <ChevronDown className="size-(--icon-12) text-text-tertiary shrink-0" />
          )}
          <FileCode className="size-(--icon-12) text-text-tertiary shrink-0" />
          {path ? (
            <span className="truncate text-code-12-medium text-text-secondary">{path}</span>
          ) : (
            <span className="truncate text-code-12-medium text-text-tertiary">
              {language ?? "code"}
            </span>
          )}
          {path && language && (
            <span className="text-caption-11 text-text-tertiary shrink-0">{`(${language})`}</span>
          )}
        </button>

        <div className="flex items-center gap-1 shrink-0">
          <button
            type="button"
            onClick={handleCopy}
            title={copied ? "Copied" : "Copy code"}
            className="flex size-(--control-height-xs) items-center justify-center rounded-sm text-text-tertiary hover:bg-surface-raised hover:text-text-primary transition-colors"
          >
            {copied ? (
              <Check className="size-(--icon-12) text-status-success" />
            ) : (
              <Copy className="size-(--icon-12)" />
            )}
          </button>
        </div>
      </div>

      {!isCollapsed && (
        <pre className="overflow-x-auto p-3 text-code-12 text-text-primary select-text">
          <code>{code}</code>
        </pre>
      )}
    </div>
  );
}
