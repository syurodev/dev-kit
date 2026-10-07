import { expect, test } from "bun:test";
import { parseApiFlow } from "./apiFlowResult";

test("keeps each step's depth and the condition its caller reaches it under", () => {
  const flow = {
    entryPointId: "e1",
    steps: [
      { depth: 0, label: "A.get", filePath: "A.java", line: 1 },
      { depth: 1, guard: "moduleBased", label: "A.byModule", filePath: "A.java", line: 5 },
    ],
  };
  const parsed = parseApiFlow(flow);
  expect(parsed?.flow.steps[1]).toEqual(flow.steps[1]);
  expect(parseApiFlow(JSON.stringify(flow))?.flow.steps[0]?.guard).toBeUndefined();
});

test("reads a transcript from before steps had a depth or a guard", () => {
  const parsed = parseApiFlow({ entryPointId: "e1", steps: [{ label: "A.get", filePath: "A.java", line: 1 }] });
  expect(parsed?.flow.steps[0]?.depth).toBeUndefined();
});
