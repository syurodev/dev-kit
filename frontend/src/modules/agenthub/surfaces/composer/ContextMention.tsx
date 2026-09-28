import * as React from "react";
import { FileCode, FolderIcon } from "lucide-react";
import { getMentionOnSelectItem } from "@platejs/mention";
import { PlateElement, type PlateElementProps } from "platejs/react";
import type { TComboboxInputElement } from "platejs";

import {
  InlineCombobox,
  InlineComboboxContent,
  InlineComboboxEmpty,
  InlineComboboxGroup,
  InlineComboboxInput,
  InlineComboboxItem,
} from "@/components/ui/inline-combobox";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import type { ContextFileItem } from "../AgentMenu";

/**
 * The files "@" can mention, handed down from AgentComposer rather than
 * fetched here: the composer already has them (`contextFiles`, one bounded
 * listing per session — internal/agenthub/service.go's ListContextFiles),
 * and a second fetch inside the popup would race it. `onSelect` mirrors the
 * "Add context" button's own callback, so picking a file inline attaches it
 * exactly the same way — the atomic chip is a second way to reach the same
 * `attachedFiles` state, not a second attachment mechanism.
 */
export const ComposerMentionFilesContext = React.createContext<{
  files: readonly ContextFileItem[];
  onSelect?: (path: string) => void;
}>({ files: EMPTY_ARRAY });

const onSelectItem = getMentionOnSelectItem();

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

/**
 * The "@" combobox — the same InlineCombobox the slash menu and NoteLink use,
 * filtered client-side (InlineComboboxItem's `keywords`) over the session's
 * already-fetched context files, mirroring NoteLinkInputElement's
 * fetch-once-then-filter shape rather than re-querying per keystroke.
 */
export function ContextMentionInputElement(props: PlateElementProps<TComboboxInputElement>) {
  const { editor, element } = props;
  const { files, onSelect } = React.useContext(ComposerMentionFilesContext);

  return (
    <PlateElement {...props} as="span">
      <InlineCombobox element={element} trigger="@">
        <InlineComboboxInput />
        <InlineComboboxContent>
          <InlineComboboxEmpty>No files</InlineComboboxEmpty>
          <InlineComboboxGroup>
            {files.map((file) => (
              <InlineComboboxItem
                key={file.path}
                value={file.path}
                label={file.name || file.path}
                keywords={[file.path, file.name ?? ""]}
                onClick={() => {
                  onSelectItem(editor, { key: file.path, text: file.name || file.path });
                  onSelect?.(file.path);
                }}
              >
                {file.kind === "folder" ? <FolderIcon className="size-3.5" /> : <FileCode className="size-3.5" />}
                <span className="min-w-0 flex-1 truncate">{file.name || file.path}</span>
                <span className="max-w-32 shrink-0 truncate text-caption-11 text-text-tertiary">{file.path}</span>
              </InlineComboboxItem>
            ))}
          </InlineComboboxGroup>
        </InlineComboboxContent>
      </InlineCombobox>
      {props.children}
    </PlateElement>
  );
}
