import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { AccountsEditor, type AccountsEditorProps } from "./AccountsEditor";

const NOW = Date.UTC(2026, 9, 23, 12, 0, 0);
const sec = NOW / 1000;

const account = (over: Partial<AgentAccount> = {}): AgentAccount => ({
  id: "claude-work",
  vendor: "claude",
  label: "Work",
  builtin: false,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
  ...over,
});
const builtin = account({ id: "claude-default", label: "System default", builtin: true });

const noop = async () => true;
const props = (over: Partial<AccountsEditorProps> = {}): AccountsEditorProps => ({
  vendor: "claude",
  vendorName: "Claude",
  accounts: [builtin],
  order: [],
  loadError: "",
  busyId: "",
  error: "",
  refreshNotes: {},
  now: NOW,
  renderSignIn: () => <div data-testid="sign-in-panel">Sign in</div>,
  onAdd: noop,
  onRename: noop,
  onRemove: noop,
  onSetShared: () => {},
  onRefreshUsage: () => {},
  onRefreshStatus: () => {},
  onMove: () => {},
  ...over,
});
const render = (over: Partial<AccountsEditorProps> = {}) =>
  renderToStaticMarkup(
    <TooltipProvider>
      <AccountsEditor {...props(over)} />
    </TooltipProvider>,
  );

const rows = (html: string) => html.match(/data-testid="agent-account-row"/g)?.length ?? 0;

describe("AccountsEditor", () => {
  test("a vendor with only its built-in account shows that and no way to reorder or remove it", () => {
    const html = render();
    expect(rows(html)).toBe(1);
    expect(html).toContain("System default");
    expect(html).toContain("Built in");
    expect(html).toContain("already uses on this computer");
    expect(html).not.toContain('aria-label="Move up"');
    expect(html).not.toContain('aria-label="Remove"');
    expect(html).not.toContain('aria-label="Rename"');
    expect(html).toContain("Add account");
  });

  test("accounts are listed in the order the rules try them, and the ends cannot move further", () => {
    const html = render({
      accounts: [builtin, account(), account({ id: "claude-spare", label: "Spare" })],
      order: ["claude-spare", "claude-work"],
    });
    expect(rows(html)).toBe(3);
    const at = (id: string) => html.indexOf(`data-account="${id}"`);
    expect(at("claude-spare")).toBeLessThan(at("claude-work"));
    expect(at("claude-work")).toBeLessThan(at("claude-default"));
    expect(html).toContain("tries them from the top");
    expect(html.match(/aria-label="Move up"/g)?.length).toBe(3);
    // The first row cannot go up and the last cannot go down.
    expect(html.match(/<button[^>]*disabled=""[^>]*aria-label="Move up"|aria-label="Move up"[^>]*disabled=""/g)?.length).toBe(1);
    expect(html.match(/<button[^>]*disabled=""[^>]*aria-label="Move down"|aria-label="Move down"[^>]*disabled=""/g)?.length).toBe(1);
  });

  test("a signed-in account shows its windows as meters, how old the reading is, and the lower bound of a shared login", () => {
    const html = render({
      accounts: [
        builtin,
        account({
          shared: true,
          observedAt: sec - 180,
          usageSource: "checked",
          usage: [
            { label: "5-hour", usedPercent: 83, resetsAt: sec + 2 * 3600, status: "warning" },
            { label: "Weekly", usedPercent: 40, resetsAt: sec + 3 * 86_400, status: "ok" },
          ],
        }),
      ],
    });
    expect(html.match(/role="meter"/g)?.length).toBe(2);
    expect(html).toContain('aria-valuenow="83"');
    expect(html).toContain("83% · resets in 2h");
    expect(html).toContain('data-tone="warning"');
    expect(html).toContain("Checked 3m ago");
    expect(html).toContain("a lower bound: other people use this login too");
    expect(html).toContain("Shared");
  });

  test("an account out of usage says until when; a past time says nothing", () => {
    const limited = render({ accounts: [builtin, account({ exhaustedUntil: sec + 3 * 3600 })] });
    expect(limited).toContain("Limit reached · available in 3h");
    const past = render({ accounts: [builtin, account({ exhaustedUntil: sec - 60, cooldownUntil: sec - 60 })] });
    expect(past).not.toContain("Limit reached");
    expect(past).not.toContain("Left out for now");
  });

  test("a signed-out account offers the sign-in and a way to check again, and draws no usage", () => {
    const html = render({
      accounts: [account({ signedIn: false, usage: [{ label: "5-hour", usedPercent: 10, resetsAt: 0, status: "" }] })],
    });
    expect(html).toContain("Not signed in");
    expect(html).toContain('data-testid="sign-in-panel"');
    expect(html).toContain("Check again");
    expect(html).not.toContain('role="meter"');
    expect(html).not.toContain('aria-label="Refresh usage"');
  });

  test("a refresh that read nothing says why, quietly", () => {
    const html = render({ accounts: [builtin, account()], refreshNotes: { "claude-work": "Nothing new: DevKit asks…" } });
    expect(html).toContain('role="status"');
    expect(html).toContain("Nothing new: DevKit asks…");
  });

  test("loading, a failed read and an empty list each say so", () => {
    expect(render({ accounts: undefined })).not.toContain('data-testid="agent-account-row"');
    expect(render({ accounts: undefined })).toContain("animate-pulse");
    const failed = render({ accounts: undefined, loadError: "Accounts are not available." });
    expect(failed).toContain('role="alert"');
    expect(failed).toContain("Accounts are not available.");
    expect(render({ accounts: [] })).toContain("No accounts yet.");
  });

  test("Add account opens a form with one field, and says the sign-in comes next", () => {
    expect(render()).not.toContain("<form");
    const html = render({ initialAddOpen: true });
    expect(html).toContain('aria-label="Add a Claude account"');
    expect(html).toContain("Account name");
    expect(html).toContain('maxLength="40"');
    expect(html).toContain("You sign in to the Claude account on the next step.");
    expect(html.match(/<input/g)?.length).toBeGreaterThanOrEqual(1);
    expect(render({ initialAddOpen: true, busyId: "add" })).toContain("Adding…");
  });

  test("the sentence Go refused with is shown as an alert", () => {
    const html = render({ accounts: [builtin, account()], error: "A session is using that account. Close it first." });
    expect(html).toContain('role="alert"');
    expect(html).toContain("A session is using that account. Close it first.");
  });

  test("an account being changed has its controls off", () => {
    const html = render({ accounts: [builtin, account()], busyId: "claude-work" });
    expect(html.match(/<button[^>]*disabled=""[^>]*aria-label="Remove"|aria-label="Remove"[^>]*disabled=""/g)?.length).toBe(1);
  });
});
