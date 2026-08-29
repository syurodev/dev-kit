import { useState, useEffect } from "react";
import { Settings2, Shield, Radio, ShieldAlert, X, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { useDatabaseStore } from "../databaseStore";
import { updateProfileOptions } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

export function ConnectionOptionsDialog() {
  const {
    profiles,
    selectedProfileId,
    showConnectionOptionsDialog,
    setShowConnectionOptionsDialog,
    envTags,
    sshProfiles,
    fetchProfiles,
  } = useDatabaseStore();

  const profile = profiles.find((p) => p.id === selectedProfileId);

  const [env, setEnv] = useState("dev");
  const [readOnly, setReadOnly] = useState(false);
  const [sshProfileId, setSshProfileId] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (profile) {
      setEnv(profile.env || "dev");
      setReadOnly(Boolean(profile.readOnly));
      setSshProfileId(profile.sshProfileId || "");
      setError("");
    }
  }, [profile, showConnectionOptionsDialog]);

  if (!showConnectionOptionsDialog || !profile) return null;

  const isProd = env.toLowerCase() === "prod" || env.toLowerCase() === "production";

  async function handleSave() {
    if (!selectedProfileId) return;
    setSaving(true);
    setError("");
    try {
      await updateProfileOptions(selectedProfileId, {
        env,
        readOnly,
        sshProfileId,
      });
      await fetchProfiles();
      setShowConnectionOptionsDialog(false);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-2xl w-full max-w-lg p-6 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-150">
        <div className="flex items-center justify-between border-b border-border pb-3">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-action-primary/10 border border-action-primary/20 flex items-center justify-center text-action-primary">
              <Settings2 className="w-4 h-4" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-text-primary">
                Connection Options · {profile.name}
              </h3>
              <p className="text-[11px] text-text-tertiary">
                Metadata-only write path (does not re-encrypt password).
              </p>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-text-tertiary hover:text-text-primary"
            onClick={() => setShowConnectionOptionsDialog(false)}
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Options List */}
        <div className="space-y-4 text-xs">
          {/* Environment Tag */}
          <div className="p-3 rounded-lg border border-border bg-surface-raised space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <label className="font-semibold text-text-primary block">Environment Policy Tag</label>
                <p className="text-[11px] text-text-tertiary">
                  Controls Capability Gateway confirmation prompts and agent tool safety.
                </p>
              </div>
              <select
                value={env}
                onChange={(e) => setEnv(e.target.value)}
                className="h-8 px-2.5 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-action-primary min-w-[120px]"
              >
                {envTags.map((tag) => (
                  <option key={tag.id} value={tag.name}>
                    {tag.name}
                  </option>
                ))}
              </select>
            </div>

            {isProd && (
              <div className="p-2 rounded bg-status-danger/10 border border-status-danger/30 text-[11px] text-status-danger flex items-start gap-2">
                <ShieldAlert className="w-4 h-4 shrink-0 mt-0.5" />
                <div>
                  <strong className="font-semibold">Production Tag Active:</strong> All queries executed via MCP agents will mandate explicit human confirmation. Destructive session actions will require additional verification.
                </div>
              </div>
            )}
          </div>

          {/* Safe Mode (Read Only) */}
          <div className="p-3 rounded-lg border border-border bg-surface-raised flex items-center justify-between">
            <div className="space-y-0.5 max-w-sm">
              <div className="flex items-center gap-1.5 font-semibold text-text-primary">
                <Shield className="w-3.5 h-3.5 text-status-warning" />
                <span>Safe-Mode (Read-Only)</span>
              </div>
              <p className="text-[11px] text-text-tertiary leading-relaxed">
                Postgres/MySQL sets <code className="font-mono bg-surface-base px-1 py-0.5 rounded">default_transaction_read_only = on</code> per session. Mutating statements are blocked by the database engine itself. Toggling will recreate the connection pool.
              </p>
            </div>
            <Switch checked={readOnly} onCheckedChange={setReadOnly} />
          </div>

          {/* SSH Tunnel Routing */}
          <div className="p-3 rounded-lg border border-border bg-surface-raised space-y-2">
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <div className="flex items-center gap-1.5 font-semibold text-text-primary">
                  <Radio className="w-3.5 h-3.5 text-status-info" />
                  <span>SSH Tunnel Forwarding</span>
                </div>
                <p className="text-[11px] text-text-tertiary">
                  Routes traffic through an encrypted SSH tunnel profile.
                </p>
              </div>
              <select
                value={sshProfileId}
                onChange={(e) => setSshProfileId(e.target.value)}
                className="h-8 px-2.5 rounded-lg border border-border bg-surface-base text-xs text-text-primary outline-none focus:border-action-primary min-w-[150px]"
              >
                <option value="">Direct (No Tunnel)</option>
                {sshProfiles.map((ssh) => (
                  <option key={ssh.id} value={ssh.id}>
                    {ssh.name} ({ssh.username}@{ssh.host})
                  </option>
                ))}
              </select>
            </div>

            {sshProfileId && (
              <div className="p-2 rounded bg-surface-base border border-border/60 text-[11px] font-mono text-text-secondary flex items-center justify-between">
                <span>Tunnel Map:</span>
                <span className="text-action-primary">127.0.0.1:&lt;dynamic_port&gt; → {profile.host}:{profile.port}</span>
              </div>
            )}
          </div>
        </div>

        {error && (
          <div className="p-2.5 rounded bg-status-danger/10 border border-status-danger/30 text-xs text-status-danger flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-3 border-t border-border">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowConnectionOptionsDialog(false)}
            disabled={saving}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={handleSave}
            disabled={saving}
          >
            {saving ? "Updating…" : "Save Options"}
          </Button>
        </div>
      </div>
    </div>
  );
}
