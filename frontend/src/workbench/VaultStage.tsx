import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { Eye, EyeOff, Vault } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { EASE_OUT, MOTION_BASE, noMotion } from "@/lib/motion";

export const VAULT_FIELD = "flex h-12 w-full items-center rounded-lg border bg-surface-well pr-3 pl-4 text-code-12 text-text-primary transition-control placeholder:text-text-tertiary focus-within:border-status-info focus-within:ring-2 focus-within:ring-status-info/20";

export function VaultPasswordField({ label, value, onChange, placeholder, autoFocus, invalid, disabled, creating = false }: {
  label: string; value: string; onChange: (value: string) => void; placeholder: string; autoFocus?: boolean; invalid?: boolean; disabled?: boolean; creating?: boolean;
}) {
  const id = useId();
  const [shown, setShown] = useState(false);
  return <div className="flex w-full flex-col gap-1.5">
    <label htmlFor={id} className="text-body-13-medium text-text-secondary">{label}</label>
    <div className={cn(VAULT_FIELD, invalid ? "border-status-danger" : "border-border-subtle", disabled && "opacity-60")}>
      <input id={id} type={shown ? "text" : "password"} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} autoFocus={autoFocus} required disabled={disabled}
        autoComplete={creating ? "new-password" : "current-password"} spellCheck={false} autoCapitalize="none" aria-invalid={invalid || undefined} aria-describedby={invalid ? "vault-form-error" : undefined}
        className="min-w-0 flex-1 bg-transparent outline-none placeholder:font-sans placeholder:text-body-13" />
      <Button type="button" variant="ghost" size="icon-sm" disabled={disabled} onClick={() => setShown((value) => !value)} aria-label={`${shown ? "Hide" : "Show"} ${label.toLowerCase()}`} aria-pressed={shown} className="text-text-tertiary">
        {shown ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
      </Button>
    </div>
  </div>;
}

/** Presentation only. The gate owns all credentials and vault state. */
export function VaultStage({ step, eyebrow, title, body, footnote, eyebrowClass, setupStep, children }: {
  step: string; eyebrow: string; title: string; body: string; footnote: string; eyebrowClass?: string; setupStep?: number; children: ReactNode;
}) {
  const reduced = useReducedMotion();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (step === "setup_recovery" || step === "setup_ready") heading.current?.focus();
  }, [step]);
  return <section aria-labelledby="vault-stage-title" className="my-auto flex w-full max-w-120 shrink-0 flex-col rounded-2xl border border-border-subtle bg-surface-base p-6 shadow-(--calm-modal-shadow) sm:p-8">
    <div className="flex items-center justify-between gap-3"><span className="flex size-11 items-center justify-center rounded-xl border border-border-subtle bg-surface-well"><Vault aria-hidden className="size-5 text-text-secondary" strokeWidth={1.5} /></span>
      {setupStep && <div aria-label={`Setup step ${setupStep} of 3`} className="flex gap-1.5">{[1, 2, 3].map((value) => <span key={value} aria-hidden className={cn("h-1 w-6 rounded-full transition-control", value <= setupStep ? "bg-action-primary" : "bg-border-subtle")} />)}</div>}
    </div>
    <motion.div key={step} initial={reduced ? false : { opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }} transition={reduced ? noMotion : { duration: MOTION_BASE, ease: EASE_OUT }}>
      <p className={cn("mt-5 text-label-11-caps text-text-tertiary", eyebrowClass)}>{eyebrow}</p>
      <h1 ref={heading} tabIndex={-1} id="vault-stage-title" className="mt-2 text-heading-24 text-text-primary outline-none">{title}</h1>
      <p className="mt-2 text-body-13 text-text-secondary">{body}</p>
      <div className="mt-6 flex w-full flex-col">{children}</div>
      <p className="mt-6 border-t border-border-subtle pt-4 text-caption-11 text-text-tertiary">{footnote}</p>
    </motion.div>
  </section>;
}
