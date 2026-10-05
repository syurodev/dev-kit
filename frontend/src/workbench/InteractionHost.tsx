import { useEffect, useState } from "react";
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
import { Badge } from "../components/ui/badge";
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

type ProfileOption = { id: string; name: string };

function envBadgeClass(env: string): string {
  const key = env.trim().toLowerCase();
  if (key === "prod" || key === "production") {
    return "border-transparent bg-red-600/20 text-red-300";
  }
  if (key === "stg" || key === "staging") {
    return "border-transparent bg-amber-500/20 text-amber-200";
  }
  if (key === "dev" || key === "development") {
    return "border-transparent bg-emerald-500/20 text-emerald-200";
  }
  return "border-transparent bg-slate-500/20 text-slate-200";
}

async function loadProfiles(kind: PendingInteraction["kind"]): Promise<ProfileOption[]> {
  if (kind === "ssh") {
    return (await listSSHProfiles()).map((p) => ({ id: p.id, name: p.name }));
  }
  if (kind === "database") {
    return (await listProfiles()).map((p) => ({ id: p.id, name: p.name }));
  }
  if (kind === "kafka") {
    return (await listKafkaProfiles()).map((p) => ({ id: p.id, name: p.name }));
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
      });
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

  if (pending.type === "select") {
    return (
      <Dialog open={open} onOpenChange={() => {}}>
        <DialogContent
          showCloseButton={false}
          className="sm:max-w-md"
          aria-describedby="interaction-select-desc"
        >
          <DialogHeader>
            <DialogTitle>Select {pending.kind || "profile"}</DialogTitle>
            <DialogDescription id="interaction-select-desc">
              An MCP tool needs an active {pending.kind || "connection"} profile.
            </DialogDescription>
            <p className="text-caption-11 text-text-tertiary">
              Requested by <code>{pending.caller || "unknown"}</code>
              {queue.length > 1 ? ` · ${queue.length - 1} more waiting` : ""}
            </p>
          </DialogHeader>
          {profiles.length === 0 ? (
            <p role="status">No profiles available. Create one in the module view first.</p>
          ) : (
            <label className="grid gap-2 text-body-13">
              <span>Profile</span>
              <select
                aria-label="interaction-profile"
                value={selectedId}
                disabled={busy}
                onChange={(e) => setSelectedId(e.target.value)}
                className="rounded-md border border-border bg-background px-3 py-2"
              >
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          )}
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
              disabled={busy || !selectedId}
              onClick={() =>
                run(async () => {
                  await activateProfile(pending.kind, selectedId);
                  await completeInteraction(pending.id, selectedId);
                })
              }
            >
              Use profile
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog open={open} onOpenChange={() => {}}>
      <DialogContent
        showCloseButton={false}
        className="sm:max-w-md"
        aria-describedby="interaction-confirm-desc"
      >
        <DialogHeader>
          <DialogTitle>Confirm MCP action</DialogTitle>
          <DialogDescription id="interaction-confirm-desc">
            Review the environment and tool before allowing this request.
          </DialogDescription>
          <p className="text-caption-11 text-text-tertiary">
            Requested by <code>{pending.caller || "unknown"}</code>
            {queue.length > 1 ? ` · ${queue.length - 1} more waiting` : ""}
          </p>
        </DialogHeader>
        <div className="grid gap-3 text-body-13">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Environment</span>
            <Badge className={envBadgeClass(pending.env)}>{pending.env || "unknown"}</Badge>
          </div>
          <div>
            <div className="text-muted-foreground">Tool</div>
            <code>{pending.tool || "unknown"}</code>
          </div>
          <div>
            <div className="text-muted-foreground">Summary</div>
            <p className="whitespace-pre-wrap break-words">{pending.summary || "—"}</p>
          </div>
        </div>
        {error ? <p role="alert">{error}</p> : null}
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => run(() => denyInteraction(pending.id))}
          >
            Deny
          </Button>
          {pending.allowAlwaysOffered ? (
            <Button
              type="button"
              variant="secondary"
              disabled={busy}
              onClick={() => run(() => completeInteraction(pending.id, "allow_always"))}
            >
              Allow always
            </Button>
          ) : null}
          <Button
            type="button"
            disabled={busy}
            onClick={() => run(() => completeInteraction(pending.id, "allow"))}
          >
            Allow
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
