#!/usr/bin/env bun
// Writes THIRD_PARTY_LICENSES.md: the open-source licenses of what ships inside DevKit.
//
//   bun scripts/third-party-licenses.mjs           # rewrite the file
//   bun scripts/third-party-licenses.mjs --check   # exit 1 if the file is out of date
//
// What it reads (nothing is guessed from a name; every entry carries the license text found with the code):
//   - Go modules linked into the app (darwin/linux/windows, GUI and `-tags server`) and into the
//     bundled TypeScript analyzer (`analyzer-ts`, tsgo) — from the module cache.
//   - npm packages reachable from frontend/package.json `dependencies` (what Vite bundles) — from
//     frontend/node_modules, so run `bun install` in frontend/ first.
//   - Maven runtime dependencies of the Java analyzer jar (`analyzer/`) — `mvn dependency:list`, jars in ~/.m2.
//   - The fonts shipped in frontend/ (Geist through @fontsource, Inter from public/).
//
// Not covered: the Claude / Codex agent runtimes, which DevKit downloads on the person's machine at run
// time and never bundles, and dev-only tooling that does not reach the app.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outFile = join(root, "THIRD_PARTY_LICENSES.md");
const run = (cmd, args, cwd = root, env = {}) =>
  execFileSync(cmd, args, { cwd, encoding: "utf8", maxBuffer: 64 << 20, env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] });

const LICENSE_FILE = /^(licen[cs]e|copying|unlicense|notice)([-._].*)?$/i;

/** @typedef {{ source: string, name: string, version: string, id: string, declared: string, texts: string[], url?: string }} Entry */
/** @type {Entry[]} */
const entries = [];

/** A short SPDX-ish id read from the text itself; "" when it cannot be told. */
function classify(text) {
  const t = text.replace(/\s+/g, " ");
  if (/Apache License,? Version 2\.0/i.test(t)) return "Apache-2.0";
  if (/Mozilla Public License,? (Version |v\. ?)?2\.0/i.test(t)) return "MPL-2.0";
  if (/SIL OPEN FONT LICENSE Version 1\.1/i.test(t)) return "OFL-1.1";
  if (/GNU LESSER GENERAL PUBLIC LICENSE/i.test(t)) return "LGPL";
  if (/GNU GENERAL PUBLIC LICENSE/i.test(t)) return "GPL";
  if (/Permission to use, copy, modify, and\/or distribute this software for any purpose with or without fee/i.test(t)) return "ISC";
  if (/Permission is hereby granted, free of charge, to any person obtaining a copy/i.test(t)) return "MIT";
  if (/Redistribution and use in source and binary forms/i.test(t)) {
    return /Neither the name|may be used to endorse or promote/i.test(t) ? "BSD-3-Clause" : "BSD-2-Clause";
  }
  if (/This is free and unencumbered software released into the public domain/i.test(t)) return "Unlicense";
  if (/Permission to use, copy, modify, and distribute this software/i.test(t)) return "ISC-style";
  return "";
}

function readLicenseFiles(dir) {
  if (!dir || !existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && LICENSE_FILE.test(e.name.replace(/\.(txt|md|markdown)$/i, "")))
    .map((e) => readFileSync(join(dir, e.name), "utf8").trim())
    .filter(Boolean);
}

// ---------------------------------------------------------------- Go
function goModules(dir, label, matrix) {
  const seen = new Map();
  for (const [goos, tags] of matrix) {
    let out;
    try {
      out = run(
        "go",
        ["list", ...(tags ? ["-tags", tags] : []), "-deps", "-f", "{{if and .Module (not .Standard) (not .Module.Main)}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}", "./..."],
        dir,
        { GOOS: goos, CGO_ENABLED: "1", GOFLAGS: "-mod=mod" },
      );
    } catch (err) {
      // A target that cannot be listed here (cgo for another OS) still compiles its pure-Go deps elsewhere.
      out = String(err.stdout ?? "");
    }
    for (const line of out.split("\n")) {
      const [path, version, modDir] = line.split("\t");
      if (path && modDir && !seen.has(`${path}@${version}`)) seen.set(`${path}@${version}`, { path, version, modDir });
    }
  }
  for (const { path, version, modDir } of seen.values()) {
    const texts = readLicenseFiles(modDir);
    entries.push({ source: label, name: path, version, id: classify(texts.join("\n")), declared: "", texts });
  }
}

