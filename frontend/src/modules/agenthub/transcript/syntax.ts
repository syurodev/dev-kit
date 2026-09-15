import { highlightCode } from "@lezer/highlight";
import { devkitSyntax } from "@/components/code-editor/devkitSyntax";

/**
 * Syntax colours for code in the transcript: an agent's code blocks and the
 * files its tools read.
 *
 * Lezer, the parser behind the app's CodeMirror editors, run once over the
 * text with no editor attached, through the same devkitSyntax table — so a
 * Java file in a Read result is the colour it is in the SQL console's
 * neighbour, and follows the theme the same way. A real parse, not the
 * regex heuristics of highlight.js.
 *
 * Every grammar is a dynamic import: the transcript's chunk carries none of
 * them, and a language is fetched the first time a block in it is shown.
 */

/** What highlightCode needs from a parser: a parse to a tree. */
type CodeParser = { parse(code: string): Parameters<typeof highlightCode>[1] };

/** A run of text and the classes it takes; "" for plain. */
export type SyntaxSegment = { text: string; className: string };

/** Grammars that are not Lezer ports yet, through CodeMirror's stream adapter. */
function legacy(load: () => Promise<Parameters<typeof import("@codemirror/language").StreamLanguage.define>[0]>) {
  return () =>
    Promise.all([import("@codemirror/language"), load()]).then(
      ([{ StreamLanguage }, mode]) => StreamLanguage.define(mode).parser as CodeParser,
    );
}

const LOADERS: Record<string, () => Promise<CodeParser>> = {
  java: () => import("@codemirror/lang-java").then((m) => m.javaLanguage.parser),
  json: () => import("@codemirror/lang-json").then((m) => m.jsonLanguage.parser),
  javascript: () => import("@codemirror/lang-javascript").then((m) => m.javascriptLanguage.parser),
  jsx: () => import("@codemirror/lang-javascript").then((m) => m.jsxLanguage.parser),
  typescript: () => import("@codemirror/lang-javascript").then((m) => m.typescriptLanguage.parser),
  tsx: () => import("@codemirror/lang-javascript").then((m) => m.tsxLanguage.parser),
  css: () => import("@codemirror/lang-css").then((m) => m.cssLanguage.parser),
  html: () => import("@codemirror/lang-html").then((m) => m.htmlLanguage.parser),
  markdown: () => import("@codemirror/lang-markdown").then((m) => m.markdownLanguage.parser),
  sql: () => import("@codemirror/lang-sql").then((m) => m.StandardSQL.language.parser),
  yaml: () => import("@codemirror/lang-yaml").then((m) => m.yamlLanguage.parser),
  go: () => import("@codemirror/lang-go").then((m) => m.goLanguage.parser),
  python: () => import("@codemirror/lang-python").then((m) => m.pythonLanguage.parser),
  rust: () => import("@codemirror/lang-rust").then((m) => m.rustLanguage.parser),
  shell: legacy(() => import("@codemirror/legacy-modes/mode/shell").then((m) => m.shell)),
  kotlin: legacy(() => import("@codemirror/legacy-modes/mode/clike").then((m) => m.kotlin)),
  groovy: legacy(() => import("@codemirror/legacy-modes/mode/groovy").then((m) => m.groovy)),
  toml: legacy(() => import("@codemirror/legacy-modes/mode/toml").then((m) => m.toml)),
  dockerfile: legacy(() => import("@codemirror/legacy-modes/mode/dockerfile").then((m) => m.dockerFile)),
  properties: legacy(() => import("@codemirror/legacy-modes/mode/properties").then((m) => m.properties)),
  diff: legacy(() => import("@codemirror/legacy-modes/mode/diff").then((m) => m.diff)),
  xml: legacy(() => import("@codemirror/legacy-modes/mode/xml").then((m) => m.xml)),
};

/** Every grammar name the transcript can colour. */
export const SYNTAX_LANGUAGES = Object.keys(LOADERS);

