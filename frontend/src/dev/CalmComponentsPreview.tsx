import { useLayoutEffect, useState } from "react";
import { ChevronDown, Database, Settings2, ShieldCheck } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Slider } from "@/components/ui/slider";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { TagInput } from "@/components/ui/tag-input";
import { NumberStepper } from "@/components/ui/number-stepper";
import { AnimatedNumber, AnimatedNumberGroup } from "@/components/ui/animated-number";
import { ExportModal } from "@/modules/database/components/ExportModal";
import { CellInspector } from "@/modules/database/components/CellInspector";
import { useDatabaseStore } from "@/modules/database/databaseStore";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select";
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";

/** Isolated development surface: theme preview never saves app preferences. */
export default function CalmComponentsPreview() {
  const [mode, setMode] = useState<"light" | "dark">("light");
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("Billing staging");
  const [environment, setEnvironment] = useState("staging");
  const [tags, setTags] = useState(["billing", "staging"]);
  const [previewSize, setPreviewSize] = useState(14);
  const [previewCount, setPreviewCount] = useState(9);
  useLayoutEffect(() => {
    const root = document.documentElement;
    const previous = root.dataset.theme;
    const dark = root.classList.contains("dark");
    root.dataset.theme = mode;
    root.classList.toggle("dark", mode === "dark");
    return () => {
      if (previous) root.dataset.theme = previous; else delete root.dataset.theme;
      root.classList.toggle("dark", dark);
    };
  }, [mode]);
  return <TooltipProvider><main className="min-h-screen bg-bg-app p-8 text-text-primary">
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <header className="flex items-start justify-between gap-4">
        <div><p className="text-caption-12 text-text-tertiary">DevKit · component foundation</p><h1 className="mt-1 text-title-16">Calm Developer UI</h1><p className="mt-2 text-body-13 text-text-secondary">Theme-aware surfaces. Quiet motion. Immediate input.</p></div>
        <Button variant="outline" onClick={() => setMode(mode === "light" ? "dark" : "light")}>Preview {mode === "light" ? "dark" : "light"}</Button>
      </header>
      <Card><CardHeader><CardTitle>Saved connection</CardTitle><CardDescription>Keep connection details clear and grouped.</CardDescription></CardHeader>
        <CardContent className="grid gap-4">
          <Input label="Connection name" value={name} onChange={(event) => setName(event.target.value)} helperText="Only used to identify this connection." />
          <div className="grid gap-2"><label className="text-label-12 text-text-secondary" htmlFor="preview-env">Environment</label><Select value={environment} onValueChange={setEnvironment}><SelectTrigger id="preview-env"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="development">Development</SelectItem><SelectItem value="staging">Staging</SelectItem><SelectItem value="production">Production</SelectItem></SelectContent></Select></div>
          <Textarea aria-label="Connection notes" placeholder="Optional notes…" />
          <Checkbox label="Connect in read-only mode" />
          <div className="flex flex-wrap gap-2"><Button onClick={() => setOpen(true)}><Database />Review connection</Button>
            <Popover><PopoverTrigger asChild><Button variant="outline"><Settings2 />Options</Button></PopoverTrigger><PopoverContent><p className="text-body-13-medium">Connection options</p><p className="text-body-12 text-text-secondary">The popup follows its anchor and uses the active theme.</p><Checkbox label="Remember this connection" defaultChecked /></PopoverContent></Popover>
            <DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost">More<ChevronDown /></Button></DropdownMenuTrigger><DropdownMenuContent><DropdownMenuItem>Duplicate connection</DropdownMenuItem><DropdownMenuItem>Export profile</DropdownMenuItem><DropdownMenuItem variant="destructive">Delete connection</DropdownMenuItem></DropdownMenuContent></DropdownMenu>
          </div>
        </CardContent>
      </Card>
      <Card><CardHeader><CardTitle>Control motion</CardTitle><CardDescription>Press, select and switch repeatedly to feel how motion follows your input.</CardDescription></CardHeader><CardContent className="grid gap-4">
        <form className="flex flex-wrap items-center gap-6" onSubmit={(event) => event.preventDefault()}><Checkbox label="Animated check" name="preview-check" defaultChecked /><Checkbox label="Partially selected" checked="indeterminate" /><Checkbox label="Disabled" disabled /><label className="flex items-center gap-2 text-body-13"><Switch name="preview-live" defaultChecked />Live updates</label><Button type="reset" variant="ghost">Reset controls</Button></form>
        <RadioGroup defaultValue="local" aria-label="Connection route" className="flex gap-4"><label className="flex items-center gap-2 text-body-13"><RadioGroupItem value="local" />Local</label><label className="flex items-center gap-2 text-body-13"><RadioGroupItem value="tunnel" />Tunnel</label></RadioGroup>
        <Tabs defaultValue="details"><TabsList><TabsTrigger value="details">Details</TabsTrigger><TabsTrigger value="options">Options</TabsTrigger><TabsTrigger value="history">History</TabsTrigger></TabsList><TabsContent value="details">Connection details</TabsContent><TabsContent value="options">Connection options</TabsContent><TabsContent value="history">Recent connections</TabsContent></Tabs>
        <div className="grid gap-2"><label className="text-label-12" htmlFor="preview-tags">Labels</label><TagInput inputId="preview-tags" value={tags} onChange={setTags} placeholder="Add a label…" /></div>
        <Slider aria-label="Connection timeout" defaultValue={[30]} max={60} />
      </CardContent></Card>
      <Card><CardHeader><CardTitle>Number motion</CardTitle><CardDescription>Values update immediately. Digits roll inside a stable control.</CardDescription></CardHeader><CardContent className="flex flex-wrap items-end gap-6">
        <div className="grid gap-2"><span className="text-label-12 text-text-secondary">Size</span><NumberStepper value={previewSize} onChange={setPreviewSize} min={10} max={24} label="Preview font size" /></div>
        <div className="grid gap-2"><span className="text-label-12 text-text-secondary">Count</span><NumberStepper value={previewCount} onChange={setPreviewCount} min={0} max={120} label="Preview count" /></div>
        <AnimatedNumberGroup><p className="pb-2 text-body-13 text-text-secondary"><AnimatedNumber value={previewCount} suffix=" items" /> · <AnimatedNumber value={previewSize} suffix=" px" /></p></AnimatedNumberGroup>
      </CardContent></Card>
      <div className="flex flex-wrap gap-2"><Button variant="outline" onClick={() => useDatabaseStore.getState().openExportModal({ columns: ["id", "name"], rows: [["1", "Sample"], ["2", "Preview"]], rowCount: 2, truncated: false }, "preview-results")}>Preview export form</Button><Button variant="outline" onClick={() => useDatabaseStore.getState().setInspectedCell({ columnName: "payload", value: JSON.stringify({ items: Array.from({ length: 60 }, (_, id) => ({ id, name: `Sample item ${id}` })) }) })}>Preview cell inspector</Button></div>
      <p className="text-caption-12 text-text-tertiary">Local preview only. No connection is opened or permission granted.</p>
    </div>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle className="flex items-center gap-2"><ShieldCheck className="size-4 text-text-secondary" />Review connection</DialogTitle><DialogDescription>Check the target before continuing.</DialogDescription></DialogHeader>
      <div className="space-y-3 text-body-13"><div className="flex justify-between gap-3"><span className="text-text-tertiary">Profile</span><span>{name}</span></div><div className="flex justify-between gap-3"><span className="text-text-tertiary">Environment</span><span>{environment}</span></div><code className="block rounded-md bg-surface-well p-3 text-code-12">staging.internal:5432</code></div>
      <DialogFooter><Button variant="outline" onClick={() => setOpen(false)}>Cancel</Button><Button onClick={() => setOpen(false)}>Done</Button></DialogFooter>
    </DialogContent></Dialog>
    <ExportModal /><CellInspector />
  </main></TooltipProvider>;
}
