import { useState, useEffect, useRef, type ReactNode } from "react";
import { TreeNextSection } from "./TreeNextGallery";
import { AgentHubTranscriptSection } from "./AgentHubTranscriptSection";
import { CodeEditorWrapSection } from "./CodeEditorWrapSection";
import { CodeIntelSection } from "./CodeIntelSection";
import {
  Activity,
  FileText,
  MessageSquare,
  PanelBottom,
  PanelLeft,
  Plus,
  Search,
  Settings as SettingsIcon,
  Terminal,
  Wrench,
  X,
} from "lucide-react";
import { TabStrip } from "../workbench/tabs/TabStrip";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import { ScrollRuler, useScrollRulerMetrics } from "@/components/ui/scroll-ruler";
import { Titlebar } from "@/components/ui/titlebar";
import { ToastSpecCard, Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import { CommandPaletteStaticCard } from "@/workbench/CommandPalette";
import { ShellMenuItemSpec } from "@/components/ui/dropdown-menu";
import {
  SidebarHeader,
  SidebarRow,
  SidebarSearch,
  SidebarSection,
} from "@/components/ui/sidebar-shell";

import { Button } from "@/components/ui/button";
import {
  CraftButton,
  CraftButtonIcon,
  CraftButtonLabel,
} from "@/components/ui/craft-button";
import { Tag } from "@/components/ui/tag";
import { TagInput } from "@/components/ui/tag-input";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsCount,
} from "@/components/ui/tabs";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Alert, AlertTitle, AlertDescription, AlertContent } from "@/components/ui/alert";
import { StepProgress } from "@/components/ui/step-progress";
import { LoadingStatus } from "@/components/ui/loading-status";
import { RecoveryKeyField } from "@/components/ui/recovery-key-field";
import { SecurityNotice } from "@/components/ui/security-notice";


