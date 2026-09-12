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
  ...rest
}: {
  label: string;
  side?: "top" | "right" | "bottom" | "left";
  children: ReactElement<Record<string, unknown>>;
} & Record<string, unknown>) {
  // Anything handed to THIS component goes on to the child.
  //
  // Because it is put inside other `asChild` triggers: a DropdownMenuTrigger
  // with asChild hands its onClick and its ref to its immediate child, and
  // when that child is this wrapper, a wrapper that swallows them leaves a
  // button that looks right and opens nothing — which is exactly what the
  // connection row's menu did until this line existed. Forwarding makes the
  // wrapper transparent, so it composes in either order.
  const named = isValidElement(children)
    ? cloneElement(children, {
        ...rest,
        // A button with visible text is already named by it; adding aria-label
        // would override what the user can see with what they cannot.
        ...(children.props["aria-label"] === undefined &&
        typeof children.props.children !== "string"
          ? { "aria-label": label }
          : {}),
      })
    : children;

  return (
    <Tooltip>
      <TooltipTrigger asChild>{named}</TooltipTrigger>
      <TooltipContent side={side}>{label}</TooltipContent>
    </Tooltip>
  );
}
