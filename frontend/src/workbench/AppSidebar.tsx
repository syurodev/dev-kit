// frontend/src/workbench/AppSidebar.tsx
import { useState } from "react";
import {
  Bot,
  ChevronRight,
  Database,
  FileText,
  RefreshCw,
  Settings,
  Terminal,
  Workflow,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "../stores/agentSessions";
import type { SessionStatus } from "../bridge/agenthub";

const MODULE_ICONS: Record<string, LucideIcon> = {
  tools: Wrench,
  notes: FileText,
  database: Database,
  ssh: Terminal,
  kafka: Workflow,
  agenthub: Bot,
  sync: RefreshCw,
  settings: Settings,
};

export type SidebarModule = { id: string; title: string; unavailable?: boolean };

type AppSidebarProps = {
  modules: SidebarModule[];
  activeModuleId: string;
  onSelectModule: (moduleId: string) => void;
};

function StatusDot({ status }: { status: SessionStatus }) {
  const colorClass =
    status === "running"
      ? "bg-(--status-running)"
      : status === "awaiting_permission"
        ? "bg-(--status-permission)"
        : status === "error"
          ? "bg-(--status-error)"
          : "bg-(--status-idle)";
  return <span className={cn("size-1.5 shrink-0 rounded-full", colorClass)} />;
}

function sessionLabel(session: { title: string | null; agentType: string; id: string }) {
  return session.title?.trim() || `${session.agentType} · ${session.id.slice(0, 8)}`;
}

export function AppSidebar({ modules, activeModuleId, onSelectModule }: AppSidebarProps) {
  const sessions = useAgentSessionsStore((state) => state.sessions);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const [agentHubOpen, setAgentHubOpen] = useState(true);
  const hasPermissionRequest = sessions.some((s) => s.status === "awaiting_permission");

  return (
    <Sidebar variant="inset" collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <div className="flex size-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Bot className="size-3.5" />
          </div>
          <span className="text-sm font-semibold group-data-[collapsible=icon]:hidden">
            DevKit
          </span>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Workspace</SidebarGroupLabel>
          <SidebarMenu>
            {modules.map((module) => {
              const Icon = MODULE_ICONS[module.id] ?? Wrench;

              if (module.id !== "agenthub") {
                return (
                  <SidebarMenuItem key={module.id}>
                    <SidebarMenuButton
                      isActive={module.id === activeModuleId}
                      disabled={module.unavailable}
                      aria-disabled={module.unavailable}
                      tooltip={module.title}
                      onClick={() => onSelectModule(module.id)}
                    >
                      <Icon />
                      <span>{module.title}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              }

              const badgeContent = sessions.length > 0 ? String(sessions.length) : null;

              return (
                <Collapsible
                  key={module.id}
                  open={agentHubOpen}
                  onOpenChange={setAgentHubOpen}
                  className="group/collapsible"
                >
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={module.id === activeModuleId}
                      disabled={module.unavailable}
                      aria-disabled={module.unavailable}
                      onClick={() => onSelectModule(module.id)}
                      tooltip={
                        sessions.length > 0
                          ? {
                              children: (
                                <div className="flex flex-col gap-1">
                                  {sessions.map((s) => (
                                    <div key={s.id} className="flex items-center gap-2 text-xs">
                                      <StatusDot status={s.status} />
                                      <span>{sessionLabel(s)}</span>
                                    </div>
                                  ))}
                                </div>
                              ),
                            }
                          : module.title
                      }
                    >
                      <Icon />
                      <span>{module.title}</span>
                    </SidebarMenuButton>

                    {badgeContent ? (
                      <SidebarMenuBadge
                        className={cn(
                          "group-data-[collapsible=icon]:right-0.5 group-data-[collapsible=icon]:top-0.5 group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:size-2 group-data-[collapsible=icon]:min-w-0 group-data-[collapsible=icon]:p-0 group-data-[collapsible=icon]:text-[0px]",
                          hasPermissionRequest &&
                            "bg-(--status-permission-soft) text-(--status-permission)",
                        )}
                      >
                        {badgeContent}
                      </SidebarMenuBadge>
                    ) : null}

                    {sessions.length > 0 ? (
                      <CollapsibleTrigger asChild>
                        <button
                          type="button"
                          className="absolute top-1.5 right-7 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden"
                          aria-label={`Toggle ${module.title} sessions`}
                        >
                          <ChevronRight className="size-3.5 transition-transform group-data-[state=open]/collapsible:rotate-90" />
                        </button>
                      </CollapsibleTrigger>
                    ) : null}

                    <CollapsibleContent>
                      <SidebarMenuSub>
                        {sessions.map((s) => (
                          <SidebarMenuSubItem key={s.id}>
                            <SidebarMenuSubButton
                              onClick={() => {
                                onSelectModule("agenthub");
                                setActiveSessionId(s.id);
                              }}
                            >
                              <StatusDot status={s.status} />
                              <span>{sessionLabel(s)}</span>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        ))}
                      </SidebarMenuSub>
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              );
            })}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground group-data-[collapsible=icon]:justify-center">
          <span className="size-1.5 shrink-0 rounded-full bg-(--status-running)" />
          <span className="group-data-[collapsible=icon]:hidden">Vault unlocked</span>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
