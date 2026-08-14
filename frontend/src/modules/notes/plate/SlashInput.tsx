import type { ReactNode } from "react";
import { ListStyleType, toggleList } from "@platejs/list";
import { insertTable } from "@platejs/table";
import {
  ChevronsUpDown,
  Code2,
  Heading1Icon,
  Heading2Icon,
  Heading3Icon,
  ListIcon,
  ListOrdered,
  Minus,
  PilcrowIcon,
  Quote,
  Square,
  Table,
} from "lucide-react";
import { KEYS, type TComboboxInputElement } from "platejs";
import { PlateElement, type PlateEditor, type PlateElementProps } from "platejs/react";

import { Kbd, KbdGroup } from "@/components/ui/kbd";
import {
  InlineCombobox,
  InlineComboboxContent,
  InlineComboboxEmpty,
  InlineComboboxGroup,
  InlineComboboxGroupLabel,
  InlineComboboxInput,
  InlineComboboxItem,
} from "@/components/ui/inline-combobox";

type SlashItem = {
  icon: ReactNode;
  keywords?: string[];
  label: string;
  shortcut?: string[];
  value: string;
  onSelect: (editor: PlateEditor) => void;
};

function modKey(): string {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/i.test(navigator.platform)
    ? "⌘"
    : "Ctrl";
}

function toggleBlock(editor: PlateEditor, type: string) {
  editor.tf.toggleBlock(type);
}

function slashItems(): SlashItem[] {
  const mod = modKey();
  return [
    {
      icon: <PilcrowIcon />,
      keywords: ["paragraph", "text"],
      label: "Text",
      value: KEYS.p,
      onSelect: (editor) => toggleBlock(editor, KEYS.p),
    },
    {
      icon: <Heading1Icon />,
      keywords: ["h1", "title"],
      label: "Heading 1",
      shortcut: [mod, "Alt", "1"],
      value: KEYS.h1,
      onSelect: (editor) => toggleBlock(editor, KEYS.h1),
    },
    {
      icon: <Heading2Icon />,
      keywords: ["h2", "subtitle"],
      label: "Heading 2",
      shortcut: [mod, "Alt", "2"],
      value: KEYS.h2,
      onSelect: (editor) => toggleBlock(editor, KEYS.h2),
    },
    {
      icon: <Heading3Icon />,
      keywords: ["h3"],
      label: "Heading 3",
      shortcut: [mod, "Alt", "3"],
      value: KEYS.h3,
      onSelect: (editor) => toggleBlock(editor, KEYS.h3),
    },
    {
      icon: <ListIcon />,
      keywords: ["ul", "bullet", "-"],
      label: "Bulleted list",
      value: "ul",
      onSelect: (editor) => toggleList(editor, { listStyleType: ListStyleType.Disc }),
    },
    {
      icon: <ListOrdered />,
      keywords: ["ol", "numbered", "1"],
      label: "Numbered list",
      value: "ol",
      onSelect: (editor) => toggleList(editor, { listStyleType: ListStyleType.Decimal }),
    },
    {
      icon: <Square />,
      keywords: ["todo", "task", "checkbox"],
      label: "To-do list",
      value: "todo",
      onSelect: (editor) => toggleList(editor, { listStyleType: "todo" }),
    },
    {
      icon: <Quote />,
      keywords: ["blockquote", ">"],
      label: "Quote",
      shortcut: [mod, "Shift", "."],
      value: KEYS.blockquote,
      onSelect: (editor) => toggleBlock(editor, KEYS.blockquote),
    },
    {
      icon: <ChevronsUpDown />,
      keywords: ["collapsible", "toggle", "fold"],
      label: "Toggle",
      value: KEYS.toggle,
      onSelect: (editor) => toggleBlock(editor, KEYS.toggle),
    },
    {
      icon: <Code2 />,
      keywords: ["```", "code"],
      label: "Code block",
      shortcut: [mod, "Alt", "8"],
      value: KEYS.codeBlock,
      onSelect: (editor) => toggleBlock(editor, KEYS.codeBlock),
    },
    {
      icon: <Table />,
      keywords: ["grid"],
      label: "Table",
      value: KEYS.table,
      onSelect: (editor) => insertTable(editor),
    },
    {
      icon: <Minus />,
      keywords: ["hr", "divider", "---"],
      label: "Divider",
      value: KEYS.hr,
      onSelect: (editor) => toggleBlock(editor, KEYS.hr),
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
          <InlineComboboxGroup>
            <InlineComboboxGroupLabel>Basic blocks</InlineComboboxGroupLabel>
            {items.map((item) => (
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
        </InlineComboboxContent>
      </InlineCombobox>
      {props.children}
    </PlateElement>
  );
}
