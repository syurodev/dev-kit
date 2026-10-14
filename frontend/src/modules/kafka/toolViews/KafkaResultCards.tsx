import * as React from "react";
import { Inbox, Send, Users, Workflow } from "lucide-react";
import { CodeBlock } from "@/components/code/CodeBlock";
import { detectLanguage } from "@/components/code/languages";
import { Chip } from "@/components/ui/chip";
import { DataTable, type DataTableColumn } from "@/components/ui/data-table/DataTable";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { notify } from "@/lib/notify";
import { dispatchIntent } from "@/workbench/intents";
import { formatClock, formatOffset, prettyValue } from "../kafkaForm";
import {
  toggleRow,
  valuePreview,
  type ConsumerGroup,
  type PeekResult,
  type PeekedMessage,
  type PublishResult,
  type TopicDetail,
} from "./parse";

/**
 * The kafka.* tool results as the module would show them
 * (docs/dev-kit/tool-result-view.mdx): records in a table, a topic's facts as
 * rows, a publish as where it landed. Each card reads only what the call
 * carried; Open in Kafka goes through `kafka.reveal`, the same way into the
 * module a reference in agent prose takes.
 */

function plural(count: number, one: string, many = `${one}s`): string {
  return `${count} ${count === 1 ? one : many}`;
}

/**
 * Opens a topic through `kafka.reveal` and, when it could not be selected,
 * says so: Kafka still comes on screen, and without a word the person would
 * be looking at some other topic. One sentence covers both reasons the intent
 * gives up — nothing connected, or a cluster without that topic.
 */
export async function openTopic(
  topic: string,
  dispatch: typeof dispatchIntent = dispatchIntent,
  toast: typeof notify = notify,
): Promise<void> {
  const answer = await dispatch("kafka.reveal", { module: "kafka", kind: "topic", id: topic, label: topic });
  if (answer === "unavailable") {
    toast({ id: `kafka:reveal:${topic}`, tone: "error", title: `“${topic}” isn't on a connected Kafka cluster` });
  }
}

function revealTopic(topic: string) {
  void openTopic(topic);
}

function OpenInKafka({ topic }: { topic: string }) {
  if (!topic) return null;
  return <FramedCardAction onClick={() => revealTopic(topic)}>Open in Kafka</FramedCardAction>;
}

function EmptyLine({ children }: { children: React.ReactNode }) {
  return (
    <FramedCardPreview>
      <span className="text-caption-11 text-text-tertiary">{children}</span>
    </FramedCardPreview>
  );
}

const MESSAGE_COLUMNS: DataTableColumn[] = [
  { key: "offset", header: "Offset", align: "right", mono: true, nowrap: true },
  { key: "key", header: "Key", mono: true, nowrap: true },
  { key: "value", header: "Value", mono: true, nowrap: true },
  { key: "time", header: "Time", mono: true, nowrap: true },
];

/** One record's whole value, pretty when it is JSON — what a row opens to. */
export function MessageValue({ message }: { message: PeekedMessage }) {
  const value = prettyValue(message.value);
  return (
    <div data-testid="kafka-message-value" className="flex min-w-0 flex-col gap-(--space-micro)">
      <div className="flex min-w-0 items-center gap-(--space-inline)">
        <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">
          {`Offset ${formatOffset(message.offset)}`}
          {message.key ? ` · key ${message.key}` : ""}
        </span>
        <Chip>{value.json ? "json" : "raw"}</Chip>
      </div>
      <CodeBlock
        code={value.text}
        language={value.json ? "json" : detectLanguage(value.text)}
        className="max-h-80 overflow-auto rounded-md border border-border-subtle bg-surface-well px-3 py-2 text-code-12 whitespace-pre-wrap text-text-secondary select-text"
      />
    </div>
  );
}

