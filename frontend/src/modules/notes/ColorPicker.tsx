import { Ban } from "lucide-react";
import { cn } from "@/lib/utils";
import { FOLDER_COLORS, folderDotVar, type FolderColor } from "./folderColors";

export function ColorPicker({
  value,
  onChange,
  allowInherit,
}: {
  value: string;
  onChange: (color: FolderColor | "") => void;
  allowInherit?: boolean;
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {allowInherit ? (
        <button
          type="button"
          aria-label="Inherit from folder"
          aria-pressed={value === ""}
          className={cn(
            "flex size-6 items-center justify-center rounded-full border border-border bg-muted text-muted-foreground ring-offset-background transition-shadow",
            value === "" && "ring-2 ring-ring ring-offset-2",
          )}
          onClick={() => onChange("")}
        >
          <Ban className="size-3.5" />
        </button>
      ) : null}
      {FOLDER_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          aria-label={color}
          aria-pressed={value === color}
          className={cn(
            "size-6 rounded-full ring-offset-background transition-shadow",
            value === color && "ring-2 ring-ring ring-offset-2",
          )}
          style={{ background: folderDotVar(color) }}
          onClick={() => onChange(color)}
        />
      ))}
    </div>
  );
}
