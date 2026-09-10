import { useCallback, useEffect, useRef, useState } from "react";

/** How long the button says "Copied" before going back. */
const CONFIRM_MS = 1200;

/**
 * Copy, with a confirmation that resets itself.
 *
 * The timer is held in a ref and cleared on unmount: copying and then leaving
 * the module would otherwise set state on a component that is gone, and copying
 * twice in quick succession would leave two timers racing to decide when the
 * label goes back.
 */
export function useCopyToClipboard() {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  const copy = useCallback(async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), CONFIRM_MS);
    } catch {
      // A denied clipboard is not worth an error card: the text is on screen
      // and selectable, which is the fallback either way.
      setCopied(false);
    }
  }, []);

  return { copy, copied };
}
