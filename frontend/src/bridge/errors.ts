export type AppError = {
  code: string;
  message: string;
  recoverable: boolean;
  details?: Record<string, unknown>;
};

export function toAppError(error: unknown): AppError {
  if (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    "message" in error
  ) {
    const value = error as Partial<AppError>;
    return {
      code: String(value.code),
      message: String(value.message),
      recoverable: Boolean(value.recoverable),
      details: value.details,
    };
  }

  if (error instanceof Error) {
    return {
      code: "unknown",
      message: error.message,
      recoverable: false,
    };
  }

  return {
    code: "unknown",
    message: String(error),
    recoverable: false,
  };
}
