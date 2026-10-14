import { useEffect, useRef, useState } from "react";
import { shellDrag } from "@/lib/panelMotion";

/**
 * Pointer-drag resize with min/max clamping, animated via the caller's own
 * framer-motion element. Extracted from components/ui/split-panel.tsx's
 * original inline implementation — SidebarSurface (workbench/) was the
 * second consumer that made sharing it worth doing, and BottomSlotSurface
 * the third, which is what added the axis.
 */
export function useResizableSize(opts: {
  size: number;
  onSizeChange: (size: number) => void;
  min: number;
  max: number;
  /** Which edge the handle drags along. */
  axis: "x" | "y";
  /**
   * Which way along that axis grows the panel. The rule is the same on both
   * axes: +1 when the panel lies *after* its handle in that direction
   * (Sidebar — handle on its right, dragging right grows it), -1 when it lies
   * before (SplitPanel — handle on its left; BottomSlot — handle on its top,
   * dragging up grows it).
   */
  sign: 1 | -1;
}) {
  const { size, onSizeChange, min, max, axis, sign } = opts;
  const [isResizing, setIsResizing] = useState(false);
  const resizeStart = useRef<{ position: number; size: number } | null>(null);

  useEffect(() => {
    if (!isResizing) return;
    function onMove(e: PointerEvent) {
      if (!resizeStart.current) return;
      const position = axis === "x" ? e.clientX : e.clientY;
      const delta = sign * (position - resizeStart.current.position);
      onSizeChange(Math.min(max, Math.max(min, resizeStart.current.size + delta)));
    }
    function onUp() {
      resizeStart.current = null;
      setIsResizing(false);
    }
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.body.style.cursor = axis === "x" ? "col-resize" : "row-resize";
    document.body.style.userSelect = "none";
    // Every shell panel follows a drag instantly, not only this one: the
    // sidebar and the columns clamp against each other (lib/panelMotion).
    shellDrag.set(true);
    return () => {
      shellDrag.set(false);
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
  }, [isResizing, min, max, axis, sign, onSizeChange]);

  function onPointerDown(e: React.PointerEvent) {
    e.preventDefault();
    resizeStart.current = {
      position: axis === "x" ? e.clientX : e.clientY,
      size,
    };
    setIsResizing(true);
  }

  return { isResizing, onPointerDown };
}
