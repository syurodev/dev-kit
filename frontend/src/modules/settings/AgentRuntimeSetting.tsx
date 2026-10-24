import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Browser } from "@wailsio/runtime";
import {
  Check,
  ChevronRight,
  Copy,
  Download,
  Loader2,
  RefreshCw,
  RotateCcw,
  Trash2,
  TriangleAlert,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { cn } from "@/lib/utils";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { createProgressBuffer } from "@/lib/progressBuffer";
import { copyText } from "@/bridge/clipboard";
import { errorMessage, isCancelError } from "@/bridge/errors";
import type { CancellableCall } from "@/bridge/client";
import {
  CLAUDE_AGENT,
  agentRuntimeActivate,
  agentRuntimeCheckLatest,
  agentRuntimeInstall,
  agentRuntimeQueryKey,
  agentRuntimeRemove,
  agentRuntimeSetClaudeBinary,
  agentRuntimeStatus,
  formatBytes,
  hintCommands,
  onAgentRuntimeInstallOutput,
  runtimeDetail,
  runtimeStatusLine,
  type AgentRuntimeStatus,
  type ClaudeBinarySource,
  type Requirement,
  compareVersions,
} from "@/bridge/agentRuntime";
import { ClaudeSignInSection } from "@/modules/agenthub/ClaudeSignIn";
import { CodexAccountSection } from "@/modules/agenthub/CodexAccountSection";
import { CLAUDE_INFO, vendorInfo, type VendorInfo } from "@/modules/agenthub/vendors";

// Module-level so Cancel works from whichever surface is on screen — the
// install started in Agent Hub can be stopped from Settings, and the other way.
const runningInstalls = new Map<string, CancellableCall<AgentRuntimeStatus>>();

/**
 * One agent runtime's status, its actions, and the install log — shared by the
 * Settings page and Agent Hub's "not installed" panel so both say the same thing.
 */
export function useAgentRuntime(agent: string = CLAUDE_AGENT) {
  const client = useQueryClient();
  const queryKey = agentRuntimeQueryKey(agent);
  const query = useQuery({ queryKey, queryFn: () => agentRuntimeStatus(agent) });
  const [lines, setLines] = useState<readonly string[]>(EMPTY_ARRAY);

  const mutation = useMutation({
    mutationFn: (action: () => Promise<AgentRuntimeStatus>) => action(),
    onSuccess: (status) => client.setQueryData(queryKey, status),
  });

  // For the hook's whole life, not only while installing: the first lines
  // arrive before the install promise is awaited (same reason as AnalyzerSetup).
  useEffect(() => {
    const buffer = createProgressBuffer<string>((batch) =>
      setLines((prev) => [...prev, ...batch].slice(-400)),
    );
    const off = onAgentRuntimeInstallOutput((from, line) => {
      if (from === agent) buffer.push(line);
    });
    return () => {
      off();
      buffer.dispose();
    };
  }, [agent]);

  function install(version = "") {
    setLines(EMPTY_ARRAY);
    mutation.mutate(() => {
      const call = agentRuntimeInstall(agent, version);
      runningInstalls.set(agent, call);
      client.setQueryData<AgentRuntimeStatus>(queryKey, (s) => (s ? { ...s, installing: true } : s));
      return call.finally(() => {
        runningInstalls.delete(agent);
        void client.invalidateQueries({ queryKey });
      });
    });
  }

  const status = query.data;
  const installing = Boolean(status?.installing);
  const failure = mutation.error && !isCancelError(mutation.error) ? mutation.error : null;

  return {
    status,
    loadError: query.error ? errorMessage(query.error) : "",
    installing,
    busy: installing || mutation.isPending,
    error: failure ? errorMessage(failure) : "",
    lines,
    install,
    cancel: () => runningInstalls.get(agent)?.cancel(),
    canCancel: runningInstalls.has(agent),
    activate: (version: string) => mutation.mutate(() => agentRuntimeActivate(agent, version)),
    remove: (version: string) => mutation.mutate(() => agentRuntimeRemove(agent, version)),
    checkLatest: () => mutation.mutate(() => agentRuntimeCheckLatest(agent)),
    setClaudeBinary: (source: ClaudeBinarySource) =>
      mutation.mutate(() => agentRuntimeSetClaudeBinary(source)),
  };
}

export type AgentRuntimeController = ReturnType<typeof useAgentRuntime>;

/**
 * Settings → Agents → Runtimes: one agent's runtime and sign-in — Claude's by default,
 * any vendor in the table by `vendor`. What differs between them is read from
 * the table: Claude's adapter runs on Node and can use a Claude Code binary
 * of the machine's; Codex is one native binary and signs in inside DevKit.
 */
export function AgentRuntimeSetting({ vendor = CLAUDE_INFO.id }: { vendor?: string } = {}) {
  const info: VendorInfo = vendorInfo(vendor);
  const rt = useAgentRuntime(info.runtimeId);
  const [confirmLatest, setConfirmLatest] = useState(false);
  const [downgrade, setDowngrade] = useState<{ version: string; install: boolean } | null>(null);
  const s = rt.status;

  if (!s) {
    return rt.loadError ? (
      <p className="text-body-12 text-status-danger">{rt.loadError}</p>
    ) : (
      <Skeleton className="h-32 w-full rounded-control" />
    );
  }

  const { headline, latest } = runtimeStatusLine(s, info.runtimeName);
  const { current, previous, testedVersion, latestVersion } = s;
  // opencode has no tested version: it is installed at npm's latest, and there
  // is no "untested" to warn about or to step back to.
  const pinned = testedVersion !== "";
  const machine = s.claudeBinary.machine;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3 rounded-control border border-border-subtle bg-surface-raised p-3.5">
        <div className="flex items-start gap-2.5">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span data-testid="agent-runtime-headline" className="text-body-13-medium text-text-primary">
              {headline}
            </span>
            {latest ? <span className="text-body-12 text-status-warning">{latest}</span> : null}
            <span className="text-caption-11 text-text-tertiary">
              {current
                ? runtimeDetail(current, s.diskBytes)
                : pinned
                  ? `DevKit has tested ${testedVersion}. ${formatBytes(s.diskBytes)} on disk.`
                  : `DevKit installs the latest release. ${formatBytes(s.diskBytes)} on disk.`}
            </span>
          </div>
          {rt.installing ? (
            <Button type="button" variant="outline" size="sm" disabled={!rt.canCancel} onClick={rt.cancel}>
              <X className="size-3.5" strokeWidth={1.5} />
              Cancel install
            </Button>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {!current ? (
            <Button type="button" size="sm" disabled={rt.busy} onClick={() => rt.install()}>
              {rt.installing ? <Loader2 className="animate-spin" /> : <Download />}
              {rt.installing ? "Installing…" : pinned ? `Install ${testedVersion}` : "Install latest"}
            </Button>
          ) : null}
          {current && pinned && current.version !== testedVersion ? (
            <Button
              type="button"
              size="sm"
              disabled={rt.busy}
              onClick={() => {
                if (compareVersions(current.version, testedVersion) > 0) {
                  setDowngrade({ version: testedVersion, install: true });
                } else {
                  rt.install();
                }
              }}
            >
              <Download />
              {/* Off an untested newer version this is a step back, and says so. */}
              {compareVersions(current.version, testedVersion) > 0
                ? `Switch back to ${testedVersion} (tested)`
                : `Update to ${testedVersion} (tested)`}
            </Button>
          ) : null}
          {current ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={rt.busy}
              onClick={() => rt.install(current.version)}
            >
              <RefreshCw />
              Reinstall
            </Button>
          ) : null}
          {latest && latestVersion ? (
            <Button
              type="button"
              variant={pinned ? "outline" : "default"}
              size="sm"
              disabled={rt.busy}
              onClick={() => (pinned ? setConfirmLatest(true) : rt.install(latestVersion))}
            >
              {pinned ? `Install ${latestVersion} (untested)` : `Update to ${latestVersion}`}
            </Button>
          ) : null}
          {previous ? (
            <>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={rt.busy}
                onClick={() => {
                  if (current && compareVersions(previous.version, current.version) < 0) {
                    setDowngrade({ version: previous.version, install: false });
                  } else rt.activate(previous.version);
                }}
              >
                <RotateCcw />
                {`Switch back to ${previous.version}`}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={rt.busy}
                onClick={() => rt.remove(previous.version)}
              >
                <Trash2 />
                {`Remove ${previous.version}`}
              </Button>
            </>
          ) : null}
          <Button type="button" variant="ghost" size="sm" disabled={rt.busy} onClick={rt.checkLatest}>
            Check for updates
          </Button>
        </div>

        {rt.error ? <p className="text-body-12 text-status-danger">{rt.error}</p> : null}
        <RuntimeInstallLog lines={rt.lines} installing={rt.installing} failed={Boolean(rt.error)} />
      </div>

      <div className="flex flex-col gap-2.5">
        <SubLabel>RUNTIME</SubLabel>
 <p className="text-caption-11 text-text-tertiary">DevKit downloads Node and npm when installing an agent. No global CLI or Node installation is required. Reinstall an older agent installation to switch it to managed Node.</p>
        <RuntimeRequirements requirements={s.requirements} />
      </div>

      {info.id === CLAUDE_INFO.id ? (
        <div className="flex flex-col gap-2.5">
          <SubLabel>CLAUDE CODE</SubLabel>
          <RadioGroup
            value={s.claudeBinary.source}
            disabled={rt.busy}
            onValueChange={(value) => rt.setClaudeBinary(value as ClaudeBinarySource)}
            className="gap-3"
          >
            <label className="flex cursor-pointer items-start gap-2.5">
              <RadioGroupItem value="bundled" className="mt-0.5" />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-body-13 text-text-primary">Bundled with the agent (recommended)</span>
                <span className="text-caption-11 text-text-tertiary">
                  Always the Claude Code version the agent&apos;s SDK was built with.
                </span>
              </span>
            </label>
            <label className={cn("flex items-start gap-2.5", machine ? "cursor-pointer" : "opacity-60")}>
              <RadioGroupItem value="machine" disabled={!machine} className="mt-0.5" />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-body-13 text-text-primary">Claude Code on this machine</span>
                <span className="truncate text-code-11 text-text-tertiary" title={machine?.path}>
                  {machine ? `${machine.path} · ${machine.version}` : "Not found"}
                </span>
                <span className="text-caption-11 text-text-tertiary">
                  Its version may not match the agent&apos;s SDK.
                </span>
              </span>
            </label>
          </RadioGroup>
        </div>
      ) : null}

      {current && info.signIn !== "none" ? (
        <div className="flex flex-col gap-2.5">
          <SubLabel>SIGN IN</SubLabel>
          {info.signIn === "inApp" ? <CodexAccountSection /> : <ClaudeSignInSection vendor={info.id} />}
        </div>
      ) : null}

      <AlertDialog
        open={downgrade !== null}
        onOpenChange={(open) => {
          if (!open) setDowngrade(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{`Switch to ${downgrade?.version ?? "an older version"}?`}</AlertDialogTitle>
            <AlertDialogDescription>
              Existing running sessions keep their runtime. An older runtime may not be able to resume sessions saved by
              a newer version. Your session data will be kept.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (downgrade) {
                  if (downgrade.install) rt.install(downgrade.version);
                  else rt.activate(downgrade.version);
                }
                setDowngrade(null);
              }}
            >
              Switch version
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={confirmLatest} onOpenChange={setConfirmLatest}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{`Install ${latestVersion ?? "the latest version"}?`}</AlertDialogTitle>
            <AlertDialogDescription>
              {`DevKit has not tested this version; it was tested with ${testedVersion}. Its dependencies are resolved at install time. Older versions may not read sessions saved by newer versions.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {s.licenseUrl ? (
            <button
              type="button"
              onClick={() => void Browser.OpenURL(s.licenseUrl!)}
              className="self-start text-body-12 text-text-primary underline underline-offset-2"
            >
              Read the license
            </button>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => latestVersion && rt.install(latestVersion)}>
              Install untested
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function SubLabel({ children }: { children: React.ReactNode }) {
  return <p className="text-label-11-caps text-text-tertiary">{children}</p>;
}

/** Node and npm: what was found and where, or what is wrong and how to fix it. */
export function RuntimeRequirements({ requirements }: { requirements: Requirement[] }) {
  return (
    <ul className="flex flex-col gap-2 text-left">
      {requirements.map((r) => (
        <li key={r.name} className="flex items-start gap-2.5">
          {r.met ? (
            <Check className="mt-0.5 size-4 shrink-0 text-status-success" />
          ) : (
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-status-warning" />
          )}
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="text-body-13 text-text-primary">
              {r.name}
              {r.met && r.found ? ` ${r.found}` : ""}
              {!r.met && r.problem ? ` — ${r.problem}` : ""}
            </span>
            {r.met && r.path ? (
              <span className="truncate text-code-11 text-text-tertiary" title={r.path}>
                {r.path}
              </span>
            ) : null}
            {!r.met && r.installHint ? (
              <>
                <span className="text-body-12 text-text-tertiary">{r.installHint.replace(/`/g, "")}</span>
                {hintCommands(r.installHint).map((command) => (
                  <CopyCommand key={command} command={command} />
                ))}
              </>
            ) : null}
          </div>
        </li>
      ))}
    </ul>
  );
}

