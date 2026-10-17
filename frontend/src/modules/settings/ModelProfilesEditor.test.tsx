import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { ModelProfile, ModelProfileCheck } from "../../bridge/agenthub";
import { ModelProfilesEditor } from "./ModelProfilesEditor";

const vendors = [{ id: "claude", name: "Claude" }, { id: "codex", name: "Codex" }];
const base: ModelProfile = { id: "review", name: "Code review", description: "Reading a lot of code", vendor: "claude", model: "haiku", effort: "low", notes: "", subagent: false, prompt: "" };
const render = (extra: Partial<Parameters<typeof ModelProfilesEditor>[0]> = {}) =>
  renderToStaticMarkup(
    <ModelProfilesEditor profiles={[base]} checks={{}} vendors={vendors} choices={{}} saving={false} error="" onVendorWanted={() => {}} onSave={async () => true} {...extra} />,
  );

describe("ModelProfilesEditor", () => {
  test("lists a profile with its agent, model and effort, and says what an empty list means", () => {
    const html = render();
    expect(html).toContain("Code review");
    expect(html).toContain("Claude");
    expect(html).toContain("haiku");
    expect(html).toContain("Low");
    expect(html).toContain("Add a profile");
    expect(render({ profiles: [] })).toContain("runs on its parent");
  });

  test("a profile whose model is gone is flagged, one whose vendor is signed out is only noted", () => {
    const check = (status: string, problem: string): Record<string, ModelProfileCheck> => ({ review: { ...base, status, problem, subagentName: "", subagentStatus: "", subagentProblem: "" } });
    const gone = render({ checks: check("unknownModel", 'claude no longer offers "haiku".') });
    expect(gone).toContain('role="alert"');
    expect(gone).toContain("Agents skip");
    const signedOut = render({ checks: check("signInRequired", "") });
    expect(signedOut).not.toContain('role="alert"');
    expect(signedOut).toContain("not signed in");
  });

  test("the form offers the vendor's models, or says why it cannot and lets the person type", () => {
    const ready = render({ initialDraft: base, choices: { claude: { state: "ready", models: [{ value: "haiku", name: "Haiku" }, { value: "sonnet", name: "Sonnet", supportedEfforts: [{ value: "low" }] }] } } });
    expect(ready).toContain("Choose a model");
    expect(ready).toContain("Sonnet");
    const blocked = render({ initialDraft: { ...base, vendor: "codex" }, choices: { codex: { state: "unavailable", reason: "Codex isn't signed in." } } });
    expect(blocked).toContain("Codex isn&#x27;t signed in.");
    expect(blocked).toContain("You can still write the profile");
    expect(blocked).not.toContain("Choose a model");
  });

  test("a model that left the vendor's list stays selectable, marked", () => {
    const html = render({ initialDraft: { ...base, model: "retired" }, choices: { claude: { state: "ready", models: [{ value: "sonnet" }] } } });
    expect(html).toContain("retired (no longer offered)");
  });

  // The form's text inputs are the default Input (--control-height-xl). A NativeSelect's default is
  // --control-height-md, 8px shorter, so side by side the fields looked misaligned: the selects ask for
  // size="lg", which is the same height token.
  test("the selects are as tall as the inputs beside them", () => {
    const ready = render({ initialDraft: base, choices: { claude: { state: "ready", models: [{ value: "haiku" }] } } });
    const selects = [...ready.matchAll(/<select[^>]*data-slot="native-select"[^>]*>/g)].map((m) => m[0]);
    expect(selects.length).toBe(3);
    for (const select of selects) expect(select).toContain('data-size="lg"');
    const typed = render({ initialDraft: base, choices: { claude: { state: "unavailable", reason: "x" } } });
    const agent = [...typed.matchAll(/<select[^>]*data-slot="native-select"[^>]*>/g)].map((m) => m[0]);
    expect(agent.length).toBe(1);
    expect(agent[0]).toContain('data-size="lg"');
  });

  test("a save error shows under the list when no form is open", () => {
    expect(render({ error: "The model profiles changed." })).toContain("The model profiles changed.");
  });

  test("a switched-on profile says what becomes of it, and a skipped one is flagged", () => {
    const on = { ...base, subagent: true, prompt: "Be brief." };
    const checked = (extra: Partial<ModelProfileCheck>): Record<string, ModelProfileCheck> => ({
      review: { ...on, status: "ok", problem: "", subagentName: "review", subagentStatus: "active", subagentProblem: "", ...extra },
    });
    const active = render({ profiles: [on], checks: checked({}) });
    expect(active).toContain("Subagent");
    expect(active).toContain("Subagent “review” in Claude sessions you start from now on.");
    expect(active).not.toContain('data-subagent="warn"');
    const skipped = render({ profiles: [on], checks: checked({ subagentStatus: "skipped", subagentProblem: 'claude no longer offers "haiku".' }) });
    expect(skipped).toContain("Not offered as a subagent");
    expect(skipped).toContain('data-subagent="warn"');
    expect(render({ profiles: [base], checks: {} })).not.toContain("as a subagent");
  });

  test("the form has the switch off by default and shows the instructions only when it is on", () => {
    const off = render({ initialDraft: base });
    expect(off).toContain("Also use as a subagent");
    expect(off).not.toContain("Subagent instructions");
    expect(off).toContain("keeps what it had until you reload it");
    const on = render({ initialDraft: { ...base, subagent: true, prompt: "Be brief." } });
    expect(on).toContain("Subagent instructions");
    expect(on).toContain("Be brief.");
    expect(render({ initialDraft: { ...base, vendor: "codex", subagent: true, prompt: "x" } })).toContain("Codex delegates only when");
    expect(on).not.toContain("Codex delegates only when");
  });
});
