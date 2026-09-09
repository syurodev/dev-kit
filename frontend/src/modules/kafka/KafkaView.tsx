import { useEffect, useMemo } from "react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { json } from "@codemirror/lang-json";
import { Loader2, PlugZap, RefreshCw, Send, ServerOff, TriangleAlert, Workflow } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/ui/chip";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { EmptyProgress, EmptyState } from "@/components/ui/empty-state";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { useSplitPanel } from "@/components/ui/split-panel";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { shortcut } from "@/lib/shortcut";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/bridge/errors";
import { KafkaSidebar } from "./KafkaSidebar";
import { MessagePanel } from "./MessagePanel";
import { NewClusterDialog } from "./NewClusterDialog";
import { formatClock, formatOffset } from "./kafkaForm";
import { useKafkaStore, type KafkaTab } from "./kafkaStore";

const CARD = "min-h-0 flex-1 overflow-auto rounded-md border border-border-subtle bg-surface-well";
const CELL_INPUT = "w-16 bg-transparent text-code-12 text-text-primary outline-none placeholder:text-text-tertiary";

/** Figma `Kafka / Field` (1136:17): a named value you change before running something. */
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex h-7 items-center gap-1.5 rounded-md border border-border-subtle bg-surface-well px-3 text-code-11 text-text-tertiary">
      {label}
      {children}
    </label>
  );
}

/**
 * Figma `Kafka / Content` (1141:63759). One connected cluster; three views of
 * it — Topics, Publish, Consumer groups — switched by a tab row in the header
 * the way the database module does, since Kafka does not opt into workbench
 * tabs. Below the row, each view is its own pane. Before a cluster is
 * connected the pane is one of Disconnected / Connecting / Error.
 */
