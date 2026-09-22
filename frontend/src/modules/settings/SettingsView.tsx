import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createEnvTag,
  deleteEnvTag,
  listEnvTags,
  type EnvTag,
} from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";
import {
  authLogout,
  authStatus,
  syncConnect,
  syncNow,
  syncStatus,
} from "../../bridge/sync";
import { toast } from "sonner";
import { useTheme, type ResolvedTheme } from "@/theme/ThemeProvider";
import { FAMILIES } from "@/theme/families";
import type { ThemeFamilyPreference } from "@/bridge/preferences";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { SecurityNotice } from "@/components/ui/security-notice";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { McpInstallPanel } from "./McpInstallPanel";
import { ConsoleStorageSetting } from "./ConsoleStorageSetting";
import { AgentRuntimeSetting } from "./AgentRuntimeSetting";
import { AgentNotificationSetting } from "./AgentNotificationSetting";
import { CodexSessionsSetting } from "./CodexSessionsSetting";
import { RevealPasswordSetting } from "./RevealPasswordSetting";
import { AgentCheckpointSetting } from "./AgentCheckpointSetting";
import { AgentSandboxSetting } from "./AgentSandboxSetting";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  monoFontOptions,
  monoFontStack,
  useAppearanceStore,
  type MonoFont,
} from "@/stores/appearance";
import { TAB_WIDTH_LIMITS, useTabSettingsStore } from "@/stores/tabSettings";
import {
  User,
  Sun,
  Monitor,
  SlidersHorizontal,
  Laptop,
  LockKeyhole,
  Database,
  Bot,
  Command,
  Trash2,
  X,
  RefreshCw,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { chordLabel } from "@/lib/shortcut";
import { KeyboardTab } from "./KeyboardTab";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";

export type SettingsTab = "account" | "appearance" | "sync" | "environments" | "database" | "agents" | "keyboard";

export interface SettingsModalProps {
  open: boolean;
  onClose: () => void;
  /** The section to land on each time it opens — "Configure Tabs…" asks for Appearance. */
  initialTab?: SettingsTab;
}

/**
 * Figma `Settings Shell` (762:1759): 940×620. A 56px header with the title and
 * a 32px close, a hairline under it, a 220px nav with a hairline on its right,
 * and the section content 32 in from both.
 *
 * On Dialog, like every other modal: the modal glass tier, the shared scrim,
 * open and close animation, focus trapping and return, Esc and click-outside.
 * It used to draw all of that itself, on the float tier — the most translucent
 * fill in the app, under the largest surface in it.
 */
export function SettingsModal({ open, onClose, initialTab }: SettingsModalProps) {
  const [activeTab, setActiveTab] = useState<SettingsTab>(initialTab ?? "account");

  // On every opening, not only the first: the modal stays mounted while
  // closed, so a useState initial value would be read once and never again.
  useEffect(() => {
    if (open && initialTab) setActiveTab(initialTab);
  }, [open, initialTab]);

  return (
    <TooltipProvider>
      <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
        <DialogContent
          showCloseButton={false}
          aria-describedby={undefined}
          // Focus the panel, not its first control: that is the close button,
          // and focusing it opens its tooltip the moment Settings appears.
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            (event.currentTarget as HTMLElement | null)?.focus();
          }}
          className="flex w-full max-w-[calc(100%-2rem)] flex-col gap-0 overflow-hidden p-0 text-text-primary select-none sm:max-w-[940px]"
        >
          {/* The height lives here, not on DialogContent: the dialog's body
              is h-fit, so the nav and the scrolling content need a box of
              their own to fill. */}
          <div className="flex h-[min(620px,calc(100vh-2rem))] flex-col">
          {/* Modal Header · 56px */}
          <div className="flex h-14 w-full shrink-0 items-center justify-between border-b border-border-subtle pr-3 pl-6">
            <DialogTitle className="text-text-primary">Settings</DialogTitle>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={onClose}
                  aria-label="Close settings"
                  className="size-8 rounded-md"
                >
                  <X className="size-4" strokeWidth={1.5} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Close settings (Esc)</TooltipContent>
            </Tooltip>
          </div>

          {/* Modal Body: Nav (220px) + Content */}
          <div className="flex min-h-0 flex-1 overflow-hidden">
            {/* The nav has no fill of its own — a hairline separates it from
                the content and the glass shows through both. */}
            <div className="flex w-[220px] shrink-0 flex-col gap-1 border-r border-border-subtle px-3 py-4">
              <NavItem
                active={activeTab === "account"}
                icon={User}
                label="Account"
                onClick={() => setActiveTab("account")}
              />
              <NavItem
                active={activeTab === "appearance"}
                icon={Sun}
                label="Appearance"
                onClick={() => setActiveTab("appearance")}
              />
              <NavItem
                active={activeTab === "sync"}
                icon={Monitor}
                label="Sync & Devices"
                onClick={() => setActiveTab("sync")}
              />
              <NavItem
                active={activeTab === "environments"}
                icon={SlidersHorizontal}
                label="Environments"
                onClick={() => setActiveTab("environments")}
              />
              <NavItem
                active={activeTab === "database"}
                icon={Database}
                label="Database"
                onClick={() => setActiveTab("database")}
              />
              <NavItem
                active={activeTab === "agents"}
                icon={Bot}
                label="Agents"
                onClick={() => setActiveTab("agents")}
              />
              <NavItem
                active={activeTab === "keyboard"}
                icon={Command}
                label="Keyboard"
                onClick={() => setActiveTab("keyboard")}
              />
            </div>

            {/* Content Panel Area */}
            <div className="flex min-h-0 flex-1 flex-col overflow-y-auto p-8">
              {activeTab === "account" && <AccountTab />}
              {activeTab === "appearance" && <AppearanceTab />}
              {activeTab === "sync" && <SyncDevicesTab />}
              {activeTab === "environments" && <EnvironmentsTab />}
              {activeTab === "database" && <DatabaseTab />}
              {activeTab === "agents" && <AgentsTab />}
              {activeTab === "keyboard" && (
                <KeyboardTab
                  intro={
                    <SectionHeader
                      title="Keyboard"
                      description={`Every shortcut in DevKit. The module you are in is listed first. Press ${chordLabel({ mod: true, key: "/" })} anywhere for a quick reference.`}
                    />
                  }
                />
              )}
            </div>
          </div>
          </div>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}

