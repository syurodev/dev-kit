import { useEffect, useState } from "react";
import { gitCommitIdentity } from "@/bridge/git";
import { describeCommitIdentity, type IdentityNote } from "./commitIdentity";

/**
 * Who the next commit will be written as, read again whenever the repository
 * changes — a branch switch can change the remote, and committing is when the
 * answer matters. A failed read shows nothing rather than an error: this line
 * is information beside the commit box, not something the commit waits on.
 */
export function useCommitIdentity(folder: string, revision: number, branch: string): IdentityNote | null {
  const [note, setNote] = useState<IdentityNote | null>(null);
  useEffect(() => {
    let current = true;
    gitCommitIdentity()
      .then((identity) => {
        if (current) setNote(describeCommitIdentity(identity));
      })
      .catch(() => {
        if (current) setNote(null);
      });
    return () => {
      current = false;
    };
  }, [folder, revision, branch]);
  return note;
}
