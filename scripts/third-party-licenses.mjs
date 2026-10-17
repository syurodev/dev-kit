#!/usr/bin/env bun
// Writes THIRD_PARTY_LICENSES.md: the open-source notices for what ships inside DevKit, one entry per
// package (name, version, SPDX license id, where its source is, the license text as its authors wrote it).
//
//   bun scripts/third-party-licenses.mjs           # rewrite the file
//   bun scripts/third-party-licenses.mjs --check   # exit 1 if the file is out of date
//
// What it reads (every entry carries the text found with the code, never one written here):
//   - Go modules linked into the app (darwin/linux/windows, GUI and `-tags server`) and into the bundled
//     TypeScript analyzer (`analyzer-ts`, tsgo) — from the module cache.
//   - npm packages reachable from frontend/package.json `dependencies` (what Vite bundles) — from
//     frontend/node_modules, so run `bun install` in frontend/ first.
//   - Maven runtime dependencies of the Java analyzer jar (`analyzer/`) — `mvn dependency:list`, jars in ~/.m2.
//   - The Inter font in frontend/public (Geist is an npm package and is listed there).
// A package that ships no license file gets its text from its own repository (raw.githubusercontent.com); what
// was fetched is kept in scripts/third-party-licenses.fetched.json so the output is reproducible offline.
//
// Not covered: the Claude / Codex agent runtimes (downloaded on the person's machine at run time, never
// bundled) and dev-only tooling that does not reach the app.
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outFile = join(root, "THIRD_PARTY_LICENSES.md");
const cacheFile = join(root, "scripts", "third-party-licenses.fetched.json");
const run = (cmd, args, cwd = root, env = {}) =>
  execFileSync(cmd, args, { cwd, encoding: "utf8", maxBuffer: 64 << 20, env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] });

const LICENSE_FILE = /^(licen[cs]e|copying|unlicense|notice)([-._].*)?$/i;
const NOTICE_FILE = /^notice([-._].*)?$/i;

/**
 * @typedef {{
 *   kind: "go" | "npm" | "maven" | "font", name: string, version: string, declared: string, texts: string[],
 *   url: string, repo?: string, directory?: string, spdx?: string, notes: string[],
 * }} Entry
 */
/** @type {Entry[]} */
const entries = [];

/** An SPDX id read from the text itself; "" when it cannot be told. */
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
  return "";
}

/** The license files at the root of a package directory; a NOTICE file is labelled as one. */
function readLicenseFiles(dir) {
  if (!dir || !existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && LICENSE_FILE.test(e.name.replace(/\.(txt|md|markdown)$/i, "")))
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((e) => {
      const text = readFileSync(join(dir, e.name), "utf8").trim();
      return text && NOTICE_FILE.test(e.name.replace(/\.(txt|md|markdown)$/i, "")) ? `NOTICE\n\n${text}` : text;
    })
    .filter(Boolean);
}

// ---------------------------------------------------------------- Go
/** A module's repository when it lives on GitHub (that is where its source and license are), else its pkg.go.dev page. */
function goSource(path, version) {
  const gh = /^github\.com\/([^/]+)\/([^/]+)/.exec(path);
  return gh ? `https://github.com/${gh[1]}/${gh[2]}` : `https://pkg.go.dev/${path}@${version}`;
}
function goModules(dir, matrix) {
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
    if (entries.some((e) => e.kind === "go" && e.name === path && e.version === version)) continue;
    entries.push({ kind: "go", name: path, version, declared: "", texts: readLicenseFiles(modDir), url: goSource(path, version), notes: [] });
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
    const declared = typeof pkg.license === "string" ? pkg.license : pkg.license?.type ?? (pkg.licenses ?? []).map((l) => l.type).join(" OR ");
    const repo = (typeof pkg.repository === "string" ? pkg.repository : pkg.repository?.url ?? "").replace(/^git\+/, "").replace(/^git:\/\//, "https://").replace(/\.git$/, "").replace(/^github:/, "https://github.com/");
    const url = repo.startsWith("http") ? repo : /^[\w.-]+\/[\w.-]+$/.test(repo) ? `https://github.com/${repo}` : pkg.homepage || `https://www.npmjs.com/package/${pkg.name}`;
    entries.push({ kind: "npm", name: pkg.name, version: pkg.version, declared, texts: readLicenseFiles(dir), url, repo: url, directory: pkg.repository?.directory, notes: [] });
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
    const pomFile = join(base, `${artifact}-${version}.pom`);
    const pom = existsSync(pomFile) ? readFileSync(pomFile, "utf8") : "";
    const offered = [...pom.matchAll(/<license>[\s\S]*?<name>([\s\S]*?)<\/name>/g)].map((x) => x[1].replace(/\s+/g, " ").trim());
    const jar = join(base, `${artifact}-${version}.jar`);
    const texts = [];
    if (existsSync(jar)) {
      const names = run("unzip", ["-Z1", jar]).split("\n").filter((n) => /^META-INF\/(licen[cs]e|notice|copying)[^/]*$/i.test(n));
      for (const n of names) {
        const text = execFileSync("unzip", ["-p", jar, n], { encoding: "utf8", maxBuffer: 16 << 20 }).trim();
        if (text) texts.push(NOTICE_FILE.test(n.split("/").pop()) ? `NOTICE\n\n${text}` : text);
      }
    }
    entries.push({
      kind: "maven", name: `${group}:${artifact}`, version, declared: offered.join(" OR "), texts,
      url: `https://central.sonatype.com/artifact/${group}/${artifact}/${version}`, notes: [],
    });
  }
}

