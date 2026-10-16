import { FileIcon } from "@/components/file-icon/FileIcon";
import { Badge } from "@/components/ui/badge";
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
      <Badge
        variant="secondary"
        contentEditable={false}
        data-context-mention={path}
        title={path}
        className="max-w-full min-w-0 h-6 items-center gap-1.5 rounded-md py-0 align-middle"
      >
        <FileIcon path={path} className="size-3" />
        <span className="min-w-0 truncate">{label}</span>
      </Badge>
      {props.children}
    </PlateElement>
  );
}
