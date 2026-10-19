import { useEffect, useState } from "react";
import { useOptionalTheme, type ResolvedTheme } from "@/theme/ThemeProvider";
import { readThemeColors } from "@/theme/themeColors";

export type MermaidState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const MERMAID_TOKENS = [
  "--surface-base",
  "--surface-raised",
  "--surface-tinted",
  "--text-primary",
  "--text-secondary",
  "--border-default",
  "--brand-text",
  "--status-info",
  "--status-warning",
  "--status-danger",
] as const;

/**
 * Mermaid's colours, from the app's tokens.
 *
 * Mermaid is handed a colour map and builds an SVG from it, so like xterm it
 * cannot follow a CSS variable — the values have to be resolved first. It
 * used to get the word "dark" or "default", which meant a diagram kept
 * Mermaid's own two palettes no matter which family the app was wearing.
 *
 * `base` is the theme name that makes Mermaid honour themeVariables at all;
 * "dark" and "default" ignore most of them.
 */
function mermaidVariables(mode: ResolvedTheme) {
  const c = readThemeColors(MERMAID_TOKENS);
  // Resolved here too: Mermaid measures its labels in the font it is told, and
  // a `var(--font-sans)` is not a font it can measure. Without it a diagram was
  // set in Mermaid's own Trebuchet/Verdana beside text in the app's.
  const fontFamily = getComputedStyle(document.documentElement).getPropertyValue("--font-sans").trim();
  return {
    ...(fontFamily ? { fontFamily } : {}),
    darkMode: mode === "dark",
    background: c["--surface-base"],
    primaryColor: c["--surface-raised"],
    primaryTextColor: c["--text-primary"],
    primaryBorderColor: c["--border-default"],
    secondaryColor: c["--surface-tinted"],
    secondaryTextColor: c["--text-primary"],
    tertiaryColor: c["--surface-base"],
    tertiaryTextColor: c["--text-secondary"],
    lineColor: c["--text-tertiary"] || c["--text-secondary"],
    textColor: c["--text-primary"],
    mainBkg: c["--surface-raised"],
    nodeBorder: c["--border-default"],
    clusterBkg: c["--surface-base"],
    clusterBorder: c["--border-default"],
    titleColor: c["--text-primary"],
    edgeLabelBackground: c["--surface-raised"],
    actorBorder: c["--border-default"],
    actorBkg: c["--surface-raised"],
    noteBkgColor: c["--surface-tinted"],
    noteTextColor: c["--text-primary"],
    noteBorderColor: c["--border-default"],
    errorBkgColor: c["--status-danger"],
    errorTextColor: c["--text-primary"],
  };
}

/** A short stable id for a render, so the same diagram is the same element id wherever it is drawn. */
function hash(text: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(36);
}

const renderCache = new Map<string, Promise<string>>();

// One render at a time. Mermaid is configured globally (theme, variables)
// before each render, so two diagrams drawn together — an answer with five of
// them — could otherwise draw with each other's settings.
let chain: Promise<unknown> = Promise.resolve();
function inTurn<T>(job: () => Promise<T>): Promise<T> {
  const run = chain.then(job, job);
  chain = run.catch(() => undefined);
  return run;
}

function renderDiagram(source: string, theme: ResolvedTheme, family: string): Promise<string> {
  // The family is part of the key: two families in the same mode are two
  // different diagrams, and without it the first one rendered would be served
  // to the second from cache. Not the instance: a diagram scrolled out of a
  // virtualised transcript and back is not drawn again.
  const key = `${family}:${theme}:${source}`;
  const cached = renderCache.get(key);
  if (cached) return cached;
  const variables = mermaidVariables(theme);
  const promise = inTurn(async () => {
    const { default: mermaid } = await import("mermaid");
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      // "base" is the only built-in that honours themeVariables; "dark" and
      // "default" override most of what is passed here.
      theme: "base",
      themeVariables: variables,
      // A syntax error is the caller's to show; Mermaid's own "bomb" diagram
      // is left out, and with it the element it would leave in the page.
      suppressErrorRendering: true,
    });
    // Parsed first: a refusal then costs no layout and leaves nothing behind.
    await mermaid.parse(source);
    return (await mermaid.render(`mmd-${hash(key)}`, source)).svg;
  });
  renderCache.set(key, promise);
  // A diagram that failed to draw is drawn again next time it is asked for
  // (the source may be the same while the cause — the library not loading —
  // is not); only a successful one is worth keeping.
  promise.catch(() => renderCache.delete(key));
  return promise;
}

/**
 * Draws Mermaid `source` as the app's theme would. `enabled: false` waits (a
 * message still streaming has half a diagram); `keepPrevious` leaves the last
 * picture up while a changed source renders, for a live preview.
 */
export function useMermaid(source: string, { enabled = true, keepPrevious = false } = {}): MermaidState {
  const theme = useOptionalTheme();
  const resolvedTheme = theme?.resolvedTheme ?? "dark";
  const family = theme?.family ?? "devkit";
  const [state, setState] = useState<MermaidState>({ status: "rendering" });

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    setState((current) => (keepPrevious && current.status === "rendered" ? current : { status: "rendering" }));
    renderDiagram(source, resolvedTheme, family)
      .then((svg) => {
        if (!cancelled) setState({ status: "rendered", svg });
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({ status: "error", message: error instanceof Error ? error.message : "Invalid diagram source." });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, family, keepPrevious, resolvedTheme, source]);

  return state;
}
