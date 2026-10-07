import { useEffect, useState } from "react";
import { useModuleVisibility } from "@/components/ui/module-visibility";
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
  // The identity is edited in Accounts, which is another module: nothing here
  // changes when it is saved. Coming back to Git, or back to the window, is
  // when the person looks at this line again, so that is when it is read.
  const visible = useModuleVisibility();
  const [focused, setFocused] = useState(0);
  useEffect(() => {
    const onFocus = () => setFocused((count) => count + 1);
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, []);
  useEffect(() => {
    if (!visible) return;
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
  }, [folder, revision, branch, visible, focused]);
  return note;
}
