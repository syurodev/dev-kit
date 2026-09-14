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

function settleableCall() {
  let resolve!: () => void;
  const promise = new Promise<void>((resolveCall) => {
    resolve = resolveCall;
  });
  return { apply: () => promise, resolve };
}

const chunk ={ id: "chunk-1", kind: "agent_message_chunk", text: "streamed while waiting" };

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

  it("rolls two failed mode changes back to the mode the agent confirmed, not the one in between", async () => {
    const t = transcript({ mode: "default" });
    const toPlan = failingCall();
    const toEdits = failingCall();

    const plan = setSessionMode("s1", "plan", toPlan.apply, t.update);
    const edits = setSessionMode("s1", "acceptEdits", toEdits.apply, t.update);
    toPlan.reject(new Error("plan failed"));
    await expect(plan).rejects.toThrow();
    expect(t.model().mode).toBe("acceptEdits");
    toEdits.reject(new Error("edits failed"));
    await expect(edits).rejects.toThrow();

    expect(t.model().mode).toBe("default");
  });

  it("rolls a failed mode change back to the one before it that succeeded", async () => {
    const t = transcript({ mode: "default" });
    const toPlan = settleableCall();
    const toEdits = failingCall();

    const plan = setSessionMode("s1", "plan", toPlan.apply, t.update);
    const edits = setSessionMode("s1", "acceptEdits", toEdits.apply, t.update);
    toPlan.resolve();
    await plan;
    toEdits.reject(new Error("edits failed"));
    await expect(edits).rejects.toThrow();

    expect(t.model().mode).toBe("plan");
  });

  it("keeps a later mode that succeeded when the earlier one fails after it", async () => {
    const t = transcript({ mode: "default" });
    const toPlan = failingCall();
    const toEdits = settleableCall();

    const plan = setSessionMode("s1", "plan", toPlan.apply, t.update);
    const edits = setSessionMode("s1", "acceptEdits", toEdits.apply, t.update);
    toEdits.resolve();
    await edits;
    toPlan.reject(new Error("plan failed"));
    await expect(plan).rejects.toThrow();

    expect(t.model().mode).toBe("acceptEdits");
  });

  it("rolls back to a mode the agent reported between two changes", async () => {
    const t = transcript({ mode: "default" });
    const toPlan = failingCall();
    const toEdits = failingCall();

    const plan = setSessionMode("s1", "plan", toPlan.apply, t.update);
    t.stream((model) => ({ ...model, mode: "bypassPermissions" }));
    const edits = setSessionMode("s1", "acceptEdits", toEdits.apply, t.update);
    toPlan.reject(new Error("plan failed"));
    await expect(plan).rejects.toThrow();
    toEdits.reject(new Error("edits failed"));
    await expect(edits).rejects.toThrow();

    expect(t.model().mode).toBe("bypassPermissions");
  });

  it("rolls two failed option changes back to the value the agent confirmed", async () => {
    const t = transcript({ configOptions: [{ id: "model", currentValue: "sonnet" }] });
    const toOpus = failingCall();
    const toHaiku = failingCall();

    const opus = setSessionConfigOption("s1", "model", "opus", toOpus.apply, t.update);
    const haiku = setSessionConfigOption("s1", "model", "haiku", toHaiku.apply, t.update);
    toOpus.reject(new Error("opus failed"));
    await expect(opus).rejects.toThrow();
    toHaiku.reject(new Error("haiku failed"));
    await expect(haiku).rejects.toThrow();

    expect(t.model().configOptions).toEqual([{ id: "model", currentValue: "sonnet" }]);
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
