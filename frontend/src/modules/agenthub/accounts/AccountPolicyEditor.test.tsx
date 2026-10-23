import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { VendorPolicy } from "@/bridge/agentAccounts";
import { AccountPolicyEditor } from "./AccountPolicyEditor";
import { AccountRiskBody, AccountRiskDialog } from "./AccountRiskDialog";

const policy: VendorPolicy = {
  enabled: false,
  thresholdPercent: 90,
  weeklyThresholdPercent: 0,
  usageMaxAgeSeconds: 300,
  order: ["claude-work"],
  onLimitRefusal: "none",
};

type Props = Parameters<typeof AccountPolicyEditor>[0];
const render = (over: Partial<Props> = {}) =>
  renderToStaticMarkup(
    <AccountPolicyEditor
      vendorName="Claude"
      policy={policy}
      signedInCount={2}
      riskAcknowledged
      saving={false}
      error=""
      onSave={async () => true}
      {...over}
    />,
  );

describe("AccountPolicyEditor", () => {
  test("starts off, with the defaults in the fields and the truth about when switching happens", () => {
    const html = render();
    expect(html).toContain("Switching");
    expect(html).toContain("only at the start of a new turn");
    expect(html).toContain("DevKit never holds a token");
    expect(html).toContain('aria-checked="false"');
    expect(html).toContain('value="90"');
    expect(html).toContain('value="300"');
    expect(html.match(/<input[^>]*type="number"/g)?.length).toBe(3);
    expect(html).toContain('min="50"');
    expect(html).toContain('max="100"');
    expect(html).toContain('min="60"');
    expect(html).toContain('max="3600"');
    expect(html).toContain("at its next turn once its account passes 90% of a window.");
  });

  test("the refusal choices are plain, the default is first and the chosen one is explained", () => {
    const html = render();
    expect(html).toContain("Do not resend (default)");
    expect(html.indexOf("Do not resend (default)")).toBeLessThan(html.indexOf("Resend if it had not started"));
    expect(html).toContain("Send a continue message");
    expect(html).toContain("Your next message starts on the next account.");
    const resend = render({ policy: { ...policy, onLimitRefusal: "resendIfNoProgress" } });
    expect(resend).toMatch(/<option[^>]*value="resendIfNoProgress" selected=""/);
    expect(resend).toContain("sends the same message once on the next account");
  });

  test("Save is off until something differs", () => {
    expect(render()).toMatch(/<button[^>]*disabled=""[^>]*>Save</);
    expect(render({ saving: true })).toContain("Saving…");
  });

  test("on with fewer than two signed-in accounts says nothing can move yet", () => {
    const on = { ...policy, enabled: true };
    expect(render({ policy: on, signedInCount: 1 })).toContain("Nothing can move until at least two Claude accounts are signed in.");
    expect(render({ policy: on, signedInCount: 2 })).not.toContain("Nothing can move");
    expect(render({ policy, signedInCount: 0 })).not.toContain("Nothing can move");
  });

  test("a weekly threshold is shown when stored and explained", () => {
    const html = render({ policy: { ...policy, weeklyThresholdPercent: 95 } });
    expect(html).toContain('value="95"');
    expect(html).toContain("or 95% of a weekly one");
  });

  test("a refused save is an alert", () => {
    const html = render({ error: "The account switching rules changed. Refresh and apply your change again." });
    expect(html).toContain('role="alert"');
    expect(html).toContain("rules changed");
  });
});

describe("the terms warning", () => {
  test("says what switching does, that terms may restrict it, and asks for a tick", () => {
    const html = renderToStaticMarkup(<AccountRiskBody checked={false} onCheckedChange={() => {}} />);
    expect(html).toContain("only at the start of a new turn");
    expect(html).toContain("restrict how subscription logins may be used");
    expect(html).toContain("check each provider&#x27;s terms");
    expect(html).toContain('role="checkbox"');
    expect(html).toContain('aria-checked="false"');
    expect(renderToStaticMarkup(<AccountRiskBody checked onCheckedChange={() => {}} />)).toContain('aria-checked="true"');
  });

  test("the dialog names the vendor and cannot be confirmed before the tick", () => {
    const html = renderToStaticMarkup(
      <AccountRiskDialog open vendorName="Codex" onCancel={() => {}} onConfirm={() => {}} portal={false} />,
    );
    expect(html).toContain("Turn on account switching for Codex?");
    expect(html).toMatch(/<button[^>]*disabled=""[^>]*>Turn on</);
    expect(html).toContain("Cancel");
  });
});
