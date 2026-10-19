import { useEffect, useState } from "react";
import { agentHubDescribeFiles, type AgentAttachmentInfo } from "@/bridge/agenthub";

/**
 * What Go says about a file chip's path (size, kind, why it cannot go),
 * remembered for a few seconds so a draft with a dozen chips, re-rendered on
 * every key, asks once. Not react-query: a chip lives in a composer that is
 * also drawn in the gallery and in tests with no query client, and the answer is
 * small and short-lived. `undefined` while it loads and when there is no bridge
 * (a browser tab); the chip then simply says less.
 */
const TTL_MS = 10_000;

type Entry = { at: number; info?: AgentAttachmentInfo; pending?: Promise<AgentAttachmentInfo | undefined> };
const entries = new Map<string, Entry>();

export function describeFile(path: string, fresh = false): Promise<AgentAttachmentInfo | undefined> {
  const entry = entries.get(path);
  if (entry && (entry.pending || (!fresh && Date.now() - entry.at < TTL_MS))) {
    return entry.pending ?? Promise.resolve(entry.info);
  }
  const pending = agentHubDescribeFiles([path])
    .then(([info]) => info as AgentAttachmentInfo | undefined)
    // A refresh that fails keeps what was known: a card must not go blank
    // because the second look could not be taken.
    .catch(() => entry?.info)
    .then((info) => {
      entries.set(path, { at: Date.now(), info });
      return info;
    });
  entries.set(path, { at: entry?.at ?? 0, info: entry?.info, pending });
  return pending;
}

/** The cached answer for `path`, read again when `refreshKey` changes (a hover card opening). */
export function useAttachmentInfo(path: string | null, refreshKey = 0): AgentAttachmentInfo | undefined {
  const [info, setInfo] = useState<AgentAttachmentInfo | undefined>(() => (path ? entries.get(path)?.info : undefined));
  useEffect(() => {
    if (!path) {
      setInfo(undefined);
      return;
    }
    let live = true;
    void describeFile(path, refreshKey > 0).then((next) => live && setInfo(next));
    return () => {
      live = false;
    };
  }, [path, refreshKey]);
  return info;
}

/** For the gallery and tests, where there is no bridge: what Go would have said, kept for good. */
export function seedAttachmentInfo(info: AgentAttachmentInfo) {
  entries.set(info.path, { at: Number.MAX_SAFE_INTEGER, info });
}

/** For tests: forget what was learned. */
export function resetAttachmentInfoCache() {
  entries.clear();
}
