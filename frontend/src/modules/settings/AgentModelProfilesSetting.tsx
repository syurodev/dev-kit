import { useMemo, useState } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  agentHubCheckModelProfiles,
  agentHubConfigCatalog,
  agentHubGetModelProfiles,
  agentHubSetModelProfiles,
  type ModelProfile,
  type ModelProfileCheck,
} from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";
import { SHIPPED_VENDORS, vendorInfo } from "../agenthub/vendors";
import { ModelProfilesEditor, type ProfileVendor } from "./ModelProfilesEditor";
import { modelChoicesOf, type VendorModelChoices } from "./modelProfiles";

const PROFILES_KEY = ["agenthub", "modelProfiles"] as const;
const CHECKS_KEY = ["agenthub", "modelProfiles", "checks"] as const;

const VENDORS: ProfileVendor[] = SHIPPED_VENDORS.map((id) => ({ id, name: vendorInfo(id).name }));

/**
 * Settings → Agents → "Models by job": the profiles an agent reads when it
 * starts a child session. Reads and saves through the UI-only bridge methods;
 * the agent has a read-only tool and no way to change them.
 *
 * A vendor's models are read (a short-lived agent, no prompt) only for the
 * vendor the open form is on, so a signed-out vendor is said in the form
 * instead of failing the list.
 */
export function AgentModelProfilesSetting() {
  const client = useQueryClient();
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState("");
  const [wanted, setWanted] = useState<Record<string, boolean>>({});

  const stored = useQuery({ queryKey: PROFILES_KEY, queryFn: agentHubGetModelProfiles });
  // Checking asks each vendor for its model list, which can take seconds, so
  // it follows the list instead of holding it up.
  const checked = useQuery({
    queryKey: [...CHECKS_KEY, stored.data?.revision ?? -1],
    queryFn: agentHubCheckModelProfiles,
    enabled: Boolean(stored.data && stored.data.profiles.length > 0),
    staleTime: 60_000,
    retry: false,
  });
  const catalogs = useQueries({
    queries: VENDORS.map((vendor) => ({
      queryKey: ["agenthub", "modelProfiles", "catalog", vendor.id],
      queryFn: () => agentHubConfigCatalog(vendor.id),
      enabled: Boolean(wanted[vendor.id]),
      staleTime: 60_000,
      retry: false,
    })),
  });

  const choices = useMemo(() => {
    const out: Record<string, VendorModelChoices> = {};
    VENDORS.forEach((vendor, i) => {
      const query = catalogs[i];
      if (!query) return;
      if (query.error) out[vendor.id] = { state: "unavailable", reason: errorMessage(query.error) };
      else if (query.data) {
        const models = modelChoicesOf(query.data);
        out[vendor.id] = models.length > 0 ? { state: "ready", models } : { state: "unavailable", reason: `${vendor.name} reported no models.` };
      } else if (wanted[vendor.id]) out[vendor.id] = { state: "loading" };
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalogs.map((q) => q.data).join("|"), catalogs.map((q) => String(q.error ?? "")).join("|"), wanted]);

  const checks = useMemo(() => {
    const out: Record<string, ModelProfileCheck> = {};
    for (const check of checked.data ?? []) out[check.id] = check;
    return out;
  }, [checked.data]);

  async function onSave(next: ModelProfile[]): Promise<boolean> {
    if (!stored.data) return false;
    setSaving(true);
    setFailed("");
    try {
      const saved = await agentHubSetModelProfiles(stored.data.revision, next);
      client.setQueryData(PROFILES_KEY, saved);
      return true;
    } catch (cause) {
      setFailed(errorMessage(cause));
      // A stale revision is fixed by reading again.
      void client.invalidateQueries({ queryKey: PROFILES_KEY });
      return false;
    } finally {
      setSaving(false);
    }
  }

  return (
    <ModelProfilesEditor
      profiles={stored.data?.profiles ?? []}
      checks={checks}
      vendors={VENDORS}
      choices={choices}
      saving={saving || stored.isPending}
      error={failed || (stored.error ? errorMessage(stored.error) : "")}
      onVendorWanted={(vendor) => setWanted((w) => (w[vendor] ? w : { ...w, [vendor]: true }))}
      onSave={onSave}
    />
  );
}
