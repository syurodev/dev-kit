import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { SettingsLayout } from "@/modules/settings/SettingsLayout";
import { SETTINGS_GROUPS, type SettingsPage } from "@/modules/settings/settingsNavigation";
import { VaultStage, VaultPasswordField } from "@/workbench/VaultStage";

/** Local layout fixtures. No vault, preference or authentication calls. */
export function VaultSettingsPreview() {
  const [settings, setSettings] = useState(false);
  const [vault, setVault] = useState(false);
  const [tab, setTab] = useState<SettingsPage>("appearance");
  const [step, setStep] = useState(false);
  const [password, setPassword] = useState("");
  const item = SETTINGS_GROUPS.flatMap((group) => [...group.items]).find((item) => item.id === tab)!;
  return <>
    <div className="flex flex-wrap gap-2"><Button variant="outline" onClick={() => setSettings(true)}>Preview settings layout</Button><Button variant="outline" onClick={() => setVault(true)}>Preview vault</Button></div>
    <Dialog open={settings} onOpenChange={setSettings}><DialogContent showCloseButton={false} aria-describedby={undefined} className="w-[calc(100vw-2rem)] max-w-235 gap-0 overflow-hidden p-0 sm:max-w-235">
      <div className="flex h-settings max-h-[90svh] flex-col"><div className="flex h-14 shrink-0 items-center justify-between border-b border-border-subtle px-5"><DialogTitle>Settings</DialogTitle><Button size="sm" variant="ghost" onClick={() => setSettings(false)}>Done</Button></div>
        <SettingsLayout activeTab={tab} onSelect={setTab}><h2 className="text-heading-24">{item.label}</h2><p className="mt-2 text-body-13 text-text-tertiary">Layout preview · no preferences are saved.</p><div className="mt-6 rounded-xl border border-border-subtle bg-surface-raised p-4 text-body-13 text-text-secondary">{item.keywords}</div></SettingsLayout>
      </div>
    </DialogContent></Dialog>
    <Dialog open={vault} onOpenChange={(value) => { setVault(value); if (!value) setPassword(""); }}><DialogContent showCloseButton={false} aria-describedby={undefined} className="border-0 bg-transparent p-0 shadow-none ring-0 sm:max-w-120 [&>div]:!block">
      <DialogTitle className="sr-only">Vault layout preview</DialogTitle>
      <VaultStage step={step ? "setup" : "unlock"} eyebrow="Vault" title={step ? "Create your vault" : "Unlock your vault"} body="Local layout preview. No password is checked or saved." footnote="Your password never leaves this device." setupStep={step ? 1 : undefined}>
        <form onSubmit={(event) => event.preventDefault()} className="space-y-4"><VaultPasswordField label="Master password" value={password} onChange={setPassword} placeholder={step ? "At least 8 characters" : "Enter master password"} creating={step} /><Button type="submit" className="h-11 w-full" disabled={!password}>{step ? "Create vault" : "Unlock vault"}</Button><Button variant="ghost" type="button" className="w-full" onClick={() => { setStep((value) => !value); setPassword(""); }}>Switch sample step</Button></form>
      </VaultStage>
    </DialogContent></Dialog>
  </>;
}
