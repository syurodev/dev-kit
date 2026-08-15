import { readFile, stat } from "node:fs/promises";
import { gzipSync } from "node:zlib";
import path from "node:path";

const DIST_DIR = path.resolve(import.meta.dirname, "../dist");
const INITIAL_JS_BUDGET = 700 * 1024;
const INITIAL_JS_GZIP_BUDGET = 225 * 1024;
const INITIAL_CSS_GZIP_BUDGET = 45 * 1024;

function assetPaths(html, relation) {
  const pattern =
    relation === "script"
      ? /<script[^>]+src="([^"]+\.js)"/g
      : relation === "modulepreload"
        ? /<link[^>]+rel="modulepreload"[^>]+href="([^"]+\.js)"/g
        : /<link[^>]+rel="stylesheet"[^>]+href="([^"]+\.css)"/g;

  return [...html.matchAll(pattern)].map((match) => match[1]);
}

function localAssetPath(asset) {
  return path.join(DIST_DIR, asset.replace(/^\//, ""));
}

async function totals(assets) {
  let raw = 0;
  let gzip = 0;
  for (const asset of new Set(assets)) {
    const file = localAssetPath(asset);
    const [metadata, contents] = await Promise.all([stat(file), readFile(file)]);
    raw += metadata.size;
    gzip += gzipSync(contents).byteLength;
  }
  return { raw, gzip };
}

function kib(bytes) {
  return `${(bytes / 1024).toFixed(1)} KiB`;
}

const html = await readFile(path.join(DIST_DIR, "index.html"), "utf8");
const initialJS = await totals([
  ...assetPaths(html, "script"),
  ...assetPaths(html, "modulepreload"),
]);
const initialCSS = await totals(assetPaths(html, "stylesheet"));

const failures = [];
if (initialJS.raw > INITIAL_JS_BUDGET) {
  failures.push(`initial JS ${kib(initialJS.raw)} > ${kib(INITIAL_JS_BUDGET)}`);
}
if (initialJS.gzip > INITIAL_JS_GZIP_BUDGET) {
  failures.push(
    `initial JS gzip ${kib(initialJS.gzip)} > ${kib(INITIAL_JS_GZIP_BUDGET)}`,
  );
}
if (initialCSS.gzip > INITIAL_CSS_GZIP_BUDGET) {
  failures.push(
    `initial CSS gzip ${kib(initialCSS.gzip)} > ${kib(INITIAL_CSS_GZIP_BUDGET)}`,
  );
}

console.log(
  `Performance budget: initial JS ${kib(initialJS.raw)} (${kib(initialJS.gzip)} gzip), CSS ${kib(initialCSS.gzip)} gzip.`,
);

if (failures.length > 0) {
  console.error(`Performance budget exceeded:\n- ${failures.join("\n- ")}`);
  process.exitCode = 1;
}
