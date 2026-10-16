import type { LanguageSupport, StreamParser } from "@codemirror/language";

/**
 * The one table of languages DevKit can colour, for every surface that shows
 * code through Lezer: CodeMirror editors (the SQL console, CodeGraph's file
 * editor, Kafka's publish box) and the read-only blocks (the Agent Hub
 * transcript, diffs, API and Kafka bodies, query history). A language added
 * here is a language all of them colour.
 *
 * Notes is the exception: Plate's code block runs on lowlight (highlight.js),
 * whose grammars live in components/editor/plugins/code-block-languages.ts.
 *
 * Every grammar is a dynamic import. No surface's chunk carries one; a
 * language is fetched the first time something in it is shown, then cached.
 */

type Loader = () => Promise<LanguageSupport>;

export type LanguageDef = {
  /** What a caller names the language by. */
  id: string;
  /** Human name, for a picker or a block's header. */
  label: string;
  /** Fence info strings and other names that mean this language. */
  aliases?: string[];
  /** File extensions, lower case, without the dot. */
  extensions?: string[];
  /** Whole file names (lower case) that carry no telling extension. */
  filenames?: string[];
  load: Loader;
};

/** Grammars that are not Lezer ports yet, through CodeMirror's stream adapter. */
function legacy(mode: () => Promise<StreamParser<unknown>>): Loader {
  return () =>
    Promise.all([import("@codemirror/language"), mode()]).then(
      ([{ LanguageSupport, StreamLanguage }, parser]) => new LanguageSupport(StreamLanguage.define(parser)),
    );
}

const clike = () => import("@codemirror/legacy-modes/mode/clike");