// ---------------------------------------------------------------- Inter (the one font that is not an npm package)
function interFont() {
  const geist = join(root, "frontend", "node_modules", "@fontsource-variable", "geist", "LICENSE");
  if (!existsSync(geist) || !existsSync(join(root, "frontend", "public", "Inter-Medium.ttf"))) return;
  const ofl = readFileSync(geist, "utf8");
  const body = ofl.slice(ofl.indexOf("This Font Software is licensed under"));
  // Read from the font's own name table (ids 0, 5 and 13), not remembered.
  const copyright = "Copyright 2020 The Inter Project Authors (https://github.com/rsms/inter)";
  entries.push({
    kind: "font", name: "Inter (Inter-Medium.ttf)", version: "3.019", declared: "OFL-1.1", texts: [`${copyright}\n\n${body.trim()}`],
    url: "https://github.com/rsms/inter", notes: [],
  });
}

goModules(root, [["darwin", ""], ["darwin", "server"], ["linux", ""], ["linux", "server"], ["windows", ""]]);
goModules(join(root, "analyzer-ts"), [["darwin", ""], ["linux", ""], ["windows", ""]]);
npmPackages();
mavenPackages();
interFont();

// ---------------------------------------------------------------- texts a package does not ship
const cache = existsSync(cacheFile) ? JSON.parse(readFileSync(cacheFile, "utf8")) : {};
let cacheChanged = false;

async function fetchText(url) {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(15000) });
    return res.ok ? (await res.text()).trim() : "";
  } catch {
    return "";
  }
}

