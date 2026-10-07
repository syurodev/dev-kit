/** Shared, theme-aware control surface. Geometry remains owned by each control. */
export const CONTROL_SURFACE =
  "border border-border-subtle bg-surface-well text-text-primary transition-control"

/** Focus adds a ring without changing the control's dimensions. */
export const CONTROL_FOCUS =
  "focus-visible:border-status-info focus-visible:ring-2 focus-visible:ring-status-info/20 focus-visible:outline-none aria-invalid:border-status-danger aria-invalid:ring-status-danger/15"
