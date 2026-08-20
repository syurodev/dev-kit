import { useEffect, useLayoutEffect, useRef, useState } from "react";
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
  const collapsedRef = useRef<HTMLDivElement | null>(null);
  const expandedRef = useRef<HTMLDivElement | null>(null);
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

  // Both forms are absolutely positioned, so the pill has no size of its own
  // to hug — and an auto width is exactly what CSS cannot transition. Measure
  // the form coming in and write the numbers straight to the box: one read per
  // flip, no state, no extra render. The box is box-content, so these are the
  // content dimensions and the 1px border falls outside them.
  //
  // Auto width was tried first, as a 2x2 grid morphing `1fr 0fr` <-> `0fr 1fr`
  // with one form per diagonal cell. The tracks do animate, but the pill does
  // not: complementary fr tracks in an indefinitely-sized container always sum
  // to the same total, so the box jumped to its full size on the first frame
  // and only the tracks traded width underneath. Measured, not assumed.
  useLayoutEffect(() => {
    const box = pillRef.current;
    const form = (expanded ? expandedRef : collapsedRef).current;
    if (!box || !form) return;
    box.style.width = `${form.offsetWidth}px`;
    box.style.height = `${form.offsetHeight}px`;
  }, [expanded, items.length]);

  // Centered on the box so both forms stay put while it springs around them.
  //
  // The fades are staggered against the spring rather than run alongside it,
  // because a form is only ever as wide as the box at the two ENDS of a morph.
  // Fading the icons in from t0 left the outermost two visibly outside the
  // pill for the first ~100ms. So each form is on screen only while the box is
  // big enough to hold it: opening, the icons wait for the spring to overshoot
  // past its target (280px by 150ms, target 262); closing, they leave first
  // and the box waits them out before it starts shrinking. The dashes take the
  // same delay on the way in so the two never overlap at half opacity.
  //
  // Checked frame by frame in a browser, both directions: no icon and no dash
  // ever crosses the pill's edge. Change a duration or a delay here and that
  // stops being true.
  const form =
    "absolute top-1/2 left-1/2 w-max -translate-x-1/2 -translate-y-1/2 motion-safe:transition-opacity";

  return (
    // Do NOT reach for framer's `layout` to morph the pill: layout projection
    // ships only in `domMax`, so under `domAnimation` the prop is a silent
    // no-op, and `domMax` measured +14.2 KiB gzip on the initial bundle. The
    // layout effect above costs nothing. LazyMotion stays because DockItem's
    // AgentHub panel needs a features ancestor.
    <LazyMotion features={domAnimation}>
      <nav className="pointer-events-none fixed inset-x-0 bottom-2 z-50 flex justify-center">
        <div
          ref={pillRef}
          // A tab stop only while collapsed, because the collapsed pill has no
          // focusable content of its own (CollapsedDock is aria-hidden lines).
          // Tabbing here fires onFocusCapture, which expands the dock, and the
          // next Tab steps into the real module buttons. Focus survives the
          // flip to -1, so the pill does not steal a second stop once open.
          //
          // Two consequences a maintainer would not expect, both accepted: the
          // flip is driven by `expanded`, so a keyboard-only user's tab order
          // varies with where the mouse happens to be resting, and Shift+Tab
          // from the first module button exits the nav rather than returning
          // to the pill (it is already -1 by then).
          tabIndex={expanded ? -1 : 0}
          // A group of controls, honestly labelled. Not role="button": Enter
          // and Space do nothing here, and promising activation that does not
          // exist is worse than a plain labelled focus stop.
          role="group"
          aria-label="Switch module"
          // :focus-visible, not bare focus. On WebView2 and WebKitGTK clicking
          // a module focuses its button, which would latch focusWithin true and
          // strand the dock expanded over content the moment the pointer left,
          // with no pointer motion able to recover it. WKWebView does not focus
          // buttons on click, so this is invisible on macOS — do not simplify
          // it back to a bare setter.
          onFocusCapture={(e) => {
            if ((e.target as HTMLElement).matches(":focus-visible")) {
              setFocusWithin(true);
            }
          }}
          onBlurCapture={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
              setFocusWithin(false);
            }
          }}
          className={cn(
            "pointer-events-auto relative box-content border border-foreground/10 bg-card/90 backdrop-blur-xl",
            // No overflow-hidden: DockItem's AgentHub panel opens above the
            // pill (absolute bottom-full) and a clipping ancestor would swallow
            // it. The outgoing form is out of sight on opacity alone.
            "motion-safe:transition-[width,height,border-radius]",
            expanded
              // Opening pops: liquid-taffy's house spring, 22% overshoot.
              ? "rounded-2xl motion-safe:duration-500 motion-safe:ease-house-spring"
              // Closing does not. The same spring undershoots ~30px below the
              // resting width on its way back, and with nothing clipping the
              // pill the dashes hung outside it for ~11 frames. That family's
              // OUT_STRONG is the curve for an exit, and an exit is what this
              // is — the pop belongs to the thing arriving.
              //
              // rounded-[7.5px] is half the resting pill's own height
              // (CollapsedDock's 3px dash between 5px paddings) — a true pill,
              // and unlike rounded-full a value that interpolates: 9999px
              // stays clamped to fully-round for the whole morph and then
              // snaps to 16px at the end. delay-100: see `form` above — the
              // shrink waits out the icons' fade.
              : "rounded-[7.5px] motion-safe:delay-100 motion-safe:duration-[350ms] motion-safe:ease-out-strong",
          )}
        >
          <div
            ref={collapsedRef}
            className={cn(
              form,
              expanded ? "opacity-0 duration-150" : "opacity-100 delay-100 duration-150",
            )}
          >
            {/* baseItems, not items: a module-contributed contextItem must not
                add a line to a pill whose whole job is saying which module you
                are in. */}
            <CollapsedDock
              count={baseItems.length}
              activeIndex={baseItems.findIndex((i) => i.isActive)}
            />
          </div>
          {/* Both forms stay mounted so either can be measured on demand, which
              leaves seven real buttons sitting invisibly over the resting pill.
              opacity-0 hides them from the eye but not from the tab order, the
              accessibility tree, or the pointer — `inert` is what takes them
              out of all three. */}
          <div
            ref={expandedRef}
            inert={!expanded}
            className={cn(
              form,
              "flex items-center gap-1 p-1.5",
              expanded ? "opacity-100 delay-150 duration-150" : "opacity-0 duration-100",
            )}
          >
            {items.map((item) => (
              <DockItem key={item.id} item={item} />
            ))}
          </div>
        </div>
      </nav>
    </LazyMotion>
  );
}
