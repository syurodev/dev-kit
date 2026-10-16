import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { ModelProfile, ModelProfileCheck } from "../../bridge/agenthub";
import { ModelProfilesEditor } from "./ModelProfilesEditor";

const vendors = [{ id: "claude", name: "Claude" }, { id: "codex", name: "Codex" }];
const base: ModelProfile = { id: "review", name: "Code review", description: "Reading a lot of code", vendor: "claude", model: "haiku", effort: "low", notes: "" };
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
    const check = (status: string, problem: string): Record<string, ModelProfileCheck> => ({ review: { ...base, status, problem } });
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

  test("a save error shows under the list when no form is open", () => {
    expect(render({ error: "The model profiles changed." })).toContain("The model profiles changed.");
  });
});
