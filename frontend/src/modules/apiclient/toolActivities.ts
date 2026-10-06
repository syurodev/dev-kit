import { parseToolOutput } from "@/workbench/toolResultViews";
import { registerToolActivity, stringField, toolRecord, type ToolActivityTarget } from "@/workbench/toolActivity";
import { parseSendResult } from "./sendResult";

type Kind = "request" | "collection" | "environment";

/**
 * The thing an api.* write acts on: what the tool answered with once it is
 * done (it echoes the saved item, id included), else what its input names.
 * `inputKey` is where the input holds it — `request` for api.saveRequest, the
 * top level for the rest.
 */
function itemTarget(kind: Kind, inputKey?: string) {
  return ({ rawInput, rawOutput, state }: { rawInput: unknown; rawOutput: unknown; state: string }): ToolActivityTarget | null => {
    const input = inputKey ? toolRecord(toolRecord(rawInput)?.[inputKey]) : toolRecord(rawInput);
    const output = state === "done" ? toolRecord(parseToolOutput(rawOutput)) : null;
    const id = stringField(output, "id");
    const name = stringField(output, "name") || stringField(input, "name");
    const detail =
      kind === "request"
        ? [stringField(output, "method") || stringField(input, "method").toUpperCase(), stringField(output, "url") || stringField(input, "url")]
            .filter(Boolean)
            .join(" ") || undefined
        : kind === "environment"
          ? stringField(output, "env") || stringField(input, "env") || undefined
          : undefined;
    if (!name) return null;
    return { label: name, detail, ref: id ? { module: "apiclient", kind, id, label: name } : undefined };
  };
}

/** A delete names only an id; there is nothing to open once it is gone. */
function deletedTarget(idKey: string) {
  return ({ rawInput }: { rawInput: unknown }): ToolActivityTarget | null => {
    const id = stringField(rawInput, idKey);
    return id ? { label: id } : null;
  };
}

/**
 * What the api.* tools that act do (workbench/toolActivity.ts): a send is a
 * real request leaving the machine, and a write changes what the person's
 * sidebar holds, so neither is folded away.
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
  registerToolActivity("api.createCollection", {
    module: "apiclient",
    running: "Creating collection",
    done: "Created collection",
    failed: "Couldn't create collection",
    target: itemTarget("collection"),
  });
  registerToolActivity("api.updateCollection", {
    module: "apiclient",
    running: "Updating collection",
    done: "Updated collection",
    failed: "Couldn't update collection",
    target: itemTarget("collection"),
  });
  registerToolActivity("api.deleteCollection", {
    module: "apiclient",
    running: "Deleting collection",
    done: "Deleted collection",
    failed: "Couldn't delete collection",
    target: deletedTarget("collectionId"),
  });
  registerToolActivity("api.saveRequest", {
    module: "apiclient",
    running: "Saving request",
    done: "Saved request",
    failed: "Couldn't save request",
    target: itemTarget("request", "request"),
  });
  registerToolActivity("api.deleteRequest", {
    module: "apiclient",
    running: "Deleting request",
    done: "Deleted request",
    failed: "Couldn't delete request",
    target: deletedTarget("requestId"),
  });
  registerToolActivity("api.createEnvironment", {
    module: "apiclient",
    running: "Creating environment",
    done: "Created environment",
    failed: "Couldn't create environment",
    target: itemTarget("environment"),
  });
  registerToolActivity("api.updateEnvironment", {
    module: "apiclient",
    running: "Updating environment",
    done: "Updated environment",
    failed: "Couldn't update environment",
    target: itemTarget("environment"),
  });
}
