import { common, createLowlight } from 'lowlight';
import clojure from 'highlight.js/lib/languages/clojure';
import cmake from 'highlight.js/lib/languages/cmake';
import dart from 'highlight.js/lib/languages/dart';
import dockerfile from 'highlight.js/lib/languages/dockerfile';
import elixir from 'highlight.js/lib/languages/elixir';
import elm from 'highlight.js/lib/languages/elm';
import erlang from 'highlight.js/lib/languages/erlang';
import fsharp from 'highlight.js/lib/languages/fsharp';
import gherkin from 'highlight.js/lib/languages/gherkin';
import groovy from 'highlight.js/lib/languages/groovy';
import haskell from 'highlight.js/lib/languages/haskell';
import http from 'highlight.js/lib/languages/http';
import julia from 'highlight.js/lib/languages/julia';
import nginx from 'highlight.js/lib/languages/nginx';
import ocaml from 'highlight.js/lib/languages/ocaml';
import powershell from 'highlight.js/lib/languages/powershell';
import properties from 'highlight.js/lib/languages/properties';
import protobuf from 'highlight.js/lib/languages/protobuf';
import scala from 'highlight.js/lib/languages/scala';

/**
 * Notes' code block languages, in one place for the editor and the static
 * renderer (they used to carry identical copies of this list).
 *
 * Notes runs on lowlight (highlight.js) because Plate's code-block plugin is
 * built on it; every other surface colours through Lezer and
 * components/code/languages.ts. The two are kept to the same everyday set.
 *
 * `all` would register every one of highlight.js' 192 grammars (~1.3 MB)
 * into the notes chunk. `common` covers the 37 languages people paste most,
 * and the imports above add the rest a dev workbench meets: build and
 * config files, JVM and BEAM languages, the functional ones.
 */
export const lowlight = createLowlight(common);
lowlight.register({
  clojure,
  cmake,
  dart,
  dockerfile,
  elixir,
  elm,
  erlang,
  fsharp,
  gherkin,
  groovy,
  haskell,
  http,
  julia,
  nginx,
  ocaml,
  powershell,
  properties,
  protobuf,
  scala,
});

export type CodeBlockLanguage = { label: string; value: string };

/**
 * The picker. Only languages that colour: an entry whose grammar is not
 * registered used to be offered and then rendered plain, which reads as a
 * bug. A block saved with one of those names still shows its name
 * (getCodeBlockLanguageLabel falls back to it). `mermaid` has no grammar on
 * purpose — Notes renders it as a diagram (notes/plate/MermaidCodeBlock).
 */
export const CODE_BLOCK_LANGUAGES: CodeBlockLanguage[] = [
  { label: 'Auto', value: 'auto' },
  { label: 'Plain Text', value: 'plaintext' },
  { label: 'Arduino', value: 'arduino' },
  { label: 'Bash', value: 'bash' },
  { label: 'C', value: 'c' },
  { label: 'C#', value: 'csharp' },
  { label: 'C++', value: 'cpp' },
  { label: 'Clojure', value: 'clojure' },
  { label: 'CMake', value: 'cmake' },
  { label: 'CSS', value: 'css' },
  { label: 'Dart', value: 'dart' },
  { label: 'Diff', value: 'diff' },
  { label: 'Docker', value: 'dockerfile' },
  { label: 'Elixir', value: 'elixir' },
  { label: 'Elm', value: 'elm' },
  { label: 'Erlang', value: 'erlang' },
  { label: 'F#', value: 'fsharp' },
  { label: 'Gherkin', value: 'gherkin' },
  { label: 'Go', value: 'go' },
  { label: 'GraphQL', value: 'graphql' },
  { label: 'Groovy', value: 'groovy' },
  { label: 'Haskell', value: 'haskell' },
  { label: 'HTML', value: 'html' },
  { label: 'HTTP', value: 'http' },
  { label: 'INI', value: 'ini' },
  { label: 'Java', value: 'java' },
  { label: 'JavaScript', value: 'javascript' },
  { label: 'JSON', value: 'json' },
  { label: 'Julia', value: 'julia' },
  { label: 'Kotlin', value: 'kotlin' },
  { label: 'Less', value: 'less' },
  { label: 'Lua', value: 'lua' },
  { label: 'Makefile', value: 'makefile' },
  { label: 'Markdown', value: 'markdown' },
  { label: 'Mermaid', value: 'mermaid' },
  { label: 'Nginx', value: 'nginx' },
  { label: 'Objective-C', value: 'objectivec' },
  { label: 'OCaml', value: 'ocaml' },
  { label: 'Perl', value: 'perl' },
  { label: 'PHP', value: 'php' },
  { label: 'PowerShell', value: 'powershell' },
  { label: 'Properties', value: 'properties' },
  { label: 'Protocol Buffers', value: 'protobuf' },
  { label: 'Python', value: 'python' },
  { label: 'R', value: 'r' },
  { label: 'Ruby', value: 'ruby' },
  { label: 'Rust', value: 'rust' },
  { label: 'Scala', value: 'scala' },
  { label: 'SCSS', value: 'scss' },
  { label: 'Shell', value: 'shell' },
  { label: 'SQL', value: 'sql' },
  { label: 'Swift', value: 'swift' },
  { label: 'TOML', value: 'toml' },
  { label: 'TypeScript', value: 'typescript' },
  { label: 'Visual Basic', value: 'vbnet' },
  { label: 'WebAssembly', value: 'wasm' },
  { label: 'XML', value: 'xml' },
  { label: 'YAML', value: 'yaml' },
];

/** A block's language as its picker label, or the raw name for one not listed. */
export function getCodeBlockLanguageLabel(lang?: string | null): string | null {
  const value = lang?.trim();
  if (!value) return null;
  return CODE_BLOCK_LANGUAGES.find((language) => language.value === value)?.label ?? value;
}
