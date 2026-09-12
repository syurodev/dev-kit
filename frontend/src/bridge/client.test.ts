import { describe, expect, it } from "bun:test";
import { callBridgeCancellable } from "./client";

/**
 * A stand-in for @wailsio/runtime's CancellablePromise: a thenable whose
 * `then` returns a child that cancels the whole chain, which is what the real
 * one documents ("child1.cancel() cancels child1 and root").
 */
function fakeCall(): { promise: Promise<number>; cancels: () => number } {
  let cancels = 0;
  const cancel = () => {
    cancels += 1;
    return Promise.resolve();
  };
  const wrap = <T>(p: Promise<T>): Promise<T> => {
    const self = p as Promise<T> & {
      cancel: () => Promise<void>;
      then: Promise<T>["then"];
    };
    self.cancel = cancel;
    const nativeThen = Promise.prototype.then.bind(p) as Promise<T>["then"];
    self.then = ((onOk?: never, onErr?: never) => wrap(nativeThen(onOk, onErr))) as never;
    return self;
  };
  return { promise: wrap(Promise.resolve(1)), cancels: () => cancels };
}

describe("callBridgeCancellable", () => {
  /**
   * The bug this exists for: `result` was the mapped promise ITSELF, cast to a
   * new type, so `result.cancel = () => mapped.cancel()` replaced the very
   * method the closure went on to call. Cancelling recursed until the stack
   * ran out — which looked, from the outside, like a Stop button that could
   * not be pressed. Every cancel in the app went through here: the console's
   * Stop, a superseded table page, a superseded row count, and Kafka's peek.
   */
  it("cancels the underlying call instead of itself", () => {
    const { promise, cancels } = fakeCall();
    const call = callBridgeCancellable(
      () => promise as never,
      (value: number) => value,
    );

    expect(() => call.cancel()).not.toThrow();
    expect(cancels(), "cancel must reach the Wails call exactly once").toBe(1);
  });

  it("still resolves through its mapper", async () => {
    const { promise } = fakeCall();
    const call = callBridgeCancellable(
      () => promise as never,
      (value: number) => value * 2,
    );
    expect(await call).toBe(2);
  });
});
