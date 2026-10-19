import { useEffect, useId, useState } from "react";
import { NodeApi, type TElement } from "platejs";
import { PlateElement, type PlateElementProps } from "platejs/react";
import { CodeBlockElement } from "@/components/ui/code-block-node";
import { Button } from "@/components/ui/button";
import { useTheme, type ResolvedTheme } from "@/theme/ThemeProvider";
import { readThemeColors } from "@/theme/themeColors";

type RenderState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const renderCache = new Map<string, Promise<{ svg: string }>>();

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
 * used to get the word "dark" or "default", which meant a diagram in a Notes
 * page kept Mermaid's own two palettes no matter which family the app was
 * wearing.
 *
 * `base` is the theme name that makes Mermaid honour themeVariables at all;
 * "dark" and "default" ignore most of them.
 */
function mermaidVariables(mode: ResolvedTheme) {
  const c = readThemeColors(MERMAID_TOKENS);
  return {
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

function renderMermaid(id: string, source: string, theme: ResolvedTheme, family: string) {
  // The family is part of the key: two families in the same mode are two
  // different diagrams, and without it the first one rendered would be served
  // to the second from cache.
  const key = `${id}:${family}:${theme}:${source}`;
  const cached = renderCache.get(key);
  if (cached) {
    return cached;
  }
  const variables = mermaidVariables(theme);
  const promise = import("mermaid").then(({ default: mermaid }) => {
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      // "base" is the only built-in that honours themeVariables; "dark" and
      // "default" override most of what is passed here.
      theme: "base",
      themeVariables: variables,
    });
    return mermaid.render(id.replace(/:/g, ""), source);
  });
  renderCache.set(key, promise);
  return promise;
}

function MermaidDiagram({ source }: { source: string }) {
  const id = useId();
  const { resolvedTheme, family } = useTheme();
  const [state, setState] = useState<RenderState>({ status: "rendering" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "rendering" });
    renderMermaid(id, source, resolvedTheme, family)
      .then(({ svg }) => {
        if (!cancelled) {
          setState({ status: "rendered", svg });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({
            status: "error",
            message: error instanceof Error ? error.message : "Invalid diagram source.",
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [family, id, resolvedTheme, source]);

  if (state.status === "error") {
    return <p className="text-body-13 text-destructive">Mermaid error: {state.message}</p>;
  }
  if (state.status === "rendering") {
    return <p className="text-body-13 text-muted-foreground">Rendering diagram…</p>;
  }
  // Mermaid's own render output, not user HTML
  return <div dangerouslySetInnerHTML={{ __html: state.svg }} />;
}

/**
 * The diagram's text, one code line per line. `NodeApi.string` on the block
 * concatenates its lines with nothing between them, which turned every
 * multi-line diagram into a single line Mermaid could not parse (a flowchart
 * failed, a sequence diagram drew its messages as one label).
 */
export function mermaidSource(block: TElement): string {
  return block.children.map((line) => NodeApi.string(line)).join("\n");
}

export function MermaidCodeBlock(props: PlateElementProps) {
  const lang = (props.element as { lang?: string }).lang;
  const [mode, setMode] = useState<"preview" | "edit">("preview");

  if (lang !== "mermaid") {
    return <CodeBlockElement {...props} />;
  }

  if (mode === "edit") {
    return (
      <PlateElement {...props} className="relative">
        <Button
          type="button"
          size="sm"
          variant="outline"
          contentEditable={false}
          className="absolute right-2 top-2 z-10"
          onClick={() => setMode("preview")}
        >
          Preview
        </Button>
        {props.children}
      </PlateElement>
    );
  }

  const source = mermaidSource(props.element);

  return (
    <PlateElement
      {...props}
      attributes={{ ...props.attributes, contentEditable: false }}
      className="relative rounded-lg border border-border bg-card p-4"
    >
      <Button
        type="button"
        size="sm"
        variant="outline"
        contentEditable={false}
        className="absolute right-2 top-2 z-10"
        onClick={() => setMode("edit")}
      >
        Edit
      </Button>
      <div contentEditable={false}>
        <MermaidDiagram source={source} />
      </div>
      <div className="hidden" contentEditable={false}>{props.children}</div>
    </PlateElement>
  );
}
