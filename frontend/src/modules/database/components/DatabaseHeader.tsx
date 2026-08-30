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
import { Badge } from "@/components/ui/badge";
import { EngineBadge } from "./EngineBadge";
import { useDatabaseStore, type DatabaseTab } from "../databaseStore";
import { testConnection } from "../../../bridge/database";
import { useState } from "react";
import { errorMessage } from "../../../bridge/errors";

const TABS: Array<{ id: DatabaseTab; label: string; icon: typeof Terminal }> = [
  { id: "console", label: "Query console", icon: Terminal },
  { id: "erd", label: "ERD diagram", icon: Network },
  { id: "activity", label: "Live activity", icon: Activity },
  { id: "history", label: "Query history", icon: History },
  { id: "saved", label: "Saved queries", icon: Bookmark },
];

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
    <header className="border-b border-border px-4 py-2.5 flex flex-col gap-2 shrink-0">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {/* Profile Info */}
        <div className="flex items-center gap-3 min-w-0">
          {profile ? (
            <>
              <div className="flex items-center gap-2">
                <h1 className="text-sm font-semibold text-text-primary truncate">
                  {profile.name}
                </h1>
                <EngineBadge engine={profile.engine} showPlaceholder />
              </div>

              <div className="flex items-center gap-1.5 text-xs text-text-tertiary">
                <span className="font-mono text-[11px]">
                  {profile.username}@{profile.host}:{profile.port}/{profile.database}
                </span>

                {isProd ? (
                  <Badge variant="destructive" className="text-[10px] px-1.5 py-0">
                    <ShieldAlert className="w-3 h-3 mr-1" />
                    PROD
                  </Badge>
                ) : profile.env ? (
                  <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-text-secondary">
                    env: {profile.env}
                  </Badge>
                ) : null}

                {profile.readOnly ? (
                  <Badge variant="destructive" className="text-[10px] px-1.5 py-0 bg-amber-500/15 text-amber-500 border-amber-500/30">
                    <Shield className="w-3 h-3 mr-1" />
                    Safe-Mode (Read-Only)
                  </Badge>
                ) : (
                  <Badge variant="secondary" className="text-[10px] px-1.5 py-0 text-text-tertiary">
                    Read / Write
                  </Badge>
                )}

                {profile.sshProfileId && (
                  <Badge variant="outline" className="text-[10px] px-1.5 py-0 text-status-info border-status-info/30">
                    <Radio className="w-3 h-3 mr-1" />
                    SSH Tunnel
                  </Badge>
                )}
              </div>
            </>
          ) : (
            <div className="text-xs text-text-tertiary font-medium">
              No connection selected
            </div>
          )}
        </div>

        {/* Global Connection Actions */}
        {profile ? (
          <div className="flex items-center gap-2">
            {testResult && (
              <span
                className={`text-xs flex items-center gap-1 px-2 py-0.5 rounded border ${
                  testResult.ok
                    ? "text-status-success bg-status-success/10 border-status-success/30"
                    : "text-status-danger bg-status-danger/10 border-status-danger/30"
                }`}
              >
                {testResult.ok ? (
                  <CheckCircle2 className="w-3 h-3" />
                ) : (
                  <AlertTriangle className="w-3 h-3" />
                )}
                {testResult.message}
              </span>
            )}

            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs"
              onClick={handleTest}
              disabled={testing}
            >
              {testing ? "Testing…" : "Test connection"}
            </Button>

            <Button
              variant="secondary"
              size="sm"
              className="h-7 text-xs gap-1.5"
              onClick={() => setShowConnectionOptionsDialog(true)}
            >
              <Settings2 className="w-3.5 h-3.5" />
              <span>Options</span>
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
        className="min-w-0 gap-0 border-t border-border/40 pt-1.5"
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
