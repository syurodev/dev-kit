import { useState } from "react";
import { Check, ChevronDown, Copy, Database, Loader2, Search, Server, ShieldCheck, Terminal } from "lucide-react";
import type { PendingInteraction } from "@/bridge/interaction";
import { copyText } from "@/bridge/clipboard";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CodeBlock } from "@/components/code/CodeBlock";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { VendorIcon } from "@/modules/agenthub/surfaces/VendorIcon";

export type MCPProfileOption = { id: string; name: string; env?: string; detail?: string };

function Environment({ value }: { value: string }) {
  const key = value.trim().toLowerCase();
  const tone = /^(prod|production)$/.test(key) ? "bg-(--feedback-danger-soft) text-status-danger"
    : /^(stg|staging)$/.test(key) ? "bg-status-warning/10 text-status-warning"
    : "bg-state-soft text-text-secondary";
  return <Badge title={`Environment: ${value || "unknown"}`} className={cn("shrink-0 gap-1.5 border-transparent px-2 py-0.5 text-caption-11", tone)}>
    <span aria-hidden className="size-1.5 rounded-full bg-current" />{value || "unknown"}
  </Badge>;
}

type Props = {
  pending: PendingInteraction;
  profiles?: MCPProfileOption[];
  selectedId?: string;
  onSelect?: (id: string) => void;
  loading?: boolean;
  busy?: boolean;
  error?: string;
  waiting?: number;
  callerName?: string;
  callerVendor?: string;
  onDeny: () => void;
  onAllow: (always: boolean) => void;
};

