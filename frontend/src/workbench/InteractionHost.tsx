import { lazy, Suspense, useEffect, useState } from "react";
import {
  accountAnswer,
  completeInteraction,
  denyInteraction,
  getInteractionPendingList,
  type PendingInteraction,
} from "../bridge/interaction";
import { listProfiles, setDBActiveProfile } from "../bridge/database";
import { listSSHProfiles, setSSHActiveProfile } from "../bridge/ssh";
import { connectKafka, listKafkaProfiles } from "../bridge/kafka";
import { errorMessage } from "../bridge/errors";
import type { MCPProfileOption } from "./MCPInteractionDialog";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { Button } from "../components/ui/button";
import { Checkbox } from "../components/ui/checkbox";
import { Field, FieldContent, FieldDescription, FieldLabel, FieldTitle } from "../components/ui/field";
import { RadioGroup, RadioGroupItem } from "../components/ui/radio-group";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";

const POLL_MS = 500;

const HarnessScopeDialog = lazy(() => import("./HarnessScopeDialog").then((module) => ({ default: module.HarnessScopeDialog })));
const MCPInteractionDialog = lazy(() => import("./MCPInteractionDialog").then((module) => ({ default: module.MCPInteractionDialog })));

type ProfileOption = MCPProfileOption;

async function loadProfiles(kind: PendingInteraction["kind"]): Promise<ProfileOption[]> {
  if (kind === "ssh") {
    return (await listSSHProfiles()).map((p) => ({ id: p.id, name: p.name, env: p.env, detail: `${p.host}:${p.port}` }));
  }
  if (kind === "database") {
    return (await listProfiles()).map((p) => ({ id: p.id, name: p.name, env: p.env, detail: [p.database, `${p.host}:${p.port}`].filter(Boolean).join(" · ") }));
  }
  if (kind === "kafka") {
    return (await listKafkaProfiles()).map((p) => ({ id: p.id, name: p.name, env: p.env, detail: p.bootstrapServers.join(", ") }));
  }
  return [];
}

async function activateProfile(
  kind: PendingInteraction["kind"],
  profileId: string,
): Promise<void> {
  if (kind === "ssh") {
    await setSSHActiveProfile(profileId);
    return;
  }
  if (kind === "database") {
    await setDBActiveProfile(profileId);
    return;
  }
  if (kind === "kafka") {
    await connectKafka(profileId);
  }
}

