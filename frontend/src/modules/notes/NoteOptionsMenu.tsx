import type * as React from "react";
import {
  ALargeSmall,
  Copy,
  CopyPlus,
  Ellipsis,
  FolderInput,
  History,
  Lock,
  MoveHorizontal,
  Trash2,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import type { NoteFont } from "@/stores/noteView";
import { folderDotVar } from "./folderColors";
import { NOTE_FONT_CLASS, type NoteView } from "./noteLayout";

const FONTS: { id: NoteFont; label: string }[] = [
  { id: "default", label: "Default" },
  { id: "serif", label: "Serif" },
  { id: "mono", label: "Mono" },
];

const UNGROUPED = "ungrouped";

/**
 * The note's ⋯ menu, after Notion's page menu: font style, then how the page
 * is laid out, then what can be done with it. Every view choice is per note
 * and per person (noteView.ts) — none of it syncs or changes the note.
 */
export function NoteOptionsMenu({
  view,
  folders,
  folderId,
  historyOpen,
  onFontChange,
  onToggleSmallText,
  onToggleFullWidth,
  onToggleLocked,
  onCopyContents,
  onDuplicate,
  onMoveTo,
  onToggleHistory,
  onDelete,
}: {
  view: NoteView;
  folders: { id: string; name: string; color: string }[];
  folderId: string;
  historyOpen: boolean;
  onFontChange: (font: NoteFont) => void;
  onToggleSmallText: () => void;
  onToggleFullWidth: () => void;
  onToggleLocked: () => void;
  onCopyContents: () => void;
  onDuplicate: () => void;
  /** `""` is Ungrouped. */
  onMoveTo: (folderId: string) => void;
  onToggleHistory: () => void;
  onDelete: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Note options"
          title="Note options"
          className="shrink-0 hover:text-text-primary data-[state=open]:text-text-primary"
        >
          <Ellipsis className="size-3.25" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel>Font style</DropdownMenuLabel>
        <div role="group" aria-label="Font style" className="grid grid-cols-3 gap-1 px-1 pb-1">
          {FONTS.map((font) => (
            <DropdownMenuItem
              key={font.id}
              bare
              role="menuitemradio"
              aria-checked={view.font === font.id}
              // Monochrome chrome: the chosen face is the bright one, ringed;
              // the others recede until hovered.
              className={cn(
                "flex flex-col items-center gap-0.5 rounded-sm py-1.5 text-text-tertiary hover:bg-state-soft hover:text-text-primary focus:bg-state-soft",
                view.font === font.id && "text-text-primary ring-1 ring-border ring-inset",
              )}
              onSelect={(event) => {
                event.preventDefault();
                onFontChange(font.id);
              }}
            >
              {/* The page's own swap of --font-sans, so the sample is the face. */}
              <span aria-hidden className={cn("text-heading-24", NOTE_FONT_CLASS[font.id])}>
                Ag
              </span>
              <span className="text-caption-12">{font.label}</span>
            </DropdownMenuItem>
          ))}
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Appearance</DropdownMenuLabel>
        <SwitchItem icon={<ALargeSmall />} checked={view.smallText} onToggle={onToggleSmallText}>
          Small text
        </SwitchItem>
        <SwitchItem icon={<MoveHorizontal />} checked={view.fullWidth} onToggle={onToggleFullWidth}>
          Full width
        </SwitchItem>
        <SwitchItem icon={<Lock />} checked={view.locked} onToggle={onToggleLocked}>
          Lock note
        </SwitchItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem icon={<Copy />} onSelect={onCopyContents}>
          Copy contents
        </DropdownMenuItem>
        <DropdownMenuItem icon={<CopyPlus />} onSelect={onDuplicate}>
          Duplicate
        </DropdownMenuItem>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger icon={<FolderInput />}>Move to</DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="max-h-80 overflow-y-auto">
            <DropdownMenuRadioGroup
              value={folderId || UNGROUPED}
              onValueChange={(value) => onMoveTo(value === UNGROUPED ? "" : value)}
            >
              <DropdownMenuRadioItem value={UNGROUPED}>Ungrouped</DropdownMenuRadioItem>
              {folders.map((folder) => (
                <DropdownMenuRadioItem
                  key={folder.id}
                  value={folder.id}
                  detail={
                    <span
                      aria-hidden
                      className="inline-block size-2 rounded-full"
                      style={{ backgroundColor: folderDotVar(folder.color) }}
                    />
                  }
                >
                  {folder.name}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuItem icon={<History />} detail={historyOpen ? "Open" : undefined} onSelect={onToggleHistory}>
          Version history
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" icon={<Trash2 />} onSelect={onDelete}>
          Move to trash
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * A switch, as Notion draws it, on a row that is the control: the row toggles,
 * the switch only shows the state, and the menu stays open so the page can be
 * seen changing behind it.
 */
function SwitchItem({
  icon,
  checked,
  onToggle,
  children,
}: {
  icon: React.ReactNode;
  checked: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) {
  return (
    <DropdownMenuItem
      role="menuitemcheckbox"
      aria-checked={checked}
      icon={icon}
      detail={<Switch checked={checked} tabIndex={-1} aria-hidden className="pointer-events-none" />}
      onSelect={(event) => {
        event.preventDefault();
        onToggle();
      }}
    >
      {children}
    </DropdownMenuItem>
  );
}
