import type { LucideIcon } from "lucide-react";
import { Info, Lightbulb, MessageSquareWarning, OctagonAlert, TriangleAlert } from "lucide-react";
import { PlateElement, type PlateElementProps } from "platejs/react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

/** GitHub's five alert types — what `> [!NOTE]` means, so a note reads the same on GitHub. */
export const CALLOUT_VARIANTS = ["note", "tip", "important", "warning", "caution"] as const;
export type CalloutVariant = (typeof CALLOUT_VARIANTS)[number];

type CalloutLook = { label: string; icon: LucideIcon; frame: string; icon_tone: string };

/**
 * How each type reads. The word and the icon carry the kind; the colour only
 * says how much it matters, from the chrome's status tokens (Important stays
 * neutral — it is emphasis, not a state).
 */
const CALLOUTS: Record<CalloutVariant, CalloutLook> = {
  note: { label: "Note", icon: Info, frame: "border-status-info/30 bg-status-info/10", icon_tone: "text-status-info" },
  tip: {
    label: "Tip",
    icon: Lightbulb,
    frame: "border-status-success/30 bg-status-success/10",
    icon_tone: "text-status-success",
  },
  important: {
    label: "Important",
    icon: MessageSquareWarning,
    frame: "border-border bg-state-soft",
    icon_tone: "text-text-primary",
  },
  warning: {
    label: "Warning",
    icon: TriangleAlert,
    frame: "border-status-warning/30 bg-status-warning/10",
    icon_tone: "text-status-warning",
  },
  caution: {
    label: "Caution",
    icon: OctagonAlert,
    frame: "border-status-danger/30 bg-status-danger/10",
    icon_tone: "text-status-danger",
  },
};

export function calloutVariant(value: unknown): CalloutVariant {
  return CALLOUT_VARIANTS.find((variant) => variant === value) ?? "note";
}

/**
 * A callout: a box of blocks with a type, drawn from the icon. Its Markdown is
 * a GitHub alert (markdownRules.ts), so what is not understood elsewhere
 * degrades to a plain quote whose first line names the type.
 */
export function CalloutElement(props: PlateElementProps) {
  const { editor, element } = props;
  const variant = calloutVariant((element as { variant?: unknown }).variant);
  const { label, icon: Icon, frame, icon_tone } = CALLOUTS[variant];

  return (
    <PlateElement {...props} className={cn("my-2 flex gap-3 rounded-control border px-3 py-2", frame)}>
      <div contentEditable={false} className="mt-1.5 shrink-0 select-none">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={`${label} callout — change type`}
              title={label}
              className={cn("rounded-sm outline-none focus-visible:ring-1 focus-visible:ring-ring", icon_tone)}
            >
              <Icon className="size-4" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuRadioGroup
              value={variant}
              onValueChange={(next) => editor.tf.setNodes({ variant: next }, { at: editor.api.findPath(element) })}
            >
              {CALLOUT_VARIANTS.map((option) => (
                <DropdownMenuRadioItem key={option} value={option}>
                  {CALLOUTS[option].label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div className="min-w-0 flex-1">{props.children}</div>
    </PlateElement>
  );
}
