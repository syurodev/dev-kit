import { describe, expect, test } from "bun:test";
import { parseAllDocuments } from "yaml";
import { applySql, formatYaml, parseYaml, sqlBlock, structure } from "./model";
const edited = (text: string, change: { from: number; to: number; insert: string }) => text.slice(0, change.from) + change.insert + text.slice(change.to);
const values = (text: string) => parseAllDocuments(text, { merge: true }).map((doc) => doc.toJS());

describe("YAML document editing", () => {
  test("formats on demand, keeping values, comments, anchors and SQL exact", () => {
    const text = '# keep\ndefaults: &defaults\n    enabled: true\nservice:\n    <<: *defaults\n    sql: |- # SQL note\n        SELECT 1;\n          -- aligned SQL comment\n    description: >-\n        folded words\n        next line\n';
    const output = edited(text, formatYaml(parseYaml(text)));
    expect(values(output)).toEqual(values(text));
    expect(output).toContain('# keep');
    expect(output).toContain('# SQL note');
    expect(output).toContain('&defaults');
    expect(output).toContain('*defaults');
    expect(output).toContain('service:\n  <<: *defaults');
  });
  test("preserves multiple documents and CRLF", () => {
    const text = '---\r\nfirst:\r\n    name: one\r\n---\r\nsecond:\r\n    name: two\r\n';
    const output = edited(text, formatYaml(parseYaml(text)));
    expect(values(output)).toEqual(values(text));
    expect(output.replace(/\r\n/g, '')).not.toContain('\n');
  });
  test("formats selected complete fields, preserving everything outside them", () => {
    const text = '# before\nroot:\n    selected: {one: 1, two: 2}\n    untouched:   value\n';
    const from = text.indexOf('    selected'), to = text.indexOf('    untouched');
    const change = formatYaml(parseYaml(text), { from, to });
    expect(change.from).toBe(from);
    expect(change.to).toBe(to);
    const output = edited(text, change);
    expect(output.startsWith(text.slice(0, from))).toBe(true);
    expect(output.endsWith(text.slice(to))).toBe(true);
    expect(values(output)).toEqual(values(text));
  });
  test("refuses formatting invalid syntax or a fragment inside SQL", () => {
    expect(() => formatYaml(parseYaml('a: [1,\n'))).toThrow('Fix YAML syntax');
    const text = 'sql: |\n  SELECT 1;\n  SELECT 2;\n';
    expect(() => formatYaml(parseYaml(text), { from: text.indexOf('  SELECT 2'), to: text.length })).toThrow('complete YAML');
  });
});
describe("SQL scalar editing", () => {
  for (const header of ['|', '|-', '|+', '>', '>2-']) test(`replaces ${header} safely, retaining surrounding source`, () => {
    const text = `# before\nitem:\n    sql: ${header} # keep header comment\n      SELECT 1;\nnext:    unchanged\n`;
    const block = sqlBlock(parseYaml(text), text.indexOf('SELECT'))!;
    expect(block).not.toBeNull();
    const code = 'SELECT 2;\n  -- exact SQL indentation\n';
    const change = applySql(parseYaml(text), block, code);
    const output = edited(text, change);
    expect(values(output)[0].item.sql).toBe(code);
    expect(output.startsWith(text.slice(0, block.from))).toBe(true);
    expect(output.endsWith('next:    unchanged\n')).toBe(true);
    expect(output).toContain('# keep header comment');
  });
  test("tracks the path after unrelated edits but refuses a changed SQL block", () => {
    const text = 'name: original\nlist:\n  - sql: |-\n      SELECT 1;\n';
    const block = sqlBlock(parseYaml(text), text.indexOf('SELECT'))!;
    const newer = text.replace('original', 'a longer name');
    const output = edited(newer, applySql(parseYaml(newer), block, 'SELECT 2;'));
    expect(values(output)[0].list[0].sql).toBe('SELECT 2;');
    expect(output).toContain('name: a longer name');
    expect(() => applySql(parseYaml(text.replace('SELECT 1', 'SELECT 3')), block, 'SELECT 2;')).toThrow('changed while SQL');
  });
  test("resolves key path and scalar from both the key and SQL body", () => {
    const text = 'changes:\n  - sql: |\n      SELECT 1;\n';
    const model = parseYaml(text);
    expect(structure(model, text.indexOf('sql')).path).toEqual(['changes', 0, 'sql']);
    expect(structure(model, text.indexOf('SELECT')).sql?.path).toEqual(['changes', 0, 'sql']);
  });
});

test("keeps SQL anchors and supports an empty SQL draft", () => {
  const text = 'sql: &statement |\n  SELECT 1;\ncopy: *statement\n';
  const block = sqlBlock(parseYaml(text), text.indexOf('SELECT'))!;
  const output = edited(text, applySql(parseYaml(text), block, 'SELECT 2;\n'));
  expect(output).toContain('&statement');
  expect(values(output)[0].copy).toBe('SELECT 2;\n');
  const plain = 'sql: | # note\n  SELECT 1;\nnext: true\n';
  const empty = edited(plain, applySql(parseYaml(plain), sqlBlock(parseYaml(plain), plain.indexOf('SELECT'))!, ''));
  expect(values(empty)[0].sql).toBe('');
  expect(sqlBlock(parseYaml(empty), empty.indexOf('sql'))).not.toBeNull();
});

test("a key at the end of the previous scalar's range belongs to its own field", () => {
  const text = 'expectedResult: 0\nsql: |\n  SELECT 1;\nnext: true\n';
  expect(structure(parseYaml(text), text.indexOf('sql')).path).toEqual(['sql']);
  expect(structure(parseYaml(text), text.indexOf('next')).path).toEqual(['next']);
});

test("formats YAML with the chosen width while preserving scalar values", () => {
  const source = "# keep\nroot:\n  child:\n    sql: |\n      SELECT 1;\n      SELECT 2;\n";
  const result = edited(source, formatYaml(parseYaml(source), undefined, 4));
  expect(result).toContain("root:\n    child:\n        sql:");
  expect(result).toContain("# keep");
  expect(values(result)).toEqual(values(source));
});
