import { foldAll, unfoldAll } from "@codemirror/language";
import type { EditorView } from "@codemirror/view";
import { FoldVertical, UnfoldVertical, WrapText } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";

/** Source-view controls; folding and wrapping never change the document. */
export function StructureControls({ wrapped, onWrapChange, getView }: {
  wrapped: boolean;
  onWrapChange: (wrapped: boolean) => void;
  getView: () => EditorView | null;
}) {
  return <div className="flex shrink-0 items-center gap-0.5" role="group" aria-label="YAML view">
    <ActionTooltip label="Wrap long lines (⌥Z)">
      <Button variant={wrapped ? "secondary" : "ghost"} size="icon-sm" aria-pressed={wrapped} onClick={() => onWrapChange(!wrapped)}><WrapText /></Button>
    </ActionTooltip>
    <ActionTooltip label="Collapse all sections">
      <Button variant="ghost" size="icon-sm" onClick={() => { const view = getView(); if (view) foldAll(view); }}><FoldVertical /></Button>
    </ActionTooltip>
    <ActionTooltip label="Expand all sections">
      <Button variant="ghost" size="icon-sm" onClick={() => { const view = getView(); if (view) unfoldAll(view); }}><UnfoldVertical /></Button>
    </ActionTooltip>
  </div>;
}
