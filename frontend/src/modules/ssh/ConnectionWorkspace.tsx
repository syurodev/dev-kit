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
import { Tabs, TabsList, TabsTrigger, TabsContent, TabsCount } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { ActionTooltip } from "@/components/ui/action-tooltip";
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
import { terminalExitNotice } from "./terminalNotice";
import { Item, ItemContent, ItemActions, ItemTitle } from "@/components/ui/item";
import { Field } from "./ConnectionDialog";
import { InlineFeedback, FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { notify } from "@/lib/notify";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { activeSSHProfileId, openSSHConnection } from "./documents";
import { useSSHWorkspaceState, initialWorkspace, runSSHAction, type SSHPage, type FeedbackScope } from "./workspaceState";
import { validateForward } from "./validation";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";

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
  const setSpec = (spec: typeof state.spec) => {
    const errors = { ...state.forwardErrors };
    for (const key of Object.keys(errors) as (keyof typeof errors)[]) {
      if (spec[key] !== state.spec[key]) delete errors[key];
    }
    patch({ spec, forwardErrors: errors });
  };
  const setApproval = (approval: typeof state.approval) => patch({ approval });
  const visible = useModuleVisibility();
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
    return activeSSHProfileId(useWorkbenchTabsStore.getState()) !== profile.id || useWorkbenchTabsStore.getState().activeModuleId !== "ssh";
  }
  function notifyResult(failed: boolean, target: FeedbackScope) {
    notify({ id: `ssh:${profile.id}:${target}`, tone: failed ? "error" : "success",
      title: failed ? "SSH action failed" : target === "commands" ? "SSH command completed" : target === "tunnels" ? "SSH tunnel updated" : "SSH action completed",
      action: { label: "Open connection", onClick: () => {
        patch({ page: target === "connection" || target === "approval" ? "terminal" : target });
        openSSHConnection(profile);
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
      onExit: (code, reason) => {
        const notice = terminalExitNotice(code, reason);
        if (notice) patch({ terminalNotice: notice });
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
    <Tabs value={page} onValueChange={(value) => setPage(value as SSHPage)} className="h-full min-h-0 flex-1 gap-0">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-label-14">{profile.name}</h2>
            <Badge variant={profile.env === "prod" ? "destructive" : "secondary"} className="uppercase">{profile.env}</Badge>
            {agentId === profile.id && <Badge variant="outline">Agent target</Badge>}
          </div>
          <p className="mt-1 truncate font-mono text-body-12 text-text-tertiary">
            {profile.username}@{profile.host}:{profile.port}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <ActionTooltip label="Edit connection" wrap><Button
            variant="ghost"
            size="icon-sm"
            aria-label="Edit connection"
            disabled={Boolean(busy)}
            onClick={onEdit}
          >
            <Pencil className="size-4" />
          </Button></ActionTooltip>
          <ActionTooltip label="Delete connection" wrap><Button
            variant="ghost"
            size="icon-sm"
            aria-label="Delete connection"
            disabled={Boolean(busy)}
            onClick={onDelete}
          >
            <Trash2 className="size-4" />
          </Button></ActionTooltip>
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
      <div className="shrink-0 overflow-x-auto border-b border-border px-4 py-2">
        <TabsList aria-label="Connection views" variant="line" className="justify-start gap-2">
          {([
            ["terminal", Terminal], ["commands", Play], ["tunnels", Network], ["details", ShieldCheck],
          ] as const).map(([value, Icon]) => (
            <TabsTrigger key={value} value={value} className="h-8 px-3 text-label-12">
              <Icon className="size-3.5" aria-hidden="true" /><span className="capitalize">{value}</span>
              {value === "tunnels" && ownedForwards.length > 0 && <TabsCount className="text-caption-11">{ownedForwards.length}</TabsCount>}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      <div className="relative min-h-0 flex-1 overflow-y-auto p-4 @container/ssh-workspace">
        {profile.env === "prod" && (
          <FeedbackBanner tone="warning" title="Production connection" className="mb-4">
            Commands and terminal input execute on {profile.host}.
          </FeedbackBanner>
        )}
        <div className="mb-3">{feedback("connection")}</div>
        {terminalNotice && <FeedbackBanner tone="warning" title="Terminal session ended" className="mb-3" action={<Button size="sm" disabled={Boolean(busy)} onClick={() => void connect()}>Reconnect</Button>}>{terminalNotice}</FeedbackBanner>}
          <TabsContent value="terminal" className="flex flex-col gap-4">
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
              <EmptyState size="rail" icon={Terminal} title="No terminal open" body="Open an interactive shell. Its session stays running when you switch modules." action={{ label: "Open terminal", disabled: Boolean(busy), onClick: () => void connect() }} />
            ) : (
              sessions.map((tab) => (
                <Item key={tab.key} variant="outline" size="sm" className="flex-nowrap">
                  <ItemContent>
                  <ItemTitle><Terminal className="size-4 text-text-tertiary" />
                  <Button variant="ghost" className="min-w-0 flex-1 justify-start truncate text-body-12" onClick={() => showTerminal(tab.key)}>
                    <span className="truncate">{tab.title}</span>
                  </Button></ItemTitle></ItemContent>
                  <ItemActions>
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
                  </ItemActions>
                </Item>
              ))
            )}
          </TabsContent>
          <TabsContent value="commands" className="flex flex-col gap-3">
            {feedback("commands")}
            <h3 className="text-label-13">Run a command</h3>
            <p className="text-body-12 text-text-secondary">
              One-shot execution. Use a terminal for interactive commands such
              as sudo or tail -f.
            </p>
            <form
              className="flex flex-col gap-2 @md/ssh-workspace:flex-row"
              onSubmit={(e) => {
                e.preventDefault();
                void act("Running command…", async () => {
                  setOutput(null);
                  setOutput(await runSSHCommand(profile.id, command, ""));
                });
              }}
            >
              <Input
                aria-label="Command"
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
                  <ActionTooltip label="Copy output" wrap><Button
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
                  </Button></ActionTooltip>
                </div>
                <pre
                  aria-label="Command output"
                  className="whitespace-pre-wrap break-all p-3 font-mono text-body-12"
                >
                  {output || "Command returned no output."}
                </pre>
              </div>
            )}
          </TabsContent>
          <TabsContent value="tunnels" className="flex max-w-3xl flex-col gap-4">
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
                <ActionTooltip label="Copy tunnel endpoint" wrap><Button
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
                </Button></ActionTooltip>
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
              <div className="grid grid-cols-1 gap-3 @md/ssh-workspace:grid-cols-2">
                <Field label="Listen host (loopback)">
                  <NativeSelect
                    className="w-full"
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
          </TabsContent>
          <TabsContent value="details" className="flex max-w-2xl flex-col gap-5">
            {feedback("details")}
            <Card size="sm" className="border border-border bg-surface-base ring-0"><CardContent>
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
              <div className="mt-4 flex flex-wrap gap-2">
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
            </CardContent></Card>
            <Card size="sm" className="border border-border bg-surface-base ring-0"><CardContent>
              <h3 className="text-label-13">Authentication</h3>
              <p className="mt-2 text-body-12 text-text-secondary">
                {profile.authMethod === "key"
                  ? `Private key · ${profile.hasKeyPassphrase ? "passphrase saved" : "no saved passphrase"}`
                  : "Password"}
              </p>
            </CardContent></Card>
            <Card size="sm" className="border border-border bg-surface-base ring-0"><CardContent>
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
            </CardContent></Card>
          </TabsContent>
      </div>
      <Dialog
        open={Boolean(approval) && visible}
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
              aria-label="Observed host key"
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
    </Tabs>
  );
}
