import { toAppError } from "@/bridge/errors";

const PROJECT_CHANGED = "project_changed";

/**
 * The write was refused because the active project changed after the file was
 * opened (Go's ErrProjectChanged, mapped in internal/bridge/errors.go).
 */
export function isProjectChangedError(error: unknown): boolean {
  return toAppError(error).code === PROJECT_CHANGED;
}

/**
 * Having no plan is the normal state for most projects; Go reports it as a
 * validation failure reading "No active plan." Anything else from the plan
 * calls is a real failure and must not be dressed up as "no plan yet".
 */
export function isNoPlanError(error: unknown): boolean {
  const e = toAppError(error);
  return e.code === "validation_failed" && /no active plan/i.test(e.message);
}
