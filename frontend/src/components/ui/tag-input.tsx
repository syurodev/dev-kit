import * as React from "react";
import { cn } from "@/lib/utils";
import { Tag } from "@/components/ui/tag";

/**
 * Tag Input (Figma 378:2698)
 * Variants: State Default / Focus
 * Standard dimensions: 320×32, min-h-(--control-height-md), r=8 (rounded-md)
 * Theme-aware recessed surface; focus adds a ring without changing geometry.
 */
export interface TagInputProps
  extends Omit<React.ComponentProps<"div">, "onChange"> {
  value: string[];
  onChange: (tags: string[]) => void;
  placeholder?: string;
  disabled?: boolean;
  maxTags?: number;
  delimiter?: string | RegExp;
  tagDotColor?: (tag: string) => string | undefined;
  /** Connect an external label to the editable input, rather than its wrapper. */
  inputId?: string;
}

const TagInput = React.forwardRef<HTMLDivElement, TagInputProps>(
  (
    {
      className,
      value = [],
      onChange,
      placeholder = "Add tag...",
      disabled = false,
      maxTags,
      delimiter = /[,]/,
      tagDotColor,
      inputId,
      "aria-label": ariaLabel,
      "aria-describedby": describedBy,
      ...props
    },
    ref,
  ) => {
    const [draft, setDraft] = React.useState("");
    const inputRef = React.useRef<HTMLInputElement>(null);

    function addTag(raw: string) {
      const trimmed = raw.trim();
      if (!trimmed) return;
      if (maxTags && value.length >= maxTags) return;

      const exists = value.some(
        (t) => t.toLowerCase() === trimmed.toLowerCase(),
      );
      if (!exists) {
        onChange([...value, trimmed]);
      }
      setDraft("");
    }

    function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
      if (e.key === "Enter" || e.key === ",") {
        e.preventDefault();
        addTag(draft);
      } else if (e.key === "Backspace" && !draft && value.length > 0) {
        e.preventDefault();
        onChange(value.slice(0, -1));
      }
    }

    function handlePaste(e: React.ClipboardEvent<HTMLInputElement>) {
      e.preventDefault();
      const pasted = e.clipboardData.getData("text");
      const tokens = pasted
        .split(delimiter)
        .map((s) => s.trim())
        .filter(Boolean);

      if (tokens.length === 0) return;

      const next = [...value];
      const seen = new Set(value.map((t) => t.toLowerCase()));
      for (const token of tokens) {
        if (maxTags && next.length >= maxTags) break;
        const lower = token.toLowerCase();
        if (!seen.has(lower)) {
          seen.add(lower);
          next.push(token);
        }
      }
      onChange(next);
      setDraft("");
    }

    function handleRemove(index: number) {
      onChange(value.filter((_, i) => i !== index));
    }

    return (
      <div
        ref={ref}
        data-slot="tag-input"
        data-disabled={disabled}
        onClick={() => inputRef.current?.focus()}
        className={cn(
          "flex min-h-(--control-height-md) w-full max-w-80 flex-wrap items-center gap-1.5 rounded-md border border-border-subtle bg-surface-well px-2 py-1 transition-control",
          "focus-within:border-status-info focus-within:ring-2 focus-within:ring-border-focus/50",
          disabled && "pointer-events-none opacity-38",
          className,
        )}
        {...props}
      >
        {value.map((tag, index) => (
          <Tag
            key={`${tag}-${index}`}
            dotColor={tagDotColor?.(tag)}
            onRemove={disabled ? undefined : () => handleRemove(index)}
          >
            {tag}
          </Tag>
        ))}
        {(!maxTags || value.length < maxTags) && (
          <input
            id={inputId}
            aria-label={ariaLabel}
            aria-describedby={describedBy}
            ref={inputRef}
            type="text"
            disabled={disabled}
            value={draft}
            placeholder={value.length === 0 ? placeholder : ""}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onBlur={() => {
              if (draft) addTag(draft);
            }}
            className="flex-1 min-w-15 bg-transparent text-body-12 text-text-primary placeholder:text-text-tertiary outline-none border-none p-0 focus:outline-none focus:ring-0"
          />
        )}
      </div>
    );
  },
);
TagInput.displayName = "TagInput";

export { TagInput };
