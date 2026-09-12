import { cloneElement, isValidElement, type ReactElement } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "./tooltip";

/**
 * The app's tooltip on an action button, with the button's name.
 *
 * Two jobs, and the second is the reason this is not four lines written out at
 * each call site. A `title` attribute is both the tooltip AND the accessible
 * name of an icon-only button; a Radix tooltip is only the tooltip — it sets
 * `aria-describedby`, which describes a control that already has a name.
 * Swapping one for the other without this leaves a row of buttons that a
 * screen reader can only call "button".
 *
 * So the label is applied twice: as the tooltip, and as `aria-label` on the
 * child — unless the child already names itself, which a button with visible
 * text does.
 *
 * The provider is mounted once, in App.tsx, so a call site needs only this.
 */
export function ActionTooltip({
  label,
  side = "bottom",
  children,
}: {
  label: string;
  side?: "top" | "right" | "bottom" | "left";
  children: ReactElement<{ "aria-label"?: string; children?: unknown }>;
}) {
  const named =
    isValidElement(children) &&
    children.props["aria-label"] === undefined &&
    // A button with visible text is already named by it; adding aria-label
    // would override what the user can see with what they cannot.
    typeof children.props.children !== "string"
      ? cloneElement(children, { "aria-label": label })
      : children;

  return (
    <Tooltip>
      <TooltipTrigger asChild>{named}</TooltipTrigger>
      <TooltipContent side={side}>{label}</TooltipContent>
    </Tooltip>
  );
}
