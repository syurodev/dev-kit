import { describe, expect, it } from "bun:test";

import { buildElicitationContent, parseElicitationForm, questionCountLabel } from "./elicitation";

// Shapes copied from claude-agent-acp 0.74.0's elicitation.js
// (askUserQuestionsToCreateRequest, refusalFallbackToCreateRequest), not
// invented: the point of these tests is that the wire the agent sends parses.
const custom = (questionId: string) => ({
  type: "string",
  title: "Other",
  description: "Type your own answer instead of choosing an option above (optional).",
  _meta: { _askUserQuestionCustomAnswer: { questionId, isCustomAnswer: true } },
});

const twoQuestions = {
  mode: "form",
  sessionId: "s1",
  toolCallId: "toolu_1",
  message: "Please answer the following questions.",
  requestedSchema: {
    type: "object",
    properties: {
      question_0: {
        type: "string",
        title: "Auth",
        description: "Which auth method?",
        oneOf: [
          { const: "OAuth", title: "OAuth", description: "Delegated sign-in" },
          {
            const: "API key",
            title: "API key",
            _meta: { "_claude/askUserQuestionOption": { preview: "Authorization: Bearer …" } },
          },
        ],
      },
      question_0_custom: custom("question_0"),
      question_1: {
        type: "array",
        title: "Features",
        description: "Which features?",
        items: { anyOf: [{ const: "Caching", title: "Caching" }, { const: "Retries", title: "Retries" }] },
      },
      question_1_custom: custom("question_1"),
    },
  },
};

const oneQuestion = {
  mode: "form",
  sessionId: "s1",
  toolCallId: "toolu_2",
  message: "Ship it today?",
  requestedSchema: {
    type: "object",
    properties: {
      question_0: { type: "string", title: "Ship", oneOf: [{ const: "Yes", title: "Yes" }, { const: "No", title: "No" }] },
      question_0_custom: custom("question_0"),
    },
  },
};

const fallback = {
  mode: "form",
  sessionId: "s1",
  message: "claude-fable declined this request. Retry with claude-opus?",
  requestedSchema: {
    type: "object",
    properties: {
      choice: {
        type: "string",
        oneOf: [
          { const: "retry_fallback", title: "Retry with claude-opus", description: "The session continues on claude-opus." },
          { const: "cancelled", title: "Keep the refusal", description: "You can send a new message." },
        ],
      },
    },
  },
};

const mcp = {
  mode: "form",
  sessionId: "s1",
  message: "Configure the deploy",
  requestedSchema: {
    type: "object",
    required: ["region", "replicas"],
    properties: {
      region: { type: "string", enum: ["us", "eu"], enumNames: ["United States", "Europe"] },
      replicas: { type: "integer", title: "Replicas" },
      ratio: { type: "number" },
      dryRun: { type: "boolean", title: "Dry run" },
      contact: { type: "string", format: "email" },
      nested: { type: "object", properties: {} },
    },
  },
};

describe("parseElicitationForm", () => {
  it("folds each Other box into the question it belongs to", () => {
    const form = parseElicitationForm(twoQuestions);
    expect(form.flavor).toBe("question");
    expect(form.message).toBe("Please answer the following questions.");
    expect(form.fields.map((f) => f.key)).toEqual(["question_0", "question_1"]);

    const [first, second] = form.fields;
    if (first.kind !== "single" || second.kind !== "multi") throw new Error(`kinds ${first.kind}, ${second.kind}`);
    expect(first.label).toBe("Which auth method?");
    expect(first.header).toBe("Auth");
    expect(first.customKey).toBe("question_0_custom");
    expect(first.choices[0]).toEqual({ value: "OAuth", label: "OAuth", description: "Delegated sign-in" });
    expect(first.choices[1].preview).toBe("Authorization: Bearer …");
    // Multi-select is the array with items.anyOf and no type on the items.
    expect(second.choices.map((c) => c.value)).toEqual(["Caching", "Retries"]);
    expect(second.customKey).toBe("question_1_custom");
    expect(questionCountLabel(form)).toBe("2 questions");
  });

  it("reads a single question from the message, since its field carries no description", () => {
    const form = parseElicitationForm(oneQuestion);
    expect(form.message).toBe("Ship it today?");
    expect(form.fields).toHaveLength(1);
    expect(form.fields[0].label).toBe("Ship");
    expect(form.fields[0].header).toBeUndefined();
  });

  it("recognises the refusal-fallback prompt by what it offers", () => {
    const form = parseElicitationForm(fallback);
    expect(form.flavor).toBe("fallback");
    expect(form.fields[0].kind).toBe("single");
  });

  it("renders an MCP server's flat schema, and degrades what it cannot render instead of throwing", () => {
    const form = parseElicitationForm(mcp);
    expect(form.flavor).toBe("generic");
    const byKey = Object.fromEntries(form.fields.map((f) => [f.key, f]));
    expect(byKey.region).toMatchObject({ kind: "single", required: true });
    if (byKey.region.kind === "single") expect(byKey.region.choices[1]).toEqual({ value: "eu", label: "Europe" });
    expect(byKey.replicas).toMatchObject({ kind: "number", integer: true, required: true });
    expect(byKey.ratio).toMatchObject({ kind: "number", integer: false, required: false });
    expect(byKey.dryRun.kind).toBe("boolean");
    expect(byKey.contact).toMatchObject({ kind: "text", inputType: "email" });
    expect(byKey.nested.kind).toBe("unsupported");
  });

  it("survives a request with no schema at all", () => {
    expect(parseElicitationForm(null, "fallback text")).toEqual({ message: "fallback text", flavor: "generic", fields: [] });
    expect(parseElicitationForm({ requestedSchema: { properties: { x: { type: "string", oneOf: [{}] } } } }).fields[0].kind).toBe("text");
  });

  it("keeps an Other box whose owner is missing as a question of its own", () => {
    const form = parseElicitationForm({ requestedSchema: { properties: { question_3_custom: custom("question_9") } } });
    expect(form.fields).toEqual([{ key: "question_3_custom", label: form.fields[0].label, header: "Other", required: false, kind: "text", inputType: "text" }]);
  });
});

describe("buildElicitationContent", () => {
  const form = parseElicitationForm(twoQuestions);

  it("sends a selection as a string and a multi-select as an array of strings", () => {
    expect(buildElicitationContent(form, { question_0: ["OAuth"], question_1: ["Caching", "Retries"] })).toEqual({
      question_0: "OAuth",
      question_1: ["Caching", "Retries"],
    });
  });

  it("sends typed text to the Other key alone, because the agent lets it override the selection", () => {
    expect(buildElicitationContent(form, { question_0: ["  Magic link "], question_1: ["Caching", "Webhooks"] })).toEqual({
      question_0_custom: "Magic link",
      question_1_custom: "Webhooks",
    });
  });

  it("leaves out what was not answered, and blank text is not an answer", () => {
    expect(buildElicitationContent(form, { question_1: ["   "] })).toEqual({});
  });

  it("gives numbers and booleans their JSON kinds, and drops a fractional integer", () => {
    const generic = parseElicitationForm(mcp);
    expect(
      buildElicitationContent(generic, {
        region: ["eu"],
        replicas: ["3"],
        ratio: ["0.5"],
        dryRun: ["false"],
        contact: ["a@b.c"],
        nested: ["ignored"],
      }),
    ).toEqual({ region: "eu", replicas: 3, ratio: 0.5, dryRun: false, contact: "a@b.c" });
    expect(buildElicitationContent(generic, { replicas: ["2.5"], ratio: ["abc"] })).toEqual({});
  });
});
