import * as React from "react";
import { Check, Code2, Copy, Workflow } from "lucide-react";

import { useMermaid } from "@/components/mermaid/useMermaid";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import { AgentCodeBlock } from "./AgentCodeBlock";

/**
 * A ```mermaid fence in an answer, drawn as the diagram it describes.
 *
 * It is the code block until the picture exists, and again whenever it cannot
 * be drawn: a message still streaming has half a diagram, a model's Mermaid is
 * wrong often enough, and in both the reader keeps what they had before this
 * existed — the source, highlighted, with Copy — rather than an error. Once
 * drawn, Source shows the text and Diagram brings the picture back.
 *
 * `settled` is the message being finished (AgentProse): nothing is drawn from
 * a partial block, so a streaming answer never flickers between the two.
 */
export function AgentMermaidBlock({ code, settled, className }: { code: string; settled: boolean; className?: string }) {
  const state = useMermaid(code, { enabled: settled });
  const [showSource, setShowSource] = React.useState(false);
  const [copied, setCopied] = React.useState(false);

  if (state.status !== "rendered") {
    return <AgentCodeBlock code={code} language="mermaid" className={className} />;
  }
  if (showSource) {
    return (
      <AgentCodeBlock
        code={code}
        language="mermaid"
        className={className}
        headerAction={
          <FramedCardAction opens={false} onClick={() => setShowSource(false)} data-testid="mermaid-show-diagram">
            <Workflow aria-hidden="true" />
            Diagram
          </FramedCardAction>
        }
      />
    );
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard is unavailable in some webviews; Source shows the text to select.
    }
  };

  return (
    <FramedCard data-testid="agent-mermaid-block" className={cn("max-w-full", className)}>
      <FramedCardHeader
        icon={Workflow}
        title={<span className="text-code-11">mermaid</span>}
        action={
          <>
            <FramedCardAction opens={false} onClick={() => setShowSource(true)} data-testid="mermaid-show-source">
              <Code2 aria-hidden="true" />
              Source
            </FramedCardAction>
            <FramedCardAction opens={false} onClick={copy} data-testid="code-block-copy">
              {copied ? <Check className="text-status-success" aria-hidden="true" /> : <Copy aria-hidden="true" />}
              {copied ? "Copied" : "Copy"}
            </FramedCardAction>
          </>
        }
      />
      <FramedCardPreview className="overflow-x-auto">
        {/* Mermaid's own render output, not user HTML (securityLevel "strict"). */}
        <div
          role="img"
          aria-label="Mermaid diagram"
          data-testid="mermaid-diagram"
          className="flex w-full justify-center [&_svg]:h-auto [&_svg]:max-w-full"
          dangerouslySetInnerHTML={{ __html: state.svg }}
        />
      </FramedCardPreview>
    </FramedCard>
  );
}
