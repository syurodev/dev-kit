import type { ModelProfile, ModelProfileCheck } from "@/bridge/agenthub";
import { ModelProfilesEditor, type ProfileVendor } from "@/modules/settings/ModelProfilesEditor";
import type { VendorModelChoices } from "@/modules/settings/modelProfiles";

/**
 * Settings → Agents → "Models by job", in the states a person meets it:
 * empty, a list with a profile whose model has gone and one whose vendor is
 * signed out, the form with the vendor's models, and the form when the vendor's
 * models cannot be read. `?gallery#agenthub-model-profiles`.
 */

const vendors: ProfileVendor[] = [
  { id: "claude", name: "Claude" },
  { id: "codex", name: "Codex" },
];

const profile = (extra: Partial<ModelProfile>): ModelProfile => ({
  id: "code-review", name: "Code review", description: "Reading or reviewing a lot of code; summarising a diff.",
  vendor: "claude", model: "haiku", effort: "low", notes: "", ...extra,
});

const profiles: ModelProfile[] = [
  profile({}),
  profile({ id: "architecture", name: "Architecture", description: "Design, trade-offs, anything hard to undo.", model: "opus", effort: "high" }),
  profile({ id: "docs", name: "Documentation", description: "", vendor: "codex", model: "gpt-5.5-mini", effort: "" }),
];

const check = (p: ModelProfile, status: string, problem = ""): ModelProfileCheck => ({ ...p, status, problem });
const checks: Record<string, ModelProfileCheck> = {
  "code-review": check(profiles[0]!, "ok"),
  architecture: check(profiles[1]!, "unknownModel", 'claude no longer offers "opus" (it offers: sonnet, haiku).'),
  docs: check(profiles[2]!, "signInRequired", "not signed in"),
};

const claudeModels: VendorModelChoices = {
  state: "ready",
  models: [
    { value: "sonnet", name: "Sonnet", supportedEfforts: [{ value: "low" }, { value: "medium" }, { value: "high" }] },
    { value: "haiku", name: "Haiku" },
  ],
};
const codexSignedOut: VendorModelChoices = { state: "unavailable", reason: "Codex isn't signed in, or the sign-in expired." };

const noop = async () => true;

export function AgentModelProfilesSection() {
  const frame = "w-full max-w-170 rounded-control border border-border-subtle p-4";
  return (
    <section id="agenthub-model-profiles" className="flex flex-col gap-6 p-6">
      <h2 className="text-heading-18 text-text-primary">Models by job</h2>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">Empty</p>
        <ModelProfilesEditor profiles={[]} checks={{}} vendors={vendors} choices={{}} saving={false} error="" onVendorWanted={() => {}} onSave={noop} />
      </div>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">A list: one fine, one whose model is gone, one whose vendor is signed out</p>
        <ModelProfilesEditor profiles={profiles} checks={checks} vendors={vendors} choices={{}} saving={false} error="" onVendorWanted={() => {}} onSave={noop} />
      </div>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">Editing, with the vendor's models</p>
        <ModelProfilesEditor
          profiles={profiles} checks={checks} vendors={vendors} choices={{ claude: claudeModels }} saving={false} error=""
          onVendorWanted={() => {}} onSave={noop} initialDraft={profiles[0]}
        />
      </div>
      <div className={frame}>
        <p className="mb-2 text-label-12 text-text-tertiary">New profile for a vendor that is signed out</p>
        <ModelProfilesEditor
          profiles={profiles} checks={checks} vendors={vendors} choices={{ codex: codexSignedOut }} saving={false} error=""
          onVendorWanted={() => {}} onSave={noop} initialDraft={{ ...profile({ id: "", name: "", description: "", vendor: "codex", model: "", effort: "" }) }}
        />
      </div>
    </section>
  );
}
