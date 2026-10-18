import { AgentComposerDesignPreview } from "./AgentComposerDesignPreview";
import { useState } from "react";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AgentContextMeter } from "@/modules/agenthub/surfaces/AgentContextMeter";
import { AgentMetaInspector } from "@/modules/agenthub/surfaces/AgentMetaInspector";
import { ModelSettingsPopover } from "@/modules/agenthub/surfaces/ModelSettingsPopover";
import type { ConfigOption } from "@/modules/agenthub/types";

const INITIAL: ConfigOption[] = [
  { id: "model", name: "Model", type: "select", category: "model", currentValue: "preview", defaultValue: "preview", options: [{ value: "preview", name: "Preview model", defaultEffort: "medium" }, { value: "alternate", name: "Alternate model" }] },
  { id: "reasoning_effort", name: "Effort", type: "select", category: "thought_level", currentValue: "medium", defaultValue: "medium", options: ["low", "medium", "high"].map((value) => ({ value, name: value })) },
];

/** A model list as long as opencode's, to see the search box: every provider with credentials adds its own. */
const PROVIDERS = ["anthropic", "openai", "openrouter", "google", "mistral"];
const LONG_NAMES = [
  { value: "openrouter/nemotron-3-super-free", name: "openrouter/Nemotron 3 Super (free)" },
  { value: "openrouter/nemotron-3.5-super-long-context", name: "openrouter/NVIDIA Nemotron 3.5 Super Long Context Instruct (free)" },
];
const MANY_MODELS = [...LONG_NAMES, ...PROVIDERS.flatMap((provider) =>
  ["fast", "mini", "pro", "ultra", "preview", "long-context", "code", "vision"].map((kind) => ({
    value: `${provider}/${kind}`,
    name: `${provider}/${kind[0]!.toUpperCase()}${kind.slice(1)}`,
  })),
)];

/** In-memory fixtures only: no session, vendor or bridge calls. */
export function AgentHubCalmPreview() {
  const [options, setOptions] = useState(INITIAL);
  const [manyOptions, setManyOptions] = useState<ConfigOption[]>([
    { id: "model", name: "Model", type: "select", category: "model", currentValue: "openai/pro", defaultValue: "anthropic/fast", options: MANY_MODELS },
  ]);
  const [manyOpen, setManyOpen] = useState(false);
  const [settings, setSettings] = useState(false);
  const [meter, setMeter] = useState(false);
  const [detail, setDetail] = useState(false);
  const [used, setUsed] = useState(24503);
  return <Card><CardHeader><CardTitle>Agent Hub</CardTitle><CardDescription>Model navigation, effort and context usage. All values are local samples.</CardDescription></CardHeader>
    <CardContent className="flex flex-wrap items-center gap-3">
      <AgentComposerDesignPreview />
      <ModelSettingsPopover options={options} open={settings} onOpenChange={setSettings} onSelect={(id, value) => setOptions((current) => current.map((option) => option.id === id ? { ...option, currentValue: value } : option))} />
      {/* 40 models: the list is searched, not scrolled. */}
      <ModelSettingsPopover options={manyOptions} open={manyOpen} onOpenChange={setManyOpen} onSelect={(id, value) => setManyOptions((current) => current.map((option) => option.id === id ? { ...option, currentValue: value } : option))} />
      <AgentContextMeter usage={{ used, size: 100000, amount: used / 100000 }} turnCount={3} open={meter} onOpenChange={setMeter} />
      <Button variant="outline" size="sm" onClick={() => setUsed((value) => value > 80000 ? 24503 : 85000)}>Change sample usage</Button>
      <Button variant="outline" size="sm" onClick={() => setDetail(true)}>Preview turn detail</Button>
      <AgentMetaInspector open={detail} onOpenChange={setDetail} agentName="Sample agent with a long descriptive name" turn={3} stopReason="end_turn" cost={0.21} inputTokens={24503} outputTokens={1500} meta={{ entries: Array.from({ length: 60 }, (_, id) => ({ id, note: "Sample vendor metadata" })) }} />
    </CardContent>
  </Card>;
}
