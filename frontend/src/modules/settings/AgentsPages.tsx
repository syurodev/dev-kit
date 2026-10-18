import { useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AgentEnvironmentPanel } from "@/modules/agenthub/environment/AgentEnvironment";
import { CODEX_INFO, SHIPPED_VENDORS, vendorInfo } from "@/modules/agenthub/vendors";
import { AgentRuntimeSetting } from "./AgentRuntimeSetting";
import { AgentModelProfilesSetting } from "./AgentModelProfilesSetting";
import { AgentSubagentLimitsSetting } from "./AgentSubagentLimitsSetting";
import { AgentCheckpointSetting } from "./AgentCheckpointSetting";
import { AgentSandboxSetting } from "./AgentSandboxSetting";
import { AgentNotificationSetting } from "./AgentNotificationSetting";
import { CodexSessionsSetting } from "./CodexSessionsSetting";
import { McpInstallPanel } from "./McpInstallPanel";
import { TelegramSetting } from "./TelegramSetting";
import { GroupLabel, SectionHeader, TabColumn } from "./SettingsPrimitives";
import { readAgentVendor, writeAgentVendor } from "./agentVendorChoice";

/**
 * Claude | Codex, a mode switch inside one page. The choice is remembered
 * across pages and restarts; only the selected vendor's panel mounts, so the
 * other one fetches nothing.
 */
function VendorSwitch({ label, render }: { label: string; render: (vendor: string) => React.ReactNode }) {
  const [vendor, setVendor] = useState(() => readAgentVendor(SHIPPED_VENDORS));
  return (
    <Tabs
      value={vendor}
      onValueChange={(next) => {
        setVendor(next);
        writeAgentVendor(next);
      }}
      className="gap-5"
    >
      <TabsList aria-label={label}>
        {SHIPPED_VENDORS.map((id) => (
          <TabsTrigger key={id} value={id}>{vendorInfo(id).name}</TabsTrigger>
        ))}
      </TabsList>
      {SHIPPED_VENDORS.map((id) => (
        <TabsContent key={id} value={id}>{render(id)}</TabsContent>
      ))}
    </Tabs>
  );
}

export function AgentRuntimesPage() {
  return (
    <TabColumn>
      <SectionHeader
        title="Runtimes"
        description="The agents Agent Hub runs. DevKit installs each into its own folder — never on your PATH. Claude and Codex at a version it has tested; opencode at its latest release."
      />
      <VendorSwitch
        label="Agent runtime"
        render={(vendor) =>
          vendor === CODEX_INFO.id ? (
            <div className="flex flex-col gap-6">
              <AgentRuntimeSetting vendor={vendor} />
              <div className="flex flex-col gap-3">
                <GroupLabel>CODEX (OBSERVED)</GroupLabel>
                <CodexSessionsSetting />
              </div>
            </div>
          ) : (
            <AgentRuntimeSetting vendor={vendor} />
          )
        }
      />
    </TabColumn>
  );
}

export function AgentModelsPage() {
  return (
    <TabColumn>
      <SectionHeader
        title="Models & subagents"
        description="Which model and effort each kind of job uses, and which profiles agents can delegate to as subagents."
      />
      <AgentModelProfilesSetting />
      <AgentSubagentLimitsSetting />
    </TabColumn>
  );
}

export function AgentSkillsPage() {
  return (
    <TabColumn>
      <SectionHeader
        title="Skills & MCP"
        description="The tools, skills and MCP servers each agent can use, and DevKit's overrides on top of your own configuration."
      />
      <VendorSwitch label="Agent tools" render={(vendor) => <AgentEnvironmentPanel vendor={vendor} />} />
    </TabColumn>
  );
}

export function AgentSafetyPage() {
  return (
    <TabColumn>
      <SectionHeader
        title="Safety"
        description="What agents may reach on this computer, and how DevKit lets you take their file changes back."
      />
      <div className="flex flex-col gap-3">
        <GroupLabel>SANDBOX</GroupLabel>
        <AgentSandboxSetting />
      </div>
      <div className="flex flex-col gap-3">
        <GroupLabel>FILE CHECKPOINTS</GroupLabel>
        <AgentCheckpointSetting />
      </div>
    </TabColumn>
  );
}

export function AgentNotificationsPage() {
  return (
    <TabColumn>
      <SectionHeader
        title="Notifications & remote"
        description="Get told when an agent needs you, and connect your phone to this DevKit to choose what it can access."
      />
      <div className="flex flex-col gap-3">
        <GroupLabel>NOTIFICATIONS</GroupLabel>
        <AgentNotificationSetting />
      </div>
      <div className="flex flex-col gap-3">
        <GroupLabel>REMOTE CONTROL</GroupLabel>
        <TelegramSetting />
      </div>
    </TabColumn>
  );
}

export function AgentMcpPage() {
  return (
    <TabColumn>
      <SectionHeader title="DevKit as MCP" description="Connect external agents to DevKit tools through its local MCP proxy." />
      <McpInstallPanel />
    </TabColumn>
  );
}