// ---------------------------------------------------------------- npm
function npmPackages() {
  const fe = join(root, "frontend");
  const nm = join(fe, "node_modules");
  if (!existsSync(nm)) throw new Error("frontend/node_modules is missing: run `bun install` in frontend/ first");
  const rootPkg = JSON.parse(readFileSync(join(fe, "package.json"), "utf8"));
  const found = new Map();
  const find = (name, from) => {
    for (let dir = from; ; dir = dirname(dir)) {
      const p = join(dir, "node_modules", name);
      if (existsSync(join(p, "package.json"))) return p;
      if (dir === fe || dir === dirname(dir)) break;
    }
    const p = join(nm, name);
    return existsSync(join(p, "package.json")) ? p : null;
  };
  const visit = (name, from) => {
    const dir = find(name, from);
    if (!dir || found.has(dir)) return;
    const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
    found.set(dir, pkg);
    for (const dep of Object.keys({ ...pkg.dependencies, ...pkg.optionalDependencies })) visit(dep, dir);
  };
  for (const dep of Object.keys(rootPkg.dependencies ?? {})) visit(dep, fe);
  for (const [dir, pkg] of found) {
    const texts = readLicenseFiles(dir);
    const declared = typeof pkg.license === "string" ? pkg.license : pkg.license?.type ?? (pkg.licenses ?? []).map((l) => l.type).join(" OR ");
    const repo = typeof pkg.repository === "string" ? pkg.repository : pkg.repository?.url ?? "";
    const url = (repo || pkg.homepage || "").replace(/^git\+/, "").replace(/\.git$/, "");
    entries.push({ source: "npm (frontend)", name: pkg.name, version: pkg.version, id: classify(texts.join("\n")) || declared, declared, texts, url });
  }
}

