import { useEffect, useState } from "react";
import { Check, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { FOLDER_ICONS } from "../folderIcons";
import type { DatabaseFolder } from "../databaseStore";

/**
 * Figma `DB / Folder Icon Picker` (532:2726) — an Edit folder dialog rather
 * than an icon picker: name, icon, and Delete folder in one place.
 */
export function EditFolderDialog({
  folder,
  onClose,
  onSave,
  onDelete,
}: {
  folder: DatabaseFolder | null;
  onClose: () => void;
  onSave: (patch: { name: string; icon: string }) => void;
  onDelete: () => void;
}) {
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("folder");

  useEffect(() => {
    if (folder) {
      setName(folder.name);
      setIcon(folder.icon);
    }
  }, [folder]);

  return (
    <Dialog open={!!folder} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-120" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>Edit folder</DialogTitle>
        </DialogHeader>

        <div className="space-y-1.5">
          <label className="text-label-12 text-text-primary" htmlFor="folder-name">
            Folder name
          </label>
          <Input
            id="folder-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Production"
            autoFocus
            // A folder arrives here named "Folder 3"; selecting it means the
            // first keystroke replaces the placeholder rather than appending
            // to it.
            onFocus={(e) => e.target.select()}
          />
        </div>

        <div className="space-y-2">
          <span className="text-label-12 text-text-primary">Icon</span>
          <div className="flex flex-wrap gap-2">
            {FOLDER_ICONS.map(({ name: iconName, Icon }) => (
              <button
                key={iconName}
                type="button"
                onClick={() => setIcon(iconName)}
                aria-pressed={icon === iconName}
                title={iconName}
                className={`h-10 w-10 rounded-lg border flex items-center justify-center transition-colors ${
                  icon === iconName
                    ? "border-action-primary bg-action-primary/10 text-action-primary"
                    : "border-border bg-surface-raised text-text-secondary hover:text-text-primary"
                }`}
              >
                <Icon className="size-4" />
              </button>
            ))}
          </div>
        </div>

        {/* The screen states its own boundary; it is worth repeating where
            someone is choosing a shield or a padlock. */}
        <p className="text-code-11 text-text-tertiary">
          the folder icon is cosmetic — grouping never changes how a profile connects
        </p>

        <DialogFooter className="sm:justify-between">
          <Button variant="destructive" size="sm" onClick={onDelete}>
            Delete folder
            <Trash2 />
          </Button>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button size="sm" onClick={() => onSave({ name, icon })} disabled={!name.trim()}>
              Save
              <Check />
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
