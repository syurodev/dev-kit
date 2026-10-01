import type { KV, VarSet, VarsUpdate } from "@/bridge/apiclient";

/**
 * A variable as the editor holds it. `existing` marks a secret whose value is
 * sealed and was never sent to this window: an empty value then means "keep
 * it", not "make it empty". `original` is the name it was loaded under, since
 * a renamed secret cannot be kept — its value is not here to move.
 */
export type VarRow = KV & { secret?: boolean; existing?: boolean; original?: string };

/** One scope's variables as rows: plain ones by name, then the secrets. */
export function rowsFromVarSet(set: VarSet): VarRow[] {
  const plain = Object.entries(set.vars ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]): VarRow => ({ key, value }));
  const secrets = [...(set.secretVars ?? [])]
    .sort((a, b) => a.localeCompare(b))
    .map((key): VarRow => ({ key, value: "", secret: true, existing: true, original: key }));
  return [...plain, ...secrets];
}

/**
 * The update a save sends. A sealed secret left empty under its own name is
 * kept; one given a value is replaced; one deleted, renamed while empty, or
 * turned plain is dropped (a plain one then carries the typed value).
 */
export function buildVarsUpdate(rows: readonly VarRow[]): VarsUpdate {
  const update: VarsUpdate = { vars: {}, secrets: {}, keepSecrets: [] };
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (!row.secret) {
      update.vars[key] = row.value;
    } else if (row.existing && row.value === "" && row.original === key) {
      update.keepSecrets.push(key);
    } else {
      update.secrets[key] = row.value;
    }
  }
  return update;
}
