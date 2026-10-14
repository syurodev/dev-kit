import { describe, expect, test } from "bun:test";
import {
  parseConsumerGroups,
  parsePeekMessages,
  parsePublishResult,
  parseTopicDetail,
  parseTopicList,
  toggleRow,
  valuePreview,
} from "./parse";

describe("kafka.peekMessages", () => {
  const messages = [
    { offset: 41, key: "user-1", value: '{"a":1}', timestamp: 1_700_000_000_000 },
    { offset: 42, value: "plain" },
  ];

  test("reads topic and partition from the input and the records from the output", () => {
    expect(parsePeekMessages({ topic: "orders", partition: 2, limit: 10 }, messages)).toEqual({
      topic: "orders",
      partition: 2,
      messages: [
        { offset: 41, key: "user-1", value: '{"a":1}', timestamp: 1_700_000_000_000 },
        { offset: 42, key: undefined, value: "plain", timestamp: undefined },
      ],
    });
  });

  test("unwraps structured()'s {result: […]} envelope", () => {
    expect(parsePeekMessages({ topic: "orders", partition: 0 }, { result: messages })?.messages).toHaveLength(2);
  });

  test("an input without a partition is partition 0, as the tool reads it", () => {
    expect(parsePeekMessages({ topic: "orders" }, messages)?.partition).toBe(0);
  });

  test("an empty peek is still an answer", () => {
    expect(parsePeekMessages({ topic: "orders", partition: 0 }, [])).toEqual({ topic: "orders", partition: 0, messages: [] });
  });

  test("anything that is not a list of records falls back to the generic view", () => {
    expect(parsePeekMessages({ topic: "orders" }, { not: "records" })).toBeNull();
    expect(parsePeekMessages({ topic: "orders" }, [{ offset: "x", value: "v" }])).toBeNull();
    expect(parsePeekMessages({ topic: "orders" }, [{ offset: 1 }])).toBeNull();
    expect(parsePeekMessages({ topic: "orders" }, null)).toBeNull();
  });
});

describe("valuePreview", () => {
  test("folds a value to one line", () => {
    expect(valuePreview('{\n  "a": 1,\n  "b": 2\n}')).toBe('{ "a": 1, "b": 2 }');
  });

  test("cuts at 120 characters with an ellipsis", () => {
    const preview = valuePreview("x".repeat(500));
    expect(preview.length).toBe(120);
    expect(preview.endsWith("…")).toBe(true);
    expect(valuePreview("y".repeat(120))).toBe("y".repeat(120));
  });

  test("reads only the head of a huge value", () => {
    const huge = `{"id":1,${"\n  ".repeat(10)}"blob":"${"q".repeat(2_000_000)}"}`;
    const preview = valuePreview(huge);
    expect(preview.startsWith('{"id":1, "blob":"qqq')).toBe(true);
    expect(preview.length).toBe(120);
  });

  test("a head of mostly whitespace still says there is more", () => {
    expect(valuePreview(`a${" ".repeat(600)}b`)).toBe("a…");
  });
});

describe("toggleRow", () => {
  test("opens a row, closes it again, and moves to another", () => {
    expect(toggleRow(null, 3)).toBe(3);
    expect(toggleRow(3, 3)).toBeNull();
    expect(toggleRow(3, 5)).toBe(5);
  });
});

describe("kafka.listTopics", () => {
  test("reads topic names", () => {
    expect(parseTopicList({ result: [{ name: "orders" }, { name: "payments" }] })).toEqual(["orders", "payments"]);
    expect(parseTopicList([])).toEqual([]);
  });

  test("anything else is null", () => {
    expect(parseTopicList([{ id: 1 }])).toBeNull();
    expect(parseTopicList({ name: "orders" })).toBeNull();
  });
});

describe("kafka.describeTopic", () => {
  test("reads name, replication factor and partitions", () => {
    expect(parseTopicDetail({ name: "orders", replication_factor: 3, partitions: [{ id: 0 }, { id: 1 }] })).toEqual({
      name: "orders",
      replicationFactor: 3,
      partitions: [0, 1],
    });
  });

  test("anything else is null", () => {
    expect(parseTopicDetail({ name: "orders" })).toBeNull();
    expect(parseTopicDetail({ partitions: [] })).toBeNull();
    expect(parseTopicDetail([])).toBeNull();
  });
});

describe("kafka.listConsumerGroups", () => {
  test("reads groups; a lag Go left out is zero", () => {
    expect(parseConsumerGroups({ result: [{ group_id: "billing", lag: 12 }, { group_id: "audit" }] })).toEqual([
      { groupId: "billing", lag: 12 },
      { groupId: "audit", lag: 0 },
    ]);
  });

  test("anything else is null", () => {
    expect(parseConsumerGroups([{ name: "x" }])).toBeNull();
    expect(parseConsumerGroups("text")).toBeNull();
  });
});

describe("kafka.publishMessage", () => {
  test("reads where the record landed, and the topic from the input", () => {
    expect(parsePublishResult({ topic: "orders", value: "{}" }, { partition: 1, offset: 99, timestamp: 1_700_000_000_000 })).toEqual({
      topic: "orders",
      partition: 1,
      offset: 99,
      timestamp: 1_700_000_000_000,
    });
  });

  test("a result that is not an acknowledgement is null", () => {
    expect(parsePublishResult({ topic: "orders" }, { offset: 1 })).toBeNull();
    expect(parsePublishResult({ topic: "orders" }, null)).toBeNull();
  });

  test("an input with no topic still shows where it landed", () => {
    expect(parsePublishResult({}, { partition: 0, offset: 1 })?.topic).toBe("");
  });
});
