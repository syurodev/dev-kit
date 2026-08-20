import { useEffect, useRef, useState } from "react";
import { Wrench } from "lucide-react";
import { LazyMotion, domAnimation } from "framer-motion";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import { useDockStore } from "./dock-store";
import { DockItem } from "./DockItem";
import { CollapsedDock } from "./CollapsedDock";
import { isPointerNear, shortcutLabel } from "./dockActivation";
import { AgentHubDockPanel } from "./AgentHubDockPanel";
import { getModule } from "../../modules/registry";
import type { DockEntry } from "./types";

export type DockModule = { id: string; title: string; unavailable?: boolean };

type DockProps = {
  modules: DockModule[];
  activeModuleId: string;
  onSelectModule: (moduleId: string) => void;
};

/** Pointer distance that expands the dock. Generous on purpose — it costs
 *  nothing, because the dock never claims events outside its own pixels. */
const ACTIVATION_MARGIN = 64;
/** Rows from the window bottom worth measuring at all. */
const BOTTOM_ZONE = 160;

export function Dock({ modules, activeModuleId, onSelectModule }: DockProps) {
  const sessions = useAgentSessionsStore((state) => state.sessions);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const contextItems = useDockStore((state) => state.contextItems);
  const pillRef = useRef<HTMLDivElement | null>(null);
  const [pointerNear, setPointerNear] = useState(false);
  const [focusWithin, setFocusWithin] = useState(false);
  const expanded = pointerNear || focusWithin;

  useEffect(() => {
    let frame = 0;
    let pending: { x: number; y: number } | null = null;
    function onPointerMove(event: PointerEvent) {
      // Cheap reject before touching layout: the dock lives at the bottom, so
      // most of the window's pointer traffic never needs a rect read.
      if (event.clientY < window.innerHeight - BOTTOM_ZONE) {
        // Drop a frame already scheduled from an in-zone move: it would fire
        // after this one and set pointerNear back to true with stale
        // coordinates, leaving the dock expanded over content with the
        // pointer at rest and no further moves coming to correct it.
        if (frame) {
          cancelAnimationFrame(frame);
          frame = 0;
        }
        // Unconditional: useState bails out when the value is unchanged, so
        // this needs no `pointerNear` read and the listener can be installed
        // once for the component's life instead of re-bound on every flip.
        setPointerNear(false);
        return;
      }
      // Trailing edge: a burst of moves inside one frame is tested at its
      // last position, not its first.
      pending = { x: event.clientX, y: event.clientY };
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        // Measured inside the callback so it reflects post-expansion layout
        // rather than the layout that was current when the event fired.
        const rect = pillRef.current?.getBoundingClientRect();
        if (!rect || !pending) return;
        setPointerNear(isPointerNear(pending, rect, ACTIVATION_MARGIN));
      });
    }
    window.addEventListener("pointermove", onPointerMove);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  const baseItems: DockEntry[] = modules.map((module, index) => {
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
      shortcut: shortcutLabel(index),
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
    // Do NOT add framer's `layout` here to animate the pill's size change:
    // layout projection ships only in `domMax`, so under `domAnimation` the
    // prop is a silent no-op, and `domMax` measured +14.2 KiB gzip on the
    // initial bundle. The box snaps; the content fades in below instead.
    <LazyMotion features={domAnimation}>
      <nav className="pointer-events-none fixed inset-x-0 bottom-2 z-50 flex justify-center">
        <div
          ref={pillRef}
          // A tab stop only while collapsed, because the collapsed pill has no
          // focusable content of its own (CollapsedDock is aria-hidden lines).
          // Tabbing here fires onFocusCapture, which expands the dock, and the
          // next Tab steps into the real module buttons. Focus survives the
          // flip to -1, so the pill does not steal a second stop once open.
          tabIndex={expanded ? -1 : 0}
          aria-label="Switch module"
          onFocusCapture={() => setFocusWithin(true)}
          onBlurCapture={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
              setFocusWithin(false);
            }
          }}
          className={cn(
            "pointer-events-auto flex items-center border border-foreground/10 bg-card/90 backdrop-blur-xl",
            expanded ? "rounded-2xl p-1.5" : "rounded-full",
          )}
        >
          {/* Keyed so the swap remounts and re-runs the fade-in — the
              outgoing form is unmounted synchronously, so only the entering
              one animates. CSS, not a motion component: tw-animate-css is
              already loaded app-wide and a class costs no JS. */}
          <div
            key={expanded ? "expanded" : "collapsed"}
            className="flex animate-in items-center gap-1 fade-in-0 duration-150"
          >
            {expanded ? (
              items.map((item) => <DockItem key={item.id} item={item} />)
            ) : (
              // baseItems, not items: a module-contributed contextItem must
              // not add a line to a pill whose whole job is saying which
              // module you are in.
              <CollapsedDock
                count={baseItems.length}
                activeIndex={baseItems.findIndex((i) => i.isActive)}
              />
            )}
          </div>
        </div>
      </nav>
    </LazyMotion>
  );
}
