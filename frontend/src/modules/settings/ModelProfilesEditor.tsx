import { useEffect, useId, useState } from "react";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import type { ModelProfile, ModelProfileCheck } from "../../bridge/agenthub";
import {
  PROFILE_LIMITS,
  blankProfile,
  checkNotice,
  effortChoicesOf,
  effortLabel,
  profileProblem,
  subagentNotice,
  withProfile,
  type VendorModelChoices,
} from "./modelProfiles";

export type ProfileVendor = { id: string; name: string };

type Props = {
  profiles: readonly ModelProfile[];
  /** By profile id; a profile with no entry has not been checked yet. */
  checks: Readonly<Record<string, ModelProfileCheck>>;
  vendors: readonly ProfileVendor[];
  /** By vendor id: what the form can offer for the vendor it is on. */
  choices: Readonly<Record<string, VendorModelChoices>>;
  saving: boolean;
  /** Why the last save or read failed. */
  error: string;
  /** The form is on this vendor — load its models if they are not there. */
  onVendorWanted: (vendor: string) => void;
  /** Resolves true when the list was stored, so the form can close. */
  onSave: (next: ModelProfile[]) => Promise<boolean>;
  /** Opens with the form already on this profile (the gallery draws it so). */
  initialDraft?: ModelProfile;
};

/** What a session's subagents are made of: a change here is what an open session does not see. */
function subagentsOf(list: readonly ModelProfile[]): string {
  return JSON.stringify(list.filter((p) => p.subagent).map((p) => [p.id, p.vendor, p.model, p.effort, p.description, p.prompt]));
}

/**
 * "Which model for which job" — the person's own list, read by an agent that is
 * about to start a child session (agenthub.listModelProfiles). A profile names
 * a model and an effort and says when to use it; it grants nothing. Choosing by
 * a profile needs no approval, which is why this list is the place to say "the
 * cheap model for review is fine", and why only this window can edit it.
 *
 * Presentational: the container reads and saves, so the gallery can draw every
 * state (`?gallery#agenthub-model-profiles`).
 */
