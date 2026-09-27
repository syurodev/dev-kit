import { highlightCode } from "@lezer/highlight";
import { devkitSyntax } from "@/components/code-editor/devkitSyntax";
import { loadLanguage } from "./languages";

/**
 * Syntax colours without an editor: Lezer, the parser behind the app's
 * CodeMirror editors, run once over the text through the same devkitSyntax
 * table — so a Java file in a tool result is the colour it is in CodeGraph's
 * editor, and follows the theme the same way.
 */

/** What highlightCode needs from a parser: a parse to a tree. */
export type CodeParser = { parse(code: string): Parameters<typeof highlightCode>[1] };

/** A run of text and the classes it takes; "" for plain. */
export type SyntaxSegment = { text: string; className: string };

/**
 * Past this a parse is a long task on the main thread, and the text stays
 * plain. Shared so every surface that colours code gives up at the same size.
 */
export const HIGHLIGHT_LIMIT = 60_000;

/** The parser for a language id, loaded once. Rejects for an unknown id. */
export function loadParser(language: string): Promise<CodeParser> {
  return loadLanguage(language).then((support) => support.language.parser as CodeParser);
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

/**
 * tokenize, split per line: `lines[i]` is line i's runs, the newlines dropped.
 * A diff colours whole lines and needs to address them by number.
 */
export function tokenizeLines(code: string, parser: CodeParser): SyntaxSegment[][] {
  const lines: SyntaxSegment[][] = [[]];
  for (const segment of tokenize(code, parser)) {
    const parts = segment.text.split("\n");
    parts.forEach((text, i) => {
      if (i > 0) lines.push([]);
      if (text) lines[lines.length - 1].push({ text, className: segment.className });
    });
  }
  return lines;
}
