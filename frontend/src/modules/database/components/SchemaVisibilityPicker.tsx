import { useState } from "react";
import { ListFilter } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { useDatabaseStore } from "../databaseStore";
import { toggleVisibleSchema } from "../schemaVisibility";

/** Filters the explorer, keeping query context and open documents independent. */
export function SchemaVisibilityPicker() {
  const [search, setSearch] = useState("");
  const { profile, namespace, selections, save, allProfiles, serverDatabases, visibleDatabases, saveDatabases } = useDatabaseStore(useShallow((s) => ({ profile: s.profiles.find((p) => p.id === s.selectedProfileId), namespace: s.schemaSnapshot?.namespace, selections: s.visibleSchemas, save: s.setVisibleSchemas, allProfiles:s.profiles,serverDatabases:s.serverDatabases,visibleDatabases:s.visibleDatabases,saveDatabases:s.setVisibleDatabases })));
  if (!profile) return null;
  const postgres=!profile.engine || profile.engine==="postgres";
  const root=allProfiles.find(p=>p.id===(profile.parentProfileId || profile.id)) ?? profile;
  const dbNames=serverDatabases[root.id] ?? [];
  const dbSelection=visibleDatabases[root.id];
  const names = Object.keys(namespace ?? {});
  const selected = selections[profile.id];
  const chosen = names.filter((name) => selected == null || selected.includes(name));
  const databases = profile.engine === "mysql" || profile.engine === "mariadb" || profile.engine === "sqlite";
  const noun = databases ? "databases" : "schemas";
  const defaultName = profile.engine === "sqlite" ? "main" : databases ? profile.database : "public";
  const filtered = names.filter((name) => name.toLowerCase().includes(search.trim().toLowerCase()));
  return <Popover onOpenChange={(open) => { if (!open) setSearch(""); }}><ActionTooltip label={`Choose visible ${noun}`}><PopoverTrigger asChild><Button variant="ghost" size="icon-xs" disabled={!names.length && !dbNames.length} aria-label={`Choose visible ${noun}`}><ListFilter className="size-3.5" /></Button></PopoverTrigger></ActionTooltip>
    <PopoverContent align="start" className="w-72 max-h-(--radix-popover-content-available-height) space-y-3 overflow-y-auto">
      <div><h3 className="text-body-13 font-medium">Visible {noun}</h3><p className="mt-1 text-body-12 text-text-tertiary">{profile.name} · {chosen.length} of {names.length}</p></div>
      {postgres && dbNames.length>0 && <div className="space-y-2 border-b border-border-subtle pb-3"><h3 className="text-body-13 font-medium">Databases on {root.name}</h3><div className="flex gap-2"><Button size="sm" variant="ghost" onClick={()=>saveDatabases(root.id,null)}>Show all databases</Button><Button size="sm" variant="ghost" onClick={()=>saveDatabases(root.id,[])}>Hide all databases</Button></div><div className="max-h-40 space-y-1 overflow-y-auto">{dbNames.map(name=><label key={name} className="flex cursor-pointer items-center gap-2.5 px-2 py-1.5"><Checkbox checked={dbSelection==null||dbSelection.includes(name)} onCheckedChange={checked=>saveDatabases(root.id,toggleVisibleSchema(dbNames,dbSelection,name,checked===true))}/><span className="truncate text-body-13">{name}</span></label>)}</div><p className="text-body-12 text-text-tertiary">Schemas below belong to {profile.database || "postgres"}.</p></div>}
      <Input inputSize="sm" aria-label={`Search ${noun}`} placeholder={`Search ${noun}…`} value={search} onChange={(e) => setSearch(e.target.value)} />
      <div className="flex flex-wrap gap-2"><Button size="sm" variant="ghost" onClick={() => save(profile.id, null)}>Show all</Button><Button size="sm" variant="ghost" onClick={() => save(profile.id, [])}>Hide all</Button>{names.includes(defaultName) && <Button size="sm" variant="ghost" onClick={() => save(profile.id, [defaultName])}>Default only</Button>}</div>
      <div className="max-h-64 space-y-1 overflow-y-auto">
        {filtered.map((name) => <label key={name} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-2 hover:bg-state-soft"><Checkbox checked={chosen.includes(name)} onCheckedChange={(checked) => save(profile.id, toggleVisibleSchema(names, selected, name, checked === true))} /><span className="min-w-0 flex-1 truncate text-body-13" title={name}>{name}</span>{name === defaultName && <span className="text-caption-11 text-text-tertiary">Default</span>}</label>)}
        {!filtered.length && <p className="p-2 text-body-12 text-text-tertiary">No {noun} match.</p>}
      </div>
      <p className="border-t border-border-subtle pt-3 text-body-12 text-text-tertiary">Changes what appears in the explorer. Open tabs and query context stay unchanged.</p>
    </PopoverContent>
  </Popover>;
}
