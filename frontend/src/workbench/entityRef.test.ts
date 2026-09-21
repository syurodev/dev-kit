import { expect, test } from "bun:test";
import { decodeEntityRefUri, encodeEntityRef } from "./entityRef";
import type { EntityRef } from "./intents";

test("encodeEntityRef matches the string form docs/dev-kit/cross-module.mdx defines", () => {
  const ref: EntityRef = {
    module: "codegraph",
    kind: "endpoint",
    id: "POST /refunds",
    label: "POST /refunds",
  };
  expect(encodeEntityRef(ref)).toBe("devkit://codegraph/endpoint/POST%20%2Frefunds?label=POST+%2Frefunds");
});

test("encode then decode round-trips every field", () => {
  const ref: EntityRef = {
    module: "codegraph",
    kind: "file",
    id: "src/main.go",
    label: "main.go",
    project: "/home/me/app",
    line: 42,
  };
  expect(decodeEntityRefUri(encodeEntityRef(ref))).toEqual(ref);
});

test("decodeEntityRefUri falls back to id as the label when none was carried", () => {
  const ref: EntityRef = { module: "tasks", kind: "task", id: "T-1", label: "" };
  const decoded = decodeEntityRefUri(encodeEntityRef(ref));
  expect(decoded?.label).toBe("T-1");
});

test("decodeEntityRefUri refuses anything that is not a devkit:// reference", () => {
  expect(decodeEntityRefUri("https://example.com/x")).toBeNull();
  expect(decodeEntityRefUri("devkit://codegraph")).toBeNull();
  expect(decodeEntityRefUri("not a uri at all")).toBeNull();
});
