import { cn } from "@/lib/utils";
import { accountLabelOf, noticeText } from "../accounts/accountModel";
import { useAgentAccounts } from "../accounts/useAgentAccounts";
import type { AccountNotice as AccountNoticeData } from "../types";

/**
 * A session moved to another account, or tried to and stayed. One quiet line in
 * the transcript, the weight of the docked plan's caption: it is news about where
 * the work runs, not something anyone has to answer. A move that did not happen
 * leads with what did not, in the warning tone, so the colour is never the only
 * signal.
 */
export function AccountNotice({
  notice,
  fromLabel,
  toLabel,
}: {
  notice: AccountNoticeData;
  fromLabel: string;
  toLabel: string;
}) {
  const failed = notice.kind === "failed";
  return (
    <p
      data-testid="account-notice"
      data-kind={notice.kind}
      className={cn("w-full text-caption-11", failed ? "text-status-warning" : "text-text-tertiary")}
    >
      {noticeText(notice, fromLabel, toLabel)}
    </p>
  );
}

/**
 * The notice with the accounts' names: asked of the same list Settings reads, so
 * a rename shows here too. An id the list no longer holds reads "a removed
 * account", and the built-in account has its own name before the list arrives.
 */
export function AccountNoticeLine({ notice, vendor }: { notice: AccountNoticeData; vendor: string }) {
  const accounts = useAgentAccounts(vendor).data;
  return (
    <AccountNotice
      notice={notice}
      fromLabel={accountLabelOf(accounts, vendor, notice.from)}
      toLabel={accountLabelOf(accounts, vendor, notice.to)}
    />
  );
}