/** `kafka.peekMessages` — the records, one line each; a click opens one whole. */
export function PeekResultCard({ peek }: { peek: PeekResult }) {
  const [open, setOpen] = React.useState<number | null>(null);
  const rows = React.useMemo(
    () =>
      peek.messages.map((message) => [
        formatOffset(message.offset),
        message.key ?? "—",
        valuePreview(message.value),
        formatClock(message.timestamp),
      ]),
    [peek.messages],
  );
  const onRowClick = React.useCallback((index: number) => setOpen((current) => toggleRow(current, index)), []);
  const rowAriaLabel = React.useCallback(
    (_row: unknown, index: number) => `Show message at offset ${peek.messages[index].offset}`,
    [peek.messages],
  );
  const opened = open === null ? undefined : peek.messages[open];
  // The value opens under the table's scroller, which may be below the fold.
  const openedRef = React.useRef<HTMLDivElement>(null);
  React.useEffect(() => {
    if (open !== null) openedRef.current?.scrollIntoView?.({ block: "nearest" });
  }, [open]);
  const title = ["Messages", peek.topic, `partition ${peek.partition}`].filter(Boolean).join(" · ");

  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Inbox}
        title={title}
        meta={plural(peek.messages.length, "message")}
        action={<OpenInKafka topic={peek.topic} />}
      />
      {peek.messages.length === 0 ? (
        <EmptyLine>No messages</EmptyLine>
      ) : (
        <FramedCardPreview flush className="py-0">
          <DataTable
            columns={MESSAGE_COLUMNS}
            rows={rows}
            dense
            maxHeight="max-h-80"
            onRowClick={onRowClick}
            rowAriaLabel={rowAriaLabel}
            expandedRow={open}
          />
          {opened ? (
            <div ref={openedRef} className="border-t border-border-subtle p-(--space-inline)">
              <MessageValue message={opened} />
            </div>
          ) : null}
        </FramedCardPreview>
      )}
    </FramedCard>
  );
}

const TOPIC_COLUMNS: DataTableColumn[] = [{ key: "topic", header: "Topic", mono: true, nowrap: true }];

/** `kafka.listTopics` — the cluster's topics; a click opens one in Kafka. */
export function TopicsResultCard({ topics }: { topics: string[] }) {
  const rows = React.useMemo(() => topics.map((name) => [name]), [topics]);
  const onRowClick = React.useCallback((index: number) => revealTopic(topics[index]), [topics]);
  const rowAriaLabel = React.useCallback((_row: unknown, index: number) => `Open ${topics[index]} in Kafka`, [topics]);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Workflow} title="Topics" meta={plural(topics.length, "topic")} />
      {topics.length === 0 ? (
        <EmptyLine>No topics</EmptyLine>
      ) : (
        <FramedCardPreview flush className="py-0">
          <DataTable
            columns={TOPIC_COLUMNS}
            rows={rows}
            dense
            maxHeight="max-h-80"
            onRowClick={onRowClick}
            rowAriaLabel={rowAriaLabel}
          />
        </FramedCardPreview>
      )}
    </FramedCard>
  );
}

/** `kafka.describeTopic` — a topic's partitions and replication, as definition rows. */
export function TopicDetailResultCard({ detail }: { detail: TopicDetail }) {
  const facts: Array<[string, string]> = [
    ["Partitions", detail.partitions.join(", ") || "—"],
    ["Replication factor", String(detail.replicationFactor)],
  ];
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Workflow}
        title={`Topic · ${detail.name}`}
        meta={plural(detail.partitions.length, "partition")}
        action={<OpenInKafka topic={detail.name} />}
      />
      <FramedCardPreview>
        <dl className="flex min-w-0 flex-col gap-(--space-micro)">
          {facts.map(([label, value]) => (
            <div key={label} className="flex min-w-0 items-baseline gap-(--space-inline)">
              <dt className="w-28 shrink-0 text-caption-11 text-text-tertiary">{label}</dt>
              <dd className="min-w-0 flex-1 truncate text-code-12 text-text-secondary" title={value}>
                {value}
              </dd>
            </div>
          ))}
        </dl>
      </FramedCardPreview>
    </FramedCard>
  );
}

const GROUP_COLUMNS: DataTableColumn[] = [
  { key: "group", header: "Group", mono: true, nowrap: true },
  { key: "lag", header: "Lag", align: "right", mono: true, nowrap: true },
];

/** `kafka.listConsumerGroups` — each group and how far behind it is. */
export function ConsumerGroupsResultCard({ groups }: { groups: ConsumerGroup[] }) {
  const rows = React.useMemo(() => groups.map((group) => [group.groupId, formatOffset(group.lag)]), [groups]);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Users} title="Consumer groups" meta={plural(groups.length, "group")} />
      {groups.length === 0 ? (
        <EmptyLine>No consumer groups</EmptyLine>
      ) : (
        <FramedCardPreview flush className="py-0">
          <DataTable columns={GROUP_COLUMNS} rows={rows} dense maxHeight="max-h-80" />
        </FramedCardPreview>
      )}
    </FramedCard>
  );
}

/** `kafka.publishMessage` — where the record landed. */
export function PublishResultCard({ result }: { result: PublishResult }) {
  const landed = [
    `partition ${result.partition}`,
    `offset ${formatOffset(result.offset)}`,
    formatClock(result.timestamp),
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={Send}
        title={result.topic ? `Published to ${result.topic}` : "Published"}
        action={<OpenInKafka topic={result.topic} />}
      />
      <FramedCardPreview>
        <span className="min-w-0 truncate text-code-12 text-text-secondary tabular-nums">{landed}</span>
      </FramedCardPreview>
    </FramedCard>
  );
}
