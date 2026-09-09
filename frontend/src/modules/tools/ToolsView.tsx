import { useMemo } from "react";
import { Wrench } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { shortcut } from "@/lib/shortcut";
import { ToolsSidebar } from "./ToolsSidebar";
import { HASH_ALGORITHMS, HASH_LABELS, useToolsStore, type HashAlgorithm } from "./toolsStore";

const WELL = "min-h-0 flex-1 rounded-md border border-border-subtle bg-surface-well";

/**
 * Figma `Tools / Content` (1145:21): a control row, an input well, an output
 * well. Every command in tools.mdx is this shape, which is why there is one
 * pane and not one per command.
 */
export function ToolsView() {
  const selected = useToolsStore((s) => s.selected);
  const algorithm = useToolsStore((s) => s.algorithm);
  const input = useToolsStore((s) => s.input);
  const output = useToolsStore((s) => s.output);
  const error = useToolsStore((s) => s.error);
  const running = useToolsStore((s) => s.running);
  const setAlgorithm = useToolsStore((s) => s.setAlgorithm);
  const setInput = useToolsStore((s) => s.setInput);
  const runHash = useToolsStore((s) => s.runHash);

  useSidebarContent(useMemo(() => <ToolsSidebar />, []));

  if (selected !== "tools.hash") {
    return (
      <EmptyState
        icon={Wrench}
        title="Pick a tool"
        body="Every tool here is a pure function: text in, text out. Nothing is written to the vault, nothing leaves the machine, and none of them need the vault unlocked."
      />
    );
  }

  return (
    <div
      className="flex h-full flex-col gap-3.5 overflow-hidden px-6 pt-3.5 pb-6"
      onKeyDown={(e) => {
        if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
          e.preventDefault();
          void runHash();
        }
      }}
    >
      <div className="flex shrink-0 items-center gap-3">
        <h1 className="text-title-16 text-text-primary">Hash</h1>
        <span className="text-code-11 text-text-tertiary">tools.hash</span>
        <span className="flex-1" />
        <Button onClick={() => void runHash()} disabled={running || !input}>
          {running ? "Running…" : "Run"}
          <span className="text-code-11 opacity-65">{shortcut("Enter")}</span>
        </Button>
      </div>

      {/* A segmented row, not a select: three options that never grow is a
          row you read, and a select would hide two thirds of it. */}
      <Tabs value={algorithm} onValueChange={(v) => setAlgorithm(v as HashAlgorithm)} className="shrink-0 gap-0">
        <TabsList>
          {HASH_ALGORITHMS.map((a) => (
            <TabsTrigger key={a} value={a}>{HASH_LABELS[a]}</TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <span className="shrink-0 text-label-11-caps text-text-tertiary">Input</span>
      <Textarea
        aria-label="hash-input"
        value={input}
        onChange={(e) => setInput(e.target.value)}
        placeholder="text to hash"
        className={`${WELL} resize-none px-4 py-3 text-code-12 text-text-primary focus-visible:ring-0`}
      />

      <span className="shrink-0 text-label-11-caps text-text-tertiary">Output</span>
      <pre
        aria-label="hash-output"
        className={`${WELL} overflow-auto px-4 py-3 text-code-12 whitespace-pre-wrap break-all select-text ${error ? "text-status-danger" : "text-text-secondary"}`}
      >
        {error || output || " "}
      </pre>

      <span className="shrink-0 text-code-11 text-text-tertiary">
        runs on this device · no vault, no network, nothing stored
      </span>
    </div>
  );
}