export function SettingsView() {
  return <SettingsModal open={true} onClose={() => {}} />;
}

/** Settings Nav Item (761:1753 Active / 761:1758 Inactive): 32 tall, 12/10
 * padding-gap, radius 8, a 16px glyph. Active is surface/raised with a
 * subtle stroke and Body/13 Medium; inactive is bare, Body/13, secondary. */
function NavItem({
  active,
  icon: Icon,
  label,
  onClick,
}: {
  active: boolean;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-8 w-full items-center gap-3 rounded-md border px-3 text-left outline-none transition-colors",
        active
          ? "border-border-subtle bg-surface-raised text-body-14-medium text-text-primary"
          : "border-transparent text-body-14 text-text-secondary hover:bg-state-soft hover:text-text-primary",
      )}
    >
      <Icon className="size-4 shrink-0" strokeWidth={1.5} />
      <span className="truncate">{label}</span>
    </button>
  );
}

/** Every section opens the same way (765:2000–2003): Heading/24 (Figma draws
 * 22 SemiBold, snapped up to the scale), 6px, Body/13 tertiary, 28px. */
/**
 * The column every tab's content sits in: 656 wide, which is the Figma
 * shell's 940 less the 220 nav and the 32 gutters.
 *
 * Declared once because it was declared five times, and five copies of a
 * measurement is five chances for one of them to be 640 after somebody
 * nudges a tab.
 */
function TabColumn({ gap = 6, children }: { gap?: 6 | 7; children: React.ReactNode }) {
  return (
    <div className={cn("flex w-full max-w-[656px] flex-col", gap === 7 ? "gap-7" : "gap-6")}>
      {children}
    </div>
  );
}

function SectionHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <h2 className="text-heading-24 text-text-primary">{title}</h2>
      <p className="text-body-14 text-text-tertiary">{description}</p>
    </div>
  );
}

/** THEME / CODE & TERMINAL — Label/11 Caps, tertiary, 10px above its control. */
function GroupLabel({ children }: { children: React.ReactNode }) {
  return <p className="text-label-11-caps text-text-tertiary">{children}</p>;
}

