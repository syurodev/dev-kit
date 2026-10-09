#!/usr/bin/env bun
// Builds a representative multi-folder workspace on disk for CodeGraph checks.
//
//   bun scripts/codegraph-fixture.mjs <dir>        # writes <dir>/<folders…>, prints a JSON manifest
//
// Folders (each its own git repo unless noted):
//   gateway/                         TypeScript + Express-style routes, src/config.ts, CRLF + Unicode + 200 KB line
//   orders-service/                  Java (Spring) controller/service/repository, src/config.ts, a 300-method class
//   orders-service/libs/common/      NESTED root: a folder of the workspace inside another one
//   shared-lib/                      TypeScript library, src/config.ts (same relative path as the others)
//   platform-config/                 YAML only — no analyzer reads it (unsupported)
//   <very long name>/                a long folder name, Lua-free, TypeScript
//
// Both gateway and orders-service end up with uncommitted changes.
import { execFileSync } from "node:child_process";
import { mkdirSync, symlinkSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const out = resolve(process.argv[2] ?? "./codegraph-fixture");
const LONG = "a-very-long-folder-name-for-the-billing-platform-integration-adapters-eu-west";

function put(folder, rel, content) {
  const p = join(out, folder, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, content);
}
const git = (folder, ...args) =>
  execFileSync("git", ["-C", join(out, folder), "-c", "user.email=fixture@example.test", "-c", "user.name=fixture", ...args], { stdio: "pipe" });
function repo(folder) {
  git(folder, "init", "-q", "-b", "main");
  git(folder, "add", ".");
  git(folder, "commit", "-q", "-m", "initial");
}

// ── gateway (TypeScript) ────────────────────────────────────────────────────
put("gateway", "package.json", JSON.stringify({ name: "gateway", version: "1.0.0", dependencies: { express: "^4.0.0" } }, null, 2));
put("gateway", "tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022", module: "esnext", moduleResolution: "bundler", strict: true, skipLibCheck: true }, include: ["src"] }, null, 2));
put("gateway", "src/config.ts", `export const config = {\n  port: 8080,\n  upstream: "http://orders-service:9000",\n};\n`);
put("gateway", "src/routes.ts", `import { config } from "./config";
import { forward } from "./proxy";

export function register(app: { get(path: string, h: (req: unknown, res: unknown) => void): void; post(path: string, h: (req: unknown, res: unknown) => void): void }) {
  app.get("/api/orders/:id", (req, res) => forward(config.upstream + "/orders", req, res));
  app.post("/api/orders", (req, res) => forward(config.upstream + "/orders", req, res));
}
`);
put("gateway", "src/proxy.ts", `export function forward(target: string, req: unknown, res: unknown): void {\n  void target; void req; void res;\n}\n`);
// CRLF + Unicode (Vietnamese, CJK, emoji outside the BMP) in one file.
put("gateway", "src/unicode.ts", ["export const greeting = \"Xin chào thế giới\"; // needle ✓", "export const cjk = \"你好，世界\"; // needle", "export const emoji = \"😀 needle 😀\";", "export function tail() { return 1; }", ""].join("\r\n"));
// One line of ~200 KB containing a match at the very end.
put("gateway", "src/minified.js", `var x=${JSON.stringify("a".repeat(200_000))};function needleAtTheEnd(){return 1}\n`);

