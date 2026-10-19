import { readFile, stat } from "node:fs/promises";
import path from "node:path";

const DIST_DIR = path.resolve(import.meta.dirname, "../dist");
// What the shell costs to boot: React + ReactDOM, the Radix/Ariakit
// primitives, the Wails runtime and the workbench itself. 700 KiB was set
// before the shell grew a split panel, a dock and a tab strip. 1 MiB held
// until the slot layout, document groups and layout presets brought the shell
// to ~911 KiB; 2 MiB now leaves room to build on rather than to shave. A
// regression still shows as the number moving — say so when it jumps.
const INITIAL_JS_BUDGET = 2 * 1024 * 1024;
// Both budgets are raw, and there are no gzip budgets. Assets are embedded in
// the Go binary and served from AssetFileServerFS over loopback (ADR-006 keeps
// even server mode loopback-only), so compressed size stands in for a download
// that never happens. What costs startup here is parse and compile, which
// tracks raw bytes — a second budget over the same bytes only ever needed
// raising twice. CSS sat at 280.0 KiB against a 280 KiB ceiling when the Notes
// table/column layout added its classes; 320 KiB is room to keep styling in
// Tailwind rather than shave or move it into inline styles.
const INITIAL_CSS_BUDGET = 320 * 1024;

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
  for (const asset of new Set(assets)) {
    raw += (await stat(localAssetPath(asset))).size;
  }
  return { raw };
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
if (initialCSS.raw > INITIAL_CSS_BUDGET) {
  failures.push(`initial CSS ${kib(initialCSS.raw)} > ${kib(INITIAL_CSS_BUDGET)}`);
}

console.log(
  `Performance budget: initial JS ${kib(initialJS.raw)}, CSS ${kib(initialCSS.raw)}.`,
);

if (failures.length > 0) {
  console.error(`Performance budget exceeded:\n- ${failures.join("\n- ")}`);
  process.exitCode = 1;
}
