import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { hasToolResultView } from "@/workbench/toolResultViews";
import { MessageSentCard } from "./message";
import { deliveryLabel, deliveryLine, parseSentMessage } from "./messageResult";
import "../manifest";

// What internal/agenthub/peer.go: SendMessageResult marshals to.
const STEERED = { messageId: "m1", delivery: "steered", sessionId: "s-beta", title: "beta", hop: 2 };
const REFUSED = { messageId: "m2", delivery: "refused", code: "loop", reason: "This exchange is 6 messages deep (the limit is 6)." };

describe("agenthub.sendMessage", () => {
  it("is drawn by Agent Hub, under Claude's wire name as well as its own", () => {
    expect(hasToolResultView("agenthub.sendMessage")).toBe(true);
    expect(hasToolResultView("mcp__devkit__agenthub_sendMessage")).toBe(true);
  });

  it("reads what the tool answered, from an object or from its JSON text", () => {
    expect(parseSentMessage(STEERED)).toMatchObject({ messageId: "m1", delivery: "steered", sessionId: "s-beta", title: "beta", hop: 2 });
    expect(parseSentMessage(JSON.stringify(REFUSED))).toMatchObject({ delivery: "refused", code: "loop" });
  });

  it("reads nothing it cannot account for, so the generic block shows instead", () => {
    expect(parseSentMessage(null)).toBeNull();
    expect(parseSentMessage("delivered")).toBeNull();
    expect(parseSentMessage({ messageId: "m1" })).toBeNull();
    expect(parseSentMessage({ delivery: "teleported" })).toBeNull();
    expect(parseSentMessage([STEERED])).toBeNull();
  });

  it("says what became of the message in the sender's terms", () => {
    expect(deliveryLabel("started")).toBe("Started");
    expect(deliveryLabel("refused")).toBe("Not delivered");
    expect(deliveryLine(parseSentMessage({ delivery: "queued" })!)).toContain("Waiting");
  });

  it("links the recipient when the message went somewhere", () => {
    const html = renderToString(<MessageSentCard message={parseSentMessage(STEERED)!} onOpenSession={() => {}} />);
    expect(html).toContain("Message to beta");
    expect(html).toContain("Steered");
    expect(html).toContain('data-delivery="steered"');
    expect(html).toContain('data-testid="message-sent-open"');
    expect(html).toContain("step 2");
  });

  it("gives a refusal its reason and nothing to open", () => {
    const html = renderToString(<MessageSentCard message={parseSentMessage(REFUSED)!} />);
    expect(html).toContain("Not delivered");
    expect(html).toContain("the limit is 6");
    expect(html).not.toContain("message-sent-open");
  });
});
