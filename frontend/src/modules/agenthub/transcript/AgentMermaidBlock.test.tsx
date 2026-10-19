import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";

const FENCE = "Here is the flow:\n\n```mermaid\nsequenceDiagram\n  Dev->>RT: POST /v1/cells\n```\n\nDone.";

const render = (settled: boolean) =>
  renderToString(
    <TooltipProvider>
      <AgentProse text={FENCE} settled={settled} />
    </TooltipProvider>,
  );

// A diagram is drawn by Mermaid in an effect, so a server render only ever sees
// the state before it — which has to be the code block the reader had before
// diagrams were drawn at all: the same fallback a diagram Mermaid refuses gets.
describe("a mermaid fence in an answer", () => {
  test("is the code block until it is drawn, settled or not", () => {
    for (const settled of [false, true]) {
      const html = render(settled);
      expect(html).toContain('data-testid="agent-code-block"');
      expect(html).toContain(">mermaid<");
      expect(html).toContain("sequenceDiagram");
      expect(html).toContain('data-testid="code-block-copy"');
      expect(html).not.toContain('data-testid="agent-mermaid-block"');
    }
  });

  test("does not touch another language's fence", () => {
    const html = renderToString(
      <TooltipProvider>
        <AgentProse text={"```go\nx := 1\n```"} settled />
      </TooltipProvider>,
    );
    expect(html).toContain('data-testid="agent-code-block"');
  });
});
