import { registerToolActivity, stringField, toolRecord } from "@/workbench/toolActivity";
import { parseSendResult } from "./sendResult";

/**
 * What the api.* tools that act do (workbench/toolActivity.ts): a send is a
 * real request leaving the machine, so it is never folded away.
 */
export function registerApiActivities(): void {
  registerToolActivity("api.send", {
    module: "apiclient",
    running: "Sending request",
    done: "Sent request",
    failed: "Couldn't send request",
    preload: () => import("./SendResultCard"),
    target: ({ rawInput, rawOutput, state }) => {
      const requestId = stringField(rawInput, "requestId");
      if (!requestId) return null;
      const sent = state === "done" ? parseSendResult(rawInput, rawOutput) : null;
      return {
        label: requestId,
        detail: sent ? `${sent.response.status} ${sent.response.statusText}`.trim() : undefined,
        ref: { module: "apiclient", kind: "request", id: requestId, label: requestId },
      };
    },
  });
  registerToolActivity("api.sendAdhoc", {
    module: "apiclient",
    running: "Sending request",
    done: "Sent request",
    failed: "Couldn't send request",
    target: ({ rawInput }) => {
      const request = toolRecord(toolRecord(rawInput)?.request);
      const method = stringField(request, "method") || "GET";
      const url = stringField(request, "url");
      return url ? { label: url, detail: method.toUpperCase() } : null;
    },
  });
}