/** Fence info strings and file extensions, onto a grammar above. */
const ALIASES: Record<string, string> = {
  java: "java",
  json: "json",
  jsonc: "json",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  javascript: "javascript",
  jsx: "jsx",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  typescript: "typescript",
  tsx: "tsx",
  css: "css",
  scss: "css",
  less: "css",
  html: "html",
  htm: "html",
  md: "markdown",
  mdx: "markdown",
  markdown: "markdown",
  sql: "sql",
  yaml: "yaml",
  yml: "yaml",
  go: "go",
  golang: "go",
  py: "python",
  python: "python",
  rs: "rust",
  rust: "rust",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  shell: "shell",
  kt: "kotlin",
  kts: "kotlin",
  kotlin: "kotlin",
  groovy: "groovy",
  gradle: "groovy",
  toml: "toml",
  dockerfile: "dockerfile",
  properties: "properties",
  diff: "diff",
  patch: "diff",
  xml: "xml",
  svg: "xml",
  plist: "xml",
};

/** A fence's info string as a grammar name, or undefined if there is none. */
export function normalizeLanguage(name?: string): string | undefined {
  if (!name) return undefined;
  return ALIASES[name.trim().toLowerCase()];
}

/** The grammar for a file, by extension (or a Dockerfile by name). */
export function languageForPath(path?: string): string | undefined {
  if (!path) return undefined;
  const base = path.split(/[\\/]/).pop()?.toLowerCase() ?? "";
  if (base === "dockerfile" || base.startsWith("dockerfile.")) return "dockerfile";
  const dot = base.lastIndexOf(".");
  return dot > 0 ? ALIASES[base.slice(dot + 1)] : undefined;
}

const parsers = new Map<string, Promise<CodeParser>>();

/** The parser for a grammar name, loaded once. Rejects for an unknown name. */
export function loadParser(language: string): Promise<CodeParser> {
  const load = LOADERS[language];
  if (!load) return Promise.reject(new Error(`No grammar for ${language}`));
  let parser = parsers.get(language);
  if (!parser) {
    parser = load();
    parsers.set(language, parser);
    // A failed fetch should not poison the cache for the rest of the session.
    parser.catch(() => parsers.delete(language));
  }
  return parser;
}

/** Runs of text with their syntax classes. Joined, they are the code exactly. */
export function tokenize(code: string, parser: CodeParser): SyntaxSegment[] {
  const segments: SyntaxSegment[] = [];
  highlightCode(
    code,
    parser.parse(code),
    devkitSyntax,
    (text, className) => segments.push({ text, className }),
    () => segments.push({ text: "\n", className: "" }),
  );
  return segments;
}

const FENCE_OPEN = /^```([\w+#.-]*)\s*$/;
const FENCE_CLOSE = /^```\s*$/;
const NUMBERED = /^\s*(\d+)(?:\t|→)(.*)$/;

/**
 * A tool's text as the listing it is. Claude's Read answers with the file
 * inside a markdown fence, each line prefixed with its number and a tab:
 * "```\n1\tpackage …\n2\t\n…\n```". Shown as is, that put the fence marks
 * and the numbers into the code. Returns the code alone, the first line's
 * number when every line is numbered in sequence, and the fence's info string;
 * null when the text is neither fenced nor numbered.
 */
export function parseListing(text: string): { code: string; startLine?: number; info?: string } | null {
  let lines = text.replace(/\r\n/g, "\n").split("\n");
  let info: string | undefined;
  const open = FENCE_OPEN.exec(lines[0] ?? "");
  if (open) {
    info = open[1] || undefined;
    lines = lines.slice(1);
    while (lines.length > 0 && lines[lines.length - 1].trim() === "") lines.pop();
    if (lines.length > 0 && FENCE_CLOSE.test(lines[lines.length - 1])) lines.pop();
  }

  const matches = lines.map((line) => NUMBERED.exec(line));
  const numbered =
    lines.length > 0 &&
    matches.every((match, index) => match && Number(match[1]) === Number(matches[0]![1]) + index);
  if (numbered) {
    return { code: matches.map((match) => match![2]).join("\n"), startLine: Number(matches[0]![1]), info };
  }
  return open ? { code: lines.join("\n"), info } : null;
}
