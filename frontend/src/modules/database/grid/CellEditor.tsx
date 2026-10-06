import { useRef, useState } from "react";
import {
  Popover,
  PopoverAnchor,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { TypedValueInput } from "./TypedValueInput";
import type { CellValue } from "../sql/updateCell";

/** The draft belongs to this editor, so typing never re-renders the table. */
export function CellEditor({
  initialValue,
  column,
  type,
  nullable,
  cells,
  onCommit,
  onCancel,
}: {
  initialValue: CellValue;
  column: string;
  type?: string;
  nullable: boolean;
  cells: number;
  onCommit: (value: CellValue) => string;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initialValue);
  const [error, setError] = useState("");
  const visible = useModuleVisibility();
  const grid = useRef<HTMLElement | null>(null);
  return (
    <Popover
      open={visible}
      onOpenChange={(open) => {
        if (!open && visible) onCancel();
      }}
    >
      <PopoverAnchor asChild>
        <span
          ref={(node) => {
            if (node) grid.current = node.closest<HTMLElement>('[role="grid"]');
          }}
          className="block truncate"
        >
          {initialValue ?? "null"}
        </span>
      </PopoverAnchor>
      <PopoverContent
        align="start"
        className="max-h-[80dvh] overflow-y-auto"
        onKeyDown={(e) => e.stopPropagation()}
        onCloseAutoFocus={(e) => {
          e.preventDefault();
          if (visible && document.activeElement === document.body)
            grid.current?.focus();
        }}
      >
        <PopoverHeader>
          <PopoverTitle>Edit {column}</PopoverTitle>
        </PopoverHeader>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            setError(onCommit(value));
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
              e.preventDefault();
              e.currentTarget.requestSubmit();
            }
          }}
        >
          <TypedValueInput
            value={value}
            type={type}
            nullable={nullable}
            error={error}
            autoFocus
            onChange={(next) => {
              setValue(next);
              setError("");
            }}
          />
          {cells > 1 ? (
            <p className="text-body-12 text-text-secondary">
              {cells} selected cells. Multiple cells will be staged for review.
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" size="sm">
              {cells > 1 ? "Stage cells" : "Save cell"}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}
