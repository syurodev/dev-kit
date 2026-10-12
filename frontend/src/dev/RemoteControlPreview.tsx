import { useMemo, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ThemeProvider } from "@/theme/ThemeProvider";
import { SettingsLayout } from "@/modules/settings/SettingsLayout";
import { TelegramSetting, type TelegramSettingsAPI } from "@/modules/settings/TelegramSetting";
import type { TelegramStatus } from "@/bridge/telegram";

/** Interactive setup fixture: no bridge, token storage, bot or network actions. */
export default function RemoteControlPreview() {
  const [version, setVersion] = useState(0);
  return <div className="min-h-screen bg-surface-base"><Button variant="ghost" onClick={() => setVersion((v) => v + 1)}>Reset setup preview</Button><Preview key={version} /></div>;
}
function Preview() {
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }));
  const api = useMemo<TelegramSettingsAPI>(() => {
    let status: TelegramStatus = { config: { enabled: false, accountId: "", workspaces: [], sendDetails: false, readTranscript: false, approveInteractions: false }, paired: false, userId: 0, candidateId: 0, candidateName: "", botName: "", error: "" };
    if (new URLSearchParams(window.location.search).has("paired")) {
      status = {...status, paired:true, userId:12345678, botName:"devkit_preview_bot", config:{...status.config,enabled:true,accountId:"preview-account",workspaces:["preview-workspace"],readTranscript:true}};
    }
    return {
      status: async () => structuredClone(status),
      configure: async (config, token) => { status = { ...status, config: { ...config, accountId: token ? "preview-account" : config.accountId }, botName: "devkit_preview_bot", candidateId: 0 }; },
      beginPair: async () => { status.candidateId = 12345678; status.candidateName = "alex_preview"; return "https://t.me/devkit_preview_bot?start=preview"; },
      acceptPair: async (id) => { status.paired = true; status.userId = id; status.candidateId = 0; },
      unpair: async () => { status.paired = false; status.userId = 0; },
      open: async () => {},
    };
  }, []);
  return <QueryClientProvider client={client}><ThemeProvider><Dialog open><DialogContent showCloseButton={false} aria-describedby={undefined} className="flex w-[calc(100vw-2rem)] max-w-235 flex-col gap-0 overflow-hidden p-0 text-text-primary sm:max-w-235">
    <div className="flex h-settings max-h-[90svh] flex-col"><div className="flex h-14 shrink-0 items-center border-b border-border-subtle px-6"><DialogTitle>Settings</DialogTitle></div>
      <SettingsLayout activeTab="remote" onSelect={() => {}}><div className="space-y-7"><div><h2 className="text-heading-24">Remote control</h2><p className="mt-2 text-body-14 text-text-tertiary">Connect your phone to this DevKit and choose what it can access.</p></div><TelegramSetting api={api} loadWorkspaces={async () => [{ id: "preview-workspace", name: "DevKit", root: "/Users/alex/Projects/devkit", instructions: "" }]} /></div></SettingsLayout>
    </div></DialogContent></Dialog></ThemeProvider></QueryClientProvider>;
}
