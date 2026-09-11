/**
 * Reading resolved theme colours out of the document.
 *
 * Most of the app never needs this: CSS custom properties are what the
 * stylesheet consumes, and a CodeMirror theme can write `var(--surface-base)`
 * straight into its rules. Three surfaces cannot, because they do not render
 * through CSS at all — xterm paints to a canvas, Mermaid is handed a colour
 * map before it builds an SVG, and both need real values.
 *
 * Resolved by USING each token rather than by reading it back. A custom
 * property's computed value has `var()` substituted but not `color-mix()`
 * evaluated — it stays as the function text — so `getPropertyValue` returns
 * something unusable for exactly the tokens Phase 0 made derived. Assigning it
 * to a real colour property and reading the computed result is what forces the
 * engine to produce an rgb().
 */

let probe: HTMLElement | null = null;

function probeElement(): HTMLElement {
  if (probe?.isConnected) {
    return probe;
  }
  probe = document.createElement("span");
  // Out of flow, out of the a11y tree, and no paint of its own.
  probe.setAttribute("aria-hidden", "true");
  probe.style.cssText = "position:absolute;width:0;height:0;visibility:hidden;pointer-events:none";
  document.body.append(probe);
  return probe;
}

/**
 * The resolved `rgb(…)` for any CSS colour expression.
 *
 * Takes an expression, not just a token, because the consumers compose: a
 * terminal's "bright" red is its red carried toward the foreground, which is
 * a color-mix, and xterm parses `#rgb`, `rgb()` and named colours — not CSS
 * colour functions. Handing it the unevaluated text would have it silently
 * fall back. Assigning the expression and reading the computed value is what
 * makes the engine do the arithmetic.
 */
export function resolveColor(expression: string): string {
  if (typeof document === "undefined") {
    return "";
  }
  const element = probeElement();
  element.style.color = "";
  element.style.color = expression;
  return element.style.color ? toRgb(getComputedStyle(element).color) : "";
}

/**
 * `color(srgb 0.87 0.61 0.64)` → `rgb(223, 156, 164)`.
 *
 * A computed color-mix comes back in the `color()` function notation, which is
 * a perfectly good CSS colour and useless to a canvas library: xterm parses
 * `#rgb`, `rgb()` and named colours, so it would have taken this as invalid
 * and silently fallen back. Found by reading the palette back in a browser —
 * twice, because the first fix resolved the mix and left it in `color()`.
 */
function toRgb(value: string): string {
  if (!value.startsWith("color(")) {
    return value;
  }
  const parts = value.match(/-?[\d.]+/g);
  if (!parts || parts.length < 3) {
    return value;
  }
  const [r, g, b, a] = parts.map(Number);
  const channel = (c: number) => Math.max(0, Math.min(255, Math.round(c * 255)));
  return a !== undefined && a < 1
    ? `rgba(${channel(r)}, ${channel(g)}, ${channel(b)}, ${a})`
    : `rgb(${channel(r)}, ${channel(g)}, ${channel(b)})`;
}

/** The resolved `rgb(…)` for one token, or "" when it does not resolve. */
export function readThemeColor(token: string): string {
  return resolveColor(`var(${token})`);
}

/** The resolved colours for several tokens, in one pass. */
export function readThemeColors<T extends readonly string[]>(
  tokens: T,
): Record<T[number], string> {
  const out = {} as Record<T[number], string>;
  for (const token of tokens) {
    out[token as T[number]] = readThemeColor(token);
  }
  return out;
}

/**
 * Calls back whenever the palette changes.
 *
 * Watches the two attributes that decide it rather than subscribing to React
 * state: xterm's theme lives inside an effect that already owns its own
 * subscriptions, and a canvas has no business reaching into a context to find
 * out what colour it is. It also catches a mode flip driven by the OS, which
 * arrives as an attribute change and nothing else.
 */
export function onThemeChange(listener: () => void): () => void {
  if (typeof MutationObserver === "undefined") {
    return () => {};
  }
  const observer = new MutationObserver(listener);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme", "data-theme-family"],
  });
  return () => observer.disconnect();
}