// ---------------------------------------------------------------- Maven
function mavenPackages() {
  const out = join(tmpdir(), `devkit-mvn-deps-${process.pid}.txt`);
  run("mvn", ["-q", "-f", join(root, "analyzer", "pom.xml"), "dependency:list", "-DincludeScope=runtime", `-DoutputFile=${out}`]);
  const m2 = join(homedir(), ".m2", "repository");
  const stripAnsi = (s) => s.replace(/\x1b\[[0-9;]*m/g, "");
  for (const raw of readFileSync(out, "utf8").split("\n")) {
    const m = stripAnsi(raw).trim().match(/^([^:\s]+):([^:\s]+):jar:([^:\s]+):/);
    if (!m) continue;
    const [, group, artifact, version] = m;
    if (version.startsWith("9999.0-empty")) continue; // Guava's empty placeholder jar: no code, no license.
    const base = join(m2, ...group.split("."), artifact, version);
    const pom = existsSync(join(base, `${artifact}-${version}.pom`)) ? readFileSync(join(base, `${artifact}-${version}.pom`), "utf8") : "";
    const declared = [...pom.matchAll(/<license>[\s\S]*?<name>([\s\S]*?)<\/name>/g)].map((x) => x[1].trim()).join(" / ");
    const jar = join(base, `${artifact}-${version}.jar`);
    const texts = [];
    if (existsSync(jar)) {
      const names = run("unzip", ["-Z1", jar]).split("\n").filter((n) => /^META-INF\/(licen[cs]e|notice|copying)[^/]*$/i.test(n));
      for (const n of names) texts.push(execFileSync("unzip", ["-p", jar, n], { encoding: "utf8", maxBuffer: 16 << 20 }).trim());
    }
    entries.push({ source: "Maven (analyzer jar)", name: `${group}:${artifact}`, version, id: classify(texts.join("\n")) || "", declared, texts: texts.filter(Boolean) });
  }
}

// ---------------------------------------------------------------- fonts
function fonts() {
  const geist = join(root, "frontend", "node_modules", "@fontsource-variable");
  for (const name of ["geist", "geist-mono"]) {
    const dir = join(geist, name);
    if (!existsSync(dir)) continue;
    const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
    // The npm entry already lists these; the font files are what ships, so say it once more by name.
    entries.push({ source: "Font", name: `${pkg.name} (font files)`, version: pkg.version, id: classify(readLicenseFiles(dir).join("\n")) || pkg.license, declared: pkg.license, texts: [] });
  }
}

// ---------------------------------------------------------------- build
function normalised(text) {
  return text.replace(/\s+/g, " ").trim();
}
function digest(text) {
  return createHash("sha256").update(normalised(text)).digest("hex").slice(0, 12);
}

goModules(root, "Go (DevKit)", [["darwin", ""], ["darwin", "server"], ["linux", ""], ["linux", "server"], ["windows", ""]]);
goModules(join(root, "analyzer-ts"), "Go (TypeScript analyzer, tsgo)", [["darwin", ""], ["linux", ""], ["windows", ""]]);
npmPackages();
mavenPackages();
fonts();

// A text a package lacks falls back to the canonical text of its license seen elsewhere in this run
// (Apache-2.0 jars that ship no LICENSE file); the entry says so.
const canonical = new Map();
for (const e of entries) {
  for (const t of e.texts) {
    const id = classify(t);
    if (id === "Apache-2.0" && t.length > 9000 && !canonical.has(id)) canonical.set(id, t);
  }
}
const OFL_FILE = join(root, "frontend", "node_modules", "@fontsource-variable", "geist", "LICENSE");
const ofl = existsSync(OFL_FILE) ? readFileSync(OFL_FILE, "utf8").trim() : "";

/** @type {Map<string, { text: string, id: string, who: string[] }>} */
const groups = new Map();
const unresolved = [];
const add = (text, id, who) => {
  const key = digest(text);
  const g = groups.get(key) ?? { text, id, who: [] };
  g.who.push(who);
  groups.set(key, g);
};
const seenWho = new Set();
for (const e of entries.sort((a, b) => a.source.localeCompare(b.source) || a.name.localeCompare(b.name))) {
  const who = `${e.name}@${e.version} (${e.source})`;
  if (seenWho.has(who)) continue;
  seenWho.add(who);
  let texts = e.texts;
  if (e.source === "Font") continue;
  if (texts.length === 0 && /apache/i.test(e.declared + e.id) && canonical.has("Apache-2.0")) {
    texts = [canonical.get("Apache-2.0")];
    e.id = "Apache-2.0 (standard text)";
  }
  if (texts.length === 0) {
    unresolved.push(`${e.name}@${e.version} (${e.source}) — declared: ${e.declared || e.id || "nothing"}${e.url ? ` — ${e.url}` : ""}`);
    continue;
  }
  for (const t of texts) add(t, classify(t) || e.id || "license", who);
}

// Read from the font's own name table (ids 0 and 13), not remembered.
const interCopyright = "Copyright 2020 The Inter Project Authors (https://github.com/rsms/inter)";
const summary = new Map();
for (const e of entries) {
  if (e.source === "Font") continue;
  const k = e.id || "unknown";
  summary.set(k, (summary.get(k) ?? 0) + 1);
}

const lines = [];
lines.push("# Third-party licenses", "");
lines.push("DevKit includes open-source software. This file lists it with each license as shipped by its authors.");
lines.push("It is generated by `bun scripts/third-party-licenses.mjs` — do not edit it by hand.", "");
lines.push("It covers what is linked or bundled into the app: Go modules (the app and the TypeScript analyzer),");
lines.push("npm packages bundled into the interface, the Java analyzer's runtime libraries and the bundled fonts.");
lines.push("The Claude and Codex agent runtimes are downloaded on your machine when you install them from");
lines.push("Settings → Agents; they are not part of DevKit and keep their own licenses.", "");
lines.push("## Summary", "", "| License | Packages |", "| --- | ---: |");
for (const [k, n] of [...summary].sort((a, b) => b[1] - a[1])) lines.push(`| ${k} | ${n} |`);
lines.push("", `Total: ${entries.filter((e) => e.source !== "Font").length} packages.`, "");
lines.push("## Fonts", "");
lines.push("- **Geist** and **Geist Mono** (via `@fontsource-variable/geist`, `@fontsource-variable/geist-mono`) — SIL Open Font License 1.1.");
lines.push(`- **Inter** (\`Inter-Medium.ttf\`) — SIL Open Font License 1.1. ${interCopyright}.`);
lines.push("", "The Open Font License text, which applies to all three:", "", "```text", ofl || "See https://openfontlicense.org/ for the SIL Open Font License 1.1.", "```", "");
lines.push("## Packages", "");
for (const g of [...groups.values()].sort((a, b) => a.id.localeCompare(b.id) || b.who.length - a.who.length || a.who[0].localeCompare(b.who[0]))) {
  lines.push(`### ${g.id}`, "", ...g.who.map((w) => `- ${w}`), "", "```text", g.text.replace(/\r\n/g, "\n"), "```", "");
}
if (unresolved.length) {
  lines.push("## Without a license file in the package", "");
  lines.push("These packages ship no license text; the license they declare is shown. Their texts can be read at the package's own repository.", "");
  for (const u of unresolved) lines.push(`- ${u}`);
  lines.push("");
}

const next = lines.join("\n");
if (process.argv.includes("--check")) {
  const current = existsSync(outFile) ? readFileSync(outFile, "utf8") : "";
  if (current !== next) {
    console.error("THIRD_PARTY_LICENSES.md is out of date: run `bun scripts/third-party-licenses.mjs`.");
    process.exit(1);
  }
  console.log("THIRD_PARTY_LICENSES.md is up to date.");
} else {
  writeFileSync(outFile, next);
  console.log(`Wrote ${outFile}: ${entries.length} packages, ${groups.size} distinct texts, ${unresolved.length} without a text.`);
  for (const u of unresolved) console.log(`  no text: ${u}`);
}
