import { describe, expect, test } from "bun:test";
import "../manifest";
import { dispatchIntent, hasIntent } from "@/workbench/intents";
import { renderToolResult } from "@/workbench/toolResultViews";

const view = (name: string, rawInput: unknown, rawOutput: unknown) => renderToolResult({ name, rawInput, rawOutput });

describe("kafka tool result views", () => {
  test("each tool's result gets a card, under its Go name and Claude's wire name", () => {
    expect(view("kafka.peekMessages", { topic: "t", partition: 0 }, { result: [{ offset: 1, value: "v" }] })).not.toBeNull();
    expect(view("mcp__devkit__kafka_peekMessages", { topic: "t" }, [{ offset: 1, value: "v" }])).not.toBeNull();
    expect(view("kafka.listTopics", {}, { result: [{ name: "t" }] })).not.toBeNull();
    expect(view("kafka.describeTopic", { topic: "t" }, { name: "t", replication_factor: 1, partitions: [{ id: 0 }] })).not.toBeNull();
    expect(view("kafka.listConsumerGroups", {}, { result: [{ group_id: "g" }] })).not.toBeNull();
    expect(view("kafka.publishMessage", { topic: "t" }, { partition: 0, offset: 3 })).not.toBeNull();
  });

  test("a shape the card cannot read falls back to the generic view", () => {
    expect(view("kafka.peekMessages", { topic: "t" }, { truncated: true })).toBeNull();
    expect(view("kafka.listTopics", {}, "not json")).toBeNull();
    expect(view("kafka.describeTopic", {}, { name: "t" })).toBeNull();
    expect(view("kafka.listConsumerGroups", {}, [{ id: 1 }])).toBeNull();
    expect(view("kafka.publishMessage", {}, { offset: 1 })).toBeNull();
  });
});

describe("kafka.reveal", () => {
  test("is registered", () => {
    expect(hasIntent("kafka.reveal")).toBe(true);
  });

  test("answers unavailable for anything but a named topic", async () => {
    expect(await dispatchIntent("kafka.reveal", { module: "kafka", kind: "group", id: "g", label: "g" })).toBe("unavailable");
    expect(await dispatchIntent("kafka.reveal", { module: "kafka", kind: "topic", id: "", label: "" })).toBe("unavailable");
  });
});
