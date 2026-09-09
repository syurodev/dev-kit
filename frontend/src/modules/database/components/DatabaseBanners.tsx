import { Radio, AlertTriangle, Settings2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";

export function DatabaseBanners() {
  const {
    profiles,
    selectedProfileId,
    envTags,
    sshProfiles,
    setShowConnectionOptionsDialog,
  } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      envTags: s.envTags,
      sshProfiles: s.sshProfiles,
      setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
    }))
  );

  const profile = profiles.find((p) => p.id === selectedProfileId);
  if (!profile) return null;

  // Screen 25: Env tag unresolved
  const isEnvUnresolved =
    profile.env && envTags.length > 0 && !envTags.some((t) => t.name === profile.env);

  // Screen 19: SSH Tunnel unavailable / missing
  const isTunnelMissing =
    Boolean(profile.sshProfileId) &&
    sshProfiles.length > 0 &&
    !sshProfiles.some((s) => s.id === profile.sshProfileId);

  return (
    <div className="flex flex-col shrink-0">
      {/* Screen 25: Env Tag Unresolved */}
      {isEnvUnresolved && (
        <div className="bg-amber-500/10 border-b border-amber-500/30 px-4 py-2.5 flex items-center justify-between text-body-12 text-amber-500">
          <div className="flex items-center gap-2">
            <AlertTriangle className="w-4 h-4 shrink-0" />
            <div>
              <strong className="font-semibold">Environment Tag Unresolved:</strong> The tag &ldquo;{profile.env}&rdquo; assigned to this profile no longer exists in Environments. Operations will fail closed until a valid tag is assigned.
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-6 text-caption-11 border-amber-500/40 text-amber-500 hover:bg-amber-500/20"
            onClick={() => setShowConnectionOptionsDialog(true)}
          >
            <Settings2 className="w-3 h-3 mr-1" />
            Fix in Options
          </Button>
        </div>
      )}

      {/* Screen 19: SSH Tunnel Unavailable */}
      {isTunnelMissing && (
        <div className="bg-status-danger/10 border-b border-status-danger/30 px-4 py-2.5 flex items-center justify-between text-body-12 text-status-danger">
          <div className="flex items-center gap-2">
            <Radio className="w-4 h-4 shrink-0" />
            <div>
              <strong className="font-semibold">SSH Tunnel Profile Missing:</strong> The configured SSH profile for this connection was deleted or is inaccessible. Direct connection fallback is forbidden for security.
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-6 text-caption-11 border-status-danger/40 text-status-danger hover:bg-status-danger/20"
            onClick={() => setShowConnectionOptionsDialog(true)}
          >
            <Settings2 className="w-3 h-3 mr-1" />
            Update Options
          </Button>
        </div>
      )}
    </div>
  );
}
