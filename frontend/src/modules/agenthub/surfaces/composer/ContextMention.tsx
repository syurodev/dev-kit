import { FileCode } from "lucide-react";
import { PlateElement, type PlateElementProps } from "platejs/react";

/**
 * The inline chip a selected file becomes. Void and inline, like
 * NoteLinkElement's note reference: Slate treats it as one atomic unit that
 * backspace removes whole, not character by character.
 */
export function ContextMentionElement(props: PlateElementProps) {
  const element = props.element as { key?: unknown; value?: unknown };
  const path = String(element.key ?? "");
  const label = String(element.value ?? path);

  return (
    <PlateElement {...props} as="span">
      <span
        contentEditable={false}
        data-context-mention={path}
        title={path}
        className="inline-flex items-center gap-1 rounded-sm border border-status-info bg-status-info/8 px-1.75 py-0.5 align-baseline text-label-12 text-status-info"
      >
        <FileCode className="size-2.75" />
        {label}
      </span>
      {props.children}
    </PlateElement>
  );
}
