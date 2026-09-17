import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";

import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { errorMessage } from "../../bridge/errors";
import {
  getAgentCheckpointAvailability,
  getAgentCheckpoints,
  saveAgentCheckpoints,
} from "../../bridge/preferences";
import {
  checkpointProblemLines,
  checkpointStatusLine,
  formatExcludes,
  parseExcludes,
  parsePositiveInteger,
} from "./agentCheckpoints";
import type { AgentCheckpointSettings } from "../../../bindings/devkit/internal/preferences/models";

const SETTINGS_KEY = ["preferences", "agent-checkpoints"] as const;
const AVAILABILITY_KEY = ["agenthub", "checkpoint-availability"] as const;

/**
 * Whether Agent Hub snapshots your project's files around each turn, and what
 * it leaves out.
 *
 * The switch and the sentence under it are two different facts: the switch is
 * what you want, the sentence is what this machine can do. A machine with no
 * usable git shows the reason and a dead switch, because a live switch that
 * changes nothing is worse than no switch.
 *
 * The numbers apply immediately rather than at the next restart — they are what
 * you reach for when a project is not being checkpointed, and having to quit
 * DevKit to find out whether raising the ceiling helped is not a setting, it is
 * a guess.
 */
export function AgentCheckpointSetting() {
  const client = useQueryClient();
  const [failed, setFailed] = useState("");

  const { data: settings, error: readError } = useQuery({
    queryKey: SETTINGS_KEY,
    queryFn: getAgentCheckpoints,
  });
  const { data: availability } = useQuery({
    queryKey: AVAILABILITY_KEY,
    queryFn: getAgentCheckpointAvailability,
  });

  const save = useMutation({
    mutationFn: saveAgentCheckpoints,
    onSuccess: (_, next) => {
      setFailed("");
      client.setQueryData(SETTINGS_KEY, next);
      // The sentence beside the switch is about the machine AND the setting,
      // so it is stale the moment either changes.
      void client.invalidateQueries({ queryKey: AVAILABILITY_KEY });
    },
    onError: (cause) => setFailed(errorMessage(cause)),
  });

  const status = checkpointStatusLine(availability);
  const problems = checkpointProblemLines(availability);
  const message = failed || (readError ? errorMessage(readError) : "");

  function update(patch: Partial<AgentCheckpointSettings>) {
    if (!settings) return;
    save.mutate({ ...settings, ...patch });
  }

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2.5 rounded-control border border-border-subtle bg-surface-raised py-2.5 pr-3 pl-3.5">
        <span className="min-w-0 flex-1 text-body-13 text-text-primary">
          Snapshot files around each turn so they can be put back
        </span>
        <Switch
          checked={(settings?.enabled ?? true) && status.switchEnabled}
          disabled={settings === undefined || !status.switchEnabled || save.isPending}
          onCheckedChange={(checked) => update({ enabled: checked })}
        />
      </label>

      <p
        data-testid="checkpoint-status"
        className={cn(
          "flex items-start gap-2 text-body-12",
          status.tone === "warning" ? "text-status-warning" : "text-text-tertiary",
        )}
      >
        {status.tone === "warning" ? (
          <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
        ) : null}
        <span className="min-w-0">{status.text}</span>
      </p>

      <p className="text-body-12 text-text-tertiary">
        Kept under {availability?.root || "your data folder"}, in a git DevKit keeps to itself — your
        own repository's history, index and branches are never touched. Dependency folders like
        node_modules are always left out, whatever your .gitignore says.
      </p>

      {settings?.enabled && status.switchEnabled ? (
        <>
          <div className="flex gap-3">
            <NumberField
              label="Largest file"
              suffix="MiB"
              value={settings.maxFileSizeMiB}
              disabled={save.isPending}
              onCommit={(value) => update({ maxFileSizeMiB: value })}
            />
            <NumberField
              label="Keep for"
              suffix="days"
              value={settings.retentionDays}
              disabled={save.isPending}
              onCommit={(value) => update({ retentionDays: value })}
            />
          </div>

          <ExcludesField
            value={settings.extraExcludes ?? []}
            disabled={save.isPending}
            onCommit={(patterns) => update({ extraExcludes: patterns })}
          />
        </>
      ) : null}

      {problems.length > 0 ? (
        <div data-testid="checkpoint-problems" className="flex flex-col gap-1">
          {problems.map((problem) => (
            <p key={problem.project} className="flex items-start gap-2 text-body-12 text-status-warning">
              <AlertTriangle aria-hidden="true" className="mt-px size-(--icon-12) shrink-0" />
              <span className="min-w-0">
                <span className="text-code-12">{problem.project}</span> — {problem.reason}
              </span>
            </p>
          ))}
        </div>
      ) : null}

      {message ? <p className="text-body-12 text-status-danger">{message}</p> : null}
    </div>
  );
}

/**
 * A number you type, saved when you leave the field or press Enter.
 *
 * Not on every keystroke: typing "25" over "10" passes through "2", and saving
 * that would clamp it to a number nobody asked for and then show it back.
 */
function NumberField({
  label,
  suffix,
  value,
  disabled,
  onCommit,
}: {
  label: string;
  suffix: string;
  value: number;
  disabled: boolean;
  onCommit: (value: number) => void;
}) {
  const [draft, setDraft] = useState(String(value));
  useEffect(() => setDraft(String(value)), [value]);

  function commit() {
    const parsed = parsePositiveInteger(draft);
    if (parsed === null) {
      setDraft(String(value));
      return;
    }
    if (parsed !== value) onCommit(parsed);
  }

  return (
    <label className="flex flex-1 flex-col gap-1">
      <span className="text-caption-11 text-text-tertiary">{`${label} (${suffix})`}</span>
      <Input
        value={draft}
        inputMode="numeric"
        disabled={disabled}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") event.currentTarget.blur();
        }}
      />
    </label>
  );
}

/** Extra gitignore patterns, one per line, saved when you leave the box. */
function ExcludesField({
  value,
  disabled,
  onCommit,
}: {
  value: readonly string[];
  disabled: boolean;
  onCommit: (patterns: string[]) => void;
}) {
  const [draft, setDraft] = useState(() => formatExcludes(value));
  useEffect(() => setDraft(formatExcludes(value)), [value]);

  return (
    <label className="flex flex-col gap-1">
      <span className="text-caption-11 text-text-tertiary">Also leave out (one pattern per line)</span>
      <Textarea
        value={draft}
        rows={3}
        spellCheck={false}
        disabled={disabled}
        placeholder={"generated/\n*.snapshot"}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => {
          const patterns = parseExcludes(draft);
          if (formatExcludes(patterns) !== formatExcludes(value)) onCommit(patterns);
        }}
      />
    </label>
  );
}
