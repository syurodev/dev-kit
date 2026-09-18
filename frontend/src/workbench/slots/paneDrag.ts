import type { DropTarget, SlotId } from "./slotModel";

/** Pointer travel before a press on a pane header becomes a drag. */
const THRESHOLD = 5;
/**
 * The largest a lifted card is carried at. A pane in the bottom row can be the
 * width of the document; carried at full size it covers the targets it is
 * being dragged to, and leaves the screen as soon as it moves.
 */
const LIFT_MAX_WIDTH = 480;
const LIFT_MAX_HEIGHT = 320;

/**
 * Lifts a pane card off its slot and carries it with the pointer, live
 * content and all — the card itself moves, not a ghost of it.
 *
 * Imperative on purpose: a drag is 60 events a second, and none of them needs
 * React. The card is taken out of flow with position: fixed while its slot
 * wrapper keeps its size, so nothing reflows under the pointer and the empty
 * wrapper marks where Esc will put the pane back. React hears about the drag
 * once, on drop.
 */
export function startPaneDrag(opts: {
  down: PointerEvent;
  card: HTMLElement;
  /** The card's slot wrapper: stays in place and shows the origin. */
  origin: HTMLElement;
  hint: HTMLElement;
  /** Called at lift, before the card leaves flow: reads the layout once. */
  measure: () => (x: number, y: number) => DropTarget | null;
  onDrop: (slot: SlotId) => void;
}) {
  const { down, card, origin, hint, measure, onDrop } = opts;
  if (down.button !== 0) return;

  const startX = down.clientX;
  const startY = down.clientY;
  let targetAt: ((x: number, y: number) => DropTarget | null) | null = null;
  let current: DropTarget | null = null;
  const label = hint.querySelector<HTMLElement>("[data-hint-label]");

  const lift = () => {
    targetAt = measure();
    const r = card.getBoundingClientRect();
    // Shrunk around the pointer: it keeps its place across the header, and
    // stays on the header, so the card still hangs from where it was grabbed.
    const width = Math.min(r.width, LIFT_MAX_WIDTH);
    const height = Math.min(r.height, LIFT_MAX_HEIGHT);
    const left = startX - (startX - r.left) * (width / r.width);
    const top = startY - Math.min(startY - r.top, height / 2);
    Object.assign(card.style, {
      position: "fixed",
      left: `${left}px`,
      top: `${top}px`,
      width: `${width}px`,
      height: `${height}px`,
      zIndex: "60",
      pointerEvents: "none",
    });
    card.dataset.lifted = "true";
    origin.dataset.origin = "true";
    document.body.style.cursor = "grabbing";
    document.body.style.userSelect = "none";
    window.getSelection()?.removeAllRanges();
  };

  const showHint = (t: DropTarget | null) => {
    current = t;
    if (!t) {
      hint.dataset.show = "false";
      return;
    }
    Object.assign(hint.style, {
      left: `${t.rect.left}px`,
      top: `${t.rect.top}px`,
      width: `${t.rect.right - t.rect.left}px`,
      height: `${t.rect.bottom - t.rect.top}px`,
    });
    if (label) label.textContent = t.label;
    hint.dataset.show = "true";
  };

  const onMove = (e: PointerEvent) => {
    const dx = e.clientX - startX;
    const dy = e.clientY - startY;
    if (!targetAt) {
      if (Math.hypot(dx, dy) < THRESHOLD) return;
      lift();
    }
    e.preventDefault();
    card.style.transform = `translate(${dx}px, ${dy}px)`;
    showHint(targetAt!(e.clientX, e.clientY));
  };

  const end = (commit: boolean) => {
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
    window.removeEventListener("pointercancel", onCancel);
    window.removeEventListener("keydown", onKey, true);
    if (!targetAt) return;
    for (const p of ["position", "left", "top", "width", "height", "zIndex", "pointerEvents", "transform"] as const) {
      card.style[p] = "";
    }
    delete card.dataset.lifted;
    delete origin.dataset.origin;
    document.body.style.cursor = "";
    document.body.style.userSelect = "";
    const slot = current?.slot;
    showHint(null);
    if (commit && slot) onDrop(slot);
  };
  const onUp = () => end(true);
  const onCancel = () => end(false);
  const onKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape") return;
    e.preventDefault();
    e.stopPropagation();
    end(false);
  };

  window.addEventListener("pointermove", onMove);
  window.addEventListener("pointerup", onUp);
  window.addEventListener("pointercancel", onCancel);
  window.addEventListener("keydown", onKey, true);
}
