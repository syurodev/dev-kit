import { callBridge } from "./client";
import { AppService } from "../../bindings/changeme/internal/bridge";

export type EnvTag = {
  id: string;
  name: string;
  requiresConfirmation: boolean;
  isBuiltin: boolean;
  locked: boolean;
};

function toTag(t: {
  id: string;
  name: string;
  requires_confirmation: boolean;
  is_builtin: boolean;
  locked: boolean;
}): EnvTag {
  return {
    id: t.id,
    name: t.name,
    requiresConfirmation: t.requires_confirmation,
    isBuiltin: t.is_builtin,
    locked: t.locked,
  };
}

export function listEnvTags(): Promise<EnvTag[]> {
  return callBridge(async () => {
    const list = await AppService.EnvListTags();
    return (list ?? []).map(toTag);
  });
}
