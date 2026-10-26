import { expect, test } from "bun:test";
import { focusManager, QueryClient, QueryObserver } from "@tanstack/react-query";
import { queryClient } from "@/lib/query";
import { agentUsageOptions } from "./useAgentUsage";

test("a stale active usage report refetches on focus despite the shared disabled default", async () => {
  const client = new QueryClient({ defaultOptions: queryClient.getDefaultOptions() });
  let reads = 0;
  const observer = new QueryObserver(client, {
    ...agentUsageOptions("claude"),
    queryFn: async () => ++reads,
    staleTime: 0,
  });
  focusManager.setFocused(false);
  client.mount();
  const off = observer.subscribe(() => {});
  let stopWaiting = () => {};
  try {
    await observer.refetch();
    const before = reads;
    const refreshed = new Promise<void>((resolve) => {
      stopWaiting = observer.subscribe((result) => {
        if (result.isSuccess && result.data > before) resolve();
      });
    });
    focusManager.setFocused(true);
    await refreshed;
    expect(reads).toBe(before + 1);
  } finally {
    stopWaiting();
    off();
    client.unmount();
    client.clear();
    focusManager.setFocused(undefined);
  }
}, 2_000);
