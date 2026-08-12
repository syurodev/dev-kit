import { useState } from "react";
import {
  Bot,
  Database,
  FileText,
  RefreshCw,
  Settings,
  Terminal,
  Workflow,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import { useDockStore } from "./dock-store";
import { DockItem } from "./DockItem";
import { AgentHubDockPanel } from "./AgentHubDockPanel";
import type { DockEntry } from "./types";

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

export type DockModule = { id: string; title: string; unavailable?: boolean };

type DockProps = {
  modules: DockModule[];
  activeModuleId: string;
  onSelectModule: (moduleId: string) => void;
};

export function Dock({ modules, activeModuleId, onSelectModule }: DockProps) {
  const sessions = useAgentSessionsStore((state) => state.sessions);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const contextItems = useDockStore((state) => state.contextItems);
  const [isHovered, setIsHovered] = useState(false);

  const baseItems: DockEntry[] = modules.map((module) => {
    const isAgentHub = module.id === "agenthub";
    const badge = isAgentHub && sessions.length > 0 ? String(sessions.length) : null;
    const urgent = isAgentHub && sessions.some((s) => s.status === "awaiting_permission");
    return {
      id: module.id,
      title: module.title,
      icon: MODULE_ICONS[module.id] ?? Wrench,
      isActive: module.id === activeModuleId,
      unavailable: module.unavailable,
      badge,
      urgent,
      onClick: () => onSelectModule(module.id),
      panel:
        isAgentHub && sessions.length > 0
          ? () => (
              <AgentHubDockPanel
                sessions={sessions}
                onSelect={(sessionId) => {
                  onSelectModule("agenthub");
                  setActiveSessionId(sessionId);
                }}
              />
            )
          : undefined,
    };
  });

  const items = [...baseItems, ...contextItems];

  return (
    <LazyMotion features={domAnimation}>
      <nav className="pointer-events-none fixed inset-x-0 bottom-6 z-50 flex justify-center">
        <m.div
          onHoverStart={() => setIsHovered(true)}
          onHoverEnd={() => setIsHovered(false)}
          animate={{ scale: isHovered ? 1 : 0.75 }}
          transition={{ type: "spring", stiffness: 300, damping: 30, mass: 0.8 }}
          className="pointer-events-auto flex items-center gap-1 rounded-2xl border border-white/10 bg-card/90 p-1.5 backdrop-blur-xl"
        >
          {items.map((item) => (
            <DockItem key={item.id} item={item} />
          ))}
        </m.div>
      </nav>
    </LazyMotion>
  );
}
