import { Chip } from "@/components/ui/chip";
import type { KafkaMessage } from "@/bridge/kafka";
import { formatOffset, prettyValue } from "./kafkaForm";

/**
 * Figma `Kafka / Panel Content` (1155:887): one record, opened from the table.
 * It exists because the Value column is the only unbounded one — at any rail
 * width the table truncates it, and a JSON body that has to be read whole
 * cannot be read in a cell. No close button: SplitPanel draws its own.
 */
export function MessagePanel({
  message,
  topic,
  partition,
}: {
  message: KafkaMessage;
  topic: string;
  partition: number;
}) {
  const value = prettyValue(message.value);
  const rows: Array<[string, string]> = [
    ["Topic", topic],
    ["Partition", String(partition)],
    ["Offset", formatOffset(message.offset)],
    ["Key", message.key ?? "—"],
    ["Timestamp", message.timestamp ? new Date(message.timestamp).toISOString() : "—"],
  ];
  return (
    <div className="flex h-full min-h-0 flex-col gap-3.5 px-5 pt-4 pb-5 pr-12">
      <span className="text-title-13 text-text-primary">Message</span>
      <dl className="flex flex-col gap-2.5">
        {rows.map(([label, v]) => (
          <div key={label} className="flex items-center gap-2">
            <dt className="w-21 shrink-0 text-label-11-caps text-text-tertiary">{label}</dt>
            <dd className="min-w-0 flex-1 truncate text-code-12 text-text-secondary" title={v}>{v}</dd>
          </div>
        ))}
      </dl>
      <span className="text-label-11-caps text-text-tertiary">Value</span>
      <pre className="min-h-0 flex-1 overflow-auto rounded-md border border-border-subtle bg-surface-well px-3 py-2 text-code-12 whitespace-pre-wrap text-text-secondary select-text">
        {value.text}
      </pre>
      <div className="flex items-center gap-2">
        <Chip>{value.json ? "json" : "raw"}</Chip>
      </div>
    </div>
  );
}
