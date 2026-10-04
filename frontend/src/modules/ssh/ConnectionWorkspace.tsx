import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Copy,
  Network,
  Play,
  ShieldCheck,
  Terminal,
  Trash2,
  Pencil,
  Bot,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogHeader,
  DialogFooter,
} from "@/components/ui/dialog";
import { errorMessage } from "@/bridge/errors";
import {
  getSSHActiveProfile,
  listSSHForwards,
  observeSSHHostKey,
  trustSSHHostKey,
  testSSHConnection,
  runSSHCommand,
  startSSHForward,
  stopSSHForward,
  startSSHTerminal,
  setSSHActiveProfile,
  type SSHProfile,
  type SSHForwardSpec,
} from "@/bridge/ssh";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { Field } from "./ConnectionDialog";
import { InlineFeedback, FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { notify } from "@/lib/notify";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useSSHStore } from "./sshStore";
import { useSSHWorkspaceState, initialWorkspace, runSSHAction, type SSHPage, type FeedbackScope } from "./workspaceState";
import { validateForward } from "./validation";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { cn } from "@/lib/utils";

export function closeSSHConnectionTabs(profileId: string) {
  const store = useTerminalTabsStore.getState();
  for (const tab of store.tabs)
    if (tab.profileId === profileId) store.closeTab(tab.key);
}

