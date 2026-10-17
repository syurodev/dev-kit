import { SubagentLimitsEditor } from "@/modules/settings/SubagentLimitsEditor";

/**
 * Settings → Agents → Models & subagents → "Subagent limits" in its states:
 * unset, a saved cap, none allowed, and a refused save. Nested in the
 * `?gallery#agenthub-model-profiles` page.
 */
const noop = async () => true;

export function SubagentLimitsSection() {
  const frame = "w-full max-w-170 rounded-control border border-border-subtle p-4";
  return (
    <section id="agenthub-subagent-limits" className="flex flex-col gap-6 p-6">
      <h2 className="text-heading-18 text-text-primary">Subagent limits</h2>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">Unset: DevKit passes Codex nothing</p>
        <SubagentLimitsEditor stored={null} saving={false} error="" onSave={noop} />
      </div>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">A cap of 2</p>
        <SubagentLimitsEditor stored={2} saving={false} error="" onSave={noop} />
      </div>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">None allowed</p>
        <SubagentLimitsEditor stored={0} saving={false} error="" onSave={noop} />
      </div>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">A save the store refused</p>
        <SubagentLimitsEditor stored={3} saving={false} error="Subagent limits: at most 16 subagents (each one is a full model session)" onSave={noop} />
      </div>
    </section>
  );
}
