type AppErrorPayload = {
  code: string;
  message: string;
  recoverable: boolean;
  details?: Record<string, unknown>;
};

// AppError is the single frontend error contract. It extends Error so React
// views and third-party tooling can use normal Error semantics while retaining
// the structured fields returned by the Go bridge.
export class AppError extends Error {
  readonly code: string;
  readonly recoverable: boolean;
  readonly details?: Record<string, unknown>;

  constructor(payload: AppErrorPayload) {
    super(payload.message);
    this.name = "AppError";
    this.code = payload.code;
    this.recoverable = payload.recoverable;
    this.details = payload.details;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function parsePayload(value: unknown): AppErrorPayload | undefined {
  if (!isRecord(value) || typeof value.code !== "string" || typeof value.message !== "string") {
    return undefined;
  }

  return {
    code: value.code,
    message: value.message,
    recoverable: value.recoverable === true,
    details: isRecord(value.details) ? value.details : undefined,
  };
}

// Wails wraps application errors in RuntimeError and places the structured Go
// payload in `cause`. Walk only a small, bounded cause chain so malformed or
// cyclic third-party errors cannot make normalization recurse indefinitely.
function findPayload(error: unknown): AppErrorPayload | undefined {
  const seen = new Set<unknown>();
  let current = error;

  for (let depth = 0; depth < 4 && isRecord(current) && !seen.has(current); depth += 1) {
    seen.add(current);
    const payload = parsePayload(current);
    if (payload) {
      return payload;
    }
    current = current.cause;
  }

  return undefined;
}

export function toAppError(error: unknown): AppError {
  if (error instanceof AppError) {
    return error;
  }

  const payload = findPayload(error);
  if (payload) {
    return new AppError(payload);
  }

  if (error instanceof Error) {
    return new AppError({
      code: "unknown",
      message: error.message,
      recoverable: false,
    });
  }

  return new AppError({
    code: "unknown",
    message: typeof error === "string" ? error : "An unexpected error occurred.",
    recoverable: false,
  });
}

// errorMessage is the only formatter UI components should use for caught
// values. It handles AppError, Wails RuntimeError, native Error, and malformed
// rejection values consistently without ever rendering "[object Object]".
export function errorMessage(error: unknown): string {
  return toAppError(error).message;
}