// ----------------------------------------------------------------------
// 1. Account Tab — Figma `Settings Content / Account` (765:1999)
// ----------------------------------------------------------------------
function AccountTab() {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const { data: auth = { authenticated: false, expiresAt: "" } } = useQuery({
    queryKey: ["auth", "status"],
    queryFn: () => authStatus(),
  });

  const logoutMutation = useMutation({
    mutationFn: () => authLogout(),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["auth", "status"] });
    },
    onError: (e) => {
      setError(errorMessage(e));
    },
  });

  async function handleSignOut() {
    setError("");
    await logoutMutation.mutateAsync();
  }

  async function handleSignIn() {
    setBusy(true);
    setError("");
    try {
      await syncConnect();
      void queryClient.invalidateQueries({ queryKey: ["auth", "status"] });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <TabColumn gap={7}>
      <SectionHeader
        title="Account"
        description="Identity used for sync. Managed by your identity provider — not stored locally."
      />

      {/* identity (765:2004): a raised card — 40px avatar on surface/well,
          the address in Body/14 Medium, the session line in Code/12, and the
          sign-out as a destructive control on the right. */}
      <div className="flex items-center gap-4 rounded-control border border-border-subtle bg-surface-raised px-4 py-4">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-full border border-border-subtle bg-surface-well text-code-12-medium text-text-secondary select-none">
          SY
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-body-14-medium text-text-primary">
            {auth.authenticated ? "syuro.dev@gmail.com" : "Not signed in"}
          </span>
          <span className="text-code-12 text-text-tertiary">
            {auth.authenticated ? "Signed in · session active" : "Offline · local only"}
          </span>
        </div>

        {auth.authenticated ? (
          <Button
            type="button"
            variant="destructive"
            size="default"
            disabled={busy}
            onClick={handleSignOut}
          >
            {busy ? "Signing out…" : "Sign out"}
          </Button>
        ) : (
          <Button
            type="button"
            variant="default"
            size="default"
            disabled={busy}
            onClick={handleSignIn}
          >
            {busy ? "Connecting browser…" : "Sign in with browser"}
          </Button>
        )}
      </div>

      {error ? <p className="text-body-12 text-status-danger">{error}</p> : null}

      <div className="flex flex-col gap-3">
        <GroupLabel>SAVED CREDENTIALS</GroupLabel>
        <RevealPasswordSetting />
      </div>

      <SecurityNotice
        type="secure"
        title="Your password never leaves your browser"
        description="DevKit signs you in through your system browser using PKCE. The password is never sent to or stored by DevKit."
      />
    </TabColumn>
  );
}

// ----------------------------------------------------------------------
// 2. Appearance Tab — Figma `Settings Content / Appearance` (763:1792)
// ----------------------------------------------------------------------

/** stepper (763:1818): − · value · + in one 104×36 raised control. The
 * value is mono; the two buttons are sans, because they are buttons. */
function SizeStepper({
  value,
  onChange,
  min = 10,
  max = 24,
  step: increment = 1,
  label,
}: {
  value: number;
  onChange: (size: number) => void;
  min?: number;
  max?: number;
  step?: number;
  /** Names what the two buttons change, when a section has more than one stepper. */
  label?: string;
}) {
  function step(delta: number) {
    onChange(Math.min(max, Math.max(min, value + delta * increment)));
  }
  return (
    <div className="flex h-9 w-26 overflow-hidden rounded-md border border-border bg-surface-raised">
      <button
        type="button"
        aria-label={label ? `${label}: smaller` : "Smaller"}
        onClick={() => step(-1)}
        className="w-8 text-body-14 text-text-secondary hover:bg-state-soft hover:text-text-primary"
      >
        −
      </button>
      <span className="flex w-10 items-center justify-center text-code-12 text-text-primary">
        {value}
      </span>
      <button
        type="button"
        aria-label={label ? `${label}: larger` : "Larger"}
        onClick={() => step(1)}
        className="w-8 text-body-14 text-text-secondary hover:bg-state-soft hover:text-text-primary"
      >
        +
      </button>
    </div>
  );
}

