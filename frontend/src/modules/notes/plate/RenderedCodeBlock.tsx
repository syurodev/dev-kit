import { useEffect, useState, type ComponentType } from "react";
import { NodeApi, type TElement } from "platejs";
import { PlateElement, type PlateElementProps } from "platejs/react";
import { CodeBlockElement } from "@/components/ui/code-block-node";
import { Button } from "@/components/ui/button";
import { useMermaid } from "@/components/mermaid/useMermaid";

type PreviewProps = { source: string };

function MermaidDiagram({ source }: PreviewProps) {
  // A diagram already on screen stays while its next version renders, so
  // editing it live does not blink "Rendering diagram…" at every pause.
  const state = useMermaid(source, { keepPrevious: true });
  if (state.status === "error") {
    return <p className="text-body-13 whitespace-pre-wrap text-destructive">Mermaid error: {state.message}</p>;
  }
  if (state.status === "rendering") {
    return <p className="text-body-13 text-muted-foreground">Rendering diagram…</p>;
  }
  // Mermaid's own render output, not user HTML
  return <div dangerouslySetInnerHTML={{ __html: state.svg }} />;
}

type MathState = { status: "rendering" } | { status: "rendered"; html: string } | { status: "error"; message: string };

/** KaTeX and its stylesheet (with the fonts it names) load with the first equation, not with the editor. */
const loadKatex = () =>
  Promise.all([import("katex"), import("katex/dist/katex.min.css")]).then(([module]) => module.default);

/** A display equation, from the ```math fence GitHub also draws as LaTeX. */
function MathEquation({ source }: PreviewProps) {
  const [state, setState] = useState<MathState>({ status: "rendering" });

  useEffect(() => {
    let cancelled = false;
    loadKatex()
      .then((katex) => {
        if (cancelled) return;
        try {
          // KaTeX writes the markup itself from the TeX; `trust` stays off, so
          // \href, \includegraphics and the like are refused rather than drawn.
          const html = katex.renderToString(source, {
            displayMode: true,
            strict: "ignore",
            throwOnError: true,
            trust: false,
          });
          setState({ status: "rendered", html });
        } catch (error) {
          setState({
            status: "error",
            message: error instanceof Error ? error.message : "Invalid equation.",
          });
        }
      })
      .catch(() => {
        if (!cancelled) setState({ status: "error", message: "The equation renderer did not load." });
      });
    return () => {
      cancelled = true;
    };
  }, [source]);

  if (state.status === "error") {
    return <p className="text-body-13 whitespace-pre-wrap text-destructive">Equation error: {state.message}</p>;
  }
  if (state.status === "rendering") {
    return <p className="text-body-13 text-muted-foreground">Rendering equation…</p>;
  }
  return <div className="overflow-x-auto" dangerouslySetInnerHTML={{ __html: state.html }} />;
}

/** The languages a code block is drawn as something other than code. */
const PREVIEWS: Record<string, ComponentType<PreviewProps>> = {
  mermaid: MermaidDiagram,
  math: MathEquation,
};

/**
 * The block's text, one code line per line. `NodeApi.string` on the block
 * concatenates its lines with nothing between them, which turned every
 * multi-line diagram into a single line Mermaid could not parse (a flowchart
 * failed, a sequence diagram drew its messages as one label).
 */
export function codeSource(block: TElement): string {
  return block.children.map((line) => NodeApi.string(line)).join("\n");
}

/** `source` once it has been still for `delay` ms: a live preview re-draws at a pause, not at each key. */
function useSettled(source: string, delay: number) {
  const [settled, setSettled] = useState(source);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(source), delay);
    return () => window.clearTimeout(timer);
  }, [source, delay]);
  return settled;
}

const EDIT_PREVIEW_DELAY_MS = 400;

/**
 * The code block of a note. Most languages are code (CodeBlockElement); the
 * ones in PREVIEWS are drawn as what they describe — a diagram, an equation —
 * and the source is one click away.
 *
 * `autoEdit` is set on a block the slash menu just inserted: it opens on its
 * source, with the preview under it. It is not Markdown, so it does not
 * survive a reload; neither does an empty block, which also opens on its source
 * (there is nothing to draw yet).
 */
export function RenderedCodeBlock(props: PlateElementProps) {
  const lang = (props.element as { lang?: string }).lang?.toLowerCase();
  const Preview = lang ? PREVIEWS[lang] : undefined;
  if (!Preview) {
    return <CodeBlockElement {...props} />;
  }
  return <PreviewableCodeBlock {...props} Preview={Preview} />;
}

function PreviewableCodeBlock({ Preview, ...props }: PlateElementProps & { Preview: ComponentType<PreviewProps> }) {
  const source = codeSource(props.element);
  const [mode, setMode] = useState<"preview" | "edit">(
    source.trim() === "" || (props.element as { autoEdit?: boolean }).autoEdit ? "edit" : "preview",
  );
  const settled = useSettled(source, EDIT_PREVIEW_DELAY_MS);

  if (mode === "edit") {
    return (
      <CodeBlockElement
        {...props}
        footer={
          <div className="relative">
            <Button
              type="button"
              size="sm"
              variant="outline"
              contentEditable={false}
              className="absolute top-0 right-0 z-10"
              onClick={() => setMode("preview")}
            >
              Done
            </Button>
            {settled.trim() === "" ? (
              <p className="text-body-13 text-muted-foreground">Nothing to draw yet.</p>
            ) : (
              <Preview source={settled} />
            )}
          </div>
        }
      />
    );
  }

  return (
    <PlateElement
      {...props}
      attributes={{ ...props.attributes, contentEditable: false }}
      className="group/rendered relative rounded-control border border-border-subtle bg-card p-4"
    >
      {/* Shown on hover or focus: always-on, it sat over the corner of a wide diagram. */}
      <Button
        type="button"
        size="sm"
        variant="outline"
        contentEditable={false}
        className="absolute top-2 right-2 z-10 opacity-0 transition-opacity focus-visible:opacity-100 group-hover/rendered:opacity-100"
        onClick={() => setMode("edit")}
      >
        Edit
      </Button>
      <div contentEditable={false}>
        <Preview source={source} />
      </div>
      <div className="hidden" contentEditable={false}>{props.children}</div>
    </PlateElement>
  );
}
