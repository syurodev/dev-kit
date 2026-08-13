import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";

type CommandPaletteProps = {
  open: boolean;
  commands: Array<{ id: string; title: string }>;
  onClose: () => void;
};

export function CommandPalette({ open, commands, onClose }: CommandPaletteProps) {
  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
    >
      <CommandInput placeholder="Search commands..." />
      <CommandList>
        <CommandEmpty>No commands are currently available.</CommandEmpty>
        <CommandGroup heading="Commands">
          {commands.map((command) => (
            <CommandItem key={command.id} value={command.title} onSelect={onClose}>
              {command.title}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
