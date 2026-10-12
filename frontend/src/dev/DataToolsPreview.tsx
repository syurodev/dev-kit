import { useEffect, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider } from "@/theme/ThemeProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { ConnectionDialog } from "@/modules/ssh/ConnectionDialog";
import { TerminalTabs } from "@/workbench/TerminalWorkspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { ResultGrid } from "@/modules/database/grid/ResultGrid";
import { DatabaseSidebar } from "@/modules/database/components/DatabaseSidebar";
import { useDatabaseStore } from "@/modules/database/databaseStore";

/** Local UI fixtures; no terminal, SSH connection or database query is started. */
export default function DataToolsPreview() {
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }));
  return <QueryClientProvider client={client}><ThemeProvider><TooltipProvider><Preview /></TooltipProvider></ThemeProvider></QueryClientProvider>;
}
function Preview() {
  const [ssh, setSSH] = useState(false);
  const [copied, setCopied] = useState("");
  const [widths, setWidths] = useState<number[]>([]);
  const namespace = { public: { users: ["id", "name"] }, billing: { invoices: ["id"] } };
  useEffect(() => {
    const originalDatabase = useDatabaseStore.getState();
    const keys = ["profiles", "selectedProfileId", "serverDatabases", "databaseErrors", "schemaSnapshot", "expandedProfiles", "visibleSchemas", "visibleDatabases", "setSelectedProfileId", "discoverDatabases", "openServerDatabase"] as const;
    const restoreDatabase = Object.fromEntries(keys.map(key => [key, originalDatabase[key]]));
    const originalTerminal = useTerminalTabsStore.getState();
    useTerminalTabsStore.setState({ tabs: [{ key: "demo", title: "Production #1 · deploy@very-long-hostname.example.test" }, { key: "demo-2", title: "Logs" }], activeKey: "demo" });
    const root={ id:"pg-preview",name:"PostgreSQL server",host:"example.test",port:5432,database:"postgres",username:"reader",sslMode:"required",env:"dev",readOnly:true,engine:"postgres" };
    const a={...root,id:"pg-app",parentProfileId:root.id,name:"PostgreSQL server / app",database:"app"};
    const b={...root,id:"pg-analytics",parentProfileId:root.id,name:"PostgreSQL server / analytics",database:"analytics"};
    const choose=(id:string)=>useDatabaseStore.setState({selectedProfileId:id,schemaSnapshot:{namespace:id===b.id?{public:{events:["id","time"]}}:namespace,indexedAt:"",columns:{}}});
    const catalogError = new URLSearchParams(window.location.search).has("catalog-error");
    useDatabaseStore.setState({databaseErrors:catalogError?{[root.id]:"Database discovery unavailable"}:{},profiles:[root,a,b],selectedProfileId:a.id,serverDatabases:{[root.id]:catalogError?[]:["postgres","app","analytics"]},schemaSnapshot:{namespace,indexedAt:"",columns:{}},expandedProfiles:{[root.id]:true},visibleSchemas:{},visibleDatabases:{},setSelectedProfileId:choose,discoverDatabases:async()=>{},openServerDatabase:async(_id,name)=>{choose(name==="analytics"?b.id:name==="app"?a.id:root.id);} });
    return () => {
      useDatabaseStore.setState(restoreDatabase);
      useTerminalTabsStore.setState({tabs: originalTerminal.tabs, activeKey: originalTerminal.activeKey});
    };
  }, []);
  return <main className="mx-auto flex max-w-4xl flex-col gap-6 p-6 text-text-primary">
    <h1 className="text-heading-24">Database and terminal controls</h1>
    <section className="rounded-xl border border-border-subtle bg-surface-raised p-4"><h2 className="mb-3 text-body-14 font-medium">Bottom slot tabs</h2><div className="flex max-w-120 items-center gap-2 overflow-hidden rounded-lg border border-border-subtle bg-surface-base p-2"><TerminalTabs showModule={false} /></div></section>
    <section className="h-96 w-80 rounded-xl border border-border-subtle bg-surface-raised p-2"><DatabaseSidebar loadData={false}/></section>
    <section className="rounded-xl border border-border-subtle bg-surface-raised p-4"><h2 className="mb-2 text-body-14 font-medium">Select a row number, then copy</h2><div className="h-64"><ResultGrid result={{ columns: ["id", "name", "email"], rows: [["1", "Ada", "ada@example.test"], ["2", "Grace", "grace@example.test"], ["3", "Linus", null]], rowCount: 3, truncated: false }} widths={widths} columnOrder={[]} onResize={setWidths} onInspectCell={() => {}} onCopySelection={async (value) => setCopied(value)} /></div><pre aria-label="Copied data" className="mt-3 whitespace-pre-wrap text-code-12">{copied || "Nothing copied yet."}</pre></section>
    <Button onClick={() => setSSH(true)}>Preview SSH form</Button>
    {ssh && <ConnectionDialog envs={["dev", "staging", "prod"]} onClose={() => setSSH(false)} onSaved={async () => {}} />}
  </main>;
}