function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <span className="mt-1 flex items-center gap-2 rounded-md border border-border-subtle bg-bg-canvas py-1 pr-1 pl-2.5">
      <code className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">{command}</code>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        onClick={() => {
          copyText(command)
            .then(() => {
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1500);
            })
            // Clipboard can be unavailable in a webview; the command is selectable.
            .catch(() => {});
        }}
      >
        {copied ? <Check className="text-status-success" /> : <Copy />}
        {copied ? "Copied" : "Copy"}
      </Button>
    </span>
  );
}

/** npm's output, collapsible: open while installing and after a failure, kept until the next install. */
export function RuntimeInstallLog({
  lines,
  installing,
  failed,
}: {
  lines: readonly string[];
  installing: boolean;
  failed: boolean;
}) {
  const [open, setOpen] = useState(false);
  const logRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (installing || failed) setOpen(true);
  }, [installing, failed]);

  // Follow the tail — the only part of an install log anyone watches.
  useEffect(() => {
    const log = logRef.current;
    if (log) log.scrollTop = log.scrollHeight;
  }, [lines, open]);

  if (lines.length === 0 && !installing) return null;

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="w-full text-left">
      <CollapsibleTrigger className="flex items-center gap-1.5 text-caption-11 text-text-tertiary hover:text-text-primary">
        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        Install log
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre
          ref={logRef}
          className="mt-2 max-h-64 overflow-auto rounded-md border border-border-subtle bg-bg-canvas p-3 text-code-11 whitespace-pre-wrap text-text-secondary"
        >
          {lines.length > 0 ? lines.join("\n") : "Waiting for npm…"}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}