export const LANGUAGES: LanguageDef[] = [
  // Lezer grammars: a real parse, with the editor extras (indentation, folding,
  // completion) that come with each package's own support function.
  {
    id: "javascript",
    label: "JavaScript",
    aliases: ["js", "node"],
    extensions: ["js", "mjs", "cjs"],
    load: () => import("@codemirror/lang-javascript").then((m) => m.javascript()),
  },
  {
    id: "jsx",
    label: "JSX",
    extensions: ["jsx"],
    load: () => import("@codemirror/lang-javascript").then((m) => m.javascript({ jsx: true })),
  },
  {
    id: "typescript",
    label: "TypeScript",
    aliases: ["ts"],
    extensions: ["ts", "mts", "cts"],
    load: () => import("@codemirror/lang-javascript").then((m) => m.javascript({ typescript: true })),
  },
  {
    id: "tsx",
    label: "TSX",
    extensions: ["tsx"],
    load: () => import("@codemirror/lang-javascript").then((m) => m.javascript({ typescript: true, jsx: true })),
  },
  {
    id: "json",
    label: "JSON",
    aliases: ["jsonc", "json5", "jsonl", "ndjson"],
    extensions: ["json", "jsonc", "json5", "jsonl", "ndjson", "har", "geojson", "webmanifest"],
    filenames: [".babelrc", ".eslintrc", ".prettierrc"],
    load: () => import("@codemirror/lang-json").then((m) => m.json()),
  },
  {
    id: "yaml",
    label: "YAML",
    aliases: ["yml"],
    extensions: ["yaml", "yml"],
    load: () => import("./grammars/yaml").then((m) => m.yamlSupport()),
  },
  {
    id: "sql",
    label: "SQL",
    aliases: ["postgresql", "postgres", "pgsql", "psql", "mysql", "mariadb", "sqlite", "plsql", "tsql"],
    extensions: ["sql", "ddl", "pgsql", "psql"],
    load: () => import("@codemirror/lang-sql").then((m) => m.sql()),
  },
  {
    id: "html",
    label: "HTML",
    aliases: ["vue", "svelte"],
    extensions: ["html", "htm", "xhtml", "vue", "svelte"],
    load: () => import("@codemirror/lang-html").then((m) => m.html()),
  },
  {
    id: "css",
    label: "CSS",
    aliases: ["scss", "less"],
    extensions: ["css", "scss", "less"],
    load: () => import("@codemirror/lang-css").then((m) => m.css()),
  },
  {
    id: "markdown",
    label: "Markdown",
    aliases: ["md", "mdx", "gfm"],
    extensions: ["md", "mdx", "markdown"],
    // GFM, not the package's CommonMark default: tables and task lists are
    // what READMEs and agent answers are written in.
    load: () =>
      import("@codemirror/lang-markdown").then((m) => m.markdown({ base: m.markdownLanguage })),
  },
  {
    id: "java",
    label: "Java",
    extensions: ["java"],
    load: () => import("@codemirror/lang-java").then((m) => m.java()),
  },
  {
    id: "go",
    label: "Go",
    aliases: ["golang"],
    extensions: ["go"],
    load: () => import("@codemirror/lang-go").then((m) => m.go()),
  },
  {
    id: "python",
    label: "Python",
    aliases: ["py", "python3"],
    extensions: ["py", "pyi", "pyw"],
    load: () => import("@codemirror/lang-python").then((m) => m.python()),
  },
  {
    id: "rust",
    label: "Rust",
    aliases: ["rs"],
    extensions: ["rs"],
    load: () => import("@codemirror/lang-rust").then((m) => m.rust()),
  },

  // Stream modes: colour, no parse tree beyond the tokens.
  {
    id: "makefile",
    label: "Makefile",
    aliases: ["make", "gnumake"],
    extensions: ["mk", "mak", "make"],
    filenames: ["makefile", "gnumakefile", "bsdmakefile"],
    load: legacy(() => import("./grammars/makefile").then((m) => m.makefile as StreamParser<unknown>)),
  },
  {
    id: "shell",
    label: "Shell",
    aliases: ["sh", "bash", "zsh", "shellscript"],
    extensions: ["sh", "bash", "zsh"],
    filenames: [".bashrc", ".zshrc", ".profile", ".bash_profile", ".zprofile", ".envrc", "gradlew"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/shell").then((m) => m.shell)),
  },
  {
    id: "powershell",
    label: "PowerShell",
    aliases: ["ps", "ps1", "pwsh"],
    extensions: ["ps1", "psm1", "psd1"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/powershell").then((m) => m.powerShell)),
  },
  {
    id: "xml",
    label: "XML",
    aliases: ["svg", "plist", "xsd", "xsl"],
    extensions: ["xml", "svg", "plist", "xsd", "xsl", "xslt", "wsdl", "pom", "csproj", "fxml"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/xml").then((m) => m.xml)),
  },
  {
    id: "toml",
    label: "TOML",
    extensions: ["toml"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/toml").then((m) => m.toml)),
  },
  {
    id: "properties",
    label: "Properties",
    aliases: ["ini", "cfg", "conf", "env", "dotenv", "editorconfig"],
    extensions: ["properties", "ini", "cfg", "conf", "env"],
    filenames: [".env", ".editorconfig", ".gitconfig", ".npmrc"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/properties").then((m) => m.properties)),
  },
  {
    id: "dockerfile",
    label: "Dockerfile",
    aliases: ["docker"],
    extensions: ["dockerfile"],
    filenames: ["dockerfile", "containerfile"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/dockerfile").then((m) => m.dockerFile)),
  },
  {
    id: "nginx",
    label: "Nginx",
    aliases: ["nginxconf"],
    filenames: ["nginx.conf"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/nginx").then((m) => m.nginx)),
  },
  {
    id: "diff",
    label: "Diff",
    aliases: ["patch", "udiff"],
    extensions: ["diff", "patch"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/diff").then((m) => m.diff)),
  },
  {
    id: "kotlin",
    label: "Kotlin",
    aliases: ["kt", "kts"],
    extensions: ["kt", "kts"],
    load: legacy(() => clike().then((m) => m.kotlin)),
  },
  {
    id: "groovy",
    label: "Groovy",
    aliases: ["gradle"],
    extensions: ["groovy", "gradle", "gvy"],
    filenames: ["jenkinsfile"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/groovy").then((m) => m.groovy)),
  },
  {
    id: "scala",
    label: "Scala",
    extensions: ["scala", "sc", "sbt"],
    load: legacy(() => clike().then((m) => m.scala)),
  },
  {
    id: "c",
    label: "C",
    aliases: ["h"],
    extensions: ["c", "h"],
    load: legacy(() => clike().then((m) => m.c)),
  },
  {
    id: "cpp",
    label: "C++",
    aliases: ["c++", "cc", "hpp", "cxx"],
    extensions: ["cpp", "cc", "cxx", "hpp", "hh", "hxx", "ino"],
    load: legacy(() => clike().then((m) => m.cpp)),
  },
  {
    id: "csharp",
    label: "C#",
    aliases: ["cs", "c#"],
    extensions: ["cs", "csx"],
    load: legacy(() => clike().then((m) => m.csharp)),
  },
  {
    id: "objectivec",
    label: "Objective-C",
    aliases: ["objc", "objective-c"],
    extensions: ["m", "mm"],
    load: legacy(() => clike().then((m) => m.objectiveC)),
  },
  {
    id: "dart",
    label: "Dart",
    extensions: ["dart"],
    load: legacy(() => clike().then((m) => m.dart)),
  },
  {
    id: "swift",
    label: "Swift",
    extensions: ["swift"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/swift").then((m) => m.swift)),
  },
  {
    id: "ruby",
    label: "Ruby",
    aliases: ["rb"],
    extensions: ["rb", "rake", "gemspec"],
    filenames: ["gemfile", "rakefile", "podfile", "fastfile"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/ruby").then((m) => m.ruby)),
  },
  {
    id: "perl",
    label: "Perl",
    aliases: ["pl"],
    extensions: ["pl", "pm"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/perl").then((m) => m.perl)),
  },
  {
    id: "r",
    label: "R",
    extensions: ["r"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/r").then((m) => m.r)),
  },
  {
    id: "lua",
    label: "Lua",
    extensions: ["lua", "rockspec"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/lua").then((m) => m.lua)),
  },
  {
    id: "haskell",
    label: "Haskell",
    aliases: ["hs"],
    extensions: ["hs"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/haskell").then((m) => m.haskell)),
  },
  {
    id: "clojure",
    label: "Clojure",
    aliases: ["clj", "edn"],
    extensions: ["clj", "cljs", "cljc", "edn"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/clojure").then((m) => m.clojure)),
  },
  {
    id: "erlang",
    label: "Erlang",
    aliases: ["erl"],
    extensions: ["erl", "hrl"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/erlang").then((m) => m.erlang)),
  },
  {
    id: "protobuf",
    label: "Protocol Buffers",
    aliases: ["proto"],
    extensions: ["proto"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/protobuf").then((m) => m.protobuf)),
  },
  {
    id: "cmake",
    label: "CMake",
    extensions: ["cmake"],
    filenames: ["cmakelists.txt"],
    load: legacy(() => import("@codemirror/legacy-modes/mode/cmake").then((m) => m.cmake)),
  },
  {
    id: "http",
    label: "HTTP",
    load: legacy(() => import("@codemirror/legacy-modes/mode/http").then((m) => m.http)),
  },
];

const byId = new Map(LANGUAGES.map((language) => [language.id, language]));
const byName = new Map<string, string>();
const byExtension = new Map<string, string>();
const byFilename = new Map<string, string>();
for (const language of LANGUAGES) {
  byName.set(language.id, language.id);
  for (const alias of language.aliases ?? []) byName.set(alias, language.id);
  for (const extension of language.extensions ?? []) byExtension.set(extension, language.id);
  for (const filename of language.filenames ?? []) byFilename.set(filename, language.id);
}

/** Every language id the registry can colour. */
export const SYNTAX_LANGUAGES = LANGUAGES.map((language) => language.id);

/** A language's definition by id. */
export function languageDef(id: string): LanguageDef | undefined {
  return byId.get(id);
}

/**
 * A name as a language id: an id, an alias, a fence's info string, a file
 * extension. Undefined when it names nothing here — "console" output is not
 * a script, and colouring it as shell would be wrong.
 */
export function normalizeLanguage(name?: string | null): string | undefined {
  if (!name) return undefined;
  const key = name.trim().toLowerCase();
  return byName.get(key) ?? byExtension.get(key);
}

/** The language of a file, by name (Dockerfile, Gemfile) or extension. */
export function languageForPath(path?: string | null): string | undefined {
  if (!path) return undefined;
  const base = path.split(/[\\/]/).pop()?.toLowerCase() ?? "";
  const named = byFilename.get(base);
  if (named) return named;
  if (base.startsWith(".env.")) return "properties";
  if (base.startsWith("containerfile.") || base.endsWith(".containerfile")) return "dockerfile";
  if (base.startsWith("dockerfile.") || base.endsWith(".dockerfile")) return "dockerfile";
  const dot = base.lastIndexOf(".");
  const extension = dot > 0 ? byExtension.get(base.slice(dot + 1)) : undefined;
  return extension ?? (/^(?:gnu|bsd)?makefile[.-]/.test(base) ? "makefile" : undefined);
}

/**
 * The language of an HTTP body by its Content-Type — `application/json`,
 * `application/problem+json`, `text/xml`, `application/x-yaml`. Undefined
 * for anything that is not code (text/plain, images, forms).
 */
export function languageForContentType(contentType?: string | null): string | undefined {
  if (!contentType) return undefined;
  const type = contentType.split(";")[0].trim().toLowerCase();
  if (type.endsWith("/json") || type.endsWith("+json") || type === "application/x-ndjson") return "json";
  if (type.endsWith("/xml") || type.endsWith("+xml")) return "xml";
  if (type === "text/html") return "html";
  if (type.endsWith("/yaml") || type.endsWith("/x-yaml") || type.endsWith("+yaml")) return "yaml";
  if (type === "text/css") return "css";
  if (type.endsWith("/javascript") || type.endsWith("/ecmascript")) return "javascript";
  if (type === "application/sql") return "sql";
  if (type === "text/markdown") return "markdown";
  return undefined;
}

/**
 * A guess from the text alone, for payloads that carry no type: a Kafka
 * record, a request body. Only the unambiguous shapes — a document that
 * parses as JSON, or one that opens like markup. Anything else is plain,
 * because colouring prose as code is worse than not colouring code.
 */
export function detectLanguage(text: string): string | undefined {
  const head = text.trimStart();
  if (!head) return undefined;
  if (head[0] === "{" || head[0] === "[") {
    try {
      JSON.parse(text);
      return "json";
    } catch {
      return undefined;
    }
  }
  if (head.startsWith("<?xml")) return "xml";
  if (/^<!doctype html|^<html[\s>]/i.test(head)) return "html";
  if (/^<[A-Za-z][\w:.-]*[\s>/]/.test(head) && head.trimEnd().endsWith(">")) return "xml";
  return undefined;
}

const supports = new Map<string, Promise<LanguageSupport>>();
const loaded = new Map<string, LanguageSupport>();

/** A language's CodeMirror support, loaded once. Rejects for an unknown id. */
export function loadLanguage(id: string): Promise<LanguageSupport> {
  const def = byId.get(id);
  if (!def) return Promise.reject(new Error(`No grammar for ${id}`));
  let support = supports.get(id);
  if (!support) {
    support = def.load();
    supports.set(id, support);
    support.then(
      (value) => loaded.set(id, value),
      // A failed fetch should not poison the cache for the rest of the session.
      () => supports.delete(id),
    );
  }
  return support;
}

/**
 * A language's support if it has already loaded, synchronously — so a second
 * block or editor in the same language paints coloured on its first frame
 * instead of flashing plain.
 */
export function loadedLanguage(id: string): LanguageSupport | undefined {
  return loaded.get(id);
}