async function fromRepository(e) {
  const key = `${e.name}@${e.version}`;
  if (cache[key]) return cache[key];
  const gh = /github\.com[/:]([^/]+)\/([^/#]+)/.exec(e.repo ?? "");
  if (!gh) return null;
  const [, owner, repo] = gh;
  const names = ["LICENSE", "LICENSE.md", "LICENSE.txt", "License.md", "License", "license", "license.md", "LICENSE-MIT", "COPYING"];
  const dirs = [e.directory, ""].filter((d, i, all) => d !== undefined && all.indexOf(d) === i);
  for (const d of dirs) {
    for (const name of names) {
      const url = `https://raw.githubusercontent.com/${owner}/${repo}/HEAD/${d ? `${d}/` : ""}${name}`;
      const text = await fetchText(url);
      if (text && text.length < 30000 && classify(text)) {
        cache[key] = { url: `https://github.com/${owner}/${repo}/blob/HEAD/${d ? `${d}/` : ""}${name}`, text };
        cacheChanged = true;
        return cache[key];
      }
    }
  }
  return null;
}

for (const e of entries) {
  if (e.texts.length) continue;
  const got = e.kind === "npm" ? await fromRepository(e) : null;
  if (got) {
    e.texts = [got.text];
    e.notes.push(`The package ships no license file; this text is from its repository (${got.url}).`);
  }
}
if (cacheChanged) writeFileSync(cacheFile, JSON.stringify(Object.fromEntries(Object.entries(cache).sort(([a], [b]) => a.localeCompare(b))), null, 2) + "\n");

// The canonical Apache-2.0 text, for Apache jars that carry none (the pom names the license).
const apache = entries.flatMap((e) => e.texts).find((t) => classify(t) === "Apache-2.0" && t.length > 9000 && !t.startsWith("NOTICE"));

// ---------------------------------------------------------------- the license each entry is used under
function chooseFrom(options) {
  return options.find((o) => /apache/i.test(o)) ?? options.find((o) => /^mit$/i.test(o)) ?? options[0];
}
/** @type {string[]} */
const withoutText = [];
for (const e of entries) {
  const classified = classify(e.texts.filter((t) => !t.startsWith("NOTICE")).join("\n"));
  if (e.kind === "npm") {
    const options = e.declared.replace(/[()]/g, "").split(/\s+OR\s+/i).map((s) => s.trim()).filter(Boolean);
    if (options.length > 1) {
      e.spdx = chooseFrom(options);
      e.notes.push(`Offered under ${options.join(" or ")}; DevKit uses it under ${e.spdx}.`);
    } else {
      e.spdx = e.declared || classified || "NOASSERTION";
    }
  } else if (e.kind === "maven") {
    const options = e.declared ? e.declared.split(" OR ") : [];
    if (options.length > 1) {
      const picked = chooseFrom(options);
      e.spdx = /apache/i.test(picked) ? "Apache-2.0" : picked;
      e.notes.push(`Offered under ${options.join(" or ")}; DevKit uses it under ${picked}.`);
    } else {
      e.spdx = classified || (options[0] && /apache/i.test(options[0]) ? "Apache-2.0" : options[0]) || "NOASSERTION";
    }
    if (!e.texts.length && e.spdx === "Apache-2.0" && apache) {
      e.texts = [apache];
      e.notes.push("The jar ships no license file; this is the standard Apache License 2.0 text its pom names.");
    }
  } else {
    e.spdx = classified || e.declared || "NOASSERTION";
  }
  if (/MPL/.test(e.spdx)) {
    e.notes.push(`Licensed under file-level terms (MPL): the source of this package is at ${e.url}; DevKit does not modify it.`);
  }
  if (!e.texts.length) withoutText.push(`${e.name}@${e.version} (${e.spdx})`);
}

// ---------------------------------------------------------------- write
const KINDS = [
  ["go", "Go modules"],
  ["npm", "npm packages (the interface)"],
  ["maven", "Java libraries (the code analyzer)"],
  ["font", "Fonts"],
];
const lines = [];
lines.push("# Third-party licenses", "");
lines.push("The following sets forth attribution notices for third-party software included in DevKit, one entry per");
lines.push("package with the license text its authors published. It is generated by");
lines.push("`bun scripts/third-party-licenses.mjs` — do not edit it by hand.", "");
lines.push("It covers what is linked or bundled into the app: Go modules (the app and the TypeScript analyzer), npm");
lines.push("packages bundled into the interface, the Java analyzer's runtime libraries and the Inter font. The Claude");
lines.push("and Codex agent runtimes are downloaded on your machine when you install them from Settings → Agents;");
lines.push("they are not part of DevKit and keep their own licenses.", "");
lines.push(`${entries.length} packages.`, "");
for (const [kind, title] of KINDS) {
  const group = entries.filter((e) => e.kind === kind).sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.version.localeCompare(b.version));
  if (!group.length) continue;
  lines.push(`## ${title}`, "");
  for (const e of group) {
    lines.push(`### ${e.name} (${e.version})`, "", `- License: ${e.spdx}`, `- Source: ${e.url}`);
    for (const n of e.notes) lines.push(`- Note: ${n}`);
    lines.push("");
    if (e.texts.length) lines.push("```text", e.texts.map((t) => t.replace(/\r\n/g, "\n")).join("\n\n"), "```", "");
    else lines.push(`The package publishes no license text anywhere in its package or repository; it declares ${e.spdx} in its metadata.`, "");
  }
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
  console.log(`Wrote ${outFile}: ${entries.length} packages, ${withoutText.length} without a text.`);
  for (const w of withoutText) console.log(`  no text: ${w}`);
}
