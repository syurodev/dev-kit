import { useState, type ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { User, Sun, Monitor, SlidersHorizontal, LockKeyhole, Database, Bot, Command, RefreshCw, Plug, Search, Cpu, Puzzle, ShieldCheck, Bell, Scale } from "lucide-react";
import { Input } from "@/components/ui/input";
import { EASE_OUT, MOTION_BASE, noMotion } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { filterSettingsNavigation, type SettingsPage } from "./settingsNavigation";

const ICONS = {
  account: User, appearance: Sun, sync: Monitor, environments: SlidersHorizontal, database: Database, keyboard: Command, security: LockKeyhole, updates: RefreshCw, licenses: Scale,
  "agents-runtimes": Bot, "agents-models": Cpu, "agents-skills": Puzzle, "agents-safety": ShieldCheck, "agents-notifications": Bell, "agents-mcp": Plug,
} satisfies Record<SettingsPage, unknown>;

/** Internal preferences navigation, independent of workbench document tabs. */
export function SettingsLayout({ activeTab, onSelect, children }: { activeTab: SettingsPage; onSelect: (tab: SettingsPage) => void; children: ReactNode }) {
  const [query, setQuery] = useState("");
  const reduced = useReducedMotion();
  const groups = filterSettingsNavigation(query);
  return <div className="flex min-h-0 flex-1 flex-col overflow-hidden sm:flex-row">
    <nav aria-label="Settings sections" className="flex max-h-52 w-full shrink-0 flex-col gap-4 border-b border-border-subtle p-3 sm:max-h-none sm:w-56 sm:border-r sm:border-b-0">
      <div className="relative shrink-0"><Search aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 z-10 size-3.5 -translate-y-1/2 text-text-tertiary" /><Input aria-label="Search settings" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Find a setting…" className="h-8 pl-8 text-body-12" /></div>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain">
        {groups.map((group) => <div key={group.label}><p className="mb-1 px-2 text-label-11-caps text-text-tertiary">{group.label}</p><div className="space-y-0.5">
          {group.items.map((item) => { const Icon = ICONS[item.id]; const active = item.id === activeTab; return <button key={item.id} type="button" id={`settings-nav-${item.id}`} onClick={() => onSelect(item.id)} aria-current={active ? "page" : undefined}
            className={cn("flex h-8 w-full items-center gap-2.5 rounded-md border px-2 text-left text-body-13 transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50", active ? "border-border-subtle bg-surface-raised text-text-primary" : "border-transparent text-text-secondary hover:bg-state-soft hover:text-text-primary")}>
            <Icon aria-hidden className="size-4 shrink-0" strokeWidth={1.5} /><span className="truncate">{item.label}</span>
          </button>; })}
        </div></div>)}
        {!groups.length && <p role="status" className="px-2 text-body-12 text-text-tertiary">No matching settings.</p>}
      </div>
    </nav>
    <div key={activeTab} role="region" aria-label={`${activeTab} settings`} className="min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain p-5 sm:p-7 [scrollbar-gutter:stable]">
      <motion.div initial={reduced ? false : { opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={reduced ? noMotion : { duration: MOTION_BASE, ease: EASE_OUT }}>
        {children}
      </motion.div>
    </div>
  </div>;
}
