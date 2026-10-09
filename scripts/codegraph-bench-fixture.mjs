#!/usr/bin/env bun
// ~10k-file, six-folder workspace for measuring CodeGraph (same shape as
// internal/wssearch/bench_test.go): duplicate relative paths in two roots, a folder nested
// in another, one 2 MB line, one file where every line matches.
//
//   bun scripts/codegraph-bench-fixture.mjs <dir>
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const out = resolve(process.argv[2] ?? "./codegraph-bench");
const body = (i) => Array.from({ length: 40 }, (_, l) => `func handler${i}_${l}(ctx context.Context) error { return process(ctx, ${l}) } // héllo`).join("\n") + "\n";
const put = (root, rel, content) => {
  const p = join(out, root, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, content);
};
for (let i = 0; i < 2000; i++) put("orders", `src/pkg${i % 40}/file${i}.go`, body(i));
for (let i = 0; i < 2000; i++) put("billing", `src/pkg${i % 40}/file${i}.go`, body(i));
for (let i = 0; i < 2500; i++) put("platform", `services/svc${i % 12}/src/f${i}.go`, body(i));
for (let i = 0; i < 500; i++) put("platform", `libs/shared/src/m${i % 20}/x${i}.go`, body(i)); // the nested folder
for (let i = 0; i < 1500; i++) put("shared-lib", `lib/m${i % 30}/x${i}.go`, body(i));
for (let i = 0; i < 1500; i++) put("config", `env/c${i}.yaml`, body(i));
put("orders", "src/minified.js", "a;needle(1);".repeat(170000));
put("billing", "src/dense.go", "needle needle needle\n".repeat(20000));
console.log(JSON.stringify({ root: out, folders: ["orders", "billing", "platform", "platform/libs/shared", "shared-lib", "config"].map((f) => join(out, f)) }));