export function ModelProfilesEditor({ profiles, checks, vendors, choices, saving, error, onVendorWanted, onSave, initialDraft }: Props) {
  const [appliedNote, setAppliedNote] = useState(false);
  const [draft, setDraft] = useState<ModelProfile | null>(initialDraft ?? null);
  const [problem, setProblem] = useState("");
  const [removing, setRemoving] = useState<string | null>(null);
  const vendorName = (id: string) => vendors.find((v) => v.id === id)?.name ?? id;

  useEffect(() => {
    if (draft) onVendorWanted(draft.vendor);
    // The form asks for models when it opens or the agent changes — not on
    // every keystroke, which would only repeat the same request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft?.vendor, draft === null]);

  async function save() {
    if (!draft) return;
    const found = profileProblem(draft, profiles);
    setProblem(found);
    if (found) return;
    const next = withProfile(profiles, draft);
    if (await onSave(next)) {
      setDraft(null);
      setAppliedNote(subagentsOf(next) !== subagentsOf(profiles));
    }
  }

  async function remove(id: string) {
    setRemoving(id);
    const next = profiles.filter((p) => p.id !== id);
    if (await onSave(next)) setAppliedNote(subagentsOf(next) !== subagentsOf(profiles));
    setRemoving(null);
  }

  const full = profiles.length >= PROFILE_LIMITS.profiles;

  return (
    <div className="flex flex-col gap-3">
      <p className="text-body-12 text-text-tertiary">
        Tell agents which model fits which job. When an agent starts a child session it reads this list, picks the profile
        that matches the job and says which one it used. A choice made through a profile needs no approval from you; any
        other model an agent picks asks you first. Profiles name a model and an effort and nothing else — they cannot
        grant an agent anything.
      </p>

      {appliedNote ? (
        <p className="rounded-control border border-border-subtle px-3.5 py-2.5 text-body-12 text-text-secondary" role="status">
          Saved. Sessions you start from now on have the subagents above. A session that is already open keeps the ones it started with
          until you open its Environment and choose Reload session.
        </p>
      ) : null}

      {profiles.length === 0 && !draft ? (
        <p className="rounded-control border border-dashed border-border-subtle px-3.5 py-3 text-body-13 text-text-secondary">
          No profiles yet. Without one, a child session runs on its parent's model and effort.
        </p>
      ) : null}

      {profiles.length > 0 ? (
        <ul className="flex flex-col gap-2" aria-label="Model profiles">
          {profiles.map((profile) => {
            const notice = checkNotice(checks[profile.id]);
            const subagent = subagentNotice(profile, checks[profile.id], vendorName(profile.vendor));
            const editing = draft?.id === profile.id;
            return (
              <li
                key={profile.id}
                className="flex flex-col gap-1.5 rounded-control border border-border-subtle bg-surface-raised px-3.5 py-2.5"
                data-editing={editing || undefined}
              >
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-body-13 font-medium text-text-primary">{profile.name}</span>
                  <Badge variant="outline">{vendorName(profile.vendor)}</Badge>
                  {profile.subagent ? <Badge variant="secondary">Subagent</Badge> : null}
                  <span className="text-code-12 text-text-secondary">
                    {profile.model}
                    {profile.effort ? ` · ${effortLabel(profile.effort)}` : ""}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Edit ${profile.name}`}
                    disabled={saving}
                    onClick={() => {
                      setProblem("");
                      setDraft({ ...profile });
                    }}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Delete ${profile.name}`}
                    disabled={saving}
                    onClick={() => void remove(profile.id)}
                  >
                    <Trash2 />
                  </Button>
                </div>
                {profile.description ? <p className="text-body-12 text-text-secondary">{profile.description}</p> : null}
                {notice ? (
                  <p className={notice.tone === "warn" ? "text-body-12 text-status-danger" : "text-body-12 text-text-tertiary"} role={notice.tone === "warn" ? "alert" : undefined}>
                    {notice.text}
                  </p>
                ) : null}
                {subagent ? (
                  <p
                    className={subagent.tone === "warn" ? "text-body-12 text-status-danger" : "text-body-12 text-text-tertiary"}
                    role={subagent.tone === "warn" ? "alert" : undefined}
                    data-subagent={subagent.tone}
                  >
                    {subagent.text}
                  </p>
                ) : null}
                {removing === profile.id ? <p className="text-body-12 text-text-tertiary">Removing…</p> : null}
              </li>
            );
          })}
        </ul>
      ) : null}

      {draft ? (
        <ProfileForm
          draft={draft}
          vendors={vendors}
          choices={choices}
          problem={problem || error}
          saving={saving}
          onChange={(next) => {
            setProblem("");
            setDraft(next);
          }}
          onCancel={() => {
            setProblem("");
            setDraft(null);
          }}
          onSave={() => void save()}
        />
      ) : (
        <div className="flex items-center gap-3">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={saving || full}
            onClick={() => {
              setProblem("");
              setDraft(blankProfile(vendors[0]?.id ?? ""));
            }}
          >
            <Plus />
            Add a profile
          </Button>
          {full ? <span className="text-body-12 text-text-tertiary">At most {PROFILE_LIMITS.profiles} profiles.</span> : null}
        </div>
      )}
      {!draft && error ? <p className="text-body-12 text-status-danger" role="alert">{error}</p> : null}
    </div>
  );
}

function ProfileForm({
  draft,
  vendors,
  choices,
  problem,
  saving,
  onChange,
  onCancel,
  onSave,
}: {
  draft: ModelProfile;
  vendors: readonly ProfileVendor[];
  choices: Readonly<Record<string, VendorModelChoices>>;
  problem: string;
  saving: boolean;
  onChange: (next: ModelProfile) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const prefix = useId();
  const available = choices[draft.vendor] ?? { state: "loading" as const };
  const models = available.state === "ready" ? available.models : [];
  const efforts = effortChoicesOf(models, draft.model);
  const gone = available.state === "ready" && draft.model !== "" && !models.some((m) => m.value === draft.model);
  const set = (patch: Partial<ModelProfile>) => onChange({ ...draft, ...patch });
  const vendorLabel = vendors.find((v) => v.id === draft.vendor)?.name ?? draft.vendor;

  return (
    <form
      className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5"
      aria-label={draft.id ? `Edit ${draft.name}` : "New model profile"}
      onSubmit={(e) => {
        e.preventDefault();
        onSave();
      }}
    >
      <Input
        label="Job"
        value={draft.name}
        maxLength={PROFILE_LIMITS.name}
        placeholder="Code review, Architecture, Documentation…"
        onChange={(e) => set({ name: e.target.value })}
      />

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`${prefix}-desc`} className="text-body-13 text-text-primary">When should an agent use it?</label>
        <Textarea
          id={`${prefix}-desc`}
          value={draft.description}
          maxLength={PROFILE_LIMITS.description}
          placeholder="Reading or reviewing a lot of code; summarising a diff; anything that needs breadth, not depth."
          onChange={(e) => set({ description: e.target.value })}
        />
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div className="flex flex-col gap-1.5">
          <label htmlFor={`${prefix}-agent`} className="text-body-13 text-text-primary">Agent</label>
          <NativeSelect
            id={`${prefix}-agent`}
            size="lg"
            className="w-full"
            value={draft.vendor}
            onChange={(e) => set({ vendor: e.target.value, model: "", effort: "" })}
          >
            {vendors.map((v) => (
              <option key={v.id} value={v.id}>{v.name}</option>
            ))}
          </NativeSelect>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor={`${prefix}-model`} className="text-body-13 text-text-primary">Model</label>
          {available.state === "ready" ? (
            <NativeSelect
              id={`${prefix}-model`}
              size="lg"
              className="w-full"
              value={draft.model}
              onChange={(e) => {
                const next = effortChoicesOf(models, e.target.value);
                set({ model: e.target.value, effort: next.some((c) => c.value === draft.effort) ? draft.effort : "" });
              }}
            >
              <option value="">Choose a model…</option>
              {gone ? <option value={draft.model}>{draft.model} (no longer offered)</option> : null}
              {models.map((m) => (
                <option key={m.value} value={m.value}>{m.name || m.value}</option>
              ))}
            </NativeSelect>
          ) : (
            <Input
              id={`${prefix}-model`}
              value={draft.model}
              placeholder={available.state === "loading" ? "Loading models…" : "Model id"}
              onChange={(e) => set({ model: e.target.value })}
            />
          )}
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor={`${prefix}-effort`} className="text-body-13 text-text-primary">Effort</label>
          {available.state === "ready" ? (
            <NativeSelect
              id={`${prefix}-effort`}
              size="lg"
              className="w-full"
              value={draft.effort}
              disabled={efforts.length === 0 && draft.effort === ""}
              onChange={(e) => set({ effort: e.target.value })}
            >
              <option value="">Model's default</option>
              {draft.effort !== "" && !efforts.some((c) => c.value === draft.effort) ? (
                <option value={draft.effort}>{effortLabel(draft.effort)} (not offered)</option>
              ) : null}
              {efforts.map((c) => (
                <option key={c.value} value={c.value}>{effortLabel(c.value)}</option>
              ))}
            </NativeSelect>
          ) : (
            <Input id={`${prefix}-effort`} value={draft.effort} placeholder="Model's default" onChange={(e) => set({ effort: e.target.value })} />
          )}
        </div>
      </div>

      {available.state === "unavailable" ? (
        <p className="text-body-12 text-text-tertiary" role="status">
          {available.reason} You can still write the profile: type the model id and effort as the agent names them. It is
          checked against the agent's list once that can be read.
        </p>
      ) : null}

      <div className="flex flex-col gap-1.5">
        <label htmlFor={`${prefix}-notes`} className="text-body-13 text-text-primary">Notes</label>
        <Textarea
          id={`${prefix}-notes`}
          value={draft.notes}
          maxLength={PROFILE_LIMITS.notes}
          placeholder="Optional: why this model, what to avoid."
          onChange={(e) => set({ notes: e.target.value })}
        />
      </div>

      <div className="flex flex-col gap-2 rounded-control border border-border-subtle p-3">
        <label className="flex items-center gap-2.5">
          <span className="min-w-0 flex-1 text-body-13 text-text-primary">Also use as a subagent</span>
          <Switch aria-label="Also use as a subagent" checked={draft.subagent} onCheckedChange={(on) => set({ subagent: on })} />
        </label>
        <p className="text-body-12 text-text-tertiary">
          {vendorLabel}'s own agent can then hand a job to this profile, running on the model and effort above with the
          instructions below. It only exists in {vendorLabel} sessions, and only in ones you start after saving (an open
          session keeps what it had until you reload it). It works under the session's own permissions: it can do nothing
          the agent calling it cannot.
          {draft.vendor === "codex" ? " Codex delegates only when you or an AGENTS.md ask it to." : ""}
        </p>
        {draft.subagent ? (
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${prefix}-prompt`} className="text-body-13 text-text-primary">Subagent instructions</label>
            <Textarea
              id={`${prefix}-prompt`}
              value={draft.prompt}
              maxLength={PROFILE_LIMITS.prompt}
              rows={5}
              placeholder="You review diffs. List findings by severity, quote the line, and do not edit files."
              onChange={(e) => set({ prompt: e.target.value })}
            />
            <p className="text-body-12 text-text-tertiary">
              Sent to the subagent exactly as written. The description above is what the calling agent reads to choose it.
            </p>
          </div>
        ) : null}
      </div>

      {problem ? <p className="text-body-12 text-status-danger" role="alert">{problem}</p> : null}

      <div className="flex items-center justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" disabled={saving} onClick={onCancel}>Cancel</Button>
        <Button type="submit" size="sm" disabled={saving}>{saving ? "Saving…" : "Save profile"}</Button>
      </div>
    </form>
  );
}
