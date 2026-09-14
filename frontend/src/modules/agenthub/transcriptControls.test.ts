import { describe, expect, it } from "bun:test";

import { setSessionConfigOption, setSessionMode } from "./transcriptControls";
import { createInitialTranscriptModel } from "./transcript";
import type { TranscriptModel } from "./types";

/** A session's transcript runtime, which the stream keeps writing while a control call is pending. */
function transcript(initial: Partial<TranscriptModel>) {
  let model: TranscriptModel = { ...createInitialTranscriptModel(), ...initial };
  const write = (update: (current: TranscriptModel) => TranscriptModel) => {
    model = update(model);
    return model;
  };
  return {
    model: () => model,
    update: (_sessionId: string, update: (current: TranscriptModel) => TranscriptModel) => write(update),
    stream: write,
  };
}

function failingCall() {
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((_resolve, rejectCall) => {
    reject = rejectCall;
  });
  return { apply: () => promise, reject };
}

const chunk = { id: "chunk-1", kind: "agent_message_chunk", text: "streamed while waiting" };

describe("optimistic session controls", () => {
  it("rolls back only the mode, keeping what streamed in while the call was pending", async () => {
    const t = transcript({ mode: "default" });
    const call = failingCall();

    const pending = setSessionMode("s1", "plan", call.apply, t.update);
    expect(t.model().mode).toBe("plan");
    t.stream((model) => ({ ...model, items: [...model.items, chunk] }));
    call.reject(new Error("set mode failed"));

    await expect(pending).rejects.toThrow("set mode failed");
    expect(t.model().mode).toBe("default");
    expect(t.model().items).toEqual([chunk]);
  });

  it("does not roll the mode back over one the agent reported meanwhile", async () => {
    const t = transcript({ mode: "default" });
    const call = failingCall();

    const pending = setSessionMode("s1", "plan", call.apply, t.update);
    t.stream((model) => ({ ...model, mode: "acceptEdits" }));
    call.reject(new Error("set mode failed"));

    await expect(pending).rejects.toThrow();
    expect(t.model().mode).toBe("acceptEdits");
  });

  it("rolls back only the one option, keeping the rest of the model", async () => {
    const t = transcript({
      configOptions: [
        { id: "model", currentValue: "sonnet" },
        { id: "effort", currentValue: "high" },
      ],
    });
    const call = failingCall();

    const pending = setSessionConfigOption("s1", "model", "opus", call.apply, t.update);
    expect(t.model().configOptions[0].currentValue).toBe("opus");
    t.stream((model) => ({ ...model, items: [...model.items, chunk] }));
    call.reject(new Error("set option failed"));

    await expect(pending).rejects.toThrow("set option failed");
    expect(t.model().configOptions).toEqual([
      { id: "model", currentValue: "sonnet" },
      { id: "effort", currentValue: "high" },
    ]);
    expect(t.model().items).toEqual([chunk]);
  });

  it("does not roll an option back over a config_option_update that arrived meanwhile", async () => {
    const t = transcript({ configOptions: [{ id: "model", currentValue: "sonnet" }] });
    const call = failingCall();

    const pending = setSessionConfigOption("s1", "model", "opus", call.apply, t.update);
    t.stream((model) => ({ ...model, configOptions: [{ id: "model", currentValue: "haiku" }] }));
    call.reject(new Error("set option failed"));

    await expect(pending).rejects.toThrow();
    expect(t.model().configOptions).toEqual([{ id: "model", currentValue: "haiku" }]);
  });
});