export function KafkaView() {
  const profiles = useKafkaStore((s) => s.profiles);
  const selectedId = useKafkaStore((s) => s.selectedId);
  const connection = useKafkaStore((s) => s.connection);
  const phase = useKafkaStore((s) => s.phase);
  const error = useKafkaStore((s) => s.error);
  const testing = useKafkaStore((s) => s.testing);
  const testResult = useKafkaStore((s) => s.testResult);
  const selectedTopic = useKafkaStore((s) => s.selectedTopic);
  const topicDetail = useKafkaStore((s) => s.topicDetail);
  const peekPartition = useKafkaStore((s) => s.peekPartition);
  const peekLimit = useKafkaStore((s) => s.peekLimit);
  const peekOffset = useKafkaStore((s) => s.peekOffset);
  const messages = useKafkaStore((s) => s.messages);
  const peeking = useKafkaStore((s) => s.peeking);
  const peeked = useKafkaStore((s) => s.peeked);
  const peekDurationMs = useKafkaStore((s) => s.peekDurationMs);
  const peekError = useKafkaStore((s) => s.peekError);
  const groups = useKafkaStore((s) => s.groups);
  const groupsLoading = useKafkaStore((s) => s.groupsLoading);
  const publish = useKafkaStore((s) => s.publish);
  const publishing = useKafkaStore((s) => s.publishing);
  const publishResult = useKafkaStore((s) => s.publishResult);
  const publishError = useKafkaStore((s) => s.publishError);
  const activeTab = useKafkaStore((s) => s.activeTab);
  const inspected = useKafkaStore((s) => s.inspected);
  const refreshProfiles = useKafkaStore((s) => s.refreshProfiles);
  const connect = useKafkaStore((s) => s.connect);
  const testConnection = useKafkaStore((s) => s.testConnection);
  const setPeek = useKafkaStore((s) => s.setPeek);
  const peek = useKafkaStore((s) => s.peek);
  const refreshGroups = useKafkaStore((s) => s.refreshGroups);
  const setPublish = useKafkaStore((s) => s.setPublish);
  const publishMessage = useKafkaStore((s) => s.publishMessage);
  const setActiveTab = useKafkaStore((s) => s.setActiveTab);
  const inspect = useKafkaStore((s) => s.inspect);
  const setShowNewCluster = useKafkaStore((s) => s.setShowNewCluster);

  useEffect(() => {
    void refreshProfiles().catch((e) => useKafkaStore.setState({ error: errorMessage(e) }));
  }, [refreshProfiles]);

  // The rail reads the store itself, so the element never needs to change.
  useSidebarContent(useMemo(() => <KafkaSidebar />, []));

  const { openPanel, closePanel } = useSplitPanel();
  useEffect(() => {
    if (!inspected) {
      closePanel();
      return;
    }
    openPanel(<MessagePanel message={inspected} topic={selectedTopic} partition={peekPartition} />, {
      width: 400,
      onClose: () => inspect(null),
    });
    return () => closePanel();
  }, [inspected, selectedTopic, peekPartition, openPanel, closePanel, inspect]);

  const [peekTableRef] = useAutoAnimate<HTMLTableSectionElement>({ duration: 180, easing: "ease-out" });
  const [groupsTableRef] = useAutoAnimate<HTMLTableSectionElement>({ duration: 180, easing: "ease-out" });

  const jsonExtension = useMemo(() => [json()], []);
  const profile = profiles.find((p) => p.id === selectedId);

  function onKeyDown(e: React.KeyboardEvent) {
    if (!(e.metaKey || e.ctrlKey) || e.key !== "Enter") return;
    e.preventDefault();
    if (activeTab === "topics") void peek();
    if (activeTab === "publish") void publishMessage();
  }

  if (!connection.connected) {
    return (
      <div className="flex h-full flex-col overflow-hidden">
        {phase === "connecting" ? (
          <EmptyState
            icon={Loader2}
            title={`Connecting to ${profile?.name ?? "cluster"}`}
            body="Reaching the broker and reading cluster metadata. Every call to a broker has a timeout — nothing here can hang the app."
          >
            <span className="text-code-11 text-text-tertiary">
              {profile?.bootstrapServers.join(", ")} · {profile?.securityProtocol}
              {profile?.saslMechanism ? ` / ${profile.saslMechanism}` : ""}
            </span>
            <EmptyProgress />
          </EmptyState>
        ) : phase === "error" ? (
          <EmptyState
            icon={TriangleAlert}
            tone="danger"
            title={`Could not connect to ${profile?.name ?? "cluster"}`}
            body="Nothing was changed, and the other saved clusters are untouched."
          >
            {/* The broker's own words, verbatim: a paraphrase is what stops
                someone recognising an error they have seen before. */}
            <pre className="max-w-140 rounded-md border border-border-subtle bg-surface-well px-4 py-3 text-left text-code-12 whitespace-pre-wrap text-status-danger select-text">
              {error}
            </pre>
            <Button onClick={() => profile && void connect(profile.id)} disabled={!profile}>
              <RefreshCw className="size-3.5" />
              Retry
            </Button>
          </EmptyState>
        ) : (
          <EmptyState
            icon={profile ? Workflow : ServerOff}
            title={profile ? "Not connected" : "No cluster picked"}
            body={
              profile
                ? "Pick a cluster in the rail to connect. DevKit holds one connection at a time — topics, groups and every peek belong to the cluster that is open."
                : "Save a cluster's bootstrap servers and credentials once; the profile is encrypted into the vault like an SSH or database profile."
            }
          >
            {profile ? (
              <>
                {testResult && (
                  <span className={cn("text-code-11", testResult.ok ? "text-status-success" : "text-status-danger")}>
                    {testResult.message}
                  </span>
                )}
                <div className="flex items-center gap-2">
                  <Button variant="outline" className="bg-surface-raised" disabled={testing} onClick={() => void testConnection(profile.id)}>
                    {testing ? "Testing…" : "Test connection"}
                  </Button>
                  <Button onClick={() => void connect(profile.id)}>
                    <PlugZap className="size-3.5" />
                    Connect {profile.name}
                  </Button>
                </div>
              </>
            ) : (
              <Button onClick={() => setShowNewCluster(true)}>Add cluster</Button>
            )}
          </EmptyState>
        )}
        <NewClusterDialog />
      </div>
    );
  }

  const partitions = topicDetail?.partitions ?? [];

  return (
    <div className="flex h-full flex-col overflow-hidden px-6 pt-3.5 pb-6" onKeyDown={onKeyDown}>
      {/* header (1137:18): what the pane is looking at, and the one action that acts on it. */}
      <div className="flex shrink-0 items-center gap-3 pb-3">
        {activeTab === "groups" ? (
          <>
            <h1 className="text-title-16 text-text-primary">Consumer groups</h1>
            <Chip>{groups.length} group{groups.length === 1 ? "" : "s"}</Chip>
          </>
        ) : activeTab === "publish" ? (
          <h1 className="text-title-16 text-text-primary">Publish</h1>
        ) : (
          <>
            <h1 className="truncate text-title-16 text-text-primary">{selectedTopic || profile?.name}</h1>
            {topicDetail && (
              <div className="flex items-center gap-2">
                <Chip>{partitions.length} partition{partitions.length === 1 ? "" : "s"}</Chip>
                <Chip>{topicDetail.replicationFactor} replica{topicDetail.replicationFactor === 1 ? "" : "s"}</Chip>
              </div>
            )}
          </>
        )}
        <span className="flex-1" />
        {activeTab === "topics" && (
          <>
            <Button variant="outline" className="bg-surface-raised" onClick={() => setActiveTab("publish")}>
              Publish
            </Button>
            <Button onClick={() => void peek()} disabled={!selectedTopic || peeking}>
              {peeking ? "Peeking…" : "Peek"}
              <span className="text-code-11 opacity-65">{shortcut("Enter")}</span>
            </Button>
          </>
        )}
        {activeTab === "publish" && (
          <Button onClick={() => void publishMessage()} disabled={publishing || !publish.topic || !publish.payload}>
            <Send className="size-3.5" />
            {publishing ? "Publishing…" : "Publish"}
            <span className="text-code-11 opacity-65">{shortcut("Enter")}</span>
          </Button>
        )}
        {activeTab === "groups" && (
          <Button variant="outline" className="bg-surface-raised" onClick={() => void refreshGroups()} disabled={groupsLoading}>
            <RefreshCw className={cn("size-3.5", groupsLoading && "animate-spin")} />
            Refresh
          </Button>
        )}
      </div>

      <Tabs
        value={activeTab}
        onValueChange={(v) => setActiveTab(v as KafkaTab)}
        className="min-h-0 flex-1 gap-3.5"
      >
        <TabsList className="shrink-0">
          <TabsTrigger value="topics">Topics</TabsTrigger>
          <TabsTrigger value="publish">Publish</TabsTrigger>
          <TabsTrigger value="groups">Consumer groups</TabsTrigger>
        </TabsList>

        {/* ---- Topics: the peek and its result. */}
        <TabsContent value="topics" className="flex min-h-0 flex-1 flex-col gap-3.5">
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <Field label="partition">
              <select
                value={peekPartition}
                onChange={(e) => setPeek({ peekPartition: Number(e.target.value) })}
                className={cn(CELL_INPUT, "w-auto")}
                aria-label="Partition"
              >
                {(partitions.length ? partitions : [0]).map((id) => (
                  <option key={id} value={id}>{id}</option>
                ))}
              </select>
            </Field>
            <Field label="from offset">
              <input
                type="number"
                min={-1}
                value={peekOffset < 0 ? "" : peekOffset}
                placeholder="latest"
                onChange={(e) => setPeek({ peekOffset: e.target.value === "" ? -1 : Number(e.target.value) })}
                className={CELL_INPUT}
                aria-label="From offset"
              />
            </Field>
            <Field label="limit">
              <input
                type="number"
                min={1}
                max={500}
                value={peekLimit}
                onChange={(e) => setPeek({ peekLimit: Number(e.target.value) })}
                className={cn(CELL_INPUT, "w-12")}
                aria-label="Limit"
              />
            </Field>
          </div>

          <div className="flex h-5 shrink-0 items-center gap-6">
            {peeked && !peekError && (
              <>
                <span className="text-body-13-medium text-text-primary">
                  {messages.length} message{messages.length === 1 ? "" : "s"}
                </span>
                {peekDurationMs != null && (
                  <span className="text-body-13-medium text-text-primary">{peekDurationMs} ms</span>
                )}
              </>
            )}
            <span className="flex-1" />
            {/* The one thing a Kafka UI can quietly mislead about, said on the screen. */}
            <Chip className="text-text-tertiary">a peek never commits an offset</Chip>
          </div>

          <div className={CARD}>
            {peekError ? (
              <pre className="px-4 py-3 text-code-12 whitespace-pre-wrap text-status-danger select-text">{peekError}</pre>
            ) : !peeked ? (
              <div className="flex h-full flex-col items-center justify-center gap-1.5 p-8 text-center">
                <span className="text-body-13 text-text-secondary">
                  {selectedTopic ? `Peek ${selectedTopic} to read its last ${peekLimit} records` : "Pick a topic in the rail"}
                </span>
                <span className="text-code-11 text-text-tertiary">
                  reads from the partition, commits nothing, holds at most 500
                </span>
              </div>
            ) : messages.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-1.5 p-8 text-center">
                <span className="text-body-13 text-text-secondary">
                  No messages in partition {peekPartition} from that offset
                </span>
                <span className="text-code-11 text-text-tertiary">
                  the partition may be empty, or the offset may sit past its end
                </span>
              </div>
            ) : (
              <table className="w-full table-fixed border-collapse text-left text-code-12" aria-label="Peeked messages">
                <thead className="sticky top-0 z-10 bg-surface-raised">
                  <tr className="text-code-12-medium text-text-secondary">
                    <th className="h-10 w-28 border-b border-border-subtle px-4">Offset</th>
                    <th className="h-10 w-50 border-b border-border-subtle px-4">Key</th>
                    <th className="h-10 border-b border-border-subtle px-4">Value</th>
                    <th className="h-10 w-35 border-b border-border-subtle px-4">Time</th>
                  </tr>
                </thead>
                <tbody ref={peekTableRef}>
                  {messages.map((m) => (
                    <tr
                      key={`${m.offset}-${m.timestamp ?? 0}`}
                      onClick={() => inspect(m)}
                      className={cn(
                        "cursor-pointer border-b border-border-subtle hover:bg-surface-raised/60",
                        inspected === m && "bg-surface-raised",
                      )}
                    >
                      <td className="h-9 truncate px-4 text-text-tertiary">{formatOffset(m.offset)}</td>
                      <td className="h-9 truncate px-4 text-text-primary">{m.key ?? ""}</td>
                      <td className="h-9 truncate px-4 text-text-secondary" title="Click to open">{m.value}</td>
                      <td className="h-9 truncate px-4 text-code-11 text-text-tertiary">{formatClock(m.timestamp)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </TabsContent>

        {/* ---- Publish: the target, the body, and the receipt. */}
        <TabsContent value="publish" className="flex min-h-0 flex-1 flex-col gap-3.5">
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <Field label="topic">
              <input
                value={publish.topic}
                onChange={(e) => setPublish({ topic: e.target.value })}
                className={cn(CELL_INPUT, "w-44")}
                aria-label="Topic"
                required
              />
            </Field>
            <Field label="key">
              <input
                value={publish.key}
                onChange={(e) => setPublish({ key: e.target.value })}
                placeholder="optional"
                className={cn(CELL_INPUT, "w-36")}
                aria-label="Key"
              />
            </Field>
            <Field label="partition">
              <input
                value={publish.partition}
                onChange={(e) => setPublish({ partition: e.target.value })}
                placeholder="auto"
                className={cn(CELL_INPUT, "w-12")}
                aria-label="Partition"
              />
            </Field>
          </div>
          <span className="shrink-0 text-label-11-caps text-text-tertiary">Payload</span>
          <div className={cn(CARD, "overflow-hidden")}>
            <CodeEditor value={publish.payload} onChange={(v) => setPublish({ payload: v })} extensions={jsonExtension} wrap />
          </div>
          {/* The receipt is the point of the form: a publish that reports
              nothing cannot be told apart from one that silently went nowhere. */}
          {publishResult && (
            <div className="flex h-9 shrink-0 items-center gap-2 rounded-md bg-status-success/10 px-4 text-code-12 text-status-success">
              <span className="size-1.5 rounded-full bg-status-success" />
              delivered to partition {publishResult.partition} at offset {formatOffset(publishResult.offset)}
            </div>
          )}
          {publishError && (
            <pre className="shrink-0 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) px-4 py-2.5 text-code-12 whitespace-pre-wrap text-status-danger select-text">
              {publishError}
            </pre>
          )}
        </TabsContent>

        {/* ---- Consumer groups: who is behind, and by how much. */}
        <TabsContent value="groups" className="flex min-h-0 flex-1 flex-col">
          <div className={CARD}>
            {groups.length === 0 ? (
              <div className="flex h-full items-center justify-center p-8 text-body-13 text-text-tertiary">
                {groupsLoading ? "Loading…" : "No consumer groups on this cluster."}
              </div>
            ) : (
              <table className="w-full table-fixed border-collapse text-left" aria-label="Consumer groups">
                <thead className="sticky top-0 z-10 bg-surface-raised">
                  <tr className="text-code-12-medium text-text-secondary">
                    <th className="h-10 border-b border-border-subtle px-4">Group</th>
                    <th className="h-10 w-28 border-b border-border-subtle px-4">Lag</th>
                  </tr>
                </thead>
                <tbody ref={groupsTableRef}>
                  {groups.map((g) => (
                    <tr key={g.groupId} className="border-b border-border-subtle">
                      <td className="h-9 truncate px-4 text-body-13 text-text-primary">{g.groupId}</td>
                      {/* Lag is the number that makes someone act, so it is the only one that shouts. */}
                      <td className={cn("h-9 px-4 text-code-12", g.lag ? "text-status-warning" : "text-text-tertiary")}>
                        {g.lag == null ? "—" : formatOffset(g.lag)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </TabsContent>
      </Tabs>

      {error && <p role="alert" className="shrink-0 pt-2 text-body-12 text-status-danger">{error}</p>}
      <NewClusterDialog />
    </div>
  );
}
