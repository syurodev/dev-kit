type CommandPaletteProps = {
  open: boolean;
  commands: Array<{ id: string; title: string }>;
  onClose: () => void;
};

export function CommandPalette({ open, commands, onClose }: CommandPaletteProps) {
  if (!open) {
    return null;
  }

  return (
    <div className="command-palette" role="dialog" aria-modal="true">
      <div className="command-palette-panel">
        <div className="command-palette-header">
          <span>Commands</span>
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
        <ul>
          {commands.map((command) => (
            <li key={command.id}>{command.title}</li>
          ))}
        </ul>
      </div>
    </div>
  );
}
