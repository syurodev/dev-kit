import { describe, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { TelegramSetting, type TelegramSettingsAPI } from "./TelegramSetting";
import type { TelegramStatus } from "@/bridge/telegram";

const BASE: TelegramStatus = { config: { enabled: false, accountId: "", workspaces: [], sendDetails: false, readTranscript: false, approveInteractions: false }, paired: false, userId: 0, candidateId: 0, candidateName: "", botName: "", error: "" };
function render(status: TelegramStatus) {
  const api: TelegramSettingsAPI = { status: async () => status, configure: async () => {}, beginPair: async () => "", acceptPair: async () => {}, unpair: async () => {}, open: async () => {} };
  const client = new QueryClient();
  client.setQueryData(["settings", "telegram", "preview"], status);
  client.setQueryData(["telegram", "workspaces", "preview"], [{ id: "private-workspace-id", name: "Payments", root: "/Projects/payments", instructions: "" }]);
  return renderToString(<QueryClientProvider client={client}><TelegramSetting api={api} /></QueryClientProvider>);
}
describe("Telegram setup", () => {
  test("starts with actionable BotFather steps before showing access or pairing", () => {
    const html = render(BASE);
    expect(html).toContain("1. Connect a bot"); expect(html).toContain("/newbot"); expect(html).toContain("Paste your bot token");
    expect(html).not.toContain("What Telegram can access"); expect(html).not.toContain("Create pairing link");
  });
  test("resumes saved-token setup at workspace access without exposing workspace IDs", () => {
    const html = render({ ...BASE, config: { ...BASE.config, accountId: "saved-token" } });
    expect(html).toContain("2. Choose access"); expect(html).toContain("Payments"); expect(html).toContain("/Projects/payments"); expect(html).not.toContain("private-workspace-id");
  });
  test("resumes pairing after access has been saved", () => {
    const html = render({ ...BASE, config: { ...BASE.config, enabled: true, accountId: "saved-token", workspaces: ["private-workspace-id"] } });
    expect(html).toContain("3. Pair your account"); expect(html).toContain("Create pairing link"); expect(html).not.toContain("Paste your bot token");
  });
  test("a paired but paused connection shows management rather than restarting setup", () => {
    const html = render({ ...BASE, paired: true, userId: 123, config: { ...BASE.config, accountId: "saved-token", workspaces: ["private-workspace-id"] } });
    expect(html).toContain("Paused"); expect(html).toContain("Unpair account"); expect(html).toContain("Save changes"); expect(html).not.toContain("Telegram setup progress");
  });
});
