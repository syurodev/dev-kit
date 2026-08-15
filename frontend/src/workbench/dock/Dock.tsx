import { useEffect, useRef, useState } from "react";
import { Wrench } from "lucide-react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import { useDockStore } from "./dock-store";
import { DockItem } from "./DockItem";
import { AgentHubDockPanel } from "./AgentHubDockPanel";
import { getModule } from "../../modules/registry";
import type { DockEntry } from "./types";

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
  const leaveTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (leaveTimeoutRef.current) {
        clearTimeout(leaveTimeoutRef.current);
      }
    };
  }, []);

  function handleHoverStart() {
    if (leaveTimeoutRef.current) {
      clearTimeout(leaveTimeoutRef.current);
      leaveTimeoutRef.current = null;
    }
    setIsHovered(true);
  }

  function handleHoverEnd() {
    leaveTimeoutRef.current = setTimeout(() => setIsHovered(false), 250);
  }

  const baseItems: DockEntry[] = modules.map((module) => {
    const isAgentHub = module.id === "agenthub";
    const registeredModule = getModule(module.id);
    const badge = isAgentHub && sessions.length > 0 ? String(sessions.length) : null;
    const urgent = isAgentHub && sessions.some((s) => s.status === "awaiting_permission");
    return {
      id: module.id,
      title: module.title,
      // Each module registers its own icon on its manifest (see e.g.
      // modules/notes/manifest.ts) — this falls back to Wrench only for an
      // id with no matching registered module, which validateModuleManifest
      // makes unreachable for anything actually passed in here.
      icon: registeredModule?.icon ?? Wrench,
      isActive: module.id === activeModuleId,
      unavailable: module.unavailable,
      badge,
      urgent,
      onClick: () => onSelectModule(module.id),
      onPreload: () => {
        const preload = registeredModule?.views[0]?.preload?.();
        // A click still renders through Suspense and owns any load error. A
        // speculative hover must never create an unhandled rejection.
        void preload?.catch(() => undefined);
      },
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
          onHoverStart={handleHoverStart}
          onHoverEnd={handleHoverEnd}
          animate={{ scale: isHovered ? 1 : 0.55, y: isHovered ? 0 : 14 }}
          transition={{ type: "spring", stiffness: 300, damping: 30, mass: 0.8 }}
          className="pointer-events-auto flex items-center gap-1 rounded-2xl border border-foreground/10 bg-card/90 p-1.5 backdrop-blur-xl"
        >
          {items.map((item) => (
            <DockItem key={item.id} item={item} />
          ))}
        </m.div>
      </nav>
    </LazyMotion>
  );
}
