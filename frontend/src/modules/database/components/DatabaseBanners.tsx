import { FeedbackBanner } from "@/components/ui/feedback";
import { Button } from "@/components/ui/button";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";

export function DatabaseBanners() {
  const { profiles, selectedProfileId, envTags, sshProfiles, setShowConnectionOptionsDialog } = useDatabaseStore(
    useShallow((s) => ({
      profiles: s.profiles,
      selectedProfileId: s.selectedProfileId,
      envTags: s.envTags,
      sshProfiles: s.sshProfiles,
      setShowConnectionOptionsDialog: s.setShowConnectionOptionsDialog,
    })),
  );

  const profile = profiles.find((p) => p.id === selectedProfileId);
  if (!profile) return null;

  // Screen 25: Env tag unresolved
  const isEnvUnresolved = profile.env && envTags.length > 0 && !envTags.some((t) => t.name === profile.env);

  // Screen 19: SSH Tunnel unavailable / missing
  const isTunnelMissing =
    Boolean(profile.sshProfileId) && sshProfiles.length > 0 && !sshProfiles.some((s) => s.id === profile.sshProfileId);

  return (
    <div className="flex shrink-0 flex-col gap-2">
      {isEnvUnresolved && (
        <FeedbackBanner tone="warning" title="Environment tag unavailable"
          action={<Button variant="outline" size="xs" onClick={() => setShowConnectionOptionsDialog(true)}>Fix in Options</Button>}>
          The tag “{profile.env}” no longer exists. Assign a valid environment before running operations.
        </FeedbackBanner>
      )}
      {isTunnelMissing && (
        <FeedbackBanner tone="error" title="SSH tunnel profile unavailable"
          action={<Button variant="outline" size="xs" onClick={() => setShowConnectionOptionsDialog(true)}>Update Options</Button>}>
          The configured SSH profile was deleted or is inaccessible. This connection requires its SSH tunnel.
        </FeedbackBanner>
      )}
    </div>
  );
}
