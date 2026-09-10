import { useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { listQueryHistory, listSavedQueries, type ConnectionProfile } from "../../../bridge/database";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";

/**
 * Figma `DB / Delete Profile Dialog` (576:7817).
 *
 * Nothing else gates this: DBDeleteProfile is scoped the same as running a
 * query, so there is no capability prompt behind it.
 *
 * The dialog's job is not the extra click. It is naming what goes with the
 * profile — the sealed password, the local history, the saved queries — none of
 * which is visible from the sidebar row.
 */
export function DeleteProfileDialog({
  profile,
  onClose,
  onConfirm,
}: {
  profile: ConnectionProfile | null;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { folders } = useDatabaseStore(
    useShallow((s) => ({
      folders: s.folders,
    }))
  );
  const [historyCount, setHistoryCount] = useState<number | null>(null);
  const [savedCount, setSavedCount] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      if (!profile) return;
      setHistoryCount(null);
      setSavedCount(null);
      // Best-effort: a locked vault makes both unreadable, and that must not
      // stop someone deleting a profile. The counts simply go unstated rather
      // than being guessed at.
      const [history, saved] = await Promise.all([
        listQueryHistory(profile.id).catch(() => null),
        listSavedQueries(profile.id).catch(() => null),
      ]);
      if (cancelled) return;
      setHistoryCount(history?.length ?? null);
      setSavedCount(saved?.length ?? null);
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [profile]);

  const folder = profile ? folders.find((f) => f.profileIds.includes(profile.id)) : undefined;

  const losses: Array<{ label: string; note: string }> = [
    { label: "Password", note: "sealed with the vault key" },
  ];
  if (historyCount !== null && historyCount > 0) {
    losses.push({
      label: `${historyCount} query history ${historyCount === 1 ? "entry" : "entries"}`,
      note: "encrypted SQL text",
    });
  }
  if (savedCount !== null && savedCount > 0) {
    losses.push({
      label: `${savedCount} saved ${savedCount === 1 ? "query" : "queries"}`,
      note: "named, never pruned",
    });
  }
  if (folder) {
    losses.push({
      label: `Membership of folder ${folder.name}`,
      note: "the folder itself stays",
    });
  }

  return (
    <Dialog open={!!profile} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[480px]">
        <DialogHeader>
          <DialogTitle>Delete {profile?.name}?</DialogTitle>
          <DialogDescription>
            The database itself is untouched — this removes the connection profile from DevKit
            and everything stored alongside it.
          </DialogDescription>
        </DialogHeader>

        <ul className="space-y-1.5 text-body-12">
          {losses.map((loss) => (
            <li key={loss.label} className="flex items-baseline gap-2">
              <span className="text-text-primary">{loss.label}</span>
              <span className="text-text-tertiary">{loss.note}</span>
            </li>
          ))}
        </ul>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            <Trash2 />
            Delete profile
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
