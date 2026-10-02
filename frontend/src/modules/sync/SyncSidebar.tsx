import { Clock, FileText, KeyRound, Laptop, Server, Trash2 } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { cn } from "@/lib/utils";
import { SIDEBAR_ROW_HEIGHT, SidebarSection } from "@/components/ui/sidebar-shell";
import type { SyncConflict, SyncStatus } from "../../bridge/sync";

/** "note", "secret", anything else → the glyph Figma's rows use (783:252). */
function glyphFor(conflict: SyncConflict) {
  if (conflict.theirsDeleted) return Trash2;
  if (conflict.recordType === "secret") return KeyRound;
  if (conflict.recordType === "note") return FileText;
  return Clock;
}

/** The kind chip: how this one gets resolved. */
export function conflictKind(conflict: SyncConflict): string {
  if (conflict.theirsDeleted) return "delete";
  if (conflict.recordType === "secret") return "secret";
  return "merge";
}

function hostOf(endpoint: string): string {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint;
  }
}

/** "09:12" from an ISO timestamp; the row is a worklist, the date is noise. */
export function clockOf(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso || "—"
    : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// Two-line rows (a title over its meta), so the `card` tier.
const ROW = `flex w-full items-center gap-2 rounded-md border px-3 text-left ${SIDEBAR_ROW_HEIGHT.card}`;

/**
 * Figma `Sync / Sidebar Content` (783:252): DEVICE — this device and the cloud
 * as two 48px rows — then CONFLICTS with a count, one 48px row per conflict
 * (glyph · "type/id" over "detected HH:MM" · a kind chip), or "Nothing to
 * resolve."
 */
export function SyncSidebar({
  connection,
  conflicts,
  selectedId,
  onSelect,
}: {
  connection: SyncStatus;
  conflicts: SyncConflict[];
  selectedId: string | null;
  onSelect: (recordId: string) => void;
}) {
  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <SidebarSection label="Device" />
      <div className={cn(ROW, "border-transparent")}>
        <Laptop className="size-4 shrink-0 text-text-secondary" />
        <span className="flex min-w-0 flex-1 flex-col gap-px">
          <span className="truncate text-body-13-medium text-text-primary">This device</span>
          <span className="truncate text-code-11 text-text-tertiary">
            {connection.deviceId || "not registered yet"}
          </span>
        </span>
      </div>
      <div className={cn(ROW, "border-transparent")}>
        <Server className="size-4 shrink-0 text-text-secondary" />
        <span className="flex min-w-0 flex-1 flex-col gap-px">
          <span className="truncate text-body-13-medium text-text-primary">Cloud</span>
          <span className="truncate text-code-11 text-text-tertiary">
            {connection.connected ? hostOf(connection.endpoint) : "not connected"}
          </span>
        </span>
      </div>

      <SidebarSection label="Conflicts" count={conflicts.length} />
      {conflicts.length === 0 ? (
        // A rail state, and a short one: the rail lists this as a row under
        // CONFLICTS (Figma 783:252), not a panel that swallows the space below.
        <EmptyState size="rail" className="py-2.5" body="Nothing to resolve." />
      ) : (
        conflicts.map((c) => {
          const Glyph = glyphFor(c);
          const selected = c.recordId === selectedId;
          return (
            <button
              key={c.recordId}
              type="button"
              onClick={() => onSelect(c.recordId)}
              className={cn(
                ROW,
                selected
                  ? "border-border-subtle bg-surface-raised"
                  : "border-transparent hover:bg-state-soft",
              )}
            >
              <Glyph className="size-4 shrink-0 text-text-secondary" />
              <span className="flex min-w-0 flex-1 flex-col gap-px">
                <span className="truncate text-body-13-medium text-text-primary">
                  {c.recordType}/{c.recordId}
                </span>
                <span className="truncate text-code-11 text-text-tertiary">
                  detected {clockOf(c.detectedAt)}
                </span>
              </span>
              <span className="flex h-5 shrink-0 items-center rounded-sm border border-border-subtle bg-surface-raised px-2 text-code-11 text-text-secondary">
                {conflictKind(c)}
              </span>
            </button>
          );
        })
      )}
    </div>
  );
}