export function ConnectionWorkspace({
  profile,
  onEdit,
  onDelete,
}: {
  profile: SSHProfile;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const client = useQueryClient();
  const state = useSSHWorkspaceState(s => s.entries[profile.id] ?? initialWorkspace);
  const patch = (update: Partial<typeof state>) => useSSHWorkspaceState.getState().patch(profile.id, update);
  const { page, busy, error, status, command, output, spec, scope, approval, terminalNotice, forwardErrors } = state;
  const setPage = (page: SSHPage) => patch({ page });
  const setStatus = (status: string) => patch({ status });
  const setCommand = (command: string) => patch({ command });
  const setOutput = (output: string | null) => patch({ output });
  const setSpec = (spec: typeof state.spec) => patch({ spec });
  const setApproval = (approval: typeof state.approval) => patch({ approval });
  const activeModuleId = useWorkbenchTabsStore(s => s.activeModuleId);
  const tabs = useTerminalTabsStore((s) => s.tabs);
  const activeKey = useTerminalTabsStore((s) => s.activeKey);
  const sessions = tabs.filter((t) => t.profileId === profile.id);
  const { data: forwards = EMPTY_ARRAY, error: forwardError } = useQuery({
    queryKey: ["ssh", "forwards"],
    queryFn: listSSHForwards,
    refetchInterval: 3000,
  });
  const { data: agentId = "" } = useQuery({
    queryKey: ["ssh", "active"],
    queryFn: getSSHActiveProfile,
  });
  const ownedForwards = forwards.filter((f) => f.profileId === profile.id);
  async function act(label: string, fn: () => Promise<void>, target: FeedbackScope = page) {
    const result = await runSSHAction(profile.id, label, target, fn);
    if (result === "busy" || !isBackground()) return;
    if (result === "error" && target !== "approval") notifyResult(true, target);
    else if (result === "success" && ["Running command…", "Starting tunnel…", "Stopping tunnel…", "Testing connection…"].includes(label)) notifyResult(false, target);
  }
  function isBackground() {
    return useSSHStore.getState().selectedId !== profile.id || useWorkbenchTabsStore.getState().activeModuleId !== "ssh";
  }
  function notifyResult(failed: boolean, target: FeedbackScope) {
    notify({ id: `ssh:${profile.id}:${target}`, tone: failed ? "error" : "success",
      title: failed ? "SSH action failed" : target === "commands" ? "SSH command completed" : target === "tunnels" ? "SSH tunnel updated" : "SSH action completed",
      action: { label: "Open connection", onClick: () => {
        patch({ page: target === "connection" || target === "approval" ? "terminal" : target });
        useSSHStore.getState().select(profile.id);
        useWorkbenchTabsStore.getState().openTab({ moduleId: "ssh", viewId: "ssh.connections", title: "SSH" });
      } } });
  }
  function feedback(target: FeedbackScope) {
    if (scope !== target) return null;
    return <div className="flex flex-col gap-2">
      {busy && <PendingStatus>{busy}</PendingStatus>}
      {error && <InlineFeedback>{error}</InlineFeedback>}
      {status && <InlineFeedback tone="success">{status}</InlineFeedback>}
    </div>;
  }
  function openTerminal() {
    const terminals = useTerminalTabsStore.getState();
    terminals.openTab({
      title: `${profile.env.toUpperCase()} · ${profile.name} #${terminals.nextId} · ${profile.username}@${profile.host}:${profile.port}`,
      profileId: profile.id,
      start: async (rows, cols) => {
        const id = await startSSHTerminal(profile.id, rows, cols);
        patch({ terminalNotice: "" });
        return id;
      },
      onExit: (code) => {
        if (code !== -1) patch({ terminalNotice: code === 0 ? "Remote terminal session ended." : `Remote terminal stopped (exit code ${code}). Open a new terminal to reconnect.` });
      },
    });
    useBottomSlotStore.getState().setOpen(true);
    setPage("terminal");
  }
  function showTerminal(key: string) {
    useTerminalTabsStore.getState().setActive(key);
    useBottomSlotStore.getState().setOpen(true);
  }
  async function connect() {
    await act("Connecting…", async () => {
      const fingerprint = await observeSSHHostKey(profile.id);
      if (fingerprint !== profile.hostKeyFingerprint)
        setApproval({ fingerprint, connect: true });
      else openTerminal();
    }, "connection");
  }
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-label-14">{profile.name}</h2>
            <span
              className={cn(
                "rounded px-1.5 py-0.5 text-caption-11 uppercase",
                profile.env === "prod"
                  ? "bg-status-danger/10 text-status-danger"
                  : "bg-state-soft text-text-secondary",
              )}
            >
              {profile.env}
            </span>
            {agentId === profile.id && (
              <span className="text-caption-11 text-text-secondary">
                Agent target
              </span>
            )}
          </div>
          <p className="mt-1 truncate font-mono text-body-12 text-text-tertiary">
            {profile.username}@{profile.host}:{profile.port}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Edit connection"
            disabled={Boolean(busy)}
            onClick={onEdit}
          >
            <Pencil className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Delete connection"
            disabled={Boolean(busy)}
            onClick={onDelete}
          >
            <Trash2 className="size-4" />
          </Button>
          <Button
            size="sm"
            disabled={Boolean(busy)}
            onClick={() => void connect()}
          >
            <Terminal className="size-4" />
            Connect
          </Button>
        </div>
      </header>
      <nav
        aria-label="Connection views"
        className="flex gap-1 border-b border-border px-4 py-2"
      >
        {(
          [
            ["terminal", Terminal],
            ["commands", Play],
            ["tunnels", Network],
            ["details", ShieldCheck],
          ] as const
        ).map(([value, Icon]) => (
          <Button
            key={value}
            variant={page === value ? "secondary" : "ghost"}
            size="sm"
            aria-pressed={page === value}
            onClick={() => setPage(value)}
          >
            <Icon className="size-3.5" />
            <span className="capitalize">{value}</span>
            {value === "tunnels" && ownedForwards.length > 0
              ? ` (${ownedForwards.length})`
              : ""}
          </Button>
        ))}
      </nav>
      <div className="flex-1 overflow-auto p-5">
        {profile.env === "prod" && (
          <FeedbackBanner tone="warning" title="Production connection" className="mb-4">
            Commands and terminal input execute on {profile.host}.
          </FeedbackBanner>
        )}
        <div className="mb-3">{feedback("connection")}</div>
        {terminalNotice && <FeedbackBanner tone="warning" title="Terminal session ended" className="mb-3" action={<Button size="sm" disabled={Boolean(busy)} onClick={() => void connect()}>Reconnect</Button>}>{terminalNotice}</FeedbackBanner>}
        {page === "terminal" && (
          <div className="flex flex-col gap-4">
            <div>
              {feedback("terminal")}
              <h3 className="text-label-13">Remote terminals</h3>
              <p className="mt-1 text-body-12 text-text-secondary">
                Interactive shells open in the terminal panel below. Switching
                connections or modules keeps them running; closing a terminal
                tab ends its session.
              </p>
            </div>
            {sessions.length === 0 ? (
              <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border py-12">
                <Terminal className="size-8 text-text-tertiary" />
                <p className="text-body-13 text-text-secondary">
                  No terminal open for this connection.
                </p>
                <Button disabled={Boolean(busy)} onClick={() => void connect()}>
                  Open terminal
                </Button>
              </div>
            ) : (
              sessions.map((tab) => (
                <div
                  key={tab.key}
                  className="flex items-center gap-2 rounded-md border border-border p-3"
                >
                  <Terminal className="size-4 text-text-tertiary" />
                  <button
                    className="min-w-0 flex-1 truncate text-left text-body-12"
                    onClick={() => showTerminal(tab.key)}
                  >
                    {tab.title}
                  </button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => showTerminal(tab.key)}
                  >
                    {tab.key === activeKey ? "Show" : "Switch to"}
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={`Close ${tab.title}`}
                    onClick={() =>
                      useTerminalTabsStore.getState().closeTab(tab.key)
                    }
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))
            )}
          </div>
        )}
        {page === "commands" && (
          <div className="flex flex-col gap-3">
            {feedback("commands")}
            <h3 className="text-label-13">Run a command</h3>
            <p className="text-body-12 text-text-secondary">
              One-shot execution. Use a terminal for interactive commands such
              as sudo or tail -f.
            </p>
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                void act("Running command…", async () => {
                  setOutput(null);
                  setOutput(await runSSHCommand(profile.id, command, ""));
                });
              }}
            >
              <Input
                aria-label="ssh-cmd"
                disabled={Boolean(busy)}
                className="font-mono"
                value={command}
                onChange={(e) => setCommand(e.target.value)}
                required
              />
              <Button
                disabled={
                  Boolean(busy) ||
                  !profile.hostKeyFingerprint ||
                  !command.trim()
                }
                type="submit"
              >
                Run
              </Button>
            </form>
            {!profile.hostKeyFingerprint && (
              <p className="text-body-12 text-text-secondary">
                Connect first to verify this server's host key.
              </p>
            )}
            {output !== null && (
              <div className="rounded-md border border-border">
                <div className="flex items-center justify-between border-b border-border px-3 py-2 text-caption-11 text-text-tertiary">
                  <span>Combined output</span>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    aria-label="Copy output"
                    onClick={() =>
                      void act("Copying…", async () => {
                        await navigator.clipboard.writeText(output);
                        setStatus("Output copied.");
                      })
                    }
                  >
                    <Copy className="size-3" />
                  </Button>
                </div>
                <pre
                  aria-label="ssh-output"
                  className="whitespace-pre-wrap break-all p-3 font-mono text-body-12"
                >
                  {output || "Command returned no output."}
                </pre>
              </div>
            )}
          </div>
        )}
        {page === "tunnels" && (
          <div className="flex max-w-3xl flex-col gap-4">
            {feedback("tunnels")}
            <div>
              <h3 className="text-label-13">SSH tunnels</h3>
              <p className="mt-1 text-body-12 text-text-secondary">
                Listeners are loopback-only. Tunnels run until stopped, the
                connection is edited/deleted, or DevKit exits.
              </p>
            </div>
            {forwardError && (
              <FeedbackBanner tone="error" title="Could not load tunnels" action={<Button size="sm" variant="secondary" onClick={() => void client.invalidateQueries({ queryKey: ["ssh", "forwards"] })}>Retry</Button>}>
                {errorMessage(forwardError)}
              </FeedbackBanner>
            )}
            {ownedForwards.map((forward) => (
              <div
                key={forward.id}
                className="flex flex-wrap items-center gap-2 rounded-md border border-border p-3"
              >
                <div className="min-w-0 flex-1">
                  <span className="text-caption-11 text-status-success">
                    Active ·{" "}
                    {forward.direction === "local"
                      ? "Local listener"
                      : "Remote listener"}
                  </span>
                  <p className="mt-1 font-mono text-body-12 break-all">
                    {forward.listenHost}:{forward.listenPort} →{" "}
                    {forward.targetHost}:{forward.targetPort}
                  </p>
                </div>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label="Copy tunnel endpoint"
                  disabled={Boolean(busy)}
                  onClick={() =>
                    void act("Copying…", async () => {
                      await navigator.clipboard.writeText(
                        `${forward.listenHost}:${forward.listenPort}`,
                      );
                      setStatus("Endpoint copied.");
                    })
                  }
                >
                  <Copy className="size-3.5" />
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={Boolean(busy)}
                  onClick={() =>
                    void act("Stopping tunnel…", async () => {
                      await stopSSHForward(forward.id);
                      await client.invalidateQueries({
                        queryKey: ["ssh", "forwards"],
                      });
                    })
                  }
                >
                  Stop
                </Button>
              </div>
            ))}
            <form
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                const errors = validateForward(spec);
                patch({ forwardErrors: errors });
                if (Object.keys(errors).length) {
                  (e.currentTarget.elements.namedItem(Object.keys(errors)[0]) as HTMLElement | null)?.focus();
                  return;
                }
                void act("Starting tunnel…", async () => {
                  await startSSHForward(profile.id, { ...spec, listenPort: Number(spec.listenPort), targetPort: Number(spec.targetPort) }, "");
                  await client.invalidateQueries({
                    queryKey: ["ssh", "forwards"],
                  });

                });
              }}
              className="flex flex-col gap-3 rounded-lg border border-border p-4"
            >
              <Field label="Direction">
                <NativeSelect
                  aria-label="ssh-forward-direction"
                  value={spec.direction}
                  onChange={(e) =>
                    setSpec({
                      ...spec,
                      direction: e.target.value as SSHForwardSpec["direction"],
                    })
                  }
                >
                  <option value="local">Local forward (-L)</option>
                  <option value="remote">Remote forward (-R)</option>
                </NativeSelect>
              </Field>
              <p className="text-body-12 text-text-secondary">
                {spec.direction === "local"
                  ? "Your computer → SSH server → destination on the remote network"
                  : "SSH server listener → your computer → destination on your local network"}
              </p>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Listen host (loopback)">
                  <NativeSelect
                    className="w-full"
                    aria-label="ssh-forward-listen-host"
                    value={spec.listenHost}
                    onChange={(e) =>
                      setSpec({ ...spec, listenHost: e.target.value })
                    }
                  >
                    <option>127.0.0.1</option>
                    <option>::1</option>
                  </NativeSelect>
                </Field>
                <Field label="Listen port (0 = automatic)" error={forwardErrors.listenPort} errorId="ssh-forward-listenPort-error">
                  <Input
                    aria-label="ssh-forward-listen-port"
                    name="listenPort"
                    aria-invalid={Boolean(forwardErrors.listenPort)}
                    aria-describedby={forwardErrors.listenPort ? "ssh-forward-listenPort-error" : undefined}
                    type="number"
                    required
                    min={0}
                    max={65535}
                    value={spec.listenPort}
                    onChange={(e) =>
                      setSpec({ ...spec, listenPort: e.target.value })
                    }
                  />
                </Field>
                <Field label="Destination host" error={forwardErrors.targetHost} errorId="ssh-forward-targetHost-error">
                  <Input
                    aria-label="ssh-forward-target-host"
                    name="targetHost"
                    aria-invalid={Boolean(forwardErrors.targetHost)}
                    aria-describedby={forwardErrors.targetHost ? "ssh-forward-targetHost-error" : undefined}
                    required
                    value={spec.targetHost}
                    onChange={(e) =>
                      setSpec({ ...spec, targetHost: e.target.value })
                    }
                  />
                </Field>
                <Field label="Destination port" error={forwardErrors.targetPort} errorId="ssh-forward-targetPort-error">
                  <Input
                    aria-label="ssh-forward-target-port"
                    name="targetPort"
                    aria-invalid={Boolean(forwardErrors.targetPort)}
                    aria-describedby={forwardErrors.targetPort ? "ssh-forward-targetPort-error" : undefined}
                    type="number"
                    required
                    min={1}
                    max={65535}
                    value={spec.targetPort}
                    onChange={(e) =>
                      setSpec({ ...spec, targetPort: e.target.value })
                    }
                  />
                </Field>
              </div>
              <Button
                className="self-start"
                type="submit"
                disabled={Boolean(busy) || !profile.hostKeyFingerprint}
              >
                Start tunnel
              </Button>
              {!profile.hostKeyFingerprint && (
                <p className="text-body-12 text-text-secondary">
                  Connect first to verify this server's host key.
                </p>
              )}
            </form>
          </div>
        )}
        {page === "details" && (
          <div className="flex max-w-2xl flex-col gap-5">
            {feedback("details")}
            <section className="rounded-lg border border-border p-4">
              <h3 className="flex items-center gap-2 text-label-13">
                <ShieldCheck className="size-4" />
                Server identity
              </h3>
              <p className="mt-2 text-body-12 text-text-secondary">
                {profile.hostKeyFingerprint
                  ? "An approved host key is pinned to this connection."
                  : "This server has not been trusted yet."}
              </p>
              <code className="mt-3 block break-all text-body-12">
                {profile.hostKeyFingerprint || "No trusted fingerprint"}
              </code>
              <div className="mt-4 flex gap-2">
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={Boolean(busy)}
                  onClick={() =>
                    void act("Observing host key…", async () => {
                      setApproval({
                        fingerprint: await observeSSHHostKey(profile.id),
                        connect: false,
                      });
                    })
                  }
                >
                  Review host key
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={Boolean(busy) || !profile.hostKeyFingerprint}
                  onClick={() =>
                    void act("Testing connection…", async () => {
                      await testSSHConnection(profile.id, "");
                      setStatus(
                        "Authentication and connection successful. No terminal was opened.",
                      );
                    })
                  }
                >
                  Test connection
                </Button>
              </div>
            </section>
            <section className="rounded-lg border border-border p-4">
              <h3 className="text-label-13">Authentication</h3>
              <p className="mt-2 text-body-12 text-text-secondary">
                {profile.authMethod === "key"
                  ? `Private key · ${profile.hasKeyPassphrase ? "passphrase saved" : "no saved passphrase"}`
                  : "Password"}
              </p>
            </section>
            <section className="rounded-lg border border-border p-4">
              <h3 className="flex items-center gap-2 text-label-13">
                <Bot className="size-4" />
                Agent target
              </h3>
              <p className="my-2 text-body-12 text-text-secondary">
                Choose the connection MCP agents use for ssh.runCommand.
                Selecting a connection in the sidebar does not change this
                target.
              </p>
              <Button
                size="sm"
                variant="secondary"
                disabled={Boolean(busy)}
                onClick={() =>
                  void act("Updating agent target…", async () => {
                    await setSSHActiveProfile(
                      agentId === profile.id ? "" : profile.id,
                    );
                    await client.invalidateQueries({
                      queryKey: ["ssh", "active"],
                    });
                  })
                }
              >
                {agentId === profile.id
                  ? "Clear agent target"
                  : "Use for agents"}
              </Button>
            </section>
          </div>
        )}
      </div>
      <Dialog
        open={Boolean(approval) && activeModuleId === "ssh"}
        onOpenChange={(open) => {
          if (!open && !busy) setApproval(null);
        }}
      >
        <DialogContent className="sm:max-w-lg" showCloseButton={!busy}>
          <DialogHeader>
            <DialogTitle>
              {approval &&
              profile.hostKeyFingerprint &&
              profile.hostKeyFingerprint !== approval.fingerprint
                ? "Server host key changed"
                : "Verify server identity"}
            </DialogTitle>
            <DialogDescription>
              Compare this fingerprint with a trusted source for {profile.host}:
              {profile.port} before approving.
            </DialogDescription>
          </DialogHeader>
          {profile.hostKeyFingerprint && (
            <div>
              <p className="text-caption-11 text-text-tertiary">
                Previously trusted
              </p>
              <code className="block break-all text-body-12 mt-1">
                {profile.hostKeyFingerprint}
              </code>
            </div>
          )}
          <div>
            <p className="text-caption-11 text-text-tertiary">
              Observed fingerprint
            </p>
            <code
              aria-label="ssh-observed-host-key"
              className="block break-all text-body-12 mt-1"
            >
              {approval?.fingerprint}
            </code>
          </div>
          {approval &&
            profile.hostKeyFingerprint &&
            profile.hostKeyFingerprint !== approval.fingerprint && (
              <p className="text-body-12 text-status-danger">
                The server identity differs from your saved key. Verify why it
                changed before replacing it.
              </p>
            )}
          {feedback("approval")}
          <DialogFooter>
            <Button
              variant="ghost"
              disabled={Boolean(busy)}
              onClick={() => setApproval(null)}
            >
              Cancel
            </Button>
            <Button
              disabled={Boolean(busy) || !approval}
              onClick={() =>
                void act("Approving host key…", async () => {
                  if (!approval) return;
                  await trustSSHHostKey(profile.id, approval.fingerprint);
                  await client.invalidateQueries({
                    queryKey: ["ssh", "profiles"],
                  });
                  const launch = approval.connect;
                  setApproval(null);
                  if (launch) openTerminal();
                  else patch({ scope: "details", status: "Host key trusted." });
                }, "approval")
              }
            >
              {busy
                ? "Verifying…"
                : approval?.connect
                  ? "Trust and connect"
                  : "Trust host key"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
