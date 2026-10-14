import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { formatClock } from "../kafkaForm";
import {
  ConsumerGroupsResultCard,
  MessageValue,
  openTopic,
  PeekResultCard,
  PublishResultCard,
  TopicDetailResultCard,
  TopicsResultCard,
} from "./KafkaResultCards";

const text = (html: string) => html.replace(/<!-- -->/g, "");

describe("PeekResultCard", () => {
  const peek = {
    topic: "orders",
    partition: 2,
    messages: [
      { offset: 41, key: "user-1", value: `{"id":1,"note":"${"z".repeat(300)}"}`, timestamp: 1_700_000_000_000 },
      { offset: 42, key: undefined, value: "plain text", timestamp: undefined },
    ],
  };

  test("names the topic and partition, and counts the messages", () => {
    const html = text(renderToString(<PeekResultCard peek={peek} />));
    expect(html).toContain("Messages · orders · partition 2");
    expect(html).toContain("2 messages");
  });

  test("lays the records out in a DataTable: offset, key, value preview, time", () => {
    const html = text(renderToString(<PeekResultCard peek={peek} />));
    expect(html).toContain("table-fixed");
    for (const header of ["Offset", "Key", "Value", "Time"]) expect(html).toContain(`>${header}`);
    expect(html).toContain("user-1");
    expect(html).toContain(formatClock(1_700_000_000_000));
    // The preview is one line of at most 120 characters, never the whole value.
    expect(html).not.toContain("z".repeat(200));
    expect(html).toContain("…");
  });

  test("offsets are mono and right-aligned", () => {
    const html = renderToString(<PeekResultCard peek={peek} />);
    const firstCell = html.split("<tbody")[1].split("<td")[1];
    expect(firstCell).toContain("text-right");
    expect(firstCell).toContain("text-code-12");
  });

  test("an absent key reads as a dash, not NULL", () => {
    const html = text(renderToString(<PeekResultCard peek={peek} />));
    const secondRow = html.split("<tbody")[1].split("<tr")[2];
    expect(secondRow.split("<td")[2]).toContain(">—<");
    expect(html).not.toContain("NULL");
  });

  test("rows can be opened — they are focusable click targets", () => {
    const html = renderToString(<PeekResultCard peek={peek} />);
    expect(html.split("<tbody")[1].match(/<tr tabindex="0"/g)?.length).toBe(2);
  });

  test("rows are named by their offset and say they are closed until opened", () => {
    const html = renderToString(<PeekResultCard peek={peek} />);
    expect(html).toContain('aria-label="Show message at offset 41"');
    expect(html).toContain('aria-label="Show message at offset 42"');
    expect(html.match(/aria-expanded="false"/g)?.length).toBe(2);
    expect(html).not.toContain("data-expanded");
  });

  test("offers Open in Kafka", () => {
    expect(renderToString(<PeekResultCard peek={peek} />)).toContain("Open in Kafka");
  });

  test("a single message is singular, an empty peek says so", () => {
    expect(text(renderToString(<PeekResultCard peek={{ ...peek, messages: [peek.messages[1]] }} />))).toContain("1 message<");
    const empty = text(renderToString(<PeekResultCard peek={{ ...peek, messages: [] }} />));
    expect(empty).toContain("0 messages");
    expect(empty).toContain("No messages");
    expect(empty).not.toContain("table-fixed");
  });
});

describe("MessageValue", () => {
  test("a JSON value is pretty-printed and tagged json", () => {
    const html = text(renderToString(<MessageValue message={{ offset: 7, value: '{"a":1}' }} />));
    expect(html).toContain('data-testid="kafka-message-value"');
    expect(html).toContain("&quot;a&quot;: 1");
    expect(html).toContain(">json<");
  });

  test("anything else is shown as it is and tagged raw", () => {
    const html = text(renderToString(<MessageValue message={{ offset: 7, value: "not json" }} />));
    expect(html).toContain("not json");
    expect(html).toContain(">raw<");
  });
});

describe("TopicsResultCard", () => {
  test("lists the topics in a compact table with a count", () => {
    const html = text(renderToString(<TopicsResultCard topics={["orders", "payments"]} />));
    expect(html).toContain("Topics");
    expect(html).toContain("2 topics");
    expect(html).toContain("orders");
    expect(html).toContain("payments");
    expect(html).toContain("table-fixed");
  });

  test("each row says it opens that topic in Kafka", () => {
    const html = renderToString(<TopicsResultCard topics={["orders", "payments"]} />);
    expect(html).toContain('aria-label="Open orders in Kafka"');
    expect(html).toContain('aria-label="Open payments in Kafka"');
    expect(html).toContain('role="button"');
    expect(html).not.toContain("aria-expanded");
  });

  test("an empty cluster says so", () => {
    expect(text(renderToString(<TopicsResultCard topics={[]} />))).toContain("No topics");
  });
});

describe("TopicDetailResultCard", () => {
  test("shows partitions and replication as definition rows", () => {
    const html = text(
      renderToString(<TopicDetailResultCard detail={{ name: "orders", replicationFactor: 3, partitions: [0, 1, 2] }} />),
    );
    expect(html).toContain("Topic · orders");
    expect(html).toContain("3 partitions");
    expect(html).toContain("Replication");
    expect(html).toContain("0, 1, 2");
    expect(html).toContain("Open in Kafka");
  });
});

describe("ConsumerGroupsResultCard", () => {
  test("lists groups with their lag", () => {
    const html = text(
      renderToString(<ConsumerGroupsResultCard groups={[{ groupId: "billing", lag: 1200 }, { groupId: "audit", lag: 0 }]} />),
    );
    expect(html).toContain("Consumer groups");
    expect(html).toContain("2 groups");
    expect(html).toContain("billing");
    expect(html).toContain(">Lag");
    expect(html).toContain("1 200");
  });
});

describe("PublishResultCard", () => {
  test("says where the record landed", () => {
    const html = text(
      renderToString(<PublishResultCard result={{ topic: "orders", partition: 1, offset: 99, timestamp: 1_700_000_000_000 }} />),
    );
    expect(html).toContain("Published to orders");
    expect(html).toContain(`partition 1 · offset 99 · ${formatClock(1_700_000_000_000)}`);
    expect(html).toContain("Open in Kafka");
  });

  test("leaves the time out when the broker gave none", () => {
    const html = text(renderToString(<PublishResultCard result={{ topic: "orders", partition: 0, offset: 5 }} />));
    expect(html).toContain("partition 0 · offset 5<");
  });
});

describe("openTopic", () => {
  test("asks kafka.reveal for the topic and stays quiet when it opened", async () => {
    const asked: unknown[] = [];
    const toasts: unknown[] = [];
    await openTopic(
      "orders",
      async (name, ref) => {
        asked.push([name, ref.kind, ref.id]);
        return "ok";
      },
      (toast) => toasts.push(toast),
    );
    expect(asked).toEqual([["kafka.reveal", "topic", "orders"]]);
    expect(toasts).toEqual([]);
  });

  test("says so when the topic could not be opened", async () => {
    const toasts: { title: string; tone?: string }[] = [];
    await openTopic("orders", async () => "unavailable", (toast) => toasts.push(toast));
    expect(toasts).toHaveLength(1);
    expect(toasts[0].tone).toBe("error");
    expect(toasts[0].title).toContain("orders");
    expect(toasts[0].title).toContain("connected");
  });
});
