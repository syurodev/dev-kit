import { useState } from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { VendorIcon } from "./VendorIcon";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export type AgentNewSessionProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  vendors?: string[];
  defaultVendor?: string;
  defaultCwd?: string;
  busy?: boolean;
  error?: string;
  /** Shown, never chosen — an agent spawning a child inherits both. */
  parentName?: string;
  depth?: number;
  maxDepth?: number;
  onCreate: (params: { vendor: string; cwd: string }) => void | Promise<void>;
  /** Opens the native folder picker; resolves to "" when cancelled. */
  onBrowse?: () => string | Promise<string>;
  /** A directory already open elsewhere in DevKit, offered as one click. */
  suggestedCwd?: string;
  suggestedLabel?: string;
  className?: string;
};

/**
 * Agent / New Session — Figma `946:2224`, states `946:2199` Default and
 * `946:2223` Error.
 *
 * Figma's note is the contract: "FR-1: choosing a vendor and a working
 * directory is the only way a session starts. Parent and depth are shown, never
 * chosen — an agent spawning a child inherits both, and cwd must stay inside
 * the parent's project."
 *
 * So there are exactly two inputs and one read-only inheritance row. The
 * previous implementation also offered a title field and a "switch to mock"
 * escape hatch, neither of which is in the design, and drew its own backdrop
 * with role="dialog" by hand — no focus trap, no escape key, no scroll lock.
 *
 * Error is not a separate layout: the working-directory input takes a danger
 * border and one danger-toned row appears above the actions. Everything else
 * holds still.
 */
export function AgentNewSession({
  open,
  onOpenChange,
  vendors = ["claude", "mock"],
  defaultVendor = "claude",
  defaultCwd = "",
  busy = false,
  error,
  parentName,
  depth,
  maxDepth = 3,
  onCreate,
  onBrowse,
  suggestedCwd,
  suggestedLabel = "Code Graph",
  className,
}: AgentNewSessionProps) {
  const [vendor, setVendor] = useState(defaultVendor);
  const [cwd, setCwd] = useState(defaultCwd);

  const handleBrowse = async () => {
    if (!onBrowse) return;
    const picked = await onBrowse();
    // "" means the user cancelled the picker. Leaving the field alone is the
    // whole contract — clearing it would punish backing out.
    if (picked) setCwd(picked);
  };

  const fieldClass =
    "flex w-full items-center gap-2 rounded-md border bg-surface-raised px-3 py-2 text-body-12 text-text-primary";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        data-testid="agent-new-session"
        data-state-variant={error ? "error" : "default"}
        showCloseButton={false}
        className={cn(
          // Figma 946:2199 sits on surface/glass-modal with its own blur and a
          // deep shadow, at radius/14 and 440 wide — DialogContent defaults to
          // popover at radius/12 and sm:max-w-sm.
          "w-[440px] max-w-[calc(100%-2rem)] gap-4 rounded-[14px] p-5 sm:max-w-[440px]",
          "border border-(--glass-border) bg-(--surface-glass-modal) backdrop-blur-[16px]",
          "shadow-[0px_20px_48px_-8px_rgba(0,0,0,0.5)] ring-0",
          className,
        )}
      >
        <DialogHeader className="p-0">
          <DialogTitle className="text-title-16 text-text-primary">
            New agent session
          </DialogTitle>
        </DialogHeader>

        <div className="flex w-full flex-col gap-2">
          <label
            htmlFor="new-session-vendor"
            className="text-caption-11-medium text-text-tertiary"
          >
            Vendor
          </label>
          <div className={cn(fieldClass, "border-border-subtle")}>
            <VendorIcon vendor={vendor} className="size-3.5 text-icon-secondary" />
            {/* A native select, styled to the field. The shared Select is a
                Radix popover with its own trigger chrome; Figma draws a plain
                field with a chevron, and the option list is three vendors. */}
            <select
              id="new-session-vendor"
              value={vendor}
              disabled={busy}
              onChange={(e) => setVendor(e.target.value)}
              data-testid="new-session-vendor"
              className="min-w-0 flex-1 appearance-none bg-transparent outline-none"
            >
              {vendors.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
            <ChevronDown
              className="size-3 shrink-0 text-text-tertiary"
              aria-hidden="true"
            />
          </div>
        </div>

        <div className="flex w-full flex-col gap-2">
          <label
            htmlFor="new-session-cwd"
            className="text-caption-11-medium text-text-tertiary"
          >
            Working directory
          </label>
          <div
            className={cn(
              fieldClass,
              error ? "border-(--border-danger)" : "border-border-subtle",
            )}
          >
            <input
              id="new-session-cwd"
              value={cwd}
              disabled={busy}
              onChange={(e) => setCwd(e.target.value)}
              data-testid="new-session-cwd"
              className="min-w-0 flex-1 bg-transparent outline-none"
            />
            {onBrowse ? (
              <button
                type="button"
                onClick={() => void handleBrowse()}
                data-testid="new-session-browse"
                title="Choose a folder"
                className="shrink-0 text-caption-11 text-text-tertiary hover:text-text-primary cursor-pointer"
              >
                Browse…
              </button>
            ) : null}
          </div>
        </div>

        {suggestedCwd && suggestedCwd !== cwd ? (
          // Same material as the inherited row below: this is also a value you
          // are being handed rather than typing, so it should not look like a
          // third kind of control.
          <button
            type="button"
            data-testid="new-session-suggested"
            onClick={() => setCwd(suggestedCwd)}
            className="flex w-full items-center gap-2 rounded-[6px] bg-(--state-soft) p-2 text-left hover:bg-surface-raised cursor-pointer"
          >
            <span className="shrink-0 text-caption-11-medium text-text-tertiary">
              {suggestedLabel}
            </span>
            <span className="min-w-0 flex-1 truncate text-caption-11 text-text-secondary">
              {suggestedCwd}
            </span>
            <span className="shrink-0 text-caption-11-medium text-text-tertiary">
              Use
            </span>
          </button>
        ) : null}

        {parentName ? (
          <div
            data-testid="new-session-inherited"
            className="flex w-full items-center gap-2 rounded-[6px] bg-(--state-soft) p-2"
          >
            <span className="shrink-0 text-caption-11-medium text-text-tertiary">
              Parent
            </span>
            <span className="shrink-0 text-caption-11 text-text-secondary">
              {`${parentName} · depth ${depth ?? 0} of ${maxDepth}`}
            </span>
          </div>
        ) : null}

        {error ? (
          <div
            data-testid="new-session-error"
            className="flex w-full items-start gap-2 rounded-[6px] bg-(--feedback-danger-soft) p-2"
          >
            <p className="min-w-0 flex-1 text-caption-11 text-status-danger">
              {error}
            </p>
          </div>
        ) : null}

        <div className="flex w-full items-center gap-2">
          <p className="min-w-0 flex-1 text-caption-11 text-text-tertiary">
            The agent can only reach paths inside this directory.
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            data-testid="new-session-cancel"
            onClick={() => onOpenChange(false)}
            className="h-8 border-border-subtle px-3 text-text-secondary"
          >
            Cancel
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={busy || !cwd.trim()}
            data-testid="new-session-create"
            onClick={() => void onCreate({ vendor, cwd: cwd.trim() })}
            className="h-8 px-3"
          >
            Create
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
