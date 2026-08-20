import { useEffect, useRef, useState } from "react";

/**
 * Pointer-drag resize with min/max clamping, animated via the caller's own
 * framer-motion element. Extracted from components/ui/split-panel.tsx's
 * original inline implementation — SidebarSurface (workbench/) is the
 * second consumer that made sharing it worth doing.
 */
export function useResizableWidth(opts: {
  width: number;
  onWidthChange: (width: number) => void;
  min: number;
  max: number;
  /** +1: dragging right grows the panel (Sidebar). -1: dragging left grows it (SplitPanel). */
  sign: 1 | -1;
}) {
  const { width, onWidthChange, min, max, sign } = opts;
  const [isResizing, setIsResizing] = useState(false);
  const resizeStart = useRef<{ x: number; width: number } | null>(null);

  useEffect(() => {
    if (!isResizing) return;
    function onMove(e: PointerEvent) {
      if (!resizeStart.current) return;
      const delta = sign * (e.clientX - resizeStart.current.x);
      onWidthChange(Math.min(max, Math.max(min, resizeStart.current.width + delta)));
    }
    function onUp() {
      resizeStart.current = null;
      setIsResizing(false);
    }
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    return () => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
  }, [isResizing, min, max, sign, onWidthChange]);

  function onPointerDown(e: React.PointerEvent) {
    e.preventDefault();
    resizeStart.current = { x: e.clientX, width };
    setIsResizing(true);
  }

  return { isResizing, onPointerDown };
}
