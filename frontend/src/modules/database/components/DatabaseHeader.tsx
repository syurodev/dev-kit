import {
  Terminal,
  Network,
  Activity,
  History,
  Bookmark,
  Settings2,
  CheckCircle2,
  AlertTriangle,
  Radio,
  Shield,
  ShieldAlert,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Chip, EngineBadge } from "./EngineBadge";
import { useDatabaseStore, type DatabaseTab } from "../databaseStore";
import { testConnection } from "../../../bridge/database";
import { useState } from "react";
import { errorMessage } from "../../../bridge/errors";
import { cn } from "@/lib/utils";

const TABS: Array<{ id: DatabaseTab; label: string; icon: typeof Terminal }> = [
  { id: "console", label: "Query console", icon: Terminal },
  { id: "erd", label: "ERD diagram", icon: Network },
  { id: "activity", label: "Live activity", icon: Activity },
  { id: "history", label: "Query history", icon: History },
  { id: "saved", label: "Saved queries", icon: Bookmark },
];

/**
 * Figma `DB / Console Content` header row (742:9–742:22): the profile name in
 * Title/16 (Figma draws 18, snapped), a row of chips for what the connection
 * is — engine, environment, read-only, tunnel — and 36px actions on the
 * right. The tab strip under it is DevKit's, not the page's: page 16 only
 * draws the console, and four working views do not get dropped for a mock.
 */
export function DatabaseHeader() {
  const {
    profiles,
    selectedProfileId,
    activeTab,
    setActiveTab,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore();

  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null);

  const profile = profiles.find((p) => p.id === selectedProfileId);

  async function handleTest() {
    if (!selectedProfileId) return;
    setTesting(true);
    setTestResult(null);
    try {
      await testConnection(selectedProfileId);
      setTestResult({ ok: true, message: "Connected successfully" });
    } catch (e) {
      setTestResult({ ok: false, message: errorMessage(e) });
    } finally {
      setTesting(false);
      setTimeout(() => setTestResult(null), 4000);
    }
  }

  const isProd = profile?.env?.toLowerCase() === "prod" || profile?.env?.toLowerCase() === "production";

  return (
    <header className="shrink-0 border-b border-border px-6 pt-3.5">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          {profile ? (
            <>
              <h1 className="truncate text-title-16 text-text-primary">{profile.name}</h1>
              <div className="flex items-center gap-2">
                <EngineBadge engine={profile.engine} />
                {isProd ? (
                  <Chip tone="danger">
                    <ShieldAlert className="size-3.5" />
                    prod
                  </Chip>
                ) : profile.env ? (
                  <Chip>{profile.env}</Chip>
                ) : null}
                {profile.readOnly ? (
                  <Chip>
                    <Shield className="size-3.5" />
                    read-only
                  </Chip>
                ) : (
                  <Chip>read / write</Chip>
                )}
                {profile.sshProfileId ? (
                  <Chip>
                    <Radio className="size-3.5" />
                    ssh tunnel
                  </Chip>
                ) : null}
              </div>
              <span className="hidden min-w-0 truncate text-code-11 text-text-tertiary lg:inline">
                {profile.username}@{profile.host}:{profile.port}/{profile.database}
              </span>
            </>
          ) : (
            <span className="text-body-13 text-text-tertiary">No connection selected</span>
          )}
        </div>

        {profile ? (
          <div className="flex items-center gap-2">
            {testResult && (
              <span
                className={cn(
                  "flex h-6 items-center gap-1.5 rounded-md border px-2.5 text-code-11",
                  testResult.ok
                    ? "border-status-success/30 bg-status-success/10 text-status-success"
                    : "border-(--border-danger) bg-(--feedback-danger-soft) text-status-danger",
                )}
              >
                {testResult.ok ? (
                  <CheckCircle2 className="size-3.5" />
                ) : (
                  <AlertTriangle className="size-3.5" />
                )}
                {testResult.message}
              </span>
            )}

            <Button
              variant="outline"
              size="lg"
              className="bg-surface-raised"
              onClick={handleTest}
              disabled={testing}
            >
              {testing ? "Testing…" : "Test connection"}
            </Button>

            <Button
              variant="outline"
              size="lg"
              className="bg-surface-raised"
              onClick={() => setShowConnectionOptionsDialog(true)}
            >
              <Settings2 className="size-3.5" />
              Options
            </Button>
          </div>
        ) : null}
      </div>

      {/* The shared Tabs primitive, not a row of buttons. Five plain buttons
          gave a screen reader no tablist, no selected state and no arrow-key
          movement between them — and this is the only tab row in the app that
          was hand-rolled. Root wraps the strip only, the same shape
          TerminalWorkspace uses. */}
      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as DatabaseTab)}
        className="min-w-0 gap-0 pt-2.5"
      >
        <TabsList className="max-w-full justify-start overflow-x-auto [&::-webkit-scrollbar]:hidden">
          {TABS.map(({ id, label, icon: Icon }) => (
            <TabsTrigger key={id} value={id} className="flex-none">
              <Icon />
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
    </header>
  );
}