/** Presentation only: approvals and profile activation remain owned by InteractionHost. */
export function MCPInteractionDialog({ pending, profiles = [], selectedId = "", onSelect, loading, busy, error, waiting = 0,
  callerName, callerVendor, onDeny, onAllow }: Props) {
  const [query, setQuery] = useState("");
  const [copied, setCopied] = useState(false);
  const selecting = pending.type === "select";
  const isSQL = !selecting && /^db\.(query|explain)$/.test(pending.tool);
  const Icon = selecting ? pending.kind === "ssh" ? Terminal : pending.kind === "kafka" ? Server : Database : ShieldCheck;
  const kind = pending.kind === "ssh" ? "SSH connection" : pending.kind === "kafka" ? "Kafka connection" : pending.kind || "connection";
  const caller = callerName || (pending.caller.startsWith("agent:") ? `Agent · ${pending.caller.slice(6, 14)}` : pending.caller || "Unknown requester");
  const filtered = profiles.filter((profile) => `${profile.name} ${profile.env ?? ""} ${profile.detail ?? ""}`.toLowerCase().includes(query.toLowerCase()));
  async function copy() {
    try { await copyText(pending.summary); setCopied(true); } catch { /* The request remains selectable. */ }
  }
  return <Dialog open onOpenChange={() => {}}>
    <DialogContent showCloseButton={false} className="gap-5 rounded-2xl p-5 sm:max-w-xl" aria-describedby="mcp-interaction-description">
      <DialogHeader className="gap-3">
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-border-subtle bg-state-soft text-text-secondary"><Icon aria-hidden className="size-5" /></div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between gap-2">
              <DialogTitle className="text-title-16">{selecting ? `Select ${kind}` : "Review MCP request"}</DialogTitle>
              {!selecting && <Environment value={pending.env} />}
            </div>
            <DialogDescription id="mcp-interaction-description" className="mt-1 text-body-12 leading-relaxed">
              {selecting ? "Choose the saved connection this tool should use." : "Review the tool and request before allowing it to run."}
            </DialogDescription>
          </div>
        </div>
        <details className="group rounded-lg border border-border-subtle bg-state-soft px-3 py-2">
          <summary className="flex cursor-pointer list-none items-center gap-2 text-body-12 text-text-secondary [&::-webkit-details-marker]:hidden">
            <VendorIcon vendor={callerVendor ?? ""} className="size-4 shrink-0" />
            <span className="shrink-0 text-text-tertiary">Requested by</span>
            <span className="min-w-0 flex-1 truncate font-medium text-text-primary" title={pending.caller}>{caller}</span>
            {waiting > 0 && <span className="shrink-0 text-caption-11 text-text-tertiary">+{waiting} waiting</span>}
            <ChevronDown aria-hidden className="size-3 shrink-0 text-text-tertiary transition-transform group-open:rotate-180" />
          </summary>
          <code className="mt-2 block break-all border-t border-border-subtle pt-2 text-code-11 text-text-tertiary">{pending.caller || "unknown"}</code>
        </details>
      </DialogHeader>
      {selecting ? <div className="grid min-w-0 gap-2">
        <div className="flex items-center justify-between text-body-12 text-text-secondary"><span>Saved profiles</span><span className="text-caption-11 text-text-tertiary">{profiles.length}</span></div>
        {profiles.length > 5 && <div className="relative"><Search aria-hidden className="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-text-tertiary" /><Input aria-label="Search profiles" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search profiles…" className="pl-9" /></div>}
        {loading ? <div role="status" className="flex items-center justify-center gap-2 rounded-xl border border-border-subtle p-8 text-body-12 text-text-tertiary"><Loader2 className="size-4 animate-spin" />Loading profiles…</div>
          : profiles.length === 0 ? <div role="status" className="rounded-xl border border-dashed border-border-subtle p-6 text-center text-body-12 text-text-secondary">No saved profiles. Create a connection in the module first.</div>
          : <RadioGroup aria-label="Connection profile" value={selectedId} onValueChange={onSelect} disabled={busy} className="max-h-64 gap-2 overflow-y-auto p-0.5">
            {filtered.map((profile) => <label key={profile.id} htmlFor={`mcp-profile-${profile.id}`}
              className={cn("flex cursor-pointer items-center gap-3 rounded-xl border p-3 transition-colors hover:bg-state-soft", selectedId === profile.id ? "border-primary/40 bg-primary/5" : "border-border-subtle", busy && "pointer-events-none opacity-60")}>
              <Icon aria-hidden className="size-4 shrink-0 text-text-tertiary" />
              <span className="min-w-0 flex-1"><span className="block truncate text-body-13 font-medium text-text-primary">{profile.name}</span>
                {profile.detail && <span className="mt-0.5 block truncate text-code-11 text-text-tertiary" title={profile.detail}>{profile.detail}</span>}</span>
              {profile.env && <Environment value={profile.env} />}
              <RadioGroupItem id={`mcp-profile-${profile.id}`} value={profile.id} />
            </label>)}
            {filtered.length === 0 && <p className="py-4 text-center text-body-12 text-text-tertiary">No matching profiles.</p>}
          </RadioGroup>}
      </div> : <div className="min-w-0 space-y-3">
        <div className="flex items-center justify-between gap-3"><span className="text-body-12 text-text-tertiary">Tool</span><code className="min-w-0 truncate rounded-md bg-state-soft px-2 py-1 text-code-12 text-text-primary" title={pending.tool}>{pending.tool || "unknown"}</code></div>
        <div className="overflow-hidden rounded-xl border border-border-subtle bg-background/40">
          <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2 text-body-12 text-text-secondary"><span>{isSQL ? "SQL query" : "Request"}</span>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={copied ? "Request copied" : "Copy request"} disabled={!pending.summary} onClick={() => void copy()}>
              {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            </Button>
          </div>
          {isSQL ? <CodeBlock code={pending.summary || "—"} language="sql" className="max-h-64 overflow-y-auto whitespace-pre-wrap break-words p-4 text-code-12 leading-6" />
            : <p className="max-h-64 overflow-y-auto whitespace-pre-wrap break-words p-4 text-body-13 leading-relaxed text-text-primary">{pending.summary || "—"}</p>}
        </div>
      </div>}
      {error && <p role="alert" className="rounded-lg bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">{error}</p>}
      <DialogFooter className="-mx-5 -mb-5 rounded-b-2xl bg-transparent px-5 py-4">
        <Button type="button" variant="outline" disabled={busy} onClick={onDeny}>{selecting ? "Cancel" : "Deny"}</Button>
        {!selecting && pending.allowAlwaysOffered && <Button type="button" variant="secondary" disabled={busy} onClick={() => onAllow(true)}>Allow always</Button>}
        <Button type="button" disabled={busy || (selecting && (loading || !selectedId))} onClick={() => onAllow(false)}>
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}{selecting ? "Use profile" : "Allow once"}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
