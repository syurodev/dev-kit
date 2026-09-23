import { useEffect, useRef, useState } from "react";
import { Ban, Pipette } from "lucide-react";
import { cn } from "@/lib/utils";
import { FOLDER_COLORS, folderDotVar, isHexColor } from "./folderColors";

/**
 * The preset swatches, then one custom swatch that opens the system colour
 * picker for any other colour. Presets are shortcuts, not a limit: the value
 * is a preset name or a "#rrggbb" (what <input type="color"> produces).
 */
export function ColorPicker({
  value,
  onChange,
  allowInherit,
}: {
  value: string;
  onChange: (color: string) => void;
  allowInherit?: boolean;
}) {
  const custom = isHexColor(value);
  // What the custom swatch shows while the system picker is open. The note
  // editor saves on every onChange, and the picker's input event fires on
  // every drag step, so only its change event — the picker closing on a
  // colour — is handed up; the drag itself is previewed here.
  const [draft, setDraft] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    const input = inputRef.current;
    if (!input) return;
    const commit = () => {
      setDraft(null);
      onChangeRef.current(input.value.toLowerCase());
    };
    input.addEventListener("change", commit);
    return () => input.removeEventListener("change", commit);
  }, []);

  const customShown = draft ?? (custom ? value : null);

  // Uncontrolled, so the picker opens on the stored colour when there is one
  // and on the browser's own default otherwise — no invented starting colour.
  useEffect(() => {
    if (custom && inputRef.current) inputRef.current.value = value;
  }, [custom, value]);

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
      <label
        title={custom ? value : "Custom colour"}
        className={cn(
          "relative flex size-6 cursor-pointer items-center justify-center rounded-full border border-border ring-offset-background transition-shadow focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2",
          custom && "ring-2 ring-ring ring-offset-2",
          !customShown && "bg-muted text-muted-foreground",
        )}
        style={customShown ? { background: customShown } : undefined}
      >
        {customShown ? null : <Pipette className="size-3.5" />}
        <input
          ref={inputRef}
          type="color"
          aria-label="Custom colour"
          className="absolute inset-0 size-full cursor-pointer opacity-0"
          onChange={(e) => setDraft(e.target.value.toLowerCase())}
        />
      </label>
    </div>
  );
}
