import { describe, expect, test } from "bun:test";
import { handleYaml } from "./service";
import type { Analysis, Suggestions } from "./types";
const analyze = (text: string, file = 'test.yaml', schema: any = 'auto') => handleYaml({ type: 'analyze', text, file, schema, position: 0 }) as Promise<Analysis>;
const complete = (text: string, file = 'compose.yaml', schema: any = 'auto') => handleYaml({ type: 'complete', text, file, schema, position: text.length, explicit: true }) as Promise<Suggestions>;

describe("offline YAML assistance", () => {
  test("reports parser errors and duplicate keys at exact offsets", async () => {
    const text = 'name: one\nname: two\n';
    const result = await analyze(text);
    expect(result.problems.some((problem) => problem.from === text.lastIndexOf('name') && problem.message.includes('unique'))).toBe(true);
    expect((await analyze('items: [one,\n')).problems.length).toBeGreaterThan(0);
  });
  test("validates Compose keys and respects syntax-only mode", async () => {
    const text = 'services:\n  app:\n    imagge: nginx\n';
    expect((await analyze(text, 'compose.yaml')).problems.some((problem) => problem.message.includes('imagge'))).toBe(true);
    expect((await analyze(text, 'compose.yaml', 'none')).problems).toEqual([]);
  });
  test("suggests nested Compose keys, skipping existing sibling keys", async () => {
    const result = await complete('services:\n  app:\n    image: nginx\n    ');
    expect(result?.options.some((option) => option.label === 'ports')).toBe(true);
    expect(result?.options.some((option) => option.label === 'image')).toBe(false);
    const partial = await complete('services:\n  app:\n    ima');
    expect(partial?.options.some((option) => option.label === 'image')).toBe(true);
  });
  test("suggests enumerated values and stays out of SQL/comment bodies", async () => {
    const result = await complete('services:\n  app:\n    restart: ');
    expect(result?.options.some((option) => option.label === 'always')).toBe(true);
    expect(await complete('services:\n  # hel')).toBeNull();
    expect(await complete('databaseChangeLog:\n  - changeSet:\n      changes:\n        - sql:\n            sql: |\n              SEL', 'change.yaml')).toBeNull();
  });
  test("uses Liquibase schema automatically and completes change-set fields", async () => {
    const text = 'databaseChangeLog:\n  - changeSet:\n      id: first\n      author: me\n      changes: []\n';
    const result = await analyze(text);
    expect(result.schema).toContain('Liquibase');
    expect(result.problems).toEqual([]);
    const options = await complete('databaseChangeLog:\n  - changeSet:\n      ', 'change.yaml');
    expect(options?.options.some((option) => option.label === 'author')).toBe(true);
  });
  test("selects the Kubernetes API kind, validates fields, and suggests spec fields", async () => {
    const text = 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\nspec:\n  replicas: wrong\n';
    const result = await analyze(text);
    expect(result.schema).toContain('Deployment');
    expect(result.problems.some((problem) => problem.message.includes('integer'))).toBe(true);
    const options = await complete(text + '  ', 'deployment.yaml');
    expect(options?.options.some((option) => option.label === 'template')).toBe(true);
    const unknown = await analyze('apiVersion: example.dev/v1\nkind: CustomResource\n', 'resource.yaml');
    expect(unknown.notice).toContain('No bundled schema');
  });
  test("validates each YAML document with its own schema", async () => {
    const text = 'apiVersion: v1\nkind: ConfigMap\nmetadata: { name: first }\ndata: { name: text }\n---\napiVersion: v1\nkind: ConfigMap\nmetadata: { name: second }\ndata: { name: 123 }\n';
    const result = await analyze(text);
    expect(result.problems.some((problem) => problem.from > text.indexOf('---') && problem.message.includes('string'))).toBe(true);
  });
});

test("accepts hand-authored Liquibase precondition maps and validates their values", async () => {
  const text = 'databaseChangeLog:\n  - changeSet:\n      id: test\n      author: me\n      preConditions:\n        onFail: MARK_RAN\n        sqlCheck:\n          expectedResult: 0\n          sql: SELECT 1\n      changes: []\n';
  expect((await analyze(text)).problems).toEqual([]);
  expect((await analyze(text.replace('MARK_RAN', 'TYPO'))).problems.length).toBeGreaterThan(0);
});

test("offers root keys in an empty document when a schema is selected", async () => {
  const result = await complete('', 'new.yaml', 'liquibase');
  expect(result?.options.some((option) => option.label === 'databaseChangeLog')).toBe(true);
  const kube = await complete('', 'new.yaml', 'kubernetes');
  expect(kube?.options.some((option) => option.label === 'apiVersion')).toBe(true);
});

test("a value suggestion replaces the complete quoted scalar", async () => {
  const text = 'services:\n  app:\n    restart: "al';
  const result = await complete(text);
  const option = result?.options.find((option) => option.label === 'always');
  expect(option).toBeDefined();
  const updated = text.slice(0, result!.from) + option!.insert + text.slice(result!.to);
  expect(updated).toContain('restart: always');
  expect((await analyze(updated, 'compose.yaml')).problems).toEqual([]);
});
