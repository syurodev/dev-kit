import { useEffect, useRef, useState } from "react";
import { ArrowLeft, Check, ChevronDown, ChevronRight, Loader2, RotateCcw, Zap } from "lucide-react";
import { motion, useReducedMotion } from "framer-motion";
import { EASE_OUT, MOTION_BASE, noMotion } from "@/lib/motion";
import { NativeSelect } from "@/components/ui/native-select";
import { Slider as SliderPrimitive } from "radix-ui";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { cn } from "@/lib/utils";
import type { ConfigOption } from "../types";
import { reasoningLabel } from "../modelSettings";

type Props = {
  options: ConfigOption[];
  onSelect: (id: string, value: string) => void | Promise<void>;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading?: boolean;
  error?: string;
  onRefresh?: () => void;
   onCloseAutoFocus?: () => void;
};

export function ModelSettingsPopover({ options, onSelect, open, onOpenChange, loading, error, onRefresh, onCloseAutoFocus }: Props) {
  const reduced = useReducedMotion();
  const [modelsOpen, setModelsOpen] = useState(false);
  const [resetting, setResetting] = useState(false);
  const backButton = useRef<HTMLButtonElement>(null);
  const modelButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (open) (modelsOpen ? backButton : modelButton).current?.focus();
  }, [modelsOpen, open]);
  const model = options.find((option) => option.category === "model" || option.id === "model");
  const effort = options.find((option) => option.category === "thought_level" || /effort|thinking/.test(option.id));
  const selected = model?.options?.find((choice) => choice.value === model.currentValue);
  const efforts = effort?.options ?? [];
  const index = Math.max(0, efforts.findIndex((choice) => choice.value === effort?.currentValue));
  const [position, setPosition] = useState(index);
  useEffect(() => setPosition(index), [index, model?.currentValue]);
  useEffect(() => { if (!open) setModelsOpen(false); }, [open]);
  const activeEffort = efforts[position];
  // Short, for the button: the model's name, or what is happening to it.
  const triggerLabel = selected?.name ?? selected?.value ?? (loading ? "Loading models…" : error ? "Models unavailable" : "Select model");
  async function reset() {
    setResetting(true);
    try {
      const defaultModel = model?.options?.find((choice) => choice.value === model.defaultValue);
      if (model?.defaultValue) await onSelect(model.id, model.defaultValue);
      const defaultEffort = defaultModel?.defaultEffort ?? effort?.defaultValue;
      if (effort && defaultEffort) await onSelect(effort.id, defaultEffort);
    } finally { setResetting(false); }
  }
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        {/* The model on the button's face, with a chevron: which model answers
            is the one setting worth reading without opening anything. Effort and
            the rest are in the popover. The name says what the button opens. */}
        <ActionTooltip label={`Model and effort: ${triggerLabel}`} side="top">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            data-testid="composer-config"
            aria-label={`Model and effort: ${triggerLabel}`}
            className="min-w-0 shrink gap-1 px-2 text-text-secondary"
          >
            <span className="max-w-28 truncate text-body-13 @max-xs:max-w-16">{triggerLabel}</span>
            <ChevronDown aria-hidden className="size-3 shrink-0 text-text-tertiary" />
          </Button>
        </ActionTooltip>
      </PopoverTrigger>
      <PopoverContent side="top" align="start" className="w-64 rounded-2xl p-3" onCloseAutoFocus={onCloseAutoFocus}>
        <motion.div key={modelsOpen ? "models" : "effort"} initial={reduced ? false : { opacity: 0, x: modelsOpen ? 6 : -6 }} animate={{ opacity: 1, x: 0 }} transition={reduced ? noMotion : { duration: MOTION_BASE, ease: EASE_OUT }}>
        {modelsOpen ? (
          <div role="group" aria-label="Select model">
            <div className="mb-2 flex items-center justify-between text-body-13 text-text-tertiary">
              <Button ref={backButton} type="button" variant="ghost" size="xs" onClick={() => setModelsOpen(false)} aria-label="Back to agent settings" className="-ml-1 gap-1 text-text-secondary"><ArrowLeft aria-hidden className="size-3" />Select model</Button>
              {onRefresh && <button type="button" aria-label="Refresh models" disabled={loading} onClick={onRefresh} className="rounded p-1 transition-control hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/20">
                <RotateCcw className={cn("size-3.5", loading && "animate-spin motion-reduce:animate-none")} />
              </button>}
            </div>
            {model?.defaultValue && <button type="button" className="mb-1 w-full rounded-lg px-2 py-1.5 text-left transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 hover:bg-surface-hover"
              onClick={() => { void onSelect(model.id, model.defaultValue!); setModelsOpen(false); }}>
              <div className="text-body-13 text-text-primary">Default</div>
              <div className="text-caption-11 text-text-tertiary">Recommended model</div>
            </button>}
            <div className="max-h-72 overflow-y-auto overscroll-contain">
              {model?.options?.map((choice) => <button key={choice.value} type="button" data-testid="composer-config-option"
                aria-pressed={choice.value === model.currentValue}
                onClick={() => { void onSelect(model.id, choice.value); setModelsOpen(false); }}
                className="flex w-full items-center justify-between gap-3 rounded-lg px-2 py-2 text-left text-body-13 text-text-primary transition-control aria-pressed:bg-state-soft focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 hover:bg-surface-hover">
                <span className="truncate">{choice.name ?? choice.value}</span>
                {choice.value === model.currentValue && <Check aria-hidden className="size-4 shrink-0 text-text-secondary" />}
              </button>)}
            </div>
          </div>
        ) : (
          <>
            <div className="flex items-start justify-between gap-2">
              <Zap aria-hidden className="mt-1 size-4 text-text-tertiary" />
              <div className="min-w-0 flex-1 text-center">
                {efforts.length > 0 && <div className="text-body-14 font-medium text-primary">{reasoningLabel(activeEffort?.name ?? activeEffort?.value ?? "")}</div>}
                <button ref={modelButton} type="button" disabled={!model?.options?.length} onClick={() => setModelsOpen(true)}
                  aria-label="Select model" className="inline-flex max-w-full items-center gap-1 rounded px-1 py-0.5 text-body-13 text-text-secondary transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 hover:text-text-primary">
                  <span className="truncate">{selected?.name ?? selected?.value ?? (loading ? "Loading models…" : "Select model")}</span>
                  <ChevronRight aria-hidden className="size-3 shrink-0" />
                </button>
              </div>
              <button type="button" aria-label="Reset model and effort" onClick={() => void reset()}
                disabled={resetting || (!model?.defaultValue && !effort?.defaultValue)} className="rounded p-1 text-text-tertiary transition-control focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 hover:text-text-primary disabled:opacity-40">
                <RotateCcw aria-hidden className={cn("size-4", resetting && "animate-spin motion-reduce:animate-none")} />
              </button>
            </div>
            {efforts.length > 1 && <div className="relative mt-3 rounded-full border border-border bg-muted">
              <div aria-hidden className="pointer-events-none absolute inset-x-3 inset-y-0 z-10 flex items-center justify-between">
                {efforts.map((choice) => <span key={choice.value} className="size-1 rounded-full bg-text-tertiary/40" />)}
              </div>
              <SliderPrimitive.Root aria-label="Reasoning effort" aria-valuetext={reasoningLabel(activeEffort?.value ?? "")}
                min={0} max={efforts.length - 1} step={1} value={[position]} disabled={resetting}
                onValueChange={([value]) => setPosition(value ?? 0)}
                onValueCommit={([value]) => { const choice = efforts[value ?? 0]; if (effort && choice) void onSelect(effort.id, choice.value); }}
                className="relative flex h-6 w-full touch-none items-center px-3 select-none">
                <SliderPrimitive.Track className="absolute inset-0 overflow-hidden rounded-full">
                  <SliderPrimitive.Range className="absolute h-full bg-primary" />
                </SliderPrimitive.Track>
                <SliderPrimitive.Thumb className="relative z-20 block size-7 rounded-full bg-text-primary shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label="Reasoning effort" aria-valuetext={reasoningLabel(activeEffort?.value ?? "")} />
              </SliderPrimitive.Root>
            </div>}
          </>
        )}
        </motion.div>
        {loading && <div role="status" className="mt-2 flex items-center gap-1 text-caption-11 text-text-tertiary"><Loader2 aria-hidden className="size-3 animate-spin motion-reduce:animate-none" />Updating models…</div>}
    {!modelsOpen && options.filter((option) => option !== model && option !== effort).map((option) => (
    <label key={option.id} className="mt-3 flex items-center justify-between gap-2 text-body-12 text-text-secondary">
      <span>{option.name ?? option.id}</span>
      <NativeSelect size="sm" aria-label={option.name ?? option.id} value={String(option.currentValue ?? "")} className="min-w-0 max-w-36"
        onChange={(event) => void onSelect(option.id, event.target.value)}>
        {option.options?.map((choice) => <option key={choice.value} value={choice.value}>{choice.name ?? choice.value}</option>)}
      </NativeSelect>
    </label>
    ))}
        {error && <div role="alert" className="mt-2 text-caption-11 text-status-danger">{error}</div>}
        {!modelsOpen && onRefresh && (error || !model?.options?.length) && <button type="button" onClick={onRefresh} disabled={loading} className="mt-2 text-body-12 text-primary">Refresh models</button>}
      </PopoverContent>
    </Popover>
  );
}
