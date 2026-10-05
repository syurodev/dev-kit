import type { CommitIdentity } from "@/bridge/git";

/** One line under the commit message: who the commit will be written as. */
export type IdentityNote = {
  text: string;
  /** Why it matters, when it does: the account would have committed differently. */
  hint: string;
  tone: "normal" | "warning";
};

const who = (name: string, email: string) => `${name} <${email}>`;

/**
 * Says who the next commit will be written as and where that came from.
 *
 * A commit pushed under the wrong email cannot be taken back, so the line is
 * not decoration: it turns amber exactly when the identity that wins is not the
 * one the account for the remote would have used, or when there is none.
 */
export function describeCommitIdentity(identity: CommitIdentity): IdentityNote {
  const current = identity.name && identity.email ? who(identity.name, identity.email) : "";
  if (!current) {
    return {
      text: "No name or email is set for commits.",
      hint: identity.account
        ? `Set one for “${identity.account}” in Accounts, or in git's configuration.`
        : "Set user.name and user.email in git's configuration.",
      tone: "warning",
    };
  }
  switch (identity.source) {
    case "account":
      return { text: `Committing as ${current} · account “${identity.account}”`, hint: "", tone: "normal" };
    case "repository": {
      const differs =
        identity.accountEmail !== "" &&
        (identity.accountEmail !== identity.email || identity.accountName !== identity.name);
      return {
        text: `Committing as ${current} · this repository's config`,
        hint: differs
          ? `Account “${identity.account}” would use ${who(identity.accountName, identity.accountEmail)}.`
          : "",
        tone: differs ? "warning" : "normal",
      };
    }
    default: {
      // git's own configuration. Worth a warning only when an account is known
      // for the remote and has nothing to say: the commit then carries whatever
      // the machine's default is, which may not be that account's.
      const unset = identity.account !== "" && identity.accountEmail === "";
      return {
        text: `Committing as ${current} · git's configuration`,
        hint: unset ? `Account “${identity.account}” has no commit identity — set one in Accounts.` : "",
        tone: unset ? "warning" : "normal",
      };
    }
  }
}
