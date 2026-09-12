import type { ReactNode } from "react";
import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from "@/components/ui/dropdown-menu";

/**
 * One row's actions, as data.
 *
 * Data because they are offered twice — by right-click and by the row's own
 * button — and two hand-written copies of the same menu drift. They already
 * had: the row carried three buttons for three of the actions while the
 * context menu carried nine, so which ones you could reach depended on how you
 * asked. A list rendered into both cannot disagree with itself.
 */
export type RowAction =
  | {
      kind: "item";
      icon?: ReactNode;
      label: ReactNode;
      onSelect: () => void;
      disabled?: boolean;
      destructive?: boolean;
    }
  | { kind: "separator" }
  | { kind: "submenu"; icon: ReactNode; label: string; items: RowAction[] };

/** The item component set for one of the two menus. */
type Family = {
  Item: typeof ContextMenuItem | typeof DropdownMenuItem;
  Separator: typeof ContextMenuSeparator | typeof DropdownMenuSeparator;
  Sub: typeof ContextMenuSub | typeof DropdownMenuSub;
  SubTrigger: typeof ContextMenuSubTrigger | typeof DropdownMenuSubTrigger;
  SubContent: typeof ContextMenuSubContent | typeof DropdownMenuSubContent;
};

const CONTEXT: Family = {
  Item: ContextMenuItem,
  Separator: ContextMenuSeparator,
  Sub: ContextMenuSub,
  SubTrigger: ContextMenuSubTrigger,
  SubContent: ContextMenuSubContent,
};

const DROPDOWN: Family = {
  Item: DropdownMenuItem,
  Separator: DropdownMenuSeparator,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
};

function render(actions: RowAction[], family: Family, keyPrefix: string): ReactNode {
  const { Item, Separator, Sub, SubTrigger, SubContent } = family;
  return actions.map((action, index) => {
    const key = `${keyPrefix}-${index}`;
    if (action.kind === "separator") {
      return <Separator key={key} />;
    }
    if (action.kind === "submenu") {
      return (
        <Sub key={key}>
          <SubTrigger>
            {action.icon}
            {action.label}
          </SubTrigger>
          <SubContent>{render(action.items, family, key)}</SubContent>
        </Sub>
      );
    }
    return (
      <Item
        key={key}
        onSelect={action.onSelect}
        disabled={action.disabled}
        variant={action.destructive ? "destructive" : undefined}
      >
        {action.icon}
        {action.label}
      </Item>
    );
  });
}

export function RowContextMenuContent({ actions }: { actions: RowAction[] }) {
  return <ContextMenuContent>{render(actions, CONTEXT, "ctx")}</ContextMenuContent>;
}

export function RowDropdownMenuContent({ actions }: { actions: RowAction[] }) {
  return (
    // Clicks are stopped here, not on each item: the menu floats over a tree
    // row that selects on click, and a click landing on the row behind would
    // switch connection on the way to choosing "Delete connection".
    <DropdownMenuContent align="end" onClick={(event) => event.stopPropagation()}>
      {render(actions, DROPDOWN, "menu")}
    </DropdownMenuContent>
  );
}
