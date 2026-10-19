import type { ReactNode } from "react";
import { ListStyleType, toggleList } from "@platejs/list";
import { insertTable } from "@platejs/table";
import {
  ChevronsUpDown,
  Code2,
  Heading1Icon,
  Heading2Icon,
  Heading3Icon,
  Info,
  ListIcon,
  ListOrdered,
  ListTree,
  Minus,
  PilcrowIcon,
  Quote,
  Sigma,
  Square,
  Table,
  Workflow,
} from "lucide-react";
import { KEYS, NodeApi, type TComboboxInputElement, type TElement } from "platejs";
import { PlateElement, type PlateEditor, type PlateElementProps } from "platejs/react";

import { Kbd, KbdGroup } from "@/components/ui/kbd";
import { modKey } from "@/lib/shortcut";
import {
  InlineCombobox,
  InlineComboboxContent,
  InlineComboboxEmpty,
  InlineComboboxGroup,
  InlineComboboxGroupLabel,
  InlineComboboxInput,
  InlineComboboxItem,
} from "@/components/ui/inline-combobox";

export type SlashItem = {
  group: "basic" | "tech";
  icon: ReactNode;
  keywords?: string[];
  label: string;
  shortcut?: string[];
  value: string;
  onSelect: (editor: PlateEditor) => void;
};

function toggleBlock(editor: PlateEditor, type: string) {
  editor.tf.toggleBlock(type);
}

const GROUP_LABELS: Record<SlashItem["group"], string> = {
  basic: "Basic blocks",
  tech: "Technical docs",
};

const MERMAID_STARTER = ["flowchart LR", "  A[Start] --> B{Decision}", "  B -->|yes| C[Done]", "  B -->|no| A"];
const MATH_STARTER = ["E = mc^2"];

/** A code block of `lang` holding `lines`, opened on its source (`autoEdit`: see RenderedCodeBlock). */
function previewBlock(lang: string, lines: string[]): TElement {
  return {
    type: KEYS.codeBlock,
    lang,
    autoEdit: true,
    children: lines.map((line) => ({ type: KEYS.codeLine, children: [{ text: line }] })),
  };
}

/**
 * Puts `node` where the slash was typed: in place of the empty paragraph that
 * is left once the "/query" is gone, or after the current block when it has
 * text. A void block is followed by a paragraph if nothing comes after it, so
 * the caret has somewhere to go.
 */
function insertBlock(editor: PlateEditor, node: TElement, options: { trailing?: boolean } = {}) {
  const entry = editor.api.block();
  let at: number[] | undefined;
  if (entry && entry[0].type === KEYS.p && NodeApi.string(entry[0]) === "") {
    at = entry[1];
    editor.tf.removeNodes({ at });
  }
  editor.tf.insertNodes(node, { at, select: true });
  if (options.trailing) {
    const last = editor.children.length - 1;
    if (editor.children[last]?.type === node.type) {
      editor.tf.insertNodes({ type: KEYS.p, children: [{ text: "" }] }, { at: [last + 1] });
    }
  }
}

