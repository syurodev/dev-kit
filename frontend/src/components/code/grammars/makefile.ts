import type { StreamParser, StringStream } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { shell } from "@codemirror/legacy-modes/mode/shell";

// Shell's state keeps its tokenizers at the top level; StreamLanguage copies
// that array for incremental parsing, including continued recipe strings.
type MakeState = { tokens: unknown[]; recipe: boolean; continuedRecipe: boolean };
const DIRECTIVE = /^(?:-?include|sinclude|define|endef|ifdef|ifndef|ifeq|ifneq|else|endif|override|export|unexport|private|vpath)\b/;

function expansion(stream: StringStream): boolean {
  if (stream.match(/\$[^({\s]/)) return true;
  const open = stream.match(/\$[({]/);
  if (!open) return false;
  const start = stream.current().slice(-1);
  const close = start === "(" ? ")" : "}";
  let depth = 1;
  while (!stream.eol() && depth > 0) {
    const char = stream.next();
    if (char === start) depth++;
    if (char === close) depth--;
  }
  return true;
}

export const makefile: StreamParser<MakeState> = {
  name: "makefile",
  startState: () => ({ tokens: [], recipe: false, continuedRecipe: false }),
  token(stream, state) {
    if (stream.sol()) {
      state.recipe = stream.peek() === "\t" || state.continuedRecipe;
      state.continuedRecipe = state.recipe && /(?:^|[^\\])(?:\\\\)*\\$/.test(stream.string);
      if (!state.recipe) state.tokens = [];
    }
    if (stream.eatSpace()) return null;
    if (expansion(stream)) return "makeVariable";
    if (state.recipe) {
      if (stream.match(/^[@+-]/)) return "operator";
      return shell.token(stream, state);
    }
    if (stream.match(/^\\./)) return null;
    if (stream.match(/^#.*/)) return "comment";
    if (stream.match(DIRECTIVE)) return "keyword";
    if (stream.match(/^[\w.-]+(?=\s*(?:::?=|[?+!]?=))/)) return "variableName.definition";
    // Targets may be paths, patterns, multiple names, or special (.PHONY).
    if (stream.match(/^[^\s:=#$]+(?=[^:=#]*:(?!=))/)) return "makeTarget";
    if (stream.match(/^(?:::?=|[?+!]?=|::?|[|;])/)) return "operator";
    if (stream.match(/^"(?:[^"\\]|\\.)*"|^'(?:[^'\\]|\\.)*'/)) return "string";
    if (stream.match(/^\d+\b/)) return "number";
    stream.next();
    stream.eatWhile(/[^\s$#:=|;'"\\]/);
    return null;
  },
  blankLine(state) { state.continuedRecipe = false; },
  languageData: { commentTokens: { line: "#" } },
  tokenTable: { makeVariable: tags.meta, makeTarget: tags.function(tags.variableName) },
};
