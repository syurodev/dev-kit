import { useEffect, useId, useMemo, useRef } from "react";
import { create } from "zustand";
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import type { RefTarget } from "./resolveFileRef";

type Rect = { left: number; top: number; width: number; height: number };

type PickerState = {
  /** Mounted hosts, oldest first; only the first draws, so two transcripts never open two pickers. */
  hosts: string[];
  anchorRect: Rect | null;
  targets: RefTarget[];
  resolve: ((target: RefTarget | null) => void) | null;
};

export const useRefPickerStore = create<PickerState>(() => ({ hosts: [], anchorRect: null, targets: [], resolve: null }));

/** Settles the open question (null = dismissed) and closes the picker. */
export function settleRefPicker(target: RefTarget | null): void {
  const { resolve } = useRefPickerStore.getState();
  useRefPickerStore.setState({ anchorRect: null, targets: [], resolve: null });
  resolve?.(target);
}

/** Asks which of several files was meant, anchored at the clicked element. */
export function pickTarget(anchor: HTMLElement | null, targets: RefTarget[]): Promise<RefTarget | null> {
  if (useRefPickerStore.getState().hosts.length === 0 || targets.length === 0) return Promise.resolve(null);
  settleRefPicker(null); // A newer click replaces an unanswered picker.
  const r = anchor?.getBoundingClientRect();
  const anchorRect = r ? { left: r.left, top: r.top, width: r.width, height: r.height } : { left: 0, top: 0, width: 0, height: 0 };
  return new Promise((resolve) => useRefPickerStore.setState({ anchorRect, targets, resolve }));
}

function splitRel(rel: string): { name: string; dir: string } {
  const cut = rel.lastIndexOf("/");
  return cut < 0 ? { name: rel, dir: "" } : { name: rel.slice(cut + 1), dir: rel.slice(0, cut) };
}

/** The candidate rows; arrow keys move between them, Enter (a button's own) picks. */
export function RefPickerList({ targets, onPick }: { targets: RefTarget[]; onPick: (target: RefTarget) => void }) {
  const listRef = useRef<HTMLDivElement>(null);
  function onKeyDown(event: React.KeyboardEvent) {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    const rows = Array.from(listRef.current?.querySelectorAll<HTMLButtonElement>("button") ?? []);
    const at = rows.indexOf(document.activeElement as HTMLButtonElement);
    const down = event.key === "ArrowDown";
    const next = at < 0 ? (down ? 0 : rows.length - 1) : (at + (down ? 1 : rows.length - 1)) % rows.length;
    rows[next]?.focus();
  }
  return (
    <div ref={listRef} role="group" aria-label="Which file?" onKeyDown={onKeyDown} className="flex flex-col">
      {targets.map((target) => {
        const { name, dir } = splitRel(target.rel);
        return (
          <button
            key={`${target.folderId}\u0000${target.rel}`}
            type="button"
            data-testid="agent-ref-pick"
            onClick={() => onPick(target)}
            className={cn(
              "flex h-7 w-full min-w-0 items-center gap-2 rounded-sm px-2 text-left text-body-13 text-text-primary outline-none transition-control",
              "hover:bg-state-soft focus-visible:bg-state-soft",
            )}
          >
            <span className="shrink-0 font-medium">{name}</span>
            <span className="min-w-0 truncate text-text-tertiary">{dir ? `${target.folderName} · ${dir}` : target.folderName}</span>
          </button>
        );
      })}
    </div>
  );
}

/** Rendered by every transcript; the first mounted one draws the picker. */
export function RefPickerHost() {
  const id = useId();
  useEffect(() => {
    useRefPickerStore.setState((s) => ({ hosts: [...s.hosts, id] }));
    return () => {
      const hosts = useRefPickerStore.getState().hosts.filter((h) => h !== id);
      useRefPickerStore.setState({ hosts });
      if (hosts.length === 0) settleRefPicker(null);
    };
  }, [id]);
  const owner = useRefPickerStore((s) => s.hosts[0] === id);
  const anchorRect = useRefPickerStore((s) => s.anchorRect);
  const targets = useRefPickerStore((s) => s.targets);
  // A virtual anchor at the clicked reference's rect, taken at the click: the
  // reference itself may re-render away, and a fixed box inside the transcript
  // would be offset by any transformed ancestor.
  const virtualRef = useMemo(() => {
    const r = anchorRect;
    return { current: r ? { getBoundingClientRect: () => DOMRect.fromRect({ x: r.left, y: r.top, width: r.width, height: r.height }) } : null };
  }, [anchorRect]);
  if (!owner || !anchorRect) return null;
  return (
    <Popover open onOpenChange={(open) => !open && settleRefPicker(null)}>
      <PopoverAnchor virtualRef={virtualRef} />
      <PopoverContent align="start" className="w-auto max-w-120 gap-1 p-1" data-testid="agent-ref-picker">
        <p className="px-2 pt-1 text-caption-11 text-text-tertiary">Which file?</p>
        <RefPickerList targets={targets} onPick={(target) => settleRefPicker(target)} />
      </PopoverContent>
    </Popover>
  );
}
