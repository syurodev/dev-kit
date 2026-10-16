import { createElement, lazy } from "react";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { parseSentMessage } from "./messageResult";

/**
 * The ToolResultView for `agenthub.sendMessage`: a message to another session
 * is a thing of Agent Hub, so Agent Hub draws its result (manifest.ts).
 */
const LazyMessageSentCard = lazy(() => import("./message").then(({ MessageSentCard }) => ({ default: MessageSentCard })));

export function registerMessageViews(): void {
  registerToolResultView("agenthub.sendMessage", ({ rawOutput, heading }) => {
    const message = parseSentMessage(rawOutput);
    return message ? createElement(LazyMessageSentCard, { message, heading }) : null;
  });
}
