import { Fragment, useDeferredValue, useState } from "react";
import { Search } from "lucide-react";
import { isMac } from "@/lib/shortcut";
import { cn } from "@/lib/utils";
import { listRegisteredModules } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { buildSections, capLabels, filterSections, type KeyCap, type ShortcutEntry, type ShortcutSection } from "./keyboard";

/**
 * Figma `Settings Content / Keyboard` (1221:42022): every shortcut in the app,
 * searchable, the module you are in first. A reference and not an editor — the
 * bindings are code, and the list is built from the same places (keyboard.ts) so
 * it says what the keys do rather than what someone remembered they did.
 */
export function KeyboardTab({ intro }: { intro: React.ReactNode }) {
  const activeModuleId = useWorkbenchTabsStore((state) => state.activeModuleId);
  const [query, setQuery] = useState("");
  // The field answers at once; the filtering of a hundred rows can follow.
  const deferred = useDeferredValue(query);
  const sections = filterSections(buildSections(listRegisteredModules(), activeModuleId), deferred);

  return (
    <div className="flex w-full max-w-164 flex-col">
      {intro}
      <label className="mt-5 flex h-9 items-center gap-2.5 rounded-md border border-border bg-surface-raised pr-3 pl-3.5 focus-within:border-border-focus">
        <Search className="size-4 shrink-0 text-text-tertiary" strokeWidth={1.5} aria-hidden />
        <input
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={`Search by action or key — try “run” or “${isMac() ? "⌘⇧" : "Ctrl+Shift"}”`}
          aria-label="Search shortcuts"
          className="min-w-0 flex-1 bg-transparent text-body-13 text-text-primary outline-none placeholder:text-text-tertiary"
        />
      </label>

      <div className="mt-6 flex flex-col gap-6">
        {sections.map((section) => (
          <Section key={section.id} section={section} />
        ))}
        {sections.length === 0 ? (
          <p className="text-body-13 text-text-tertiary" role="status">
            No shortcut matches “{deferred.trim()}”.
          </p>
        ) : null}
      </div>
    </div>
  );
}

function Section({ section }: { section: ShortcutSection }) {
  return (
    <section aria-label={section.title}>
      <h3 className="flex items-start gap-2 text-label-11-caps text-text-tertiary">
        <span>
          {section.title}
          {section.here ? " · you are here" : ""}
        </span>
        <span className="text-code-11 normal-case opacity-70">{section.entries.length}</span>
      </h3>
      <ul className="mt-1.5">
        {section.entries.map((entry, index) => (
          <Row key={entry.id} entry={entry} first={index === 0} />
        ))}
      </ul>
    </section>
  );
}

function Row({ entry, first }: { entry: ShortcutEntry; first: boolean }) {
  return (
    <li className={cn("flex items-center gap-3 py-2", !first && "border-t border-border")}>
      <span className="min-w-0 flex-1 text-body-13 text-text-secondary">{entry.action}</span>
      {entry.context ? <span className="shrink-0 text-body-12 text-text-tertiary">{entry.context}</span> : null}
      <span className="flex shrink-0 items-center gap-1">
        {entry.keys.map((cap, index) => (
          <Cap key={index} cap={cap} />
        ))}
      </span>
    </li>
  );
}

/** One key cap — or two joined by a dash for a range. */
function Cap({ cap }: { cap: KeyCap }) {
  // Drawn for the platform the app is on; capLabels is the same words the search reads.
  const labels = capLabels(cap, isMac());
  return (
    <Fragment>
      {labels.map((label, index) => (
        <Fragment key={label}>
          {index > 0 ? <span className="text-caption-11 text-text-tertiary">–</span> : null}
          <kbd className="inline-flex items-center rounded-md border border-border-subtle bg-surface-raised p-1 text-code-11 whitespace-nowrap text-text-tertiary">
            {label}
          </kbd>
        </Fragment>
      ))}
    </Fragment>
  );
}