export function slashItems(): SlashItem[] {
  const mod = modKey();
  return [
    {
      group: "basic",
      icon: <PilcrowIcon />,
      keywords: ["paragraph", "text"],
      label: "Text",
      value: KEYS.p,
      onSelect: (editor) => toggleBlock(editor, KEYS.p),
    },
    {
      group: "basic",
      icon: <Heading1Icon />,
      keywords: ["h1", "title"],
      label: "Heading 1",
      shortcut: [mod, "Alt", "1"],
      value: KEYS.h1,
      onSelect: (editor) => toggleBlock(editor, KEYS.h1),
    },
    {
      group: "basic",
      icon: <Heading2Icon />,
      keywords: ["h2", "subtitle"],
      label: "Heading 2",
      shortcut: [mod, "Alt", "2"],
      value: KEYS.h2,
      onSelect: (editor) => toggleBlock(editor, KEYS.h2),
    },
    {
      group: "basic",
      icon: <Heading3Icon />,
      keywords: ["h3"],
      label: "Heading 3",
      shortcut: [mod, "Alt", "3"],
      value: KEYS.h3,
      onSelect: (editor) => toggleBlock(editor, KEYS.h3),
    },
    {
      group: "basic",
      icon: <ListIcon />,
      keywords: ["ul", "bullet", "-"],
      label: "Bulleted list",
      value: "ul",
      onSelect: (editor) => toggleList(editor, { listStyleType: ListStyleType.Disc }),
    },
    {
      group: "basic",
      icon: <ListOrdered />,
      keywords: ["ol", "numbered", "1"],
      label: "Numbered list",
      value: "ol",
      onSelect: (editor) => toggleList(editor, { listStyleType: ListStyleType.Decimal }),
    },
    {
      group: "basic",
      icon: <Square />,
      keywords: ["todo", "task", "checkbox"],
      label: "To-do list",
      value: "todo",
      onSelect: (editor) => toggleList(editor, { listStyleType: "todo" }),
    },
    {
      group: "basic",
      icon: <Quote />,
      keywords: ["blockquote", ">"],
      label: "Quote",
      shortcut: [mod, "Shift", "."],
      value: KEYS.blockquote,
      onSelect: (editor) => toggleBlock(editor, KEYS.blockquote),
    },
    {
      group: "basic",
      icon: <ChevronsUpDown />,
      keywords: ["collapsible", "toggle", "fold"],
      label: "Toggle",
      value: KEYS.toggle,
      onSelect: (editor) => toggleBlock(editor, KEYS.toggle),
    },
    {
      group: "basic",
      icon: <Code2 />,
      keywords: ["```", "code"],
      label: "Code block",
      shortcut: [mod, "Alt", "8"],
      value: KEYS.codeBlock,
      onSelect: (editor) => toggleBlock(editor, KEYS.codeBlock),
    },
    {
      group: "basic",
      icon: <Table />,
      keywords: ["grid"],
      label: "Table",
      value: KEYS.table,
      onSelect: (editor) => insertTable(editor),
    },
    {
      group: "basic",
      icon: <Minus />,
      keywords: ["hr", "divider", "---"],
      label: "Divider",
      value: KEYS.hr,
      onSelect: (editor) => toggleBlock(editor, KEYS.hr),
    },
    {
      group: "tech",
      icon: <Info />,
      keywords: ["callout", "note", "tip", "warning", "caution", "alert", "admonition", "info"],
      label: "Callout",
      value: KEYS.callout,
      onSelect: (editor) => {
        const entry = editor.api.block();
        if (!entry) return;
        editor.tf.wrapNodes({ type: KEYS.callout, variant: "note", children: [] }, { at: entry[1] });
        editor.tf.select(editor.api.start(entry[1])!);
      },
    },
    {
      group: "tech",
      icon: <Workflow />,
      keywords: ["mermaid", "diagram", "flowchart", "sequence", "chart", "uml", "graph", "er"],
      label: "Mermaid diagram",
      value: "mermaid",
      onSelect: (editor) => insertBlock(editor, previewBlock("mermaid", MERMAID_STARTER)),
    },
    {
      group: "tech",
      icon: <Sigma />,
      keywords: ["math", "equation", "latex", "katex", "formula", "tex"],
      label: "Math equation",
      value: "math",
      onSelect: (editor) => insertBlock(editor, previewBlock("math", MATH_STARTER)),
    },
    {
      group: "tech",
      icon: <ListTree />,
      keywords: ["toc", "contents", "outline", "headings"],
      label: "Table of contents",
      value: KEYS.toc,
      onSelect: (editor) => insertBlock(editor, { type: KEYS.toc, children: [{ text: "" }] }, { trailing: true }),
    },
  ];
}

export function SlashInputElement(props: PlateElementProps<TComboboxInputElement>) {
  const { editor, element } = props;
  const items = slashItems();

  return (
    <PlateElement {...props} as="span">
      <InlineCombobox element={element} trigger="/">
        <InlineComboboxInput />
        <InlineComboboxContent>
          <InlineComboboxEmpty>No results</InlineComboboxEmpty>
          {(Object.keys(GROUP_LABELS) as SlashItem["group"][]).map((group) => (
          <InlineComboboxGroup key={group}>
            <InlineComboboxGroupLabel>{GROUP_LABELS[group]}</InlineComboboxGroupLabel>
            {items.filter((item) => item.group === group).map((item) => (
              <InlineComboboxItem
                key={item.value}
                value={item.value}
                label={item.label}
                keywords={item.keywords}
                onClick={() => item.onSelect(editor)}
              >
                {item.icon}
                <span className="flex-1 truncate">{item.label}</span>
                {item.shortcut ? (
                  <KbdGroup className="ml-auto shrink-0">
                    {item.shortcut.map((part) => (
                      <Kbd key={part}>{part}</Kbd>
                    ))}
                  </KbdGroup>
                ) : null}
              </InlineComboboxItem>
            ))}
          </InlineComboboxGroup>
          ))}
        </InlineComboboxContent>
      </InlineCombobox>
      {props.children}
    </PlateElement>
  );
}
