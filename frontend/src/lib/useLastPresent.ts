import { useState } from "react";

/**
 * The value, or the last non-null one it had.
 *
 * For dialogs opened by `open={!!thing}`: closing sets `thing` to null in the
 * same render the exit animation starts, so without this the dialog fades out
 * with its title already blank ("Delete ?"). What it shows while leaving is
 * what it showed while open.
 */
export function useLastPresent<T>(value: T | null | undefined): T | null | undefined {
  const [last, setLast] = useState(value);
  if (value != null && value !== last) setLast(value);
  return value ?? last;
}
