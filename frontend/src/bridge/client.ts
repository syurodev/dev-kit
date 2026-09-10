import type { CancellablePromise } from "@wailsio/runtime";
import { toAppError, isCancelError } from "./errors";

export async function callBridge<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    throw toAppError(error);
  }
}

/** A bridge call that can still be stopped, with its Go context cancelled. */
export type CancellableCall<T> = Promise<T> & {
  /** Rejects the call with a CancelError and cancels the Go side's context. */
  cancel: () => void;
};

/**
 * callBridge for the handful of operations that are slow enough to need a
 * Cancel button (smoothness 5).
 *
 * `callBridge` awaits inside an async function, which flattens the binding's
 * CancellablePromise and throws `cancel` away. This keeps the chain cancellable:
 * @wailsio/runtime's `Call` sends a `CancelCall` with the call id and the Go
 * bridge cancels the context it gave the driver, so cancelling really stops the
 * query, peek or connect — not just the UI's interest in the answer.
 *
 * `then` on a CancellablePromise propagates cancellation up the chain, so the
 * mapped promise returned here cancels the underlying call. A CancelError is
 * rethrown untouched: it is a control signal, not a failure to show the user.
 */
export function callBridgeCancellable<R, T>(
  start: () => CancellablePromise<R>,
  map: (raw: R) => T,
): CancellableCall<T> {
  const call = start();
  const mapped = call.then(map, (error: unknown) => {
    if (isCancelError(error)) throw error;
    throw toAppError(error);
  });
  const result = mapped as unknown as CancellableCall<T>;
  result.cancel = () => {
    void mapped.cancel();
  };
  return result;
}
