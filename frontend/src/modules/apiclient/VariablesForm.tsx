import { ShieldCheck } from "lucide-react";
import type { EnvTag } from "@/bridge/environments";
import { Input } from "@/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { KeyValueTable } from "./KeyValueTable";
import type { VarRow } from "./variablesModel";

/** Shared editor content for global, collection and environment variables. */
export function VariablesForm({
  rows,
  onRowsChange,
  environmentDetails,
  name,
  onNameChange,
  tag,
  onTagChange,
  envTags,
}: {
  rows: VarRow[];
  onRowsChange: (rows: VarRow[]) => void;
  environmentDetails: boolean;
  name: string;
  onNameChange: (name: string) => void;
  tag: string;
  onTagChange: (tag: string) => void;
  envTags: EnvTag[];
}) {
  return (
    <div className="mx-auto max-w-4xl space-y-6">
      {environmentDetails ? (
        <section className="rounded-xl border border-border-subtle bg-surface-base p-4">
          <h2 className="mb-4 text-body-13-medium">Environment details</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="flex flex-col gap-2 text-caption-11-medium text-text-secondary">
              Name
              <Input
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                aria-label="Environment name"
                placeholder="e.g. API staging"
              />
            </label>
            <label className="flex min-w-0 flex-col gap-2 text-caption-11-medium text-text-secondary">
              Environment tag
              <NativeSelect
                className="w-full"
                value={tag}
                onChange={(e) => onTagChange(e.target.value)}
                aria-label="Environment tag"
              >
                {envTags.map((t) => (
                  <NativeSelectOption key={t.id} value={t.name}>
                    {t.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
          </div>
          <p className="mt-3 flex items-center gap-2 text-caption-11 text-text-tertiary">
            <ShieldCheck className="size-3.5 shrink-0" />
            {envTags.find((t) => t.name === tag)?.requiresConfirmation !== false
              ? "This tag requires approval before every send."
              : "This tag allows sending without an environment approval."}
          </p>
        </section>
      ) : null}
      <section className="space-y-3">
        <div>
          <h2 className="text-body-13-medium">Variables</h2>
          <p className="mt-1 text-caption-11 text-text-tertiary">
            Use <code className="font-mono">{"{{name}}"}</code> in URLs, headers
            and request bodies.
          </p>
        </div>
        <KeyValueTable
          rows={rows}
          onChange={(next) => onRowsChange(next as VarRow[])}
          keyPlaceholder="Variable"
          valuePlaceholder="Value"
          toggleable={false}
          secrets={rows.map((row) => !!row.secret)}
          onToggleSecret={(index) =>
            onRowsChange(
              rows.map((row, i) =>
                i === index
                  ? { ...row, secret: !row.secret, existing: false }
                  : row,
              ),
            )
          }
          secretPlaceholder={(index) =>
            rows[index]?.existing
              ? "•••••• sealed — type to replace"
              : "secret value"
          }
        />
        <p className="text-caption-11 text-text-tertiary">
          Lock a value to seal it in the vault. Leave an existing secret blank
          to keep it; enter a value to replace it.
        </p>
      </section>
      <section className="rounded-lg border border-border-subtle bg-surface-well/50 p-4">
        <h2 className="text-caption-11-medium text-text-secondary">
          Which value is used?
        </h2>
        <p className="mt-2 text-body-13 text-text-secondary">
          Globals <span aria-hidden>→</span> Collection & folders{" "}
          <span aria-hidden>→</span> Environment
        </p>
        <p className="mt-1 text-caption-11 text-text-tertiary">
          A value on the right overrides the same name on the left. Manage
          shared credentials in Accounts, then select them in the request’s Auth
          tab.
        </p>
      </section>
    </div>
  );
}