function ScrollRulerGalleryDemo({ height = 400 }: { height?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const metrics = useScrollRulerMetrics(ref);
  return (
    <div
      id="measure-scroll-ruler-frame"
      className="relative flex overflow-hidden rounded-lg border border-border-subtle bg-surface-base"
      style={{ height, width: 320 }}
    >
      <div ref={ref} className="flex-1 overflow-auto p-4">
        {Array.from({ length: 40 }, (_, i) => (
          <p key={i} className="text-body-13 text-text-secondary">
            Row {i + 1} — scroll to move the position tick.
          </p>
        ))}
      </div>
      <ScrollRuler
        id="measure-scroll-ruler"
        {...metrics}
        milestones={[0.2, 0.55, 0.85]}
        className="absolute inset-y-2 right-1"
      />
    </div>
  );
}

function TagInputGalleryDemo() {
  const [tags, setTags] = useState(["design", "tokens", "reconcile"]);
  return (
    <TagInput
      value={tags}
      onChange={setTags}
      placeholder="Type and press Enter..."
      tagDotColor={(tag) =>
        tag === "design" ? "#c6ff34" : tag === "tokens" ? "#99e1d9" : undefined
      }
    />
  );
}

/**
 * A browser-reachable gallery of the shared components, so they can be looked
 * at without the desktop app.
 *
 * `App.tsx` wraps everything in `VaultGate`, which calls `vaultStatus()` over
 * the Wails bridge. In a plain browser that never resolves, so the workbench and
 * every component in it is unreachable behind "Checking secure storage" — which
 * leaves `renderToString` + `toContain` as the only check, and string assertions
 * cannot see overlap, overflow or an invisible element.
 *
 * Reached at `?gallery=1`, mounted before the gate, lazy-loaded so it stays out
 * of the initial bundle. Bridge-backed data is unavailable here by design —
 * every entry renders from static props, which is also what makes it a fair
 * comparison against a Figma node.
 *
 * Extend it: one `<Spec>` per component you reconcile, labelled with the Figma
 * node id you actually opened.
 */
export default function ComponentGallery() {
  return (
    // h-full + its own scroller, not min-h-screen: the document no longer
    // scrolls (see index.css), so a page that relied on it would be stuck
    // showing only its first screen.
    <div className="h-full overflow-y-auto bg-background-app text-text-primary">
      <header className="border-b border-border-subtle px-6 py-4">
        <h1 className="text-title-16">DevKit component gallery</h1>
        <p className="text-body-12 text-text-secondary">
          Static props only — the Wails bridge is not available in a browser.
          Compare each entry against the Figma node in its label.
        </p>
        <div className="mt-3 flex flex-wrap gap-2">
          {(["info", "success", "warning", "error", "loading"] as const).map((tone) => (
            <Button key={tone} variant="secondary" onClick={() => toast[tone]("Theme-aware notification", {
              id: "gallery-live-toast",
              description: "Geist typography, shared spacing and the active palette.",
              duration: Infinity,
              closeButton: true,
              action: { label: "Done", onClick: () => {} },
            })}>
              Toast {tone}
            </Button>
          ))}
        </div>
      </header>
      <Toaster />

      <Section title="03 Button & Icon Button">
        <Spec node="27:54" label="Button — Size=default, 112×32, r=8, label 13 Medium">
          <div className="flex flex-wrap items-center gap-3">
            <Button>Continue</Button>
            <Button variant="secondary">Secondary</Button>
            <Button variant="outline">Outline</Button>
            <Button variant="ghost">Ghost</Button>
            <Button variant="destructive">Destructive</Button>
            <Button disabled>Disabled</Button>
          </div>
        </Spec>

        <Spec node="27:103 / 27:109" label="Size=LG — 40 tall, r=10">
          <div className="flex flex-wrap items-center gap-3">
            <Button size="lg">Primary LG</Button>
            <Button size="lg" variant="secondary">
              Secondary LG
            </Button>
          </div>
        </Spec>

        <Spec node="—" label="Glass — a variant: material only, height and radius from size">
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="glass" size="sm">
              Small
            </Button>
            <Button variant="glass">Continue</Button>
            <Button variant="glass" size="lg">
              Large
            </Button>
          </div>
        </Spec>

        <Spec node="—" label="Craft — a component: the icon's wash fills the pill and the label follows">
          <div className="flex flex-wrap items-center gap-3">
            <CraftButton>
              <CraftButtonIcon>
                <Plus className="size-3" />
              </CraftButtonIcon>
              <CraftButtonLabel>New connection</CraftButtonLabel>
            </CraftButton>
          </div>
        </Spec>

        <Spec node="27:85" label="Icon Button — Button at size=icon. Neutral is variant=secondary, Primary is variant=primary">
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="secondary" size="icon" aria-label="Neutral icon button">
              <Plus className="size-4" />
            </Button>
            <Button variant="primary" size="icon" aria-label="Primary icon button">
              <Plus className="size-4" />
            </Button>
            <Button variant="primary" size="icon" disabled aria-label="Disabled primary">
              <Plus className="size-4" />
            </Button>
          </div>
        </Spec>
      </Section>

      <Section title="04 Input & Selection">
        <Spec node="378:2674" label="Tag / Chip — 74×20, r=6, text-caption-11-medium">
          <div className="flex flex-wrap items-center gap-3">
            <Tag>backend</Tag>
            <Tag dotColor="#c6ff34">With dot</Tag>
            <Tag onRemove={() => {}}>Dismissible</Tag>
            <Tag dotColor="#99e1d9" onRemove={() => {}}>Full chip</Tag>
          </div>
        </Spec>

        <Spec node="378:2698" label="Tag Input — 320×32, r=8, Default & Focus states">
          <div className="flex max-w-[320px] flex-col gap-3">
            <TagInputGalleryDemo />
            <TagInput
              value={["read-only", "locked"]}
              onChange={() => {}}
              disabled
            />
          </div>
        </Spec>

        <Spec node="28:62" label="Input — Field 360×40, r=8; Type=Password has a reveal toggle">
          <div className="flex max-w-[360px] flex-col gap-4">
            <Input id="measure-input-rest" placeholder="Enter value" />
            <Input id="measure-input-password" type="password" placeholder="Master password" defaultValue="secret123" />
            <Input
              id="measure-input-labeled"
              label={<span id="measure-input-label">Account Name</span>}
              placeholder="e.g. production-db"
              helperText={<span id="measure-input-helper">Must be unique across your workspace</span>}
            />
            <Input
              id="measure-input-error"
              label="Recovery Email"
              defaultValue="invalid-email"
              error="Please enter a valid email address"
            />
            <Input id="measure-input-disabled" placeholder="Disabled input" disabled />
          </div>
        </Spec>

        <Spec node="28:78" label="Checkbox — Control 18×18, r=6, stroke 1.5; row 220×32">
          <div className="flex flex-wrap items-center gap-6">
            <Checkbox id="measure-checkbox-unchecked" />
            <Checkbox id="measure-checkbox-checked" defaultChecked />
            <Checkbox id="measure-checkbox-disabled" disabled />
            <div id="measure-checkbox-row-container">
              <Checkbox id="measure-checkbox-row" label="Remember this choice" defaultChecked />
            </div>
            <Checkbox label="Disabled option" disabled />
          </div>
        </Spec>

        <Spec node="570:59" label="Switch — 32×18, r=full; Off bg-white/6, On #c6ff34">
          <div className="flex items-center gap-6">
            <Switch id="measure-switch-off" aria-label="Toggle off" />
            <Switch id="measure-switch-on" defaultChecked aria-label="Toggle on" />
            <Switch id="measure-switch-disabled" disabled aria-label="Disabled off" />
            <Switch disabled defaultChecked aria-label="Disabled on" />
          </div>
        </Spec>

        <Spec node="378:61" label="Select / Trigger — 220×33 (snapped to 32, r=8), chevron 13px, border-status-info/16">
          <div className="flex flex-wrap items-center gap-4">
            <Select defaultValue="postgres">
              <SelectTrigger id="measure-select-trigger">
                <SelectValue placeholder="Select vendor" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="postgres">PostgreSQL</SelectItem>
                <SelectItem value="mysql">MySQL</SelectItem>
                <SelectItem value="sqlite">SQLite</SelectItem>
              </SelectContent>
            </Select>
            <Select disabled>
              <SelectTrigger id="measure-select-trigger-disabled">
                <SelectValue placeholder="Disabled trigger" />
              </SelectTrigger>
            </Select>
          </div>
        </Spec>

        <Spec node="378:70 / 378:71" label="Select / Menu & Option — Menu 220×142 r=10 glass-modal; Option 32px r=6 Selected adds bg-white/6 + check">
          <div id="measure-select-menu" className="flex w-[220px] flex-col rounded-control border border-glass-border bg-surface-glass-modal p-1.5 shadow-2xl backdrop-blur-xl">
            <div id="measure-select-option-selected" className="flex h-8 w-full items-center justify-between rounded-sm bg-white/6 px-2.5 py-1.5 text-body-13 text-text-primary">
              <span>Production Replica</span>
              <span className="text-text-primary">✓</span>
            </div>
            <div id="measure-select-option-unselected" className="flex h-8 w-full items-center justify-between rounded-sm bg-transparent px-2.5 py-1.5 text-body-13 text-text-primary hover:bg-white/4">
              <span>Staging Cluster</span>
            </div>
            <div className="flex h-8 w-full items-center justify-between rounded-sm bg-transparent px-2.5 py-1.5 text-body-13 text-text-primary hover:bg-white/4">
              <span>Local Development</span>
            </div>
          </div>
        </Spec>

        <Spec node="318:385" label="Tabs / List — Default 300×32 r=8 bg-black/25; Line no fill, gap 4">
          <div className="flex flex-col gap-4">
            <Tabs defaultValue="all">
              <TabsList id="measure-tabs-list-default" variant="default">
                <TabsTrigger value="all">
                  All <TabsCount>12</TabsCount>
                </TabsTrigger>
                <TabsTrigger value="active">Active</TabsTrigger>
                <TabsTrigger value="archived">Archived</TabsTrigger>
              </TabsList>
            </Tabs>
            <Tabs defaultValue="overview">
              <TabsList id="measure-tabs-list-line" variant="line">
                <TabsTrigger value="overview">Overview</TabsTrigger>
                <TabsTrigger value="runs">
                  Runs <TabsCount>3</TabsCount>
                </TabsTrigger>
                <TabsTrigger value="settings">Settings</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
        </Spec>

        <Spec node="317:380" label="Tabs / Trigger — Default+Active bg-white/10 + hairline; Line+Active rule 36×2 text/primary (not lime!)">
          <div className="flex flex-col gap-4">
            <div className="flex items-center gap-2">
              <div id="measure-tabs-trigger-default-active" className="inline-flex h-6 items-center gap-1.5 rounded-sm border border-white/8 bg-white/10 px-2 py-0.5 text-body-13 text-text-primary">
                <span>Default Active</span>
                <span id="measure-tabs-count" className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-white/6 px-1 text-caption-11-medium text-text-tertiary">2</span>
              </div>
              <div id="measure-tabs-trigger-default-inactive" className="inline-flex h-6 items-center gap-1.5 rounded-sm px-2 py-0.5 text-body-13 text-text-tertiary">
                <span>Default Inactive</span>
              </div>
            </div>
            <div className="flex items-center gap-4">
              <div id="measure-tabs-trigger-line-active" className="relative inline-flex h-6 items-center gap-1.5 px-2 py-0.5 text-body-13 text-text-primary after:absolute after:bottom-0 after:inset-x-0 after:h-0.5 after:bg-text-primary">
                <span>Line Active</span>
                <span className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-white/6 px-1 text-caption-11-medium text-text-tertiary">5</span>
              </div>
              <div id="measure-tabs-trigger-line-inactive" className="inline-flex h-6 items-center gap-1.5 px-2 py-0.5 text-body-13 text-text-tertiary">
                <span>Line Inactive</span>
              </div>
            </div>
          </div>
        </Spec>

        <Spec node="28:79" label="Tooltip — 132×32 r=8, fill #1d1d1d@72%, stroke #ffffff@10%, text 12 Medium">
          <div className="flex flex-wrap items-center gap-6">
            <div id="measure-tooltip" className="inline-flex h-8 items-center gap-1.5 rounded-md border border-glass-border bg-surface-glass-floating px-3 text-label-12 text-text-primary shadow-xl backdrop-blur-md">
              <span>View branch history</span>
            </div>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="outline" size="sm">Hover for tooltip</Button>
                </TooltipTrigger>
                <TooltipContent>
                  <span>Quick action spec</span>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </Spec>

        <Step2MeasurementsInspector />
      </Section>

      <Section title="05 Feedback & Progress">
        <Spec node="29:39" label="Alert — 4 Types: Info 29:6, Success 29:13, Warning 29:21, Error 29:30">
          <div className="flex flex-col gap-4">
            <Alert id="measure-alert-info" variant="info">
              <AlertContent>
                <AlertTitle id="measure-alert-title">System Information</AlertTitle>
                <AlertDescription id="measure-alert-desc">Vault backup completed successfully to encrypted storage.</AlertDescription>
              </AlertContent>
            </Alert>
            <Alert id="measure-alert-success" variant="success">
              <AlertContent>
                <AlertTitle>Encryption Verified</AlertTitle>
                <AlertDescription>All active sessions are secured with AES-GCM-256.</AlertDescription>
              </AlertContent>
            </Alert>
            <Alert id="measure-alert-warning" variant="warning">
              <AlertContent>
                <AlertTitle>Emergency Access</AlertTitle>
                <AlertDescription>Recovery mode requires the 29-character offline key.</AlertDescription>
              </AlertContent>
            </Alert>
            <Alert id="measure-alert-error" variant="error">
              <AlertContent>
                <AlertTitle>Authentication Failed</AlertTitle>
                <AlertDescription>Invalid passphrase provided. 3 attempts remaining.</AlertDescription>
              </AlertContent>
            </Alert>
          </div>
        </Spec>

        <Spec node="29:76" label="Step Progress — 4 Steps: 29:40, 29:49, 29:58, 29:67 (600px frame, fixed centres)">
          <div className="flex flex-col gap-6" id="measure-step-progress">
            <StepProgress currentStep={1} />
            <StepProgress currentStep={2} />
            <StepProgress currentStep={3} />
            <StepProgress currentStep={4} />
          </div>
        </Spec>

        <Spec node="29:77" label="Loading / Vault status — 320×48, r=12, grey spinner #c6c8c2">
          <div className="flex flex-wrap items-center gap-6">
            <LoadingStatus id="measure-loading-status" text="Checking secure storage status…" />
            <LoadingStatus text="Deriving argon2id keys…" />
          </div>
        </Spec>
      </Section>

      <Section title="06 Security Patterns">
        <Spec node="30:51" label="Recovery Key Field — 3 States: Hidden 30:6, Revealed 30:21, Copied 30:36">
          <div className="flex flex-col gap-6">
            <div id="measure-recovery-hidden">
              <RecoveryKeyField
                recoveryKey="7A3B-9F2K-M4P8-X1C5-D6E9-W2H4"
                defaultRevealed={false}
              />
            </div>
            <div id="measure-recovery-revealed">
              <RecoveryKeyField
                recoveryKey="7A3B-9F2K-M4P8-X1C5-D6E9-W2H4"
                defaultRevealed={true}
              />
            </div>
          </div>
        </Spec>

        <Spec node="30:68" label="Security Notice — 2 Types: Secure 30:52 (72px), Irreversible 30:59 (84px)">
          <div className="flex flex-col gap-4">
            <SecurityNotice id="measure-security-secure" type="secure" />
            <SecurityNotice id="measure-security-irreversible" type="irreversible" />
          </div>
        </Spec>


        <Step3MeasurementsInspector />
      </Section>

      <Section title="07 App Shell — chrome">
        <Spec node="732:2170" label="Titlebar — 1440×40, no material, Windowed (88px offset) vs Fullscreen (16px)">
          <div className="flex flex-col gap-4 w-full bg-background-app p-2 rounded-lg border border-border-subtle">
            <div className="text-caption-11 text-text-tertiary">Windowed Mode (traffic lights reserved at left, x=88 sidebar toggle):</div>
            <Titlebar id="measure-titlebar-windowed" className="w-full">
              <div className="mx-1 size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft">
                <PanelLeft className="size-4 text-text-secondary" />
              </div>
              <div className="flex h-full min-w-0 flex-1 items-center gap-1">
                <div id="measure-module-actions" className="flex min-w-0 items-center gap-1">
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft">
                    <FileText className="size-4 text-text-secondary" />
                  </div>
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft">
                    <Terminal className="size-4 text-text-secondary" />
                  </div>
                </div>
                <div className="min-w-0 flex-1 select-none truncate text-caption-11 text-text-tertiary">
                  dev-kit-app
                </div>
                <div id="measure-app-actions" className="flex shrink-0 items-center gap-1">
                  <div id="measure-system-status-neutral-spec" className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft text-text-secondary">
                    <Activity className="size-4" />
                  </div>
                  <span className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden />
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft text-text-secondary">
                    <Search className="size-4" />
                  </div>
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft text-text-secondary">
                    <SettingsIcon className="size-4" />
                  </div>
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft text-text-secondary">
                    <PanelBottom className="size-4" />
                  </div>
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft text-text-secondary">
                    <MessageSquare className="size-4" />
                  </div>
                </div>
              </div>
            </Titlebar>

            <div className="text-caption-11 text-text-tertiary">Fullscreen Mode (no traffic lights, pl-4 = 16px):</div>
            <Titlebar id="measure-titlebar-fullscreen" fullscreen className="w-full">
              <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft">
                <PanelLeft className="size-4 text-text-secondary" />
              </div>
              <div className="flex h-full min-w-0 flex-1 items-center gap-1">
                <div className="flex min-w-0 items-center gap-1">
                  <div className="size-7 shrink-0 rounded-md flex items-center justify-center hover:bg-state-soft">
                    <FileText className="size-4 text-text-secondary" />
                  </div>
                </div>
                <div className="min-w-0 flex-1" />
              </div>
            </Titlebar>
          </div>
        </Spec>

        <Spec node="674:47115" label="Titlebar Action — 28×28, radius 8, glyph 16px, state-soft hover">
          <div className="flex flex-wrap items-center gap-4">
            <div className="flex flex-col gap-1 items-center">
              <span className="text-caption-11 text-text-tertiary">Default</span>
              <Button
                id="measure-titlebar-action-default"
                type="button"
                variant="titlebar"
                size="icon-sm"
              >
                <Search id="measure-titlebar-action-icon" className="size-4" />
              </Button>
            </div>
            <div className="flex flex-col gap-1 items-center">
              <span className="text-caption-11 text-text-tertiary">Hover (Simulated)</span>
              <button
                id="measure-titlebar-action-hover"
                type="button"
                className="size-7 shrink-0 rounded-md flex items-center justify-center bg-state-soft text-text-primary transition-colors cursor-pointer"
              >
                <Search className="size-4" />
              </button>
            </div>
            <div className="flex flex-col gap-1 items-center">
              <span className="text-caption-11 text-text-tertiary">Active</span>
              <button
                id="measure-titlebar-action-active"
                type="button"
                className="size-7 shrink-0 rounded-md flex items-center justify-center bg-accent text-foreground transition-colors cursor-pointer"
              >
                <Search className="size-4" />
              </button>
            </div>
          </div>
        </Spec>

        <Spec node="616:932" label="System Status — 3 States (Neutral: r=8 wrapper; Attention/Active: full-bleed 28×28 graphic, no radius)">
          <div className="flex flex-wrap items-center gap-6">
            <div className="flex flex-col gap-1 items-center">
              <span className="text-caption-11 text-text-tertiary">Neutral (r=8 wrapper, 16px glyph)</span>
              <div
                className="size-7 shrink-0 rounded-md flex items-center justify-center text-text-secondary hover:bg-state-soft cursor-pointer"
              >
                <Activity className="size-4" />
              </div>
            </div>
            <div className="flex flex-col gap-1 items-center">
              <span className="text-caption-11 text-text-tertiary">Attention (Full-bleed 28×28, no radius)</span>
              <div
                id="measure-system-status-attention"
                className="size-7 shrink-0 rounded-none p-0 flex items-center justify-center text-status-danger cursor-pointer"
              >
                <svg width="28" height="28" viewBox="0 0 28 28" fill="none" className="size-7">
                  <path d="M5 14H8.5L11 8.5L14.5 19.5L17 14H23" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
                  <circle cx="21" cy="7" r="3" fill="currentColor" />
                </svg>
              </div>
            </div>
            <div className="flex flex-col gap-1 items-center">
              <span className="text-caption-11 text-text-tertiary">Active (Full-bleed 28×28, no radius)</span>
              <div
                id="measure-system-status-active"
                className="size-7 shrink-0 rounded-none p-0 flex items-center justify-center text-action-primary cursor-pointer"
              >
                <svg width="28" height="28" viewBox="0 0 28 28" fill="none" className="size-7">
                  <path d="M5 14H8.5L11 8.5L14.5 19.5L17 14H23" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
                  <circle cx="21" cy="7" r="2.5" fill="currentColor" />
                </svg>
              </div>
            </div>
          </div>
        </Spec>

        <Spec node="449:325" label="Tab Strip — 48px, p-2, gap-2, 28×28 radius 8 new-tab button with 13px plus, trailing spacer">
          <div className="w-full max-w-[900px] border border-border-subtle rounded-lg overflow-hidden bg-background-app">
            <div id="measure-tab-strip">
              <TabStrip
                tabs={[
                  { id: "tab-1", moduleId: "notes", viewId: "notes.detail", title: "Active Note", status: "running" },
                  { id: "tab-2", moduleId: "notes", viewId: "notes.list", title: "Inactive Tab", status: "idle" },
                  { id: "tab-3", moduleId: "database", viewId: "db.query", title: "Preview Tab", status: "idle", preview: true },
                ]}
                activeTabId="tab-1"
                onSelect={() => {}}
                onClose={() => {}}
                onReorder={() => {}}
                keyboardBrowsing={false}
                onExitBrowsing={() => {}}
              />
            </div>
          </div>
        </Spec>

        <Spec node="448:343" label="Tab Pill — 200×32, radius 6, px-3, gap-2 (Active / Inactive / Preview / Focused)">
          <div className="flex flex-wrap items-center gap-4">
            {/* Active */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Active (surface-raised, solid border, 6px dot, 12px close)</span>
              <div
                id="measure-tab-pill-active"
                className="group relative flex h-8 max-w-[200px] shrink-0 items-center rounded-sm border border-border bg-surface-raised px-3 gap-2 text-label-12 text-text-primary"
              >
                <Wrench className="size-3.5 shrink-0 opacity-85" />
                <span className="truncate flex-1 min-w-0">Active Pill</span>
                <span id="measure-tab-pill-active-dot" className="size-1.5 shrink-0 rounded-full bg-status-running" />
                <span id="measure-tab-pill-active-close" className="z-10 flex size-3 shrink-0 items-center justify-center text-text-secondary hover:text-text-primary">
                  <X className="size-3" />
                </span>
              </div>
            </div>

            {/* Inactive */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Inactive (state-soft, subtle border, 12px close, no dot)</span>
              <div
                id="measure-tab-pill-inactive"
                className="group relative flex h-8 max-w-[200px] shrink-0 items-center rounded-sm border border-solid border-border-subtle bg-state-soft px-3 gap-2 text-label-12 text-text-secondary"
              >
                <Wrench className="size-3.5 shrink-0 opacity-85" />
                <span className="truncate flex-1 min-w-0">Inactive Pill</span>
                <span className="z-10 flex size-3 shrink-0 items-center justify-center text-text-secondary hover:text-text-primary">
                  <X className="size-3" />
                </span>
              </div>
            </div>

            {/* Preview */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Preview (state-soft, subtle dashed border, body-12, no close)</span>
              <div
                id="measure-tab-pill-preview"
                className="group relative flex h-8 max-w-[200px] shrink-0 items-center rounded-sm border border-dashed border-border-subtle bg-state-soft px-3 gap-2 text-body-12 text-text-secondary"
              >
                <Wrench className="size-3.5 shrink-0 opacity-85" />
                <span className="truncate flex-1 min-w-0 font-normal">Preview Pill</span>
              </div>
            </div>

            {/* Focused */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Focused (Separate radius 8 ring, 2px border-focus)</span>
              <div
                id="measure-tab-pill-focused"
                className="group relative flex h-8 max-w-[200px] shrink-0 items-center rounded-sm border border-border bg-surface-raised px-3 gap-2 text-label-12 text-text-primary"
              >
                <span
                  id="measure-tab-pill-focus-ring"
                  className="pointer-events-none absolute -inset-[2px] rounded-md border-2 border-[var(--border-focus,#b2d5e5)]"
                  aria-hidden="true"
                />
                <Wrench className="size-3.5 shrink-0 opacity-85" />
                <span className="truncate flex-1 min-w-0">Focused Pill</span>
                <span className="size-1.5 shrink-0 rounded-full bg-status-running" />
                <span className="z-10 flex size-3 shrink-0 items-center justify-center text-text-secondary">
                  <X className="size-3" />
                </span>
              </div>
            </div>
          </div>
        </Spec>

        <Step4aMeasurementsInspector />
      </Section>

      {/* 07 App Shell — overlays */}
      <Section title="07 App Shell — overlays">
        <Spec node="107:137" label="App Shell — 1440×900 Layout Geometry (Scaled Static Diagram)">
          <div className="flex flex-col gap-3">
            <div className="rounded border border-border-subtle bg-surface-raised p-2 text-caption-11 text-text-secondary">
              <strong>Note:</strong> Scaled static diagram representing the 1440×900 App Shell frame geometry from Figma 107:137 / 116:1333 / 204:428. This is a diagram, not the running shell.
            </div>

            {/* Scaled Frame: 1440×900 at 0.5x = 720×450 */}
            <div
              id="measure-shell-diagram-frame"
              className="relative mx-auto flex flex-col overflow-hidden rounded-lg border border-border bg-black/60 shadow-xl select-none"
              style={{ width: 720, height: 450 }}
            >
              {/* Titlebar: 0,0 1440×40 -> scaled 720×20 */}
              <div
                id="measure-shell-diagram-titlebar"
                className="flex h-5 w-full shrink-0 items-center justify-between border-b border-border-subtle bg-surface-base px-2 text-(length:--type-diagram-title-size) font-mono text-text-tertiary"
              >
                <span>titlebar (1440×40)</span>
                <span>x=88 moduleActions · x=1236 appActions</span>
              </div>

              {/* Content row: 8,40 1424×852 (8px gutters: mx-1 mb-1 scaled to 4px) -> scaled 712×426 */}
              <div
                id="measure-shell-diagram-content-row"
                className="relative m-1 flex min-h-0 flex-1 gap-1 overflow-hidden"
              >
                {/* Sidebar Surface: 280×852 -> scaled 140px */}
                <div
                  id="measure-shell-diagram-sidebar"
                  style={{ width: 140 }}
                  className="relative flex shrink-0 flex-col justify-between rounded border border-border-subtle bg-[rgba(29,29,29,0.4)] p-1 text-(length:--type-diagram-size) font-mono text-text-tertiary"
                >
                  <div className="flex flex-col gap-0.5">
                    <span className="font-semibold text-text-secondary">sidebar-surface</span>
                    <span>280×852</span>
                    <span className="text-text-tertiary/70">content 264×836</span>
                  </div>
                  {/* scroll ruler indicator */}
                  <div className="absolute top-1 right-0.5 h-[calc(100%-8px)] w-1 rounded-full bg-text-tertiary/20" title="scroll-ruler 8×836" />
                </div>

                {/* Sidebar Sash: 8×852 -> scaled 4px */}
                <div
                  id="measure-shell-diagram-sidebar-sash"
                  style={{ width: 4 }}
                  className="shrink-0 rounded-full bg-border-default/30"
                  title="sidebar-sash (8×852)"
                />

                {/* Content Column: 808×852 -> scaled 404px */}
                <div
                  id="measure-shell-diagram-content-col"
                  className="flex min-w-0 flex-1 flex-col gap-1"
                >
                  {/* Content Surface: 808×624 (Bottom=Open) or 808×852 (Closed) */}
                  <div className="relative flex flex-1 flex-col rounded border border-border bg-surface-base p-1 text-(length:--type-diagram-size) font-mono text-text-tertiary">
                    <div className="flex h-6 items-center justify-between border-b border-border-subtle px-1">
                      <span className="font-semibold text-text-secondary">tab-strip (808×48)</span>
                      <span>Tab Pill 200×32</span>
                    </div>
                    <div className="flex flex-1 items-center justify-center text-text-tertiary">
                      content-slot (808×804 / 808×576)
                    </div>
                  </div>

                  {/* Bottom Slot Split (Bottom=Open: 204:428) */}
                  <div className="relative flex h-24 shrink-0 flex-col rounded border border-border bg-surface-base p-1 text-(length:--type-diagram-size) font-mono text-text-tertiary">
                    <div className="flex h-5 items-center justify-between border-b border-border-subtle px-1">
                      <span className="font-semibold text-text-secondary">terminal-tabs (808×48)</span>
                      <span className="text-action-primary">Tab Pill 190×30 (EXCEPTION)</span>
                    </div>
                    <div className="flex flex-1 items-center justify-center text-text-tertiary">
                      bottom-content (808×172)
                    </div>
                    <div className="absolute top-1 right-0.5 h-[calc(100%-8px)] w-1 rounded-full bg-text-tertiary/20" title="scroll-ruler 8×156" />
                  </div>
                </div>

                {/* Panel Sash: 8×852 -> scaled 4px */}
                <div
                  id="measure-shell-diagram-panel-sash"
                  style={{ width: 4 }}
                  className="shrink-0 rounded-full bg-border-default/30"
                  title="panel-sash (8×852)"
                />

                {/* Panel Surface: 320×852 -> scaled 160px */}
                <div
                  id="measure-shell-diagram-panel"
                  style={{ width: 160 }}
                  className="relative flex shrink-0 flex-col justify-between rounded border border-border bg-surface-base p-1 text-(length:--type-diagram-size) font-mono text-text-tertiary"
                >
                  <div className="flex flex-col gap-0.5">
                    <span className="font-semibold text-text-secondary">panel-surface</span>
                    <span>320×852</span>
                    <span className="text-text-tertiary/70">panel-content 320×852</span>
                  </div>
                  {/* scroll ruler indicator */}
                  <div className="absolute top-1 right-0.5 h-[calc(100%-8px)] w-1 rounded-full bg-text-tertiary/20" title="scroll-ruler 8×836" />
                </div>
              </div>
            </div>
          </div>
        </Spec>

        <Spec node="700:1100" label="Shell / Scroll Ruler — one tick per milestone + current position; no tick grid; hidden when content fits">
          <div className="flex flex-wrap items-start gap-8">
            <div className="flex flex-col gap-2">
              <span className="text-caption-11 text-text-tertiary">Overflowing — 3 milestones + current position</span>
              <ScrollRulerGalleryDemo height={400} />
            </div>
            <div className="flex flex-col gap-2">
              <span className="text-caption-11 text-text-tertiary">Static props, scrolled 50%</span>
              <div className="relative h-[400px] w-[80px] overflow-hidden rounded-lg border border-border-subtle bg-surface-base">
                <ScrollRuler
                  id="measure-scroll-ruler-mid"
                  contentHeight={2000}
                  viewportHeight={400}
                  scrollTop={800}
                  milestones={[0.2, 0.55, 0.85]}
                  className="absolute inset-y-2 right-1"
                />
              </div>
            </div>

            <div className="flex flex-col gap-2">
              <span className="text-caption-11 text-text-tertiary">Fits — renders no node at all</span>
              <div className="relative flex h-[120px] w-[320px] overflow-hidden rounded-lg border border-border-subtle bg-surface-base">
                <div className="flex-1 overflow-auto p-4">
                  <p className="text-body-13 text-text-secondary">Short content.</p>
                </div>
                <ScrollRuler
                  id="measure-scroll-ruler-absent"
                  contentHeight={100}
                  viewportHeight={120}
                  scrollTop={0}
                  className="absolute inset-y-2 right-1"
                />
              </div>
            </div>
          </div>
        </Spec>

        <Spec node="699:1104" label="Shell / Resize Handle — 4 Variants (Orientation × State)">
          <div className="flex flex-wrap gap-8 items-start">
            {/* Vertical Rest */}
            <div className="flex flex-col gap-2 items-center">
              <span className="text-caption-11 text-text-tertiary">Vertical Rest (Invisible / Empty)</span>
              <div className="h-32 w-8 bg-surface-raised/40 p-2 rounded flex justify-center">
                <ShellResizeHandle
                  id="measure-shell-resize-v-rest"
                  orientation="vertical"
                  forceState="rest"
                  aria-label="Resize column rest"
                  style={{ height: 120 }}
                />
              </div>
            </div>

            {/* Vertical Hover */}
            <div className="flex flex-col gap-2 items-center">
              <span className="text-caption-11 text-text-tertiary">Vertical Hover (1px hairline + 3×32 grip)</span>
              <div className="h-32 w-8 bg-surface-raised/40 p-2 rounded flex justify-center">
                <ShellResizeHandle
                  id="measure-shell-resize-v-hover"
                  orientation="vertical"
                  forceState="hover"
                  aria-label="Resize column hover"
                  style={{ height: 120 }}
                />
              </div>
            </div>

            {/* Vertical Focus */}
            <div className="flex flex-col gap-2 items-center">
              <span className="text-caption-11 text-text-tertiary">Vertical Focus (2px border-focus overflowing 8px gutter)</span>
              <div className="h-32 w-8 bg-surface-raised/40 p-2 rounded flex justify-center">
                <ShellResizeHandle
                  id="measure-shell-resize-v-focus"
                  orientation="vertical"
                  forceState="focus"
                  aria-label="Resize column focus"
                  style={{ height: 120 }}
                />
              </div>
            </div>

            {/* Horizontal Hover */}
            <div className="flex flex-col gap-2 items-start">
              <span className="text-caption-11 text-text-tertiary">Horizontal Hover (120×8, 1px hairline + 32×3 grip)</span>
              <div className="w-36 h-8 bg-surface-raised/40 p-2 rounded flex items-center justify-center">
                <ShellResizeHandle
                  id="measure-shell-resize-h-hover"
                  orientation="horizontal"
                  forceState="hover"
                  aria-label="Resize row hover"
                  style={{ width: 120 }}
                />
              </div>
            </div>
          </div>
        </Spec>

        <Spec node="727:1682" label="Shell / Toast — 5 Tones × Action Boolean (380 wide, r14, blur 20, no top highlight)">
          <div className="flex flex-col gap-4 items-start">
            {/* Info with Action Chip */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Info Tone + Action Chip (28 tall, r8, px-2, label-12)</span>
              <ToastSpecCard
                id="measure-shell-toast-info"
                tone="info"
                title="Re-indexing workspace"
                body="3 files remaining in background."
                actionLabel="View Details"
                onClose={() => {}}
              />
            </div>

            {/* Success */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Success Tone</span>
              <ToastSpecCard
                id="measure-shell-toast-success"
                tone="success"
                title="Branch pushed to origin"
                body="Commit 7f8a9b synced successfully."
                onClose={() => {}}
              />
            </div>

            {/* Warning */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Warning Tone</span>
              <ToastSpecCard
                id="measure-shell-toast-warning"
                tone="warning"
                title="Disk space low"
                body="Vault cache size is approaching quota."
                onClose={() => {}}
              />
            </div>

            {/* Error */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Error Tone (Title and body stay neutral!)</span>
              <ToastSpecCard
                id="measure-shell-toast-error"
                tone="error"
                title="Connection refused"
                body="Could not connect to target host on port 22."
                onClose={() => {}}
              />
            </div>

            {/* Loading */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Loading Tone</span>
              <ToastSpecCard
                id="measure-shell-toast-loading"
                tone="loading"
                title="Exporting database snapshot"
                body="Writing tables to disk..."
                onClose={() => {}}
              />
            </div>
          </div>
        </Spec>

        <Spec node="719:1475" label="Shell / Command Palette — 2 States: Results & Empty (640 wide, r14, blur 20, no shadow, no inset highlight)">
          <div className="flex flex-col gap-6 items-start">
            <div className="flex flex-col gap-2">
              <span className="text-caption-11 text-text-tertiary">Results State (h56 search, Mono 11 uppercase group, active surface-raised, inactive transparent, h40 footer)</span>
              <CommandPaletteStaticCard id="measure-shell-palette-results" state="results" />
            </div>

            <div className="flex flex-col gap-2">
              <span className="text-caption-11 text-text-tertiary">Empty State (px-2 gap-2 pt-10 pb-12, 16 SemiBold text-secondary title, 13 Regular text-tertiary body)</span>
              <CommandPaletteStaticCard id="measure-shell-palette-empty" state="empty" />
            </div>
          </div>
        </Spec>

        <Spec node="816:2423" label="Shell / Menu Item — 4 Variants: Tone × State (200×28, r6, px-2 py-1, gap-2, glyph 14)">
          <div className="flex flex-wrap gap-4 items-start">
            {/* Default Rest */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Default Rest (no bg, label text-primary)</span>
              <ShellMenuItemSpec
                id="measure-shell-menu-default-rest"
                tone="default"
                state="rest"
                label="Open File"
                icon={<FileText className="size-3.5" />}
                shortcut="⌘O"
              />
            </div>

            {/* Default Hover */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Default Hover (state-soft rgba(255,255,255,0.06))</span>
              <ShellMenuItemSpec
                id="measure-shell-menu-default-hover"
                tone="default"
                state="hover"
                label="Open File"
                icon={<FileText className="size-3.5" />}
                shortcut="⌘O"
              />
            </div>

            {/* Danger Rest */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Danger Rest (no bg at rest! Only label is status-danger)</span>
              <ShellMenuItemSpec
                id="measure-shell-menu-danger-rest"
                tone="danger"
                state="rest"
                label="Delete Note"
                icon={<X className="size-3.5" />}
                shortcut="⌫"
              />
            </div>

            {/* Danger Hover */}
            <div className="flex flex-col gap-1">
              <span className="text-caption-11 text-text-tertiary">Danger Hover (feedback-danger-soft 14% rgba(216,120,134,0.14))</span>
              <ShellMenuItemSpec
                id="measure-shell-menu-danger-hover"
                tone="danger"
                state="hover"
                label="Delete Note"
                icon={<X className="size-3.5" />}
                shortcut="⌫"
              />
            </div>
          </div>
        </Spec>

        <Spec node="204:428" label="Terminal Tab Pill — 190×30 (EXCEPTION: Not 200×32 of Tab Pill 448:343)">
          <div className="flex flex-col gap-2 items-start">
            <span className="text-caption-11 text-text-tertiary">Terminal Tab Pill (190×30, r6, label-12)</span>
            <div
              id="measure-shell-terminal-pill"
              className="flex h-(--control-height-sm) w-[190px] items-center gap-1 rounded-sm border border-border-subtle bg-surface-raised px-2 text-label-12 text-text-primary"
            >
              <Terminal className="size-3.5 shrink-0 opacity-80" />
              <span className="truncate flex-1">zsh (terminal 1)</span>
              <X className="size-3 shrink-0 opacity-60 hover:opacity-100" />
            </div>
          </div>
        </Spec>

        <Spec node="116:1367" label="Sidebar rail — three row tiers, one set of everything else">
          <div className="flex w-[280px] flex-col gap-4">
            <SidebarHeader>
              <SidebarSearch value="" onChange={() => {}} placeholder="Search…" />
            </SidebarHeader>
            <div id="measure-rail-tiers" className="flex flex-col">
              <SidebarSection label="Tree — 24" count={173} />
              <SidebarRow tier="tree" label="orders" icon={<Terminal className="size-3" />} />
              <SidebarRow tier="tree" label="order_items" icon={<Terminal className="size-3" />} active />
              <SidebarSection label="List — 32" />
              <SidebarRow tier="list" label="Hash text" icon={<Terminal className="size-3.5" />} />
              <SidebarRow tier="list" label="Decode JWT" icon={<Terminal className="size-3.5" />} active />
              <SidebarSection label="Card — 48" />
              <SidebarRow
                tier="card"
                label={
                  <span className="flex flex-col gap-px">
                    <span className="truncate text-body-13-medium text-text-primary">Cloud</span>
                    <span className="truncate text-code-11 text-text-tertiary">sync.devkit.io</span>
                  </span>
                }
              />
            </div>
          </div>
        </Spec>

        <Step4bMeasurementsInspector />
      </Section>

      <AgentHubTranscriptSection />

      <CodeEditorWrapSection />

      <CodeIntelSection />

      <TreeNextSection />
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="border-b border-border-subtle px-6 py-5">
      <h2 className="text-label-11-caps text-text-tertiary">{title}</h2>
      <div className="mt-4 flex flex-col gap-6">{children}</div>
    </section>
  );
}

function Spec({
  node,
  label,
  children,
}: {
  node: string;
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline gap-2">
        <span className="text-code-11 text-text-tertiary">{node}</span>
        <span className="text-body-12 text-text-secondary">{label}</span>
      </div>
      <div className="rounded-md border border-border-subtle bg-surface-base p-4">
        {children}
      </div>
    </div>
  );
}

function Step2MeasurementsInspector() {
  const [data, setData] = useState<Record<string, Record<string, string | number>>>({});

  useEffect(() => {
    const ids = [
      "measure-input-rest",
      "measure-input-password",
      "measure-input-label",
      "measure-input-helper",
      "measure-input-error",
      "measure-input-disabled",
      "measure-checkbox-unchecked",
      "measure-checkbox-checked",
      "measure-checkbox-disabled",
      "measure-checkbox-row",
      "measure-switch-off",
      "measure-switch-on",
      "measure-switch-disabled",
      "measure-select-trigger",
      "measure-select-trigger-disabled",
      "measure-select-menu",
      "measure-select-option-selected",
      "measure-select-option-unselected",
      "measure-tabs-list-default",
      "measure-tabs-list-line",
      "measure-tabs-trigger-default-active",
      "measure-tabs-trigger-default-inactive",
      "measure-tabs-trigger-line-active",
      "measure-tabs-count",
      "measure-tooltip",
    ];

    const results: Record<string, Record<string, string | number>> = {};

    ids.forEach((id) => {
      const el = document.getElementById(id);
      if (el) {
        const rect = el.getBoundingClientRect();
        const style = window.getComputedStyle(el);
        results[id] = {
          width: Math.round(rect.width * 10) / 10,
          height: Math.round(rect.height * 10) / 10,
          color: style.color,
          backgroundColor: style.backgroundColor,
          borderColor: style.borderColor,
          borderWidth: style.borderWidth,
          borderRadius: style.borderRadius,
          fontSize: style.fontSize,
          fontWeight: style.fontWeight,
        };
      }
    });

    setData(results);
  }, []);

  return (
    <div className="mt-8 rounded-md border border-border bg-surface-base p-4">
      <h3 className="text-label-12 text-text-secondary mb-2">Live DOM Measurements (Step 2 Acceptance Comparison)</h3>
      <pre id="step2-measurements-output" className="text-caption-11 text-text-primary whitespace-pre-wrap overflow-x-auto bg-black/40 p-4 rounded-md">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}

function Step3MeasurementsInspector() {
  const [data, setData] = useState<Record<string, Record<string, string | number>>>({});

  useEffect(() => {
    const ids = [
      "measure-alert-info",
      "measure-alert-success",
      "measure-alert-warning",
      "measure-alert-error",
      "measure-alert-title",
      "measure-alert-desc",
      "measure-step-progress",
      "measure-loading-status",
      "measure-recovery-hidden",
      "measure-recovery-revealed",
      "measure-security-secure",
      "measure-security-irreversible",
    ];

    const results: Record<string, Record<string, string | number>> = {};

    ids.forEach((id) => {
      const el = document.getElementById(id);
      if (el) {
        const rect = el.getBoundingClientRect();
        const style = window.getComputedStyle(el);
        results[id] = {
          width: Math.round(rect.width * 10) / 10,
          height: Math.round(rect.height * 10) / 10,
          color: style.color,
          backgroundColor: style.backgroundColor,
          borderColor: style.borderColor,
          borderWidth: style.borderWidth,
          borderRadius: style.borderRadius,
          fontSize: style.fontSize,
          fontWeight: style.fontWeight,
          boxShadow: style.boxShadow,
          backdropFilter: style.backdropFilter || (style as unknown as Record<string, string>)["-webkit-backdrop-filter"] || "none",
        };
      }
    });

    setData(results);
  }, []);

  return (
    <div className="mt-8 rounded-md border border-border bg-surface-base p-4">
      <h3 className="text-label-12 text-text-secondary mb-2">Live DOM Measurements (Step 3 Feedback & Security Comparison)</h3>
      <pre id="step3-measurements-output" className="text-caption-11 text-text-primary whitespace-pre-wrap overflow-x-auto bg-black/40 p-4 rounded-md">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}

function Step4aMeasurementsInspector() {
  const [data, setData] = useState<Record<string, Record<string, string | number>>>({});

  useEffect(() => {
    const ids = [
      "measure-titlebar-windowed",
      "measure-titlebar-fullscreen",
      "measure-titlebar-action-default",
      "measure-titlebar-action-hover",
      "measure-titlebar-action-active",
      "measure-titlebar-action-icon",
      "measure-module-actions",
      "measure-app-actions",
      "measure-system-status-neutral-spec",
      "measure-system-status-attention",
      "measure-system-status-active",
      "measure-tab-strip",
      "measure-tab-pill-active",
      "measure-tab-pill-active-dot",
      "measure-tab-pill-active-close",
      "measure-tab-pill-inactive",
      "measure-tab-pill-preview",
      "measure-tab-pill-focused",
      "measure-tab-pill-focus-ring",
    ];

    const results: Record<string, Record<string, string | number>> = {};

    ids.forEach((id) => {
      const el = document.getElementById(id);
      if (el) {
        const rect = el.getBoundingClientRect();
        const style = window.getComputedStyle(el);
        results[id] = {
          width: Math.round(rect.width * 10) / 10,
          height: Math.round(rect.height * 10) / 10,
          color: style.color,
          backgroundColor: style.backgroundColor,
          borderColor: style.borderColor,
          borderWidth: style.borderWidth,
          borderStyle: style.borderStyle,
          borderRadius: style.borderRadius,
          fontSize: style.fontSize,
          fontWeight: style.fontWeight,
          letterSpacing: style.letterSpacing,
          lineHeight: style.lineHeight,
          boxShadow: style.boxShadow,
          backdropFilter:
            style.backdropFilter ||
            (style as unknown as Record<string, string>)["-webkit-backdrop-filter"] ||
            "none",
          padding: style.padding,
          gap: style.gap,
        };
      }
    });

    setData(results);
  }, []);

  return (
    <div className="mt-8 rounded-md border border-border bg-surface-base p-4">
      <h3 className="text-label-12 text-text-secondary mb-2">
        Live DOM Measurements (Step 4a Shell Chrome Comparison)
      </h3>
      <pre
        id="step4a-measurements-output"
        className="text-caption-11 text-text-primary whitespace-pre-wrap overflow-x-auto bg-black/40 p-4 rounded-md"
      >
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}

function Step4bMeasurementsInspector() {
  const [data, setData] = useState<Record<string, Record<string, string | number>>>({});

  useEffect(() => {
    const ids = [
      "measure-shell-resize-v-rest",
      "measure-shell-resize-v-hover",
      "measure-shell-resize-v-hover-hairline",
      "measure-shell-resize-v-hover-grip",
      "measure-shell-resize-v-focus",
      "measure-shell-resize-h-hover",
      "measure-shell-toast-info",
      "measure-shell-toast-info-title",
      "measure-shell-toast-info-body",
      "measure-shell-toast-action",
      "measure-shell-toast-error",
      "measure-shell-palette-results",
      "measure-shell-palette-empty",
      "measure-shell-menu-default-rest",
      "measure-shell-menu-default-hover",
      "measure-shell-menu-danger-rest",
      "measure-shell-menu-danger-hover",
      "measure-shell-terminal-pill",
    ];

    const results: Record<string, Record<string, string | number>> = {};

    ids.forEach((id) => {
      const el = document.getElementById(id);
      if (el) {
        const rect = el.getBoundingClientRect();
        const style = window.getComputedStyle(el);
        results[id] = {
          width: Math.round(rect.width * 10) / 10,
          height: Math.round(rect.height * 10) / 10,
          color: style.color,
          backgroundColor: style.backgroundColor,
          borderColor: style.borderColor,
          borderWidth: style.borderWidth,
          borderRadius: style.borderRadius,
          fontSize: style.fontSize,
          fontWeight: style.fontWeight,
          boxShadow: style.boxShadow,
          backdropFilter:
            style.backdropFilter ||
            (style as unknown as Record<string, string>)["-webkit-backdrop-filter"] ||
            "none",
          padding: style.padding,
          gap: style.gap,
        };
      }
    });

    setData(results);
  }, []);

  return (
    <div className="mt-8 rounded-md border border-border bg-surface-base p-4">
      <h3 className="text-label-12 text-text-secondary mb-2">
        Live DOM Measurements (Step 4b Shell Overlays & Composition)
      </h3>
      <pre
        id="step4b-measurements-output"
        className="text-caption-11 text-text-primary whitespace-pre-wrap overflow-x-auto bg-black/40 p-4 rounded-md"
      >
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}

