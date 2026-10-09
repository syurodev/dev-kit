import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Check, Copy, MoreHorizontal, Save, Trash2, Variable } from "lucide-react";
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
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { ListSkeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/ui/empty-state";
import { useApiClientStore } from "./apiclientStore";
import { ConfirmDialog } from "./Dialogs";
import { closeTabByParams, openVariables, renameApiDocument, VARIABLES_VIEW } from "./documents";
import { VariablesForm } from "./VariablesForm";
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
  const visible = useModuleVisibility();
  const activeEnvironmentId = useApiClientStore((s) => s.activeEnvironmentId);
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
  const [saveError, setSaveError] = useState("");
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
    setSaveError("");
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
  if (rows === null) return <ListSkeleton rows={5} />;

  async function save() {
    if (!rows || busy || (scope === "environment" && (!name.trim() || !tag))) return;
    const duplicate = firstDuplicate(rows);
    if (duplicate) {
      setSaveError(`“${duplicate}” is defined twice. Use a unique name for each variable.`);
      return;
    }
    const update: VarsUpdate = buildVarsUpdate(rows);
    setBusy(true);
    setSaveError("");
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
        renameApiDocument(VARIABLES_VIEW, { scope: "environment", id: saved.id }, saved.name);
        setName(saved.name);
        setTag(saved.env);
        setRows(rowsFromVarSet(saved));
      }
      await store.refresh();
      toast.success("Variables saved");
    } catch (e) {
      setSaveError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  const title =
    scope === "globals" ? "Globals" : scope === "collection" ? `${collection?.name ?? ""} · variables` : name || "Environment";
  const isActive = !!environment && environment.id === activeEnvironmentId;

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
      <header className="flex flex-wrap items-center gap-2 border-b border-border-subtle px-4 py-3">
        <Variable className="size-4 text-text-tertiary" aria-hidden />
        <span className="min-w-28 flex-1 truncate text-title-14 text-text-primary">{isNew ? "New environment" : title}</span>
        {environment ? <>
          <Button size="sm" variant="outline" disabled={isActive} onClick={() => store.setActiveEnvironment(environment.id)}><Check />{isActive ? "Active environment" : "Use for sending"}</Button>
          <DropdownMenu><DropdownMenuTrigger asChild><Button size="icon-sm" variant="ghost" aria-label="Environment actions"><MoreHorizontal /></Button></DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => void duplicateEnvironment(environment.id).then(async (copy) => { await store.refresh(); openVariables("environment", copy.id, copy.name); }).catch((e) => toast.error("Could not duplicate", { description: errorMessage(e) }))}><Copy />Duplicate</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={() => setConfirmDelete(true)}><Trash2 />Delete…</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </> : null}
        <Button size="sm" disabled={busy || (scope === "environment" && (!name.trim() || !tag))} onClick={() => void save()} title={`Save (${shortcut("S")})`}><Save />{busy ? "Saving…" : isNew ? "Create environment" : "Save changes"}</Button>
      </header>

      <div className="min-h-0 flex-1 overflow-auto overscroll-contain p-4">
        {saveError ? <p role="alert" className="mx-auto mb-4 max-w-4xl rounded-lg bg-status-danger/10 p-3 text-caption-11 text-status-danger">{saveError}</p> : null}
        <fieldset disabled={busy} className="min-w-0"><VariablesForm rows={rows} onRowsChange={setRows} environmentDetails={scope === "environment"} name={name} onNameChange={setName} tag={tag} onTagChange={setTag} envTags={envTags} /></fieldset>
      </div>

      <ConfirmDialog
        open={confirmDelete} visible={visible}
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
