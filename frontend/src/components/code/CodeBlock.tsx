import * as React from "react";

import { HIGHLIGHT_LIMIT, loadParser, tokenize, type SyntaxSegment } from "./highlight";
import { normalizeLanguage } from "./languages";

const CACHE_LIMIT = 64;

/**
 * Parsed blocks by language and text. Virtualised lists (the transcript, the
 * query history) remount a block scrolled away and back; without this it
 * would flash plain and re-parse every time it came into view.
 */
const highlighted = new Map<string, SyntaxSegment[]>();

function remember(key: string, segments: SyntaxSegment[]) {
  highlighted.delete(key);
  highlighted.set(key, segments);
  if (highlighted.size > CACHE_LIMIT) highlighted.delete(highlighted.keys().next().value!);
}

export type Highlight = { language: string; code: string; segments: SyntaxSegment[] };

/**
 * The code's syntax runs, once its grammar has loaded. Parsed in an effect,
 * after the plain text has painted: the text is the same either way, so the
 * colours arrive without moving anything. `language` is any name the
 * registry knows — an id, an alias, a fence's info string.
 */
export function useHighlight(code: string, language?: string | null): Highlight | null {
  const id = normalizeLanguage(language);
  const [highlight, setHighlight] = React.useState<Highlight | null>(() => {
    const hit = id ? highlighted.get(`${id} ${code}`) : undefined;
    return id && hit ? { language: id, code, segments: hit } : null;
  });
  const parsedOnce = highlight !== null;

  React.useEffect(() => {
    if (!id || code.length > HIGHLIGHT_LIMIT) return;
    const key = `${id} ${code}`;
    const hit = highlighted.get(key);
    if (hit) {
      setHighlight({ language: id, code, segments: hit });
      return;
    }
    let cancelled = false;
    // A block that is streaming changes every frame. After its first parse it
    // is re-parsed at most every 120 ms; in between, the text that arrived
    // since shows plain after the coloured part (see renderHighlighted).
    const timer = setTimeout(
      () => {
        loadParser(id)
          .then((parser) => {
            if (cancelled) return;
            const segments = tokenize(code, parser);
            remember(key, segments);
            setHighlight({ language: id, code, segments });
          })
          .catch(() => {
            // No grammar, or it failed to load: the plain text is already there.
          });
      },
      parsedOnce ? 120 : 0,
    );
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [id, code]);

  return highlight && highlight.language === id ? highlight : null;
}

/**
 * The code with its colours. A parse of an earlier version still applies to
 * the part of the text it covered — a streaming block only ever grows — so
 * that part keeps its colour and only the new tail is plain until the next
 * parse. Anything else shows plain.
 */
export function renderHighlighted(code: string, highlight: Pick<Highlight, "code" | "segments"> | null): React.ReactNode {
  if (!highlight || !code.startsWith(highlight.code)) return code;
  const nodes: React.ReactNode[] = highlight.segments.map((segment, index) =>
    segment.className ? (
      <span key={index} className={segment.className}>
        {segment.text}
      </span>
    ) : (
      segment.text
    ),
  );
  const tail = code.slice(highlight.code.length);
  if (tail) nodes.push(tail);
  return nodes;
}

export type CodeBlockProps = Omit<React.ComponentProps<"pre">, "children"> & {
  code: string;
  /** Any name the registry knows; unknown or absent shows the text plain. */
  language?: string | null;
};

/**
 * Read-only code, coloured. Only the `<pre>`: the card around it, a header,
 * a Copy button are the caller's, because every surface that shows code
 * already has its own frame. Everything but `code` and `language` goes onto
 * the `<pre>`.
 */
export function CodeBlock({ code, language, ...props }: CodeBlockProps) {
  const highlight = useHighlight(code, language);
  return (
    <pre data-language={highlight?.language} {...props}>
      <code>{renderHighlighted(code, highlight)}</code>
    </pre>
  );
}
