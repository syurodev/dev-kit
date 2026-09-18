import { useState, useEffect } from "react";
import { Settings2, Shield, Radio, ShieldAlert, X, AlertTriangle, Network } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { useShallow } from "zustand/react/shallow";
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
  } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      showConnectionOptionsDialog: s.showConnectionOptionsDialog,
      setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
      envTags: s.envTags,
      sshProfiles: s.sshProfiles,
      fetchProfiles: s.fetchProfiles,
    }))
  );

  const profile = profiles.find((p) => p.id === selectedProfileId);

  const [env, setEnv] = useState("dev");
  const [readOnly, setReadOnly] = useState(false);
  const [trustedNetwork, setTrustedNetwork] = useState(false);
  const [sshProfileId, setSshProfileId] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (profile) {
      setEnv(profile.env || "dev");
      setReadOnly(Boolean(profile.readOnly));
      setTrustedNetwork(Boolean(profile.trustedNetwork));
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
        trustedNetwork,
        sshProfileId,
        // Sent back as it is: this dialog does not edit the bundle, and an
        // omitted one reads as "remove it" on the Go side.
        caCert: profile?.caCert,
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
      <div className="bg-surface-base border border-border rounded-xl shadow-2xl w-full max-w-lg p-6 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-(--motion-base)">
        <div className="flex items-center justify-between border-b border-border pb-3">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-action-primary/10 border border-action-primary/20 flex items-center justify-center text-action-primary">
              <Settings2 className="w-4 h-4" />
            </div>
            <div>
              <h3 className="text-title-14 text-text-primary">
                Connection Options · {profile.name}
              </h3>
              <p className="text-caption-11 text-text-tertiary">
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
        <div className="space-y-4 text-body-12">
          {/* Environment Tag */}
          <div className="p-3 rounded-lg border border-border bg-surface-raised space-y-2">
            <div className="flex items-center justify-between">
              <div>
                <label className="font-semibold text-text-primary block">Environment Policy Tag</label>
                <p className="text-caption-11 text-text-tertiary">
                  Controls Capability Gateway confirmation prompts and agent tool safety.
                </p>
              </div>
              {/* The ground stays on the wrapper: NativeSelect is transparent
                  by design, and these two sit on a raised panel rather than on
                  the dialog, so without it they would read as part of the card
                  they are in. */}
              <NativeSelect
                value={env}
                onChange={(e) => setEnv(e.target.value)}
                className="min-w-[120px] rounded-control bg-surface-base"
              >
                {envTags.map((tag) => (
                  <NativeSelectOption key={tag.id} value={tag.name}>
                    {tag.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>

            {isProd && (
              <div className="p-2 rounded bg-status-danger/10 border border-status-danger/30 text-caption-11 text-status-danger flex items-start gap-2">
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
              <p className="text-caption-11 text-text-tertiary">
                Postgres/MySQL sets <code className="font-mono bg-surface-base px-1 py-0.5 rounded">default_transaction_read_only = on</code> per session. Mutating statements are blocked by the database engine itself. Toggling will recreate the connection pool.
              </p>
            </div>
            <Switch checked={readOnly} onCheckedChange={setReadOnly} />
          </div>

          {/* Only a plaintext profile has anything to decide here; the
              backend drops the flag on any other mode. */}
          {profile.sslMode === "disable" && (
            <div className="p-3 rounded-lg border border-border bg-surface-raised flex items-center justify-between">
              <div className="space-y-0.5 max-w-sm">
                <div className="flex items-center gap-1.5 font-semibold text-text-primary">
                  <Network className="w-3.5 h-3.5 text-status-info" />
                  <span>Network is already encrypted</span>
                </div>
                <p className="text-caption-11 text-text-tertiary">
                  Tailscale, WireGuard or a VPN. Allows <code className="font-mono bg-surface-base px-1 py-0.5 rounded">sslmode=disable</code> to a remote host. Toggling will recreate the connection pool.
                </p>
              </div>
              <Switch checked={trustedNetwork} onCheckedChange={setTrustedNetwork} />
            </div>
          )}

          {/* SSH Tunnel Routing */}
          <div className="p-3 rounded-lg border border-border bg-surface-raised space-y-2">
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <div className="flex items-center gap-1.5 font-semibold text-text-primary">
                  <Radio className="w-3.5 h-3.5 text-status-info" />
                  <span>SSH Tunnel Forwarding</span>
                </div>
                <p className="text-caption-11 text-text-tertiary">
                  Routes traffic through an encrypted SSH tunnel profile.
                </p>
              </div>
              <NativeSelect
                value={sshProfileId}
                onChange={(e) => setSshProfileId(e.target.value)}
                className="min-w-[150px] rounded-control bg-surface-base"
              >
                <NativeSelectOption value="">Direct (No Tunnel)</NativeSelectOption>
                {sshProfiles.map((ssh) => (
                  <NativeSelectOption key={ssh.id} value={ssh.id}>
                    {ssh.name} ({ssh.username}@{ssh.host})
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>

            {sshProfileId && (
              <div className="p-2 rounded bg-surface-base border border-border/60 text-code-11 text-text-secondary flex items-center justify-between">
                <span>Tunnel Map:</span>
                <span className="text-action-primary">127.0.0.1:&lt;dynamic_port&gt; → {profile.host}:{profile.port}</span>
              </div>
            )}
          </div>
        </div>

        {error && (
          <div className="p-2.5 rounded bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger flex items-center gap-2">
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
