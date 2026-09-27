import { useMemo } from "react";
import { Check, Copy, Eraser, Wrench } from "lucide-react";
import { CodeBlock } from "@/components/code/CodeBlock";
import { detectLanguage } from "@/components/code/languages";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSidebarContent } from "@/components/ui/sidebar-shell";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { shortcut } from "@/lib/shortcut";
import { ToolsSidebar } from "./ToolsSidebar";
import {
  HASH_ALGORITHMS,
  HASH_LABELS,
  UUID_LABELS,
  UUID_VERSIONS,
  toolById,
  type HashAlgorithm,
  type UUIDVersion,
} from "./toolCatalogue";
import { useToolsStore } from "./toolsStore";
import { useCopyToClipboard } from "./useCopyToClipboard";

const WELL = "min-h-0 flex-1 rounded-md border border-border-subtle bg-surface-well";

/**
 * Figma `Tools / Content` (1145:21): a control row, an input well, an output
 * well. Every command is that shape, which is why there is one pane and not one
 * per command — what varies is the control row, and only in three ways.
 */
export function ToolsView() {
  const selected = useToolsStore((s) => s.selected);
  const algorithm = useToolsStore((s) => s.algorithm);
  const uuidVersion = useToolsStore((s) => s.uuidVersion);
  const input = useToolsStore((s) => s.inputs[s.selected] ?? "");
  const output = useToolsStore((s) => s.outputs[s.selected] ?? "");
  const error = useToolsStore((s) => s.errors[s.selected] ?? "");
  const running = useToolsStore((s) => s.running);
  const setAlgorithm = useToolsStore((s) => s.setAlgorithm);
  const setUuidVersion = useToolsStore((s) => s.setUuidVersion);
  const setInput = useToolsStore((s) => s.setInput);
  const run = useToolsStore((s) => s.run);
  const clear = useToolsStore((s) => s.clear);

  const { copy, copied } = useCopyToClipboard();

  useSidebarContent(useMemo(() => <ToolsSidebar />, []));

  const tool = toolById(selected);
  if (!tool) {
    return (
      <EmptyState
        icon={Wrench}
        title="Pick a tool"
        body="Every tool here is a pure function: text in, text out. Nothing is written to the vault, nothing leaves the machine, and none of them need the vault unlocked."
      />
    );
  }

  const takesInput = tool.shape !== "generator";
  // A generator has nothing to be missing, so its button is never disabled for
  // want of input — pressing Run again is how you ask for another value.
  const canRun = !running && (!takesInput || input.length > 0);
  const Icon = tool.icon;

  return (
    <div
      className="flex h-full flex-col gap-3.5 overflow-hidden px-6 pt-3.5 pb-6"
      onKeyDown={(e) => {
        if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
          e.preventDefault();
          if (canRun) void run();
        }
      }}
    >
      <div className="flex shrink-0 items-center gap-3">
        <Icon className="size-(--icon-16) shrink-0 text-text-secondary" />
        <h1 className="text-title-16 text-text-primary">{tool.name}</h1>
        <span className="text-code-11 text-text-tertiary">{tool.id}</span>
        <span className="flex-1" />
        {takesInput && (
          <Button variant="ghost" size="sm" onClick={clear} disabled={running || (!input && !output)}>
            <Eraser className="size-3.5" />
            Clear
          </Button>
        )}
        <Button onClick={() => void run()} disabled={!canRun}>
          {running ? "Running…" : tool.shape === "generator" ? "Generate" : "Run"}
          <span className="text-code-11 opacity-65">{shortcut("Enter")}</span>
        </Button>
      </div>

      <p className="shrink-0 text-body-13 text-text-secondary">{tool.blurb}</p>

      {/* A segmented row, not a select: a handful of options that never grow is
          a row you read, and a select would hide most of it. */}
      {tool.shape === "text-with-algorithm" && (
        <Tabs value={algorithm} onValueChange={(v) => setAlgorithm(v as HashAlgorithm)} className="shrink-0 gap-0">
          <TabsList>
            {HASH_ALGORITHMS.map((a) => (
              <TabsTrigger key={a} value={a}>{HASH_LABELS[a]}</TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}
      {tool.shape === "generator" && (
        <Tabs value={uuidVersion} onValueChange={(v) => setUuidVersion(v as UUIDVersion)} className="shrink-0 gap-0">
          <TabsList>
            {UUID_VERSIONS.map((v) => (
              <TabsTrigger key={v} value={v}>{UUID_LABELS[v]}</TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}

      {takesInput && (
        <>
          <span className="shrink-0 text-label-11-caps text-text-tertiary">Input</span>
          <Textarea
            aria-label="tool-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={tool.inputPlaceholder}
            spellCheck={false}
            className={`${WELL} resize-none px-4 py-3 text-code-12 text-text-primary focus-visible:ring-0`}
          />
        </>
      )}

      <div className="flex h-5 shrink-0 items-center gap-2">
        <span className="text-label-11-caps text-text-tertiary">Output</span>
        <span className="flex-1" />
        {/* Copy is the point of most of these: the value is going somewhere
            else. Mounted only with an output so the row does not offer an
            action that would put an empty string on the clipboard. */}
        {output && (
          <Button variant="ghost" size="xs" onClick={() => void copy(output)}>
            {copied ? <Check className="size-3.5 text-status-success" /> : <Copy className="size-3.5" />}
            {copied ? "Copied" : "Copy"}
          </Button>
        )}
      </div>
      {/* A formatted document, a decoded JWT or base64 that turned out to be
          JSON reads as code; a hash or an encoded string stays plain. */}
      <CodeBlock
        aria-label="tool-output"
        code={error || output || " "}
        language={error ? undefined : detectLanguage(output)}
        className={`${WELL} overflow-auto scrollbar-gutter-stable px-4 py-3 text-code-12 whitespace-pre-wrap break-all select-text ${
          error ? "text-status-danger" : "text-text-secondary"
        }`}
      />

      <span className="shrink-0 text-code-11 text-text-tertiary">
        runs on this device · no vault, no network, nothing stored
      </span>
    </div>
  );
}
