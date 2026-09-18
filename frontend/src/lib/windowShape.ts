import { Events } from "@wailsio/runtime";

/** window_shape.go's windowShapeRequestEvent and windowShapeEvent. */
export const WINDOW_SHAPE_REQUEST_EVENT = "devkit:window-shape:request";
export const WINDOW_SHAPE_EVENT = "devkit:window-shape";

/**
 * The window's corner radius as the payload states it, or null when it says
 * nothing usable — in which case --window-radius keeps index.css's default.
 */
export function radiusFromPayload(data: unknown): number | null {
  const payload = Array.isArray(data) ? data[0] : data;
  const radius = (payload as { radius?: unknown } | null)?.radius;
  return typeof radius === "number" && Number.isFinite(radius) && radius > 0 && radius < 64 ? radius : null;
}

/**
 * Asks the backend for the OS window's corner radius and sets --window-radius,
 * from which the shell cards take theirs (--shell-radius, index.css). Asked
 * rather than waited for: the page can load, or reload, after anything the
 * backend sent at startup, and an answer to a question is never missed.
 */
export function installWindowShape(root: HTMLElement = document.documentElement): () => void {
  const off = Events.On(WINDOW_SHAPE_EVENT, (event) => {
    const radius = radiusFromPayload(event.data);
    if (radius !== null) root.style.setProperty("--window-radius", `${radius}px`);
  });
  void Events.Emit(WINDOW_SHAPE_REQUEST_EVENT).catch(() => undefined);
  return off;
}