function FontSettingRow({
  label,
  font,
  size,
  onFont,
  onSize,
}: {
  label: string;
  font: MonoFont;
  size: number;
  onFont: (font: MonoFont) => void;
  onSize: (size: number) => void;
}) {
  return (
    <div className="flex items-end gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <span className="text-label-12 text-text-secondary">{label}</span>
        <Select value={font} onValueChange={(value) => onFont(value as MonoFont)}>
          <SelectTrigger className="h-9 w-full border-border bg-surface-raised text-code-12">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {monoFontOptions.map((option) => (
              <SelectItem
                key={option}
                value={option}
                className="text-code-12"
                style={{ fontFamily: monoFontStack(option) }}
              >
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="text-label-12 text-text-secondary">Size</span>
        <SizeStepper value={size} onChange={onSize} />
      </div>
    </div>
  );
}

const THEME_OPTIONS = ["system", "light", "dark"] as const;

/**
 * A family, painted in its own colours.
 *
 * The family's tokens go on the swatch as inline custom properties, so every
 * utility inside it resolves to that family without anything being applied to
 * the document. Not the applied stylesheet's selector, which is keyed on
 * `:root` and matches only <html>; not an iframe either, which would be a
 * document per swatch to preview five palettes.
 *
 * It previews the mode you are actually in, because the tokens it does not set
 * are inherited — and the ones it sets are exactly what the app would set.
 */
function PaletteSwatch({ family, mode }: { family: ThemeFamilyPreference; mode: ResolvedTheme }) {
  return (
    <span
      style={FAMILIES[family][mode] as React.CSSProperties}
      className="flex h-12 items-end gap-1 rounded-sm border border-border-subtle bg-surface-base p-1.5"
    >
      <span className="h-full flex-1 rounded-xs bg-surface-raised" />
      <span className="h-full w-3 rounded-xs bg-action-primary" />
      <span className="h-full w-1.5 rounded-xs bg-status-info" />
      <span className="h-full w-1.5 rounded-xs bg-status-success" />
      <span className="h-full w-1.5 rounded-xs bg-status-danger" />
    </span>
  );
}

function AppearanceTab() {
  const { theme, setTheme, resolvedTheme, family, setFamily } = useTheme();
  const { codeFont, codeSize, terminalFont, terminalSize, setAppearance } = useAppearanceStore(
    useShallow((s) => ({
      codeFont: s.codeFont,
      codeSize: s.codeSize,
      terminalFont: s.terminalFont,
      terminalSize: s.terminalSize,
      setAppearance: s.setAppearance,
    }))
  );
  const { tabMinWidth, tabMaxWidth, setTabWidths } = useTabSettingsStore(
    useShallow((s) => ({ tabMinWidth: s.minWidth, tabMaxWidth: s.maxWidth, setTabWidths: s.setTabWidths })),
  );

  return (
    <TabColumn gap={7}>
      <SectionHeader
        title="Appearance"
        description="Theme follows your explicit choice, or the OS when set to System."
      />

      <div className="flex flex-col gap-3">
        <GroupLabel>Theme</GroupLabel>
        {/* segmented/theme (763:1799): a surface/well trough with 4px of
            padding; the chosen segment is raised with a subtle stroke. */}
        <div
          role="radiogroup"
          aria-label="Theme"
          className="flex w-fit gap-1 rounded-control border border-border-subtle bg-surface-well p-1"
        >
          {THEME_OPTIONS.map((option) => {
            const active = theme === option;
            return (
              <button
                key={option}
                type="button"
                role="radio"
                aria-checked={active}
                onClick={() => {
                  setTheme(option).catch((error: unknown) => {
                    toast.error(`Could not save the theme: ${errorMessage(error)}`);
                  });
                }}
                className={cn(
                  "h-8 rounded-sm border px-4.5 capitalize transition-colors",
                  active
                    ? "border-border-subtle bg-surface-raised text-body-14-medium text-text-primary"
                    : "border-transparent text-body-14 text-text-secondary hover:text-text-primary",
                )}
              >
                {option}
              </button>
            );
          })}
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <GroupLabel>Palette</GroupLabel>
        {/* Swatches, not a dropdown: a palette is the one setting where the
            value IS what it looks like, and a list of names makes you pick
            blind and then go look. Each card paints itself in the family it
            offers, in the mode currently resolved, by carrying the family's
            own attributes — the same two the applied stylesheet keys on. */}
        <div role="radiogroup" aria-label="Palette" className="flex flex-wrap gap-2">
          {Object.values(FAMILIES).map((definition) => {
            const active = family === definition.id;
            return (
              <button
                key={definition.id}
                type="button"
                role="radio"
                aria-checked={active}
                aria-label={definition.label}
                onClick={() => {
                  // Not `void`: setFamily applies the palette optimistically
                  // and puts the old one back if the save rejects, so a
                  // swallowed rejection shows up as the palette silently
                  // snapping back — which is exactly how a missing capability
                  // grant was reported, with nothing anywhere saying why.
                  setFamily(definition.id).catch((error: unknown) => {
                    toast.error(`Could not save the palette: ${errorMessage(error)}`);
                  });
                }}
                className={cn(
                  "flex w-33 flex-col gap-2 rounded-control border p-2 text-left transition-colors",
                  active
                    ? "border-border-focus bg-surface-raised"
                    : "border-border-subtle hover:bg-state-soft",
                )}
              >
                <PaletteSwatch family={definition.id} mode={resolvedTheme} />
                <span
                  className={cn(
                    "px-0.5",
                    active ? "text-body-12-medium text-text-primary" : "text-body-12 text-text-secondary",
                  )}
                >
                  {definition.label}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <GroupLabel>Code &amp; terminal</GroupLabel>
        <div className="flex flex-col gap-4">
          <FontSettingRow
            label="Code font"
            font={codeFont}
            size={codeSize}
            onFont={(font) => setAppearance({ codeFont: font })}
            onSize={(size) => setAppearance({ codeSize: size })}
          />
          <FontSettingRow
            label="Terminal font"
            font={terminalFont}
            size={terminalSize}
            onFont={(font) => setAppearance({ terminalFont: font })}
            onSize={(size) => setAppearance({ terminalSize: size })}
          />
        </div>
        {/* preview (763:1843): the chosen face at the chosen size, on a well. */}
        <p
          className="mt-1.5 rounded-md border border-border-subtle bg-surface-well px-4 py-3 text-text-secondary"
          style={{ fontFamily: monoFontStack(codeFont), fontSize: codeSize }}
        >
          {`const vault = unlock(masterKey); // ${codeSize}px preview`}
        </p>
      </div>

      {/* Where the tab strip's "Configure Tabs…" lands. */}
      <div className="flex flex-col gap-3">
        <GroupLabel>Tabs</GroupLabel>
        <div className="flex items-end gap-4">
          <div className="flex flex-col gap-1.5">
            <span className="text-label-12 text-text-secondary">Minimum width</span>
            <SizeStepper
              value={tabMinWidth}
              min={TAB_WIDTH_LIMITS.min}
              max={TAB_WIDTH_LIMITS.max}
              step={10}
              label="Minimum tab width"
              onChange={(minWidth) => setTabWidths({ minWidth })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-label-12 text-text-secondary">Maximum width</span>
            <SizeStepper
              value={tabMaxWidth}
              min={TAB_WIDTH_LIMITS.min}
              max={TAB_WIDTH_LIMITS.max}
              step={10}
              label="Maximum tab width"
              onChange={(maxWidth) => setTabWidths({ maxWidth })}
            />
          </div>
        </div>
        <p className="text-body-12 text-text-tertiary">
          Tabs share the strip equally between these widths. Past the minimum, the strip scrolls.
        </p>
      </div>
    </TabColumn>
  );
}

// ----------------------------------------------------------------------
// 3. Sync & Devices Tab — Figma `Settings Content / Sync & Devices` (765:2019)
// ----------------------------------------------------------------------

/** device (765:2024): one raised card per device — 18px glyph, the name in
 * Body/14 Medium, the last-seen line in Code/12, and Revoke as a destructive
 * control. This device's Revoke is drawn but disabled at 45%. */
function DeviceRow({
  icon: Icon,
  name,
  detail,
  current,
}: {
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  name: string;
  detail: string;
  current?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 rounded-control border border-border-subtle bg-surface-raised px-4 py-3">
      <Icon className="size-4.5 shrink-0 text-text-secondary" strokeWidth={1.5} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-body-14-medium text-text-primary">{name}</span>
        <span className="text-code-12 text-text-tertiary">{detail}</span>
      </div>
      <Button
        type="button"
        variant="destructive"
        size="default"
        disabled={current}
        className={cn(current && "opacity-45")}
      >
        Revoke
      </Button>
    </div>
  );
}

function SyncDevicesTab() {
  const queryClient = useQueryClient();
  const [isSyncing, setIsSyncing] = useState(false);
  const [error, setError] = useState("");

  useQuery({
    queryKey: ["sync", "status"],
    queryFn: () => syncStatus(),
  });

  async function handleSyncNow() {
    setIsSyncing(true);
    setError("");
    try {
      await syncNow();
      void queryClient.invalidateQueries({ queryKey: ["sync", "status"] });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setIsSyncing(false);
    }
  }

  return (
    <TabColumn gap={7}>
      <div className="flex items-start justify-between gap-4">
        <SectionHeader
          title="Sync & Devices"
          description="Devices with access to your synced vault. Revoking a device blocks it until it signs in again."
        />
        {/* Not in the Figma section, kept: it is the one way to force a sync
            from here, and dropping a working action to match a mock is a
            regression dressed as fidelity. */}
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={isSyncing}
          onClick={handleSyncNow}
          className="shrink-0"
        >
          <RefreshCw className={cn("size-3.5", isSyncing && "animate-spin")} strokeWidth={1.5} />
          Sync now
        </Button>
      </div>

      {/* ponytail: three hard-coded devices until GET /v1/sync/devices is
          wired to the desktop bridge (implementation-status.mdx lists device
          management UI as still to do). */}
      <div className="flex flex-col gap-2">
        <DeviceRow icon={Laptop} name="MacBook Pro" detail="This device · active now" current />
        <DeviceRow icon={Monitor} name="Mac mini (office)" detail="Last active 2 days ago" />
        <DeviceRow icon={Monitor} name="Studio setup" detail="Last active 19 days ago" />
      </div>

      {error ? <p className="text-body-12 text-status-danger">{error}</p> : null}

      <SecurityNotice
        type="irreversible"
        title="A revoked device is blocked immediately"
        description="It is denylisted right away and must sign in again before it can push or pull."
      />
    </TabColumn>
  );
}

// ----------------------------------------------------------------------
// Database Tab — where the module keeps what it writes to disk
// ----------------------------------------------------------------------
function DatabaseTab() {
  return (
    <TabColumn>
      <SectionHeader
        title="Database"
        description="Where the Database module keeps the files it writes. Connections and saved queries are not files — they live in the vault and sync."
      />
      <div className="flex flex-col gap-3">
        <GroupLabel>QUERY CONSOLES</GroupLabel>
        <ConsoleStorageSetting />
      </div>
    </TabColumn>
  );
}

function AgentsTab() {
  return (
    <TabColumn>
      <SectionHeader
        title="Agents"
        description="The agents Agent Hub runs. DevKit installs each into its own folder — never on your PATH — at a version it has tested."
      />
      <div className="flex flex-col gap-3">
        <GroupLabel>CLAUDE AGENT</GroupLabel>
        <AgentRuntimeSetting />
      </div>
      <div className="flex flex-col gap-3">
        <GroupLabel>FILE CHECKPOINTS</GroupLabel>
        <AgentCheckpointSetting />
      </div>
      <div className="flex flex-col gap-3">
        <GroupLabel>NOTIFICATIONS</GroupLabel>
        <AgentNotificationSetting />
      </div>
      <div className="flex flex-col gap-3">
        <GroupLabel>CODEX (OBSERVED)</GroupLabel>
        <CodexSessionsSetting />
      </div>
      <div className="flex flex-col gap-3">
        <GroupLabel>SANDBOX</GroupLabel>
        <AgentSandboxSetting />
      </div>
    </TabColumn>
  );
}

// ----------------------------------------------------------------------
// 4. Environments Tab — Figma `Settings Content / Environments` (766:1999)
// ----------------------------------------------------------------------
function EnvironmentsTab() {
  const queryClient = useQueryClient();
  const [newOpen, setNewOpen] = useState(false);
  const [newName, setNewName] = useState("");
  const [newRequiresConfirmation, setNewRequiresConfirmation] = useState(false);
  const [busy, setBusy] = useState(false);
  const [mutationError, setMutationError] = useState("");

  const { data: tags = EMPTY_ARRAY, error: queryError } = useQuery({
    queryKey: ["environments", "tags"],
    queryFn: () => listEnvTags(),
  });

  const error = mutationError || (queryError ? errorMessage(queryError) : "");

  async function handleCreateTag(e?: React.FormEvent) {
    if (e) e.preventDefault();
    if (!newName.trim()) return;

    setBusy(true);
    setMutationError("");
    try {
      await createEnvTag(newName.trim().toLowerCase(), newRequiresConfirmation);
      setNewName("");
      setNewRequiresConfirmation(false);
      setNewOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["environments", "tags"] });
    } catch (e) {
      setMutationError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function handleDeleteTag(tag: EnvTag) {
    setMutationError("");
    try {
      await deleteEnvTag(tag.id);
      void queryClient.invalidateQueries({ queryKey: ["environments", "tags"] });
    } catch (e) {
      setMutationError(errorMessage(e));
    }
  }

  return (
    <TabColumn>
      <SectionHeader
        title="Environments"
        description="Env tags required by SSH, Database and Kafka profiles. prod is locked and always requires confirmation."
      />

      {/* tag/* (766:2004): one raised card per tag — the name in Code/12
          Medium, a danger-soft "requires confirmation" chip where it applies,
          and the delete glyph on the right for tags that are not built in. */}
      <div className="flex flex-col gap-2">
        {tags.map((tag) => (
          <div
            key={tag.id}
            className="flex items-center gap-3 rounded-control border border-border-subtle bg-surface-raised py-3 pr-3 pl-3.5"
          >
            <span className="text-code-12-medium text-text-primary">{tag.name}</span>
            <span className="flex-1" />
            {tag.requiresConfirmation ? (
              <span className="flex h-6 items-center gap-1.5 rounded-sm border border-(--border-danger) bg-(--feedback-danger-soft) pr-2.5 pl-2 text-code-11 text-status-danger">
                <LockKeyhole className="size-3.5" strokeWidth={1.5} />
                requires confirmation
              </span>
            ) : null}
            {tag.isBuiltin ? null : (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => void handleDeleteTag(tag)}
                    aria-label={`Delete ${tag.name}`}
                  >
                    <Trash2 className="size-4 text-text-tertiary hover:text-status-danger" strokeWidth={1.5} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Delete environment tag</TooltipContent>
              </Tooltip>
            )}
          </div>
        ))}
      </div>

      <div>
        <Button
          type="button"
          variant="outline"
          size="default"
          className="bg-surface-raised"
          onClick={() => setNewOpen(true)}
        >
          New tag
        </Button>
      </div>

      {error ? <p className="text-body-12 text-status-danger">{error}</p> : null}

      {/* Settings / New Tag Dialog (766:2045): a dialog over the shell, not a
          form unfolding inside the list. */}
      <Dialog open={newOpen} onOpenChange={setNewOpen}>
        <DialogContent className="max-w-114 px-6 pt-5.5 pb-5 sm:max-w-114">
          <form onSubmit={handleCreateTag} className="flex flex-col">
            <DialogHeader>
              <DialogTitle className="text-title-16">New environment tag</DialogTitle>
            </DialogHeader>
            <div className="mt-5 flex flex-col gap-1.5">
              <label className="text-label-12 text-text-secondary">Tag name</label>
              <Input
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="e.g. canary"
                autoFocus
                required
                className="h-9 border-border bg-surface-well text-code-12"
              />
              <span className="mt-0.5 text-body-12 text-text-tertiary">
                Lowercase, no spaces. Must be unique.
              </span>
            </div>
            <label className="mt-4.5 flex cursor-pointer items-center gap-3">
              <Checkbox
                checked={newRequiresConfirmation}
                onCheckedChange={(checked) => setNewRequiresConfirmation(Boolean(checked))}
              />
              <span className="text-body-14 text-text-secondary">
                Requires confirmation before MCP tools can use this tag
              </span>
            </label>
            <DialogFooter className="mt-5.5 gap-2">
              <Button type="button" variant="outline" className="bg-surface-raised" onClick={() => setNewOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={busy || !newName.trim()}>
                {busy ? "Creating…" : "Create tag"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* MCP Proxy Install section */}
      <div className="border-t border-border-subtle pt-4">
        <McpInstallPanel />
      </div>
    </TabColumn>
  );
}
