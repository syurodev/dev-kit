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
import { Badge } from "@/components/ui/badge";
import { EngineBadge } from "./EngineBadge";
import { useDatabaseStore, type DatabaseTab } from "../databaseStore";
import { testConnection } from "../../../bridge/database";
import { useState } from "react";
import { errorMessage } from "../../../bridge/errors";

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

      {/* Subview Tabs */}
      <div className="flex items-center gap-1 border-t border-border/40 pt-1.5">
        <NavTab
          id="console"
          label="Query Console"
          icon={<Terminal className="w-3.5 h-3.5" />}
          active={activeTab === "console"}
          onClick={() => setActiveTab("console")}
        />
        <NavTab
          id="erd"
          label="ERD Diagram"
          icon={<Network className="w-3.5 h-3.5" />}
          active={activeTab === "erd"}
          onClick={() => setActiveTab("erd")}
        />
        <NavTab
          id="activity"
          label="Live Activity"
          icon={<Activity className="w-3.5 h-3.5" />}
          active={activeTab === "activity"}
          onClick={() => setActiveTab("activity")}
        />
        <NavTab
          id="history"
          label="Query History"
          icon={<History className="w-3.5 h-3.5" />}
          active={activeTab === "history"}
          onClick={() => setActiveTab("history")}
        />
        <NavTab
          id="saved"
          label="Saved Queries"
          icon={<Bookmark className="w-3.5 h-3.5" />}
          active={activeTab === "saved"}
          onClick={() => setActiveTab("saved")}
        />
      </div>
    </header>
  );
}

function NavTab({
  id,
  label,
  icon,
  active,
  onClick,
}: {
  id: DatabaseTab;
  label: string;
  icon: React.ReactNode;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors ${
        active
          ? "bg-surface-raised text-text-primary shadow-2xs border border-border"
          : "text-text-secondary hover:text-text-primary hover:bg-surface-raised/50"
      }`}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}