export function InteractionHost() {
  const [queue, setQueue] = useState<PendingInteraction[]>([]);
  const pending = queue[0] ?? null;
  const [profilesLoading, setProfilesLoading] = useState(false);
  const requester = useAgentSessionsStore((state) => state.sessionsById[pending?.caller.replace(/^agent:/, "") ?? ""]);
  const [profiles, setProfiles] = useState<ProfileOption[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [accountId, setAccountId] = useState("");
  const [remember, setRemember] = useState(true);

  useEffect(() => {
    let mounted = true;
    const tick = async () => {
      try {
        const next = await getInteractionPendingList();
        if (!mounted) {
          return;
        }
        setQueue((prev) => {
          const same =
            prev.length === next.length && prev.every((p, i) => p.id === next[i]?.id);
          return same ? prev : next;
        });
      } catch {
        // Ignore transient poll errors; next tick retries.
      }
    };
    void tick();
    const timer = window.setInterval(() => {
      void tick();
    }, POLL_MS);
    return () => {
      mounted = false;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    if (!pending || pending.type !== "select") {
      setProfiles([]);
      setSelectedId("");
      setError("");
      return;
    }
    let cancelled = false;
    setError("");
    setProfiles([]);
    setSelectedId("");
    setProfilesLoading(true);
    void loadProfiles(pending.kind)
      .then((list) => {
        if (cancelled) {
          return;
        }
        setProfiles(list);
        setSelectedId(list[0]?.id ?? "");
      })
      .catch((e) => {
        if (!cancelled) {
          setError(errorMessage(e));
        }
      })
      .finally(() => { if (!cancelled) setProfilesLoading(false); });
    return () => {
      cancelled = true;
    };
  }, [pending?.id, pending?.type, pending?.kind]);

  // A new question starts from its first option, with "remember" on when the
  // question offers it: the usual answer is "this organisation, always".
  useEffect(() => {
    setAccountId(pending?.options[0]?.id ?? "");
    setRemember(true);
  }, [pending?.id]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
      setQueue((prev) => prev.filter((p) => p.id !== pending?.id));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  if (!pending) {
    return null;
  }

  const open = true;

  if (pending.type === "select" && pending.kind === "harness_scope") {
    return (
      <Suspense fallback={null}>
        <HarnessScopeDialog
          key={pending.id}
          pending={pending}
          busy={busy}
          error={error}
          waiting={queue.length - 1}
          onCancel={() => void run(() => denyInteraction(pending.id))}
          onChoose={(scope) => void run(() => completeInteraction(pending.id, scope))}
        />
      </Suspense>
    );
  }

  if (pending.type === "select" && pending.kind === "harness_group_request") {
    return (
      <Suspense fallback={null}>
        <HarnessScopeDialog
          key={pending.id}
          pending={pending}
          busy={busy}
          error={error}
          waiting={queue.length - 1}
          title="A session asks for more tool groups"
          fallbackSummary="A session asks to turn on tool groups for itself."
          choiceLabel="Your answer"
          confirmLabel="Continue"
          onCancel={() => void run(() => denyInteraction(pending.id))}
          onChoose={(answer) => void run(() => completeInteraction(pending.id, answer))}
        />
      </Suspense>
    );
  }

  if (pending.type === "select" && pending.kind === "git_account") {
    return (
      <Dialog open={open} onOpenChange={() => {}}>
        <DialogContent
          showCloseButton={false}
          className="sm:max-w-md"
          aria-describedby="interaction-account-desc"
        >
          <DialogHeader>
            <DialogTitle>Choose an account</DialogTitle>
            <DialogDescription id="interaction-account-desc">
              {pending.summary || "More than one account could be used here."}
            </DialogDescription>
            <p className="text-caption-11 text-text-tertiary">
              Requested by <code>{pending.caller || "unknown"}</code>
              {queue.length > 1 ? ` · ${queue.length - 1} more waiting` : ""}
            </p>
          </DialogHeader>
          <RadioGroup
            aria-label="interaction-account"
            value={accountId}
            onValueChange={setAccountId}
            disabled={busy}
          >
            {pending.options.map((option) => (
              <FieldLabel key={option.id} htmlFor={`account-${option.id}`}>
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>{option.label}</FieldTitle>
                    <FieldDescription>
                      {[option.provider, option.subject].filter(Boolean).join(" · ")}
                    </FieldDescription>
                  </FieldContent>
                  <RadioGroupItem value={option.id} id={`account-${option.id}`} />
                </Field>
              </FieldLabel>
            ))}
          </RadioGroup>
          {pending.remember ? (
            <Field orientation="horizontal">
              <Checkbox
                id="account-remember"
                checked={remember}
                disabled={busy}
                onCheckedChange={(checked) => setRemember(checked === true)}
              />
              <FieldLabel htmlFor="account-remember">Use this for {pending.remember}</FieldLabel>
            </Field>
          ) : null}
          {error ? <p role="alert">{error}</p> : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => run(() => denyInteraction(pending.id))}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={busy || !accountId}
              onClick={() =>
                run(() =>
                  completeInteraction(
                    pending.id,
                    accountAnswer(accountId, remember && Boolean(pending.remember)),
                  ),
                )
              }
            >
              Use account
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Suspense fallback={null}>
      <MCPInteractionDialog
        key={pending.id}
        pending={pending}
        profiles={profiles}
        selectedId={selectedId}
        onSelect={setSelectedId}
        loading={pending.type === "select" && profilesLoading}
        busy={busy}
        error={error}
        waiting={queue.length - 1}
        callerName={requester?.title || requester?.projectPath.split("/").pop()}
        callerVendor={requester?.agentType}
        onDeny={() => void run(() => denyInteraction(pending.id))}
        onAllow={(always) => void run(async () => {
          if (pending.type === "select") {
            await activateProfile(pending.kind, selectedId);
            await completeInteraction(pending.id, selectedId);
          } else {
            await completeInteraction(pending.id, always ? "allow_always" : "allow");
          }
        })}
      />
    </Suspense>
  );
}
