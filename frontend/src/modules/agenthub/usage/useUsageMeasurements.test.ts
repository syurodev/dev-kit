import { test, expect } from "bun:test";
import {
  focusManager,
  QueryClient,
  QueryObserver,
} from "@tanstack/react-query";
import { queryClient } from "@/lib/query";
import { measurementOptions } from "./useUsageMeasurements";
for (const kind of ["tools", "moves"] as const) {
  test(`stale ${kind} refetches on focus with the shared query defaults`, async () => {
    const client = new QueryClient({
      defaultOptions: queryClient.getDefaultOptions(),
    });
    let reads = 0;
    const observer = new QueryObserver(client, {
      ...measurementOptions("claude", "2026-10-01", "2026-10-26", "")[kind],
      queryFn: async () => ++reads,
      staleTime: 0,
    });
    focusManager.setFocused(false);
    client.mount();
    const off = observer.subscribe(() => {});
    let offWait = () => {};
    try {
      await observer.refetch();
      const before = reads;
      const refreshed = new Promise<void>((resolve) => {
        offWait = observer.subscribe((r) => {
          if (r.isSuccess && r.data > before) resolve();
        });
      });
      focusManager.setFocused(true);
      await refreshed;
      expect(reads).toBe(before + 1);
    } finally {
      offWait();
      off();
      client.unmount();
      client.clear();
      focusManager.setFocused(undefined);
    }
  }, 2000);
}
test("late responses for old vendor/window/cursor cannot replace the displayed report", async () => {
  const client = new QueryClient({
    defaultOptions: queryClient.getDefaultOptions(),
  });
  let release!: (v: string) => void;
  const old = new Promise<string>((resolve) => {
    release = resolve;
  });
  const observer = new QueryObserver(client, {
    ...measurementOptions("claude", "2026-10-01", "2026-10-26", "old-page")
      .moves,
    queryFn: () => old,
  });
  const off = observer.subscribe(() => {});
  try {
    observer.setOptions({
      ...measurementOptions("codex", "2026-10-10", "2026-10-26", "").moves,
      queryFn: async () => "current",
    });
    await observer.refetch();
    release("stale");
    await old;
    await Promise.resolve();
    expect(observer.getCurrentResult().data).toBe("current");
  } finally {
    off();
    client.clear();
  }
});
