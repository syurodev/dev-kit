import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Browser } from "@wailsio/runtime";
import { ArrowRight, Check, Copy, ExternalLink, Link2, Loader2, Send } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import { errorMessage } from "../../bridge/errors";
import { listWorkspaces } from "../../bridge/workspace";
import { telegramStatus, configureTelegram, beginTelegramPair, acceptTelegramPair, unpairTelegram, type TelegramConfig } from "../../bridge/telegram";

const LIVE_API = { status: telegramStatus, configure: configureTelegram, beginPair: beginTelegramPair, acceptPair: acceptTelegramPair, unpair: unpairTelegram, open: (url: string) => Browser.OpenURL(url) };
export type TelegramSettingsAPI = typeof LIVE_API;
type Step = 1 | 2 | 3;
const STEPS = ["Connect bot", "Choose access", "Pair account"] as const;

/** The injected API is for the bridge-free gallery; the product uses LIVE_API. */
export function TelegramSetting({ api = LIVE_API, loadWorkspaces = listWorkspaces }: { api?: TelegramSettingsAPI; loadWorkspaces?: typeof listWorkspaces }) {
  const client = useQueryClient();
  const key = ["settings", "telegram", api === LIVE_API ? "live" : "preview"];
  const { data, error } = useQuery({ queryKey: key, queryFn: api.status, refetchInterval: 2000 });
  const { data: workspaces, error: workspaceError } = useQuery({ queryKey: ["telegram", "workspaces", api === LIVE_API ? "live" : "preview"], queryFn: loadWorkspaces });
  const [draft, setDraft] = useState<TelegramConfig | null>(null);
  const [stepOverride, setStep] = useState<Step | null>(null);
  const [token, setToken] = useState("");
  const [pairLink, setPairLink] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState("");
  const section = useRef<HTMLDivElement>(null);
  const [copied, setCopied] = useState(false);
  const cfg = draft ?? data?.config;
  const step = stepOverride ?? (!data?.config.accountId ? 1 : !(data.config.workspaces?.length || data.config.approveInteractions) ? 2 : 3);
  const connected = Boolean(data?.paired && stepOverride === null);
  useEffect(() => {
    const region = section.current?.closest('[role="region"]');
    if (region) region.scrollTop = 0;
    section.current?.querySelector<HTMLElement>('[data-setup-heading]')?.focus({ preventScroll: true });
  }, [step, connected]);
  async function perform(action: () => Promise<void>) {
    setBusy(true); setFailed("");
    try { await action(); }
    catch (cause) { setFailed(errorMessage(cause)); }
    finally { setBusy(false); }
  }
  async function refresh() {
    await client.cancelQueries({ queryKey: key });
    await client.fetchQuery({ queryKey: key, queryFn: api.status, staleTime: 0 });
  }
  async function save(next: TelegramConfig, nextToken = "") {
    await api.configure(next, nextToken);
    await refresh();
    setToken(""); setDraft(null); setPairLink("");
  }
  const message = failed || (error ? errorMessage(error) : "") || data?.error;
  if (!cfg) return <p role="status" className="text-body-13 text-text-tertiary">{message || "Loading remote control…"}</p>;

  function permission(field: "sendDetails" | "readTranscript" | "approveInteractions", title: string, description: string) {
    return <label className="flex cursor-pointer items-start gap-3 py-3">
      <span className="min-w-0 flex-1"><span className="block text-body-13 text-text-primary">{title}</span><span className="mt-1 block text-body-12 text-text-tertiary">{description}</span></span>
      <Switch className="mt-0.5 shrink-0" checked={cfg?.[field] ?? false} disabled={busy || (field === "approveInteractions" && !cfg?.sendDetails && !cfg?.approveInteractions)} onCheckedChange={(value) => setDraft({ ...cfg!, [field]: value, ...(field === "sendDetails" && !value ? { approveInteractions: false } : {}) })} />
    </label>;
  }
  const access = <div className="space-y-5">
    <div>
      <h3 className="text-body-13 font-medium">Workspaces</h3>
      <p className="mt-1 text-body-12 text-text-tertiary">Select where Telegram can create sessions, send prompts and stop agents.</p>
      <div className="mt-3 divide-y divide-border-subtle rounded-lg border border-border-subtle px-3">
        {(workspaces ?? []).map((w) => <label key={w.id} className="flex cursor-pointer items-center gap-3 py-3">
          <Checkbox checked={(cfg.workspaces ?? []).includes(w.id)} disabled={busy} onCheckedChange={(checked) => setDraft({ ...cfg, workspaces: checked === true ? [...(cfg.workspaces ?? []), w.id] : (cfg.workspaces ?? []).filter((id) => id !== w.id) })} />
          <span className="min-w-0"><span className="block text-body-13">{w.name}</span><span className="block truncate text-body-12 text-text-tertiary">{w.root}</span></span>
        </label>)}
        {!workspaces?.length && <p className="py-3 text-body-12 text-text-tertiary">{workspaceError ? errorMessage(workspaceError) : workspaces ? "No workspaces yet. Add one from the workspace menu, then return here." : "Loading workspaces…"}</p>}
      </div>
    </div>
    <div><h3 className="text-body-13 font-medium">What Telegram can access</h3>
      <div className="mt-1 divide-y divide-border-subtle">
        {permission("sendDetails", "Review requests and answer questions", "Send tool details and agent questions from the selected workspaces. Approvals apply once.")}
        {permission("readTranscript", "Read conversations", "Read session messages and tool results from the selected workspaces.")}
        {permission("approveInteractions", "Review desktop and terminal confirmations", "Applies across this computer, including production operations and guarded Git actions. Requires request details above.")}
      </div>
      <p className="mt-2 text-body-12 text-text-tertiary">Enabled content is sent through Telegram. DevKit must stay running with the vault unlocked.</p>
    </div>
  </div>;

  const continueButton = (label: string, action: () => Promise<void>, disabled = false) => <Button disabled={busy || disabled} onClick={() => void perform(action)}>{busy ? <Loader2 className="size-4 animate-spin" /> : null}{label}{!busy && <ArrowRight className="size-4" />}</Button>;
  const commands = <details className="border-t border-border-subtle pt-4 text-body-12 text-text-secondary">
    <summary className="cursor-pointer text-body-13">Telegram commands</summary>
    <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-5 gap-y-2">
      <dt className="font-mono">/sessions</dt><dd>Select a conversation</dd>
      <dt className="font-mono">/read · /more · /older</dt><dd>Read messages and history</dd>
      <dt className="font-mono">/models</dt><dd>Choose a model</dd>
      <dt className="font-mono">/effort</dt><dd>Choose reasoning effort</dd>
      <dt className="font-mono">/new</dt><dd>Create and configure a session</dd>
      <dt className="font-mono">/stop</dt><dd>Stop the selected agent</dd>
      <dt className="font-mono">/help</dt><dd>See all commands</dd>
    </dl><p className="mt-3">Send a message to prompt the selected agent. Reply to a question message to answer it.</p>
  </details>;

  return <div ref={section} className="space-y-5 text-text-primary">
    <div className="flex items-start gap-3"><span className="rounded-lg border border-border-subtle bg-surface-raised p-2"><Send className="size-5 text-text-secondary" /></span><div className="min-w-0 flex-1"><h3 className="text-body-14-medium">Telegram</h3><p className="mt-1 text-body-13 text-text-tertiary">Control this DevKit from your private Telegram chat.</p></div>{data?.paired && <span className="rounded-full bg-state-soft px-2 py-1 text-body-12 text-text-secondary">{data.config.enabled ? "Connected" : "Paused"}</span>}</div>
    {!connected && <ol aria-label="Telegram setup progress" className="grid grid-cols-3 gap-2">
      {STEPS.map((label, index) => <li key={label} aria-current={index + 1 === step ? "step" : undefined} className={cn("flex items-center gap-2 border-b-2 pb-3 text-body-12", index + 1 === step ? "border-action-primary text-text-primary" : "border-border-subtle text-text-tertiary")}><span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-state-soft text-caption-11">{index + 1 < step ? <Check className="size-3" /> : index + 1}</span><span>{label}</span></li>)}
    </ol>}
    <div className="space-y-4 rounded-xl border border-border-subtle bg-surface-raised p-4">
      {connected ? <>
        <label className="flex items-start gap-3"><span className="min-w-0 flex-1"><span className="block text-body-13 font-medium">Remote control</span><span className="mt-1 block text-body-12 text-text-tertiary">{data?.botName ? `@${data.botName} · ` : ""}Paired with Telegram user {data?.userId}</span></span><Switch checked={data?.config.enabled ?? false} disabled={busy || draft !== null} onCheckedChange={(enabled) => void perform(() => save({ ...data!.config, enabled }))} /></label>
        {access}
        {draft !== null && <p className="text-body-12 text-text-tertiary">Save access changes before pausing or resuming remote control.</p>}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border-subtle pt-4"><Button disabled={busy || draft === null} onClick={() => void perform(() => save(cfg))}>Save changes</Button><Button variant="ghost" disabled={busy} onClick={() => void perform(async () => { await api.unpair(); setDraft(null); await refresh(); setPairLink(""); setStep(3); })}>Unpair account</Button></div>
        <details className="text-body-12 text-text-tertiary"><summary className="cursor-pointer">Change bot</summary><p className="mt-2">Changing the bot removes the current pairing.</p><Button variant="outline" size="sm" className="mt-3" onClick={() => { setToken(""); setStep(1); }}>Replace bot token</Button></details>
      </> : step === 1 ? <>
        <div><h3 data-setup-heading tabIndex={-1} className="text-body-14-medium outline-none">1. Connect a bot</h3><p className="mt-1 text-body-13 text-text-tertiary">Use a dedicated bot for this computer.</p></div>
        <ol className="space-y-2 text-body-13 text-text-secondary">
          <li className="flex items-start gap-3"><span className="text-text-tertiary">1</span><span>Open <button type="button" className="text-text-primary underline underline-offset-4" onClick={() => void perform(() => api.open("https://t.me/BotFather"))}>BotFather <ExternalLink className="inline size-3" /></button> in Telegram and send <code className="font-mono text-text-primary">/newbot</code>.</span></li>
          <li className="flex items-start gap-3"><span className="text-text-tertiary">2</span><span>Choose a name and username, then copy the token BotFather gives you.</span></li>
          <li className="flex items-start gap-3"><span className="text-text-tertiary">3</span><span>Paste the token below. DevKit will check the bot before continuing.</span></li>
        </ol>
        <label className="block space-y-2 text-body-13"><span>Bot token</span><Input type="password" autoComplete="new-password" value={token} disabled={busy} onChange={(e) => setToken(e.target.value)} placeholder={cfg.accountId ? "Token saved · enter a new one to replace it" : "Paste your bot token"} /></label>
        <p className="text-body-12 text-text-tertiary">Stored encrypted on this computer. The token is never shown again.</p>
        <div className="flex flex-wrap justify-end gap-2 border-t border-border-subtle pt-3">{data?.paired && <Button variant="ghost" disabled={busy} onClick={() => { setStep(null); setToken(""); }}>Cancel</Button>}{continueButton("Connect bot", async () => { await save({ ...cfg, enabled: true }, token.trim()); setStep(2); }, !token.trim() && (!cfg.accountId || Boolean(data?.paired)))}</div>
      </> : step === 2 ? <>
        <div><h3 data-setup-heading tabIndex={-1} className="text-body-14-medium outline-none">2. Choose access</h3><p className="mt-1 text-body-13 text-text-tertiary">Choose workspaces and what you want to do from Telegram.</p></div>
        {access}
        <div className="flex flex-wrap justify-between gap-2 border-t border-border-subtle pt-3"><Button variant="ghost" disabled={busy} onClick={() => setStep(1)}>Back</Button>{continueButton("Continue to pairing", async () => { await save({ ...cfg, enabled: true }); setStep(3); }, !(cfg.workspaces?.length || cfg.approveInteractions) || Boolean(cfg.approveInteractions && !cfg.sendDetails))}</div>
      </> : <>
        <div><h3 data-setup-heading tabIndex={-1} className="text-body-14-medium outline-none">3. Pair your account</h3><p className="mt-1 text-body-13 text-text-tertiary">Only the Telegram account you confirm here can control DevKit.</p></div>
        <ol className="space-y-4 text-body-13 text-text-secondary">
          <li className="flex gap-3"><span className="text-text-tertiary">1</span><div className="min-w-0 flex-1"><p>Create a pairing link, then open it in Telegram.</p><div className="mt-3 flex flex-wrap gap-2"><Button disabled={busy} onClick={() => void perform(async () => { if (!data?.config.enabled) await save({ ...cfg, enabled: true }); setPairLink(await api.beginPair()); setCopied(false); })}>{pairLink ? "Create new link" : "Create pairing link"}<Link2 className="size-4" /></Button>{pairLink && <><Button variant="outline" disabled={busy} onClick={() => void perform(() => api.open(pairLink))}>Open Telegram<ExternalLink className="size-4" /></Button><Button variant="ghost" disabled={busy} onClick={() => void perform(async () => { await navigator.clipboard.writeText(pairLink); setCopied(true); })}><Copy className="size-4" />{copied ? "Copied" : "Copy link"}</Button></>}</div>{pairLink && <p className="mt-2 text-body-12 text-text-tertiary">The link expires after 5 minutes. You can create a new one here.</p>}</div></li>
          <li className="flex gap-3"><span className="text-text-tertiary">2</span><div><p>Tap <strong className="font-medium text-text-primary">Start</strong> in the bot chat.</p>{pairLink && !data?.candidateId && <p role="status" className="mt-2 flex items-center gap-2 text-body-12 text-text-tertiary"><Loader2 className="size-3 animate-spin" />Waiting for your Telegram account…</p>}</div></li>
          <li className="flex gap-3"><span className="text-text-tertiary">3</span><div className="min-w-0 flex-1"><p>Return here and confirm that the account is yours.</p>{data?.candidateId ? <div className="mt-3 space-y-3 rounded-lg border border-border-subtle bg-surface-base p-3"><p className="text-text-primary">{data.candidateName ? `@${data.candidateName}` : "Telegram account"}<span className="mt-1 block text-body-12 text-text-tertiary">User ID: {data.candidateId}</span></p><Button disabled={busy} onClick={() => void perform(async () => { await api.acceptPair(data.candidateId); await refresh(); setPairLink(""); setStep(null); })}><Check className="size-4" />Confirm my account</Button></div> : null}</div></li>
        </ol>
        <div className="border-t border-border-subtle pt-3"><Button variant="ghost" disabled={busy} onClick={() => { setPairLink(""); setStep(2); }}>Back to access</Button></div>
      </>}
    </div>
    {message && <p role="alert" className="text-body-12 text-status-danger">{message}</p>}
    {(connected || step === 3) && commands}
  </div>;
}
