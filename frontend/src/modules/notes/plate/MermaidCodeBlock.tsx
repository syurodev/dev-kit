import { useEffect, useId, useState } from "react";
import { NodeApi } from "platejs";
import { PlateElement, type PlateElementProps } from "platejs/react";
import { CodeBlockElement } from "@/components/ui/code-block-node";
import { Button } from "@/components/ui/button";

type RenderState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const renderCache = new Map<string, Promise<{ svg: string }>>();

function renderMermaid(id: string, source: string) {
  const key = `${id}:${source}`;
  const cached = renderCache.get(key);
  if (cached) {
    return cached;
  }
  const promise = import("mermaid").then(({ default: mermaid }) => {
    mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: "dark" });
    return mermaid.render(id.replace(/:/g, ""), source);
  });
  renderCache.set(key, promise);
  return promise;
}

function MermaidDiagram({ source }: { source: string }) {
  const id = useId();
  const [state, setState] = useState<RenderState>({ status: "rendering" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "rendering" });
    renderMermaid(id, source)
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
  }, [id, source]);

  if (state.status === "error") {
    return <p className="text-sm text-destructive">Mermaid error: {state.message}</p>;
  }
  if (state.status === "rendering") {
    return <p className="text-sm text-muted-foreground">Rendering diagram…</p>;
  }
  // eslint-disable-next-line react/no-danger -- Mermaid's own render output, not user HTML
  return <div dangerouslySetInnerHTML={{ __html: state.svg }} />;
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

  const source = NodeApi.string(props.element);

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