// ── orders-service (Java, Spring-style) ─────────────────────────────────────
put("orders-service", "pom.xml", `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>
<groupId>com.acme</groupId><artifactId>orders-service</artifactId><version>1.0.0</version></project>\n`);
put("orders-service", "src/config.ts", `export const config = { port: 9000 };\n`);
const pkg = "src/main/java/com/acme/orders";
put("orders-service", `${pkg}/OrdersController.java`, `package com.acme.orders;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/orders")
public class OrdersController {
    private final OrdersService service;
    public OrdersController(OrdersService service) { this.service = service; }

    @GetMapping("/{id}")
    public String get(@PathVariable String id) { return service.find(id); }

    @PostMapping
    public String create(@RequestBody String body) { return service.create(body); }
}
`);
put("orders-service", `${pkg}/OrdersService.java`, `package com.acme.orders;

import org.springframework.stereotype.Service;

@Service
public class OrdersService {
    private final OrdersRepository repository;
    public OrdersService(OrdersRepository repository) { this.repository = repository; }
    public String find(String id) { return repository.load(id); }
    public String create(String body) { return repository.store(body); }
}
`);
put("orders-service", `${pkg}/OrdersRepository.java`, `package com.acme.orders;

import org.springframework.stereotype.Repository;

@Repository
public class OrdersRepository {
    public String load(String id) { return "order-" + id; }
    public String store(String body) { return body; }
}
`);
// A class with 300 methods, for Outline and symbol-at-cursor.
put("orders-service", `${pkg}/BigClass.java`, `package com.acme.orders;\n\npublic class BigClass {\n${Array.from({ length: 300 }, (_, i) => `    public int method${i}(int v) {\n        return v + ${i};\n    }\n`).join("")}}\n`);

// 3000 lines that all match "needle", so a search hits its match cap.
put("orders-service", "src/main/resources/dense.txt", Array.from({ length: 3000 }, (_, i) => `needle line ${i}`).join("\n") + "\n");

// NESTED root: a library inside orders-service, also a member of the workspace.
put("orders-service", "libs/common/pom.xml", `<?xml version="1.0"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>
<groupId>com.acme</groupId><artifactId>common</artifactId><version>1.0.0</version></project>\n`);
put("orders-service", "libs/common/src/main/java/com/acme/common/Ids.java", `package com.acme.common;\n\npublic final class Ids {\n    public static String next() { return "id-needle"; }\n}\n`);
put("orders-service", "libs/common/src/config.ts", `export const config = { common: true };\n`);

// ── shared-lib (TypeScript) ─────────────────────────────────────────────────
put("shared-lib", "package.json", JSON.stringify({ name: "shared-lib", version: "1.0.0" }, null, 2));
put("shared-lib", "tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022", module: "esnext", strict: true }, include: ["src"] }, null, 2));
put("shared-lib", "src/config.ts", `export const config = { shared: true };\n`);
put("shared-lib", "src/util.ts", `export function needle(): string {\n  return "needle";\n}\n`);

// ── platform-config (no analyzer) ───────────────────────────────────────────
put("platform-config", "charts/values.yaml", "replicas: 3\nimage: acme/orders:1.0.0\n");
put("platform-config", "main.tf", `resource "null_resource" "needle" {}\n`);

// ── a long folder name ──────────────────────────────────────────────────────
put(LONG, "package.json", JSON.stringify({ name: "billing-adapters", version: "1.0.0" }, null, 2));
put(LONG, "tsconfig.json", JSON.stringify({ compilerOptions: { target: "es2022", strict: true }, include: ["src"] }, null, 2));
put(LONG, "src/config.ts", `export const config = { billing: true };\n`);
put(LONG, "src/deeply/nested/directory/structure/with/many/segments/adapter-implementation-details.ts", `export const adapter = "needle";\n`);

for (const f of ["gateway", "orders-service", "shared-lib", "platform-config", LONG]) repo(f);

// Uncommitted changes in two repositories.
put("gateway", "src/config.ts", `export const config = {\n  port: 8081,\n  upstream: "http://orders-service:9000",\n};\n`);
put("orders-service", `${pkg}/OrdersRepository.java`, `package com.acme.orders;\n\nimport org.springframework.stereotype.Repository;\n\n@Repository\npublic class OrdersRepository {\n    public String load(String id) { return "order-v2-" + id; }\n    public String store(String body) { return body; }\n}\n`);
put("shared-lib", "src/untracked.ts", "export const fresh = true;\n");

// A symlink that points outside every root: it must never be followed.
const outside = join(out, "_outside");
put("_outside", "secret.txt", "needle outside of every root\n");
try {
  symlinkSync(outside, join(out, "gateway", "src", "escape"));
} catch {}

console.log(
  JSON.stringify(
    {
      root: out,
      folders: ["gateway", "orders-service", "orders-service/libs/common", "shared-lib", "platform-config", LONG].map((f) => join(out, f)),
      primary: join(out, "gateway"),
      longName: LONG,
    },
    null,
    2,
  ),
);
