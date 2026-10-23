import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { AccountPolicy, AgentAccount } from "@/bridge/agentAccounts";
import { AccountSignIn } from "./AccountSignIn";
import { AgentAccountsSetting } from "./AgentAccountsSetting";
import { AGENT_ACCOUNT_POLICY_KEY, agentAccountsKey } from "./useAgentAccounts";

const account = (over: Partial<AgentAccount>): AgentAccount => ({
  id: "claude-default",
  vendor: "claude",
  label: "System default",
  builtin: true,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
  ...over,
});

const policy: AccountPolicy = {
  revision: 2,
  riskAcknowledgedAt: "2026-10-23T08:00:00Z",
  vendors: {
    claude: {
      enabled: true,
      thresholdPercent: 85,
      weeklyThresholdPercent: 0,
      usageMaxAgeSeconds: 300,
      order: ["claude-work", "claude-default"],
      onLimitRefusal: "none",
    },
  },
};

function render(client: QueryClient) {
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AgentAccountsSetting vendor="claude" />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("AgentAccountsSetting", () => {
  test("draws the accounts in the saved order with the rules under them", () => {
    const client = new QueryClient();
    client.setQueryData(agentAccountsKey("claude"), [
      account({}),
      account({ id: "claude-work", label: "Work", builtin: false }),
      account({ id: "claude-spare", label: "Spare", builtin: false, signedIn: false }),
    ]);
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, policy);
    const html = render(client);
    const at = (id: string) => html.indexOf(`data-account="${id}"`);
    expect(at("claude-work")).toBeGreaterThan(-1);
    expect(at("claude-work")).toBeLessThan(at("claude-default"));
    expect(at("claude-default")).toBeLessThan(at("claude-spare"));
    expect(html).toContain("Switching");
    expect(html).toContain('value="85"');
    // The signed-out account carries Claude's own sign-in, with where it goes.
    expect(html).toContain('data-testid="claude-sign-in"');
    expect(html).toContain("this account&#x27;s own folder");
  });

  test("before the first answer both halves wait instead of showing an empty list", () => {
    const html = render(new QueryClient());
    expect(html).not.toContain('data-testid="agent-account-row"');
    expect(html).not.toContain("No accounts yet.");
    expect(html.match(/animate-pulse/g)?.length).toBe(2);
  });

  test("the exported component keys its body by vendor, so nothing outlives a vendor switch", () => {
    const claude = AgentAccountsSetting({ vendor: "claude" }) as unknown as { key: string | null; type: unknown; props: { vendor: string } };
    const codex = AgentAccountsSetting({ vendor: "codex" }) as unknown as { key: string | null; type: unknown; props: { vendor: string } };
    expect(claude.key).toBe("claude");
    expect(codex.key).toBe("codex");
    expect(claude.props.vendor).toBe("claude");
    expect(claude.type).toBe(codex.type);
    expect(claude.type).not.toBe(AgentAccountsSetting);
  });

  test("with every account signed in no sign-in is drawn", () => {
    const client = new QueryClient();
    client.setQueryData(agentAccountsKey("claude"), [account({}), account({ id: "claude-work", label: "Work", builtin: false })]);
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, policy);
    const html = render(client);
    expect(html).not.toContain("claude-sign-in");
    expect(html).not.toContain("codex-sign-in");
  });

  test("with no stored policy the form shows the defaults", () => {
    const client = new QueryClient();
    client.setQueryData(agentAccountsKey("claude"), [account({})]);
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, { revision: 0, riskAcknowledgedAt: "", vendors: {} } satisfies AccountPolicy);
    const html = render(client);
    expect(html).toContain('value="90"');
    expect(html).toContain('value="300"');
    expect(html).toContain('aria-checked="false"');
  });

  test("ids in the saved order that no longer exist do not hide or repeat an account", () => {
    const client = new QueryClient();
    client.setQueryData(agentAccountsKey("claude"), [account({}), account({ id: "claude-work", label: "Work", builtin: false })]);
    client.setQueryData(AGENT_ACCOUNT_POLICY_KEY, {
      ...policy,
      vendors: { claude: { ...policy.vendors.claude, order: ["claude-gone", "claude-work", "claude-gone", "claude-default", "claude-work"] } },
    } satisfies AccountPolicy);
    const html = render(client);
    expect(html.match(/data-account="claude-default"/g)?.length).toBe(1);
    expect(html.match(/data-account="claude-work"/g)?.length).toBe(1);
    expect(html).not.toContain("claude-gone");
  });
});

describe("AccountSignIn", () => {
  const draw = (vendor: string) =>
    renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <AccountSignIn account={account({ id: `${vendor}-spare`, vendor, signedIn: false, builtin: false })} />
      </QueryClientProvider>,
    );

  test("a codex account signs in inside DevKit", () => {
    const html = draw("codex");
    expect(html).toContain('data-testid="codex-sign-in"');
    expect(html).not.toContain("claude-sign-in");
  });

  test("a claude account gets Claude's sign-in with where it goes", () => {
    const html = draw("claude");
    expect(html).toContain('data-testid="claude-sign-in"');
    expect(html).toContain("this account&#x27;s own folder");
    expect(html).not.toContain("codex-sign-in");
  });
});
