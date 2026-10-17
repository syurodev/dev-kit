import { useRef } from "react";
import type { Command, EditorView } from "@codemirror/view";
import { copyLineDown, indentLess, indentMore, moveLineDown, moveLineUp, redo, toggleComment, undo } from "@codemirror/commands";
import { openSearchPanel } from "@codemirror/search";
import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuCheckboxItem, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { INDENT_SIZES, indentSize, type IndentStyle } from "./editing";
import { useEditorPreferences } from "./preferences";

const actions: { label: string; run: Command; shortcut?: string; edit?: boolean }[] = [
  { label: "Indent lines", run: indentMore, shortcut: "Tab", edit: true },
  { label: "Outdent lines", run: indentLess, shortcut: "Shift+Tab", edit: true },
  { label: "Toggle comment", run: toggleComment, edit: true },
  { label: "Move line up", run: moveLineUp, edit: true },
  { label: "Move line down", run: moveLineDown, edit: true },
  { label: "Duplicate line", run: copyLineDown, edit: true },
  { label: "Undo", run: undo, edit: true },
  { label: "Redo", run: redo, edit: true },
  { label: "Find / Replace", run: openSearchPanel },
];

/** Operates on the live view; changing preferences never rewrites the source. */
export function EditingControls({ getView, indentStyle, readOnly = false }: {
  getView: () => EditorView | null;
  indentStyle?: IndentStyle;
  readOnly?: boolean;
}) {
  const showIndentGuides = useEditorPreferences((s) => s.showIndentGuides);
  const size = useEditorPreferences((s) => s.size);
  const preferredStyle = useEditorPreferences((s) => s.style);
  const style = indentStyle ?? preferredStyle;
  const pending = useRef<Command | null>(null);
  return <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <Button variant="ghost" size="xs" aria-label="Editor indentation and actions">{style === "spaces" ? "Spaces" : "Tab size"}: {size}<ChevronDown /></Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" onCloseAutoFocus={(event) => {
      const command = pending.current; pending.current = null;
      const view = getView();
      if (command && view) { event.preventDefault(); view.focus(); command(view); }
    }}>
      <DropdownMenuLabel>{style === "spaces" ? "Spaces per indent" : "Tab display width"}</DropdownMenuLabel>
      <DropdownMenuRadioGroup value={String(size)} onValueChange={(value) => useEditorPreferences.getState().setSize(indentSize(Number(value)))}>
        {INDENT_SIZES.map((value) => <DropdownMenuRadioItem key={value} value={String(value)}>{value} spaces</DropdownMenuRadioItem>)}
      </DropdownMenuRadioGroup>
      {indentStyle ? <DropdownMenuLabel>{indentStyle === "spaces" ? "This file uses spaces" : "Makefile recipes use tabs"}</DropdownMenuLabel> : <>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup value={style} onValueChange={(value) => useEditorPreferences.getState().setStyle(value as IndentStyle)}>
          <DropdownMenuRadioItem value="spaces">Insert spaces</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="tabs">Insert tabs</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </>}
      <DropdownMenuSeparator />
      <DropdownMenuCheckboxItem checked={showIndentGuides} onCheckedChange={(checked) => useEditorPreferences.getState().setShowIndentGuides(checked)}>Indent guides</DropdownMenuCheckboxItem>
      <DropdownMenuSeparator />
      {actions.map((action) => <DropdownMenuItem key={action.label} shortcut={action.shortcut} disabled={readOnly && action.edit} onSelect={() => { pending.current = action.run; }}>{action.label}</DropdownMenuItem>)}
      <DropdownMenuSeparator />
      <DropdownMenuLabel>Escape, then Tab leaves the editor</DropdownMenuLabel>
    </DropdownMenuContent>
  </DropdownMenu>;
}
