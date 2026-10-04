import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Check, Copy, Save, Trash2, Variable } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import {
  createEnvironment,
  deleteEnvironment,
  duplicateEnvironment,
  getGlobals,
  setCollectionVariables,
  setGlobals,
  updateEnvironment,
  type KV,
  type VarsUpdate,
} from "@/bridge/apiclient";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { useApiClientStore } from "./apiclientStore";
import { ConfirmDialog } from "./Dialogs";
import { closeTabByParams, openVariables, VARIABLES_VIEW } from "./documents";
import { KeyValueTable } from "./KeyValueTable";
import { buildVarsUpdate, rowsFromVarSet, type VarRow } from "./variablesModel";
import { shortcut } from "@/lib/shortcut";

/**
 * One scope's variables: Globals, a collection (or folder), or an
 * environment. Precedence is the Postman one — globals, then the collection
 * from its root folder down, then the environment — and the page says so,
 * because "which value wins" is the question every variables screen gets.
 *
 * A secret is write-only. It is sealed in the vault item's payload, named
 * here and never shown again; typing into its value replaces it, and leaving
 * it empty keeps it.
 */
export function VariablesView({ params }: { params?: Record<string, unknown> } = {}) {
  const scope = params?.scope;
  const id = typeof params?.id === "string" ? params.id : "";
  const { collections, environments, envTags } = useApiClientStore(
    useShallow((s) => ({ collections: s.collections, environments: s.environments, envTags: s.envTags })),
  );
  const store = useApiClientStore.getState();

  const [rows, setRows] = useState<VarRow[] | null>(null);
  const [name, setName] = useState("");
  const [tag, setTag] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [loadError, setLoadError] = useState("");
  const isNew = scope === "environment" && id === "new";

  useEffect(() => {
    if (!collections || !environments) void store.refresh();
  }, [collections, environments, store]);

  const environment = scope === "environment" && !isNew ? environments?.find((env) => env.id === id) : undefined;
  const collection = scope === "collection" ? collections?.find((c) => c.id === id) : undefined;

  // Load once per scope; the store refreshing under an open editor must not
  // throw away what is being typed.
  useEffect(() => {
    setRows(null);
    setLoadError("");
  }, [scope, id]);

  useEffect(() => {
    if (rows !== null) return;
    if (scope === "globals") {
      getGlobals()
        .then((set) => setRows(rowsFromVarSet(set)))
        .catch((e) => setLoadError(errorMessage(e)));
    } else if (isNew) {
      setName("New environment");
      setTag(envTags.find((t) => !t.requiresConfirmation)?.name ?? envTags[0]?.name ?? "");
      setRows([]);
    } else if (environment) {
      setName(environment.name);
      setTag(environment.env);
      setRows(rowsFromVarSet(environment));
    } else if (collection) {
      setRows(rowsFromVarSet({ vars: collection.vars ?? null, secretVars: collection.secretVars ?? null }));
    }
  }, [rows, scope, isNew, environment, collection, envTags]);

  // A tag list that arrives after a new environment opened still gets a default.
  useEffect(() => {
    if (isNew && !tag && envTags.length) {
      setTag(envTags.find((t) => !t.requiresConfirmation)?.name ?? envTags[0].name);
    }
  }, [isNew, tag, envTags]);

  if (scope !== "globals" && scope !== "environment" && scope !== "collection") {
    return <EmptyState icon={Variable} title="Nothing to edit" body="Open Globals, an environment or a collection's variables from the sidebar." />;
  }
  if (loadError) return <EmptyState icon={Variable} title="Variables could not be loaded" body={loadError} />;
  if (collections && environments && !isNew && scope !== "globals" && !environment && !collection) {
    return <EmptyState icon={Variable} title="This no longer exists" body="It was deleted, here or on another device." />;
  }
  if (rows === null) return null;

  async function save() {
    if (!rows) return;
    const duplicate = firstDuplicate(rows);
    if (duplicate) {
      toast.error(`“${duplicate}” is defined twice`, { description: "A name can hold one value per scope." });
      return;
    }
    const update: VarsUpdate = buildVarsUpdate(rows);
    setBusy(true);
    try {
      if (scope === "globals") {
        setRows(rowsFromVarSet(await setGlobals(update)));
      } else if (scope === "collection" && collection) {
        const saved = await setCollectionVariables(collection.id, update);
        setRows(rowsFromVarSet({ vars: saved.vars ?? null, secretVars: saved.secretVars ?? null }));
      } else if (isNew) {
        const created = await createEnvironment(name.trim(), tag, update.vars, update.secrets);
        await store.refresh();
        if (!useApiClientStore.getState().activeEnvironmentId) store.setActiveEnvironment(created.id);
        openVariables("environment", created.id, created.name);
        closeTabByParams(VARIABLES_VIEW, { scope: "environment", id: "new" });
        toast.success(`Created ${created.name}`);
        return;
      } else if (environment) {
        const saved = await updateEnvironment(environment.id, name.trim(), tag, update);
        setName(saved.name);
        setTag(saved.env);
        setRows(rowsFromVarSet(saved));
      }
      await store.refresh();
      toast.success("Variables saved");
    } catch (e) {
      toast.error("Could not save the variables", { description: errorMessage(e) });
    } finally {
      setBusy(false);
    }
  }

  const title =
    scope === "globals" ? "Globals" : scope === "collection" ? `${collection?.name ?? ""} · variables` : name || "Environment";
  const isActive = !!environment && environment.id === useApiClientStore.getState().activeEnvironmentId;

  return (
    <div
      className="flex h-full min-h-0 flex-col"
      onKeyDownCapture={(event) => {
        if ((event.metaKey || event.ctrlKey) && (event.key === "s" || event.key === "S")) {
          event.preventDefault();
          event.stopPropagation();
          void save();
        }
      }}
    >
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-1.5">
        <Variable className="size-(--icon-14) text-text-tertiary" aria-hidden />
        {scope === "environment" ? (
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            aria-label="Environment name"
            className="min-w-0 flex-1 truncate rounded-md bg-transparent px-1 text-body-13-medium text-text-primary outline-none hover:bg-surface-raised focus:bg-surface-well"
          />
        ) : (
          <span className="min-w-0 flex-1 truncate text-body-13-medium text-text-primary">{title}</span>
        )}
        {scope === "environment" ? (
          <label className="flex items-center gap-1.5 text-caption-11 text-text-secondary">
            Tag
            <NativeSelect size="sm" value={tag} onChange={(event) => setTag(event.target.value)} aria-label="Environment tag">
              {envTags.map((t) => (
                <NativeSelectOption key={t.id} value={t.name}>
                  {t.name}
                  {t.requiresConfirmation ? " — asks before every send" : ""}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </label>
        ) : null}
        {environment ? (
          <>
            <Button size="xs" variant="ghost" disabled={isActive} onClick={() => store.setActiveEnvironment(environment.id)}>
              <Check /> {isActive ? "In use" : "Use for sending"}
            </Button>
            <Button
              size="xs"
              variant="ghost"
              onClick={() =>
                void duplicateEnvironment(environment.id)
                  .then(async (copy) => {
                    await store.refresh();
                    openVariables("environment", copy.id, copy.name);
                  })
                  .catch((e) => toast.error("Could not duplicate", { description: errorMessage(e) }))
              }
            >
              <Copy /> Duplicate
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirmDelete(true)}>
              <Trash2 /> Delete
            </Button>
          </>
        ) : null}
        <Button size="xs" disabled={busy || (scope === "environment" && (!name.trim() || !tag))} onClick={() => void save()} title={`Save (${shortcut("S")})`}>
          <Save /> {isNew ? "Create" : "Save"}
        </Button>
      </header>

      <div className="min-h-0 flex-1 overflow-auto p-3">
        <KeyValueTable
          rows={rows}
          onChange={(next) => setRows(next as VarRow[])}
          keyPlaceholder="Variable"
          valuePlaceholder="Value"
          toggleable={false}
          secrets={rows.map((row) => !!row.secret)}
          onToggleSecret={(index) =>
            setRows(rows.map((row, i) => (i === index ? { ...row, secret: !row.secret, existing: false } : row)))
          }
          secretPlaceholder={(index) => (rows[index]?.existing ? "•••••• sealed — type to replace" : "secret value")}
        />
        <div className="flex flex-col gap-1 pt-3 text-caption-11 text-text-tertiary">
          <p>
            Use a variable as <code className="font-mono">{"{{name}}"}</code> in the URL, params, headers or body. When a
            name is set in several places the nearest wins: Globals, then the collection and its folders, then the
            environment.
          </p>
          <p>
            A secret (lock) is sealed with your vault key and never shown again — not here, not in history, not in Copy as
            cURL. For a token reused across requests, an account in Accounts is the better home: rotate it once and every
            request follows.
          </p>
          {scope === "environment" ? (
            <p>The tag decides whether a send asks first. Every send under a tag that asks is confirmed, every time.</p>
          ) : null}
        </div>
      </div>

      <ConfirmDialog
        open={confirmDelete}
        title={`Delete “${name}”?`}
        description="Its variables, secret ones included, are removed on every device this vault syncs to. Requests are not touched."
        onClose={() => setConfirmDelete(false)}
        onConfirm={() => {
          setConfirmDelete(false);
          if (!environment) return;
          void deleteEnvironment(environment.id)
            .then(async () => {
              closeTabByParams(VARIABLES_VIEW, { scope: "environment", id: environment.id });
              await store.refresh();
            })
            .catch((e) => toast.error("Could not delete", { description: errorMessage(e) }));
        }}
      />
    </div>
  );
}

function firstDuplicate(rows: readonly KV[]): string {
  const seen = new Set<string>();
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (seen.has(key)) return key;
    seen.add(key);
  }
  return "";
}
