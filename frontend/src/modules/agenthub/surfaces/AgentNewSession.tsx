import { useState, type FormEvent } from "react";
import { AlertTriangle, Bot, Folder, Plus } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentNewSessionProps = {
  vendors?: string[];
  defaultVendor?: string;
  defaultCwd?: string;
  busy?: boolean;
  error?: string;
  onCreate: (params: { vendor: string; cwd: string; title?: string }) => void | Promise<void>;
  onCancel?: () => void;
  onSwitchToMock?: () => void;
  className?: string;
};

/**
 * Agent / New Session (Figma 440×304–352)
 * Setup card/dialog for configuring and launching a new agent session.
 */
export function AgentNewSession({
  vendors = ["mock", "claude"],
  defaultVendor = "mock",
  defaultCwd = ".",
  busy = false,
  error = "",
  onCreate,
  onCancel,
  onSwitchToMock,
  className,
}: AgentNewSessionProps) {
  const [vendor, setVendor] = useState(defaultVendor);
  const [cwd, setCwd] = useState(defaultCwd);
  const [title, setTitle] = useState("");

  const isClaudeSandboxIssue =
    vendor === "claude" &&
    (error.toLowerCase().includes("sandbox") || error.toLowerCase().includes("operation not permitted"));

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    onCreate({
      vendor,
      cwd: cwd.trim() || ".",
      title: title.trim() || undefined,
    });
  }

  return (
    <div
      role="region"
      aria-label="New Agent Session"
      data-testid="agent-new-session"
      className={cn(
        "flex w-[440px] flex-col rounded-xl border border-border-subtle bg-surface-base p-5 shadow-lg",
        className,
      )}
    >
      {/* Header */}
      <div className="flex items-center gap-2 pb-3 border-b border-border-subtle">
        <div className="flex size-7 items-center justify-center rounded-control bg-surface-tinted text-text-primary">
          <Bot className="size-4 shrink-0" aria-hidden="true" />
        </div>
        <div>
          <h3 className="text-title-16 text-text-primary">New Agent Session</h3>
          <p className="text-caption-11 text-text-tertiary">
            Spawn an autonomous agent inside your workspace jail
          </p>
        </div>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-3.5 pt-4">
        {/* Vendor Selection */}
        <div className="flex flex-col gap-1.5">
          <label htmlFor="agent-vendor-select" className="text-label-12 text-text-secondary">
            Vendor Adapter
          </label>
          <div className="relative">
            <select
              id="agent-vendor-select"
              data-testid="new-session-vendor"
              value={vendor}
              disabled={busy}
              onChange={(e) => setVendor(e.target.value)}
              className="h-8 w-full rounded-control border border-border-subtle bg-surface-raised px-2.5 text-body-13 text-text-primary outline-none focus:border-action-primary cursor-pointer disabled:opacity-50"
            >
              {vendors.map((v) => (
                <option key={v} value={v}>
                  {v.charAt(0).toUpperCase() + v.slice(1)} {v === "mock" ? "(Deterministic test)" : "(ACP v1)"}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Working Directory */}
        <div className="flex flex-col gap-1.5">
          <label htmlFor="agent-cwd-input" className="text-label-12 text-text-secondary flex items-center gap-1">
            <Folder className="size-3 text-text-tertiary shrink-0" aria-hidden="true" />
            <span>Working Directory</span>
          </label>
          <input
            id="agent-cwd-input"
            type="text"
            data-testid="new-session-cwd"
            value={cwd}
            disabled={busy}
            onChange={(e) => setCwd(e.target.value)}
            placeholder="."
            className="h-8 w-full rounded-control border border-border-subtle bg-surface-raised px-2.5 text-code-12 text-text-primary outline-none focus:border-action-primary disabled:opacity-50"
          />
        </div>

        {/* Optional Title */}
        <div className="flex flex-col gap-1.5">
          <label htmlFor="agent-title-input" className="text-label-12 text-text-secondary">
            Session Title (Optional)
          </label>
          <input
            id="agent-title-input"
            type="text"
            data-testid="new-session-title"
            value={title}
            disabled={busy}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="e.g. Implement database migrations"
            className="h-8 w-full rounded-control border border-border-subtle bg-surface-raised px-2.5 text-body-13 text-text-primary outline-none focus:border-action-primary disabled:opacity-50"
          />
        </div>

        {/* Error Banner */}
        {error ? (
          <div
            role="alert"
            data-testid="new-session-error"
            className="flex flex-col gap-2 rounded-control border border-status-danger/30 bg-surface-tinted p-2.5"
          >
            <div className="flex items-start gap-1.5">
              <AlertTriangle className="size-3.5 text-status-danger shrink-0 mt-0.5" aria-hidden="true" />
              <p className="text-caption-11 text-status-danger">{error}</p>
            </div>
            {isClaudeSandboxIssue || onSwitchToMock ? (
              <button
                type="button"
                data-testid="new-session-fallback-mock"
                onClick={() => {
                  setVendor("mock");
                  onSwitchToMock?.();
                }}
                className="self-start rounded-sm border border-border-subtle bg-surface-base px-2 py-0.5 text-caption-11 text-text-primary hover:bg-surface-tinted cursor-pointer"
              >
                Switch to mock vendor
              </button>
            ) : null}
          </div>
        ) : null}

        {/* Action Buttons */}
        <div className="flex items-center justify-end gap-2 pt-2 border-t border-border-subtle">
          {onCancel ? (
            <button
              type="button"
              data-testid="new-session-cancel"
              disabled={busy}
              onClick={onCancel}
              className="h-8 rounded-control border border-border-subtle bg-surface-raised px-3 text-label-12 text-text-secondary transition-colors hover:text-text-primary hover:bg-surface-tinted cursor-pointer disabled:opacity-50"
            >
              Cancel
            </button>
          ) : null}

          <button
            type="submit"
            data-testid="new-session-submit"
            disabled={busy}
            className="inline-flex h-8 items-center gap-1.5 rounded-control bg-action-primary px-3 text-label-12 text-action-primary-foreground transition-colors hover:opacity-90 cursor-pointer disabled:opacity-50"
          >
            <Plus className="size-3.5 shrink-0" aria-hidden="true" />
            <span>{busy ? "Launching..." : "Create Session"}</span>
          </button>
        </div>
      </form>
    </div>
  );
}
