import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Switch } from "@/components/ui/switch";
import type { VendorPolicy } from "@/bridge/agentAccounts";
import { AccountRiskDialog } from "./AccountRiskDialog";
import {
  MAX_AGE_MAX,
  MAX_AGE_MIN,
  REFUSAL_CHOICES,
  SWITCHING_TRUTH,
  THRESHOLD_MAX,
  THRESHOLD_MIN,
  acknowledgeFor,
  fieldsDiffer,
  fieldsOf,
  parseFields,
  refusalOf,
  riskFlow,
  thresholdMeaning,
  type PolicyFields,
} from "./accountPolicy";

type Problem = { field: "threshold" | "weekly" | "maxAge"; text: string };

/**
 * When a session moves to another account, and what to do with a turn the vendor
 * refused. Presentational like SubagentLimitsEditor: the container reads and
 * saves (compare-and-swap on the whole preference), so the gallery can draw every
 * state. The order is the list above it; this form never changes it.
 */
export function AccountPolicyEditor({
  vendorName,
  policy,
  signedInCount,
  riskAcknowledged,
  saving,
  error,
  onSave,
}: {
  vendorName: string;
  policy: VendorPolicy;
  /** How many of the vendor's accounts are signed in: with fewer than two nothing can move. */
  signedInCount: number;
  /** The terms warning was accepted before; turning switching on then asks nothing. */
  riskAcknowledged: boolean;
  saving: boolean;
  error: string;
  /** `acknowledgeRisk` is true only when the person just accepted the warning. Resolves true when stored. */
  onSave: (next: VendorPolicy, acknowledgeRisk: boolean) => Promise<boolean>;
}) {
  const stored = fieldsOf(policy);
  const storedKey = JSON.stringify(stored);
  const [others, setOthers] = useState<PolicyFields>(stored);
  const [flow, setFlow] = useState(() => riskFlow.initial(stored.enabled));
  const [problem, setProblem] = useState<Problem | null>(null);
  const [saved, setSaved] = useState(false);
  const { askRisk, accepted } = flow;
  // The switch lives in the flow beside the terms dialog; the rest of the form is `others`.
  const fields: PolicyFields = { ...others, enabled: flow.enabled };

  // A save elsewhere (or the first read) moves what is stored under the form.
  useEffect(() => {
    const next = JSON.parse(storedKey) as PolicyFields;
    setOthers(next);
    setFlow((current) => riskFlow.storedChanged(current, next.enabled));
    setProblem(null);
  }, [storedKey]);

  const parsed = parseFields(fields);
  const dirty = fieldsDiffer(fields, stored);
  const refusal = REFUSAL_CHOICES.find((choice) => choice.value === fields.refusal) ?? REFUSAL_CHOICES[0];
  const edit = (change: Partial<PolicyFields>) => {
    setOthers((current) => ({ ...current, ...change }));
    setProblem(null);
    setSaved(false);
  };
  const move = (next: typeof flow) => {
    if (next.enabled !== flow.enabled) {
      setProblem(null);
      setSaved(false);
    }
    setFlow(next);
  };

  async function save() {
    const next = parseFields(fields);
    if (!next.ok) {
      setProblem({ field: next.field, text: next.problem });
      return;
    }
    setProblem(null);
    setSaved(false);
    if (await onSave({ ...next.value, order: policy.order }, acknowledgeFor(accepted, next.value.enabled, riskAcknowledged))) {
      setSaved(true);
      setFlow(riskFlow.saved);
    }
  }

  const fieldError = (field: Problem["field"]) => (problem?.field === field ? problem.text : undefined);

  return (
    <section className="flex flex-col gap-3" aria-label={`${vendorName} switching`}>
      <h3 className="text-body-14-medium text-text-primary">Switching</h3>
      <p className="text-body-12 text-text-tertiary">{SWITCHING_TRUTH}</p>
      <form
        className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
      >
        <label className="flex items-center gap-2.5">
          <span className="min-w-0 flex-1 text-body-13 text-text-primary">{`Move ${vendorName} sessions to the next account at a limit`}</span>
          <Switch
            aria-label={`Switch ${vendorName} accounts at a limit`}
            checked={fields.enabled}
            disabled={saving}
            onCheckedChange={(on) => move(riskFlow.toggle(flow, on, riskAcknowledged))}
          />
        </label>
        {fields.enabled && signedInCount < 2 ? (
          <p className="text-body-12 text-status-warning" data-testid="account-policy-needs-two">
            Nothing can move until at least two {vendorName} accounts are signed in.
          </p>
        ) : null}

        <div className="flex flex-wrap items-start gap-3">
          <div className="w-44">
            <Input
              label="Move at (% used)"
              type="number"
              inputMode="numeric"
              min={THRESHOLD_MIN}
              max={THRESHOLD_MAX}
              step={1}
              value={fields.threshold}
              error={fieldError("threshold")}
              onChange={(event) => edit({ threshold: event.target.value })}
            />
          </div>
          <div className="w-44">
            <Input
              label="Weekly window (% used)"
              type="number"
              inputMode="numeric"
              min={THRESHOLD_MIN}
              max={THRESHOLD_MAX}
              step={1}
              value={fields.weekly}
              placeholder="Same as the left"
              error={fieldError("weekly")}
              onChange={(event) => edit({ weekly: event.target.value })}
            />
          </div>
          <div className="w-44">
            <Input
              label="Re-check after (seconds)"
              type="number"
              inputMode="numeric"
              min={MAX_AGE_MIN}
              max={MAX_AGE_MAX}
              step={1}
              value={fields.maxAge}
              error={fieldError("maxAge")}
              onChange={(event) => edit({ maxAge: event.target.value })}
            />
          </div>
        </div>
        <p className="text-body-12 text-text-secondary">{thresholdMeaning(parsed)}</p>
        <p className="text-body-12 text-text-tertiary">
          Before a turn, DevKit asks the vendor again about a shared login whose last check is older than the re-check time.
          Usage is never read on a timer.
        </p>

        <label className="flex flex-col gap-1">
          <span className="text-caption-11 text-text-tertiary">When the vendor refuses a turn at its limit</span>
          <NativeSelect
            aria-label="When the vendor refuses a turn at its limit"
            value={fields.refusal}
            disabled={saving}
            onChange={(event) => edit({ refusal: refusalOf(event.target.value) })}
          >
            {REFUSAL_CHOICES.map((choice) => (
              <NativeSelectOption key={choice.value} value={choice.value}>
                {choice.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <span className="text-caption-11 text-text-tertiary">{refusal.detail}</span>
        </label>

        <div className="flex items-center gap-3">
          <Button type="submit" size="lg" disabled={saving || !dirty}>
            {saving ? "Saving…" : "Save"}
          </Button>
          {saved ? (
            <p className="text-body-12 text-text-secondary" role="status">
              Saved. Applies to turns started from now on.
            </p>
          ) : null}
        </div>
        {error ? (
          <p className="text-body-12 text-status-danger" role="alert">
            {error}
          </p>
        ) : null}
      </form>

      <AccountRiskDialog
        open={askRisk}
        vendorName={vendorName}
        onCancel={() => move(riskFlow.cancel(flow))}
        onConfirm={() => move(riskFlow.confirm())}
      />
    </section>
  );
}
