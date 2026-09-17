/**
 * Reading an agent's question (ACP elicitation/create, form mode) into fields a
 * person can answer, and turning their answers back into the content the agent
 * reads.
 *
 * This is the one place that knows the shapes vendors actually send. Three
 * flows arrive through the same method once DevKit declares `elicitation.form`:
 * Claude's AskUserQuestion, an MCP server's own question, and the
 * refusal-fallback prompt. The agent keeps no deadline on any of them, so a
 * schema this cannot render must still come out as something — an
 * `unsupported` field — never as a throw: the surface always has a Skip, and a
 * question nobody can answer or skip is a turn that hangs for good.
 *
 * Detection is by feature, not by tool name: a field that carries a
 * custom-answer companion is a question, whoever sent it.
 */

export type ElicitationChoice = {
  value: string;
  label: string;
  description?: string;
  /** A mockup or snippet the option wants shown when it is picked (Claude's `_meta` extension). */
  preview?: string;
};

type FieldBase = {
  key: string;
  /** What the field asks: its description, else its title, else its key. */
  label: string;
  /** A short header shown beside the label (AskUserQuestion's `header`), when both exist. */
  header?: string;
  required: boolean;
};

export type ElicitationField =
  | (FieldBase & {
      kind: "single" | "multi";
      choices: ElicitationChoice[];
      /** The free-text "Other" companion (`question_N_custom`), when the agent sent one. */
      customKey?: string;
    })
  | (FieldBase & { kind: "text"; inputType: "text" | "email" | "url" | "date" | "datetime-local" })
  | (FieldBase & { kind: "number"; integer: boolean })
  | (FieldBase & { kind: "boolean" })
  | (FieldBase & { kind: "unsupported" });

export type ElicitationForm = {
  message: string;
  /** question: AskUserQuestion-shaped; fallback: the retry-on-another-model prompt; generic: anything else. */
  flavor: "question" | "fallback" | "generic";
  fields: ElicitationField[];
};

/** The answers a form produced, by field key: every value the field's controls submitted. */
export type ElicitationAnswers = Record<string, string[]>;

export type ElicitationContent = Record<string, string | number | boolean | string[]>;

const CUSTOM_ANSWER_META = "_askUserQuestionCustomAnswer";
const OPTION_META = "_claude/askUserQuestionOption";
const FALLBACK_RETRY = "retry_fallback";

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}

/** `oneOf`/`anyOf` of `{const, title}`, or a bare `enum` (with the legacy `enumNames`). */
function readChoices(schema: Record<string, unknown>): ElicitationChoice[] | null {
  const titled = Array.isArray(schema.oneOf) ? schema.oneOf : Array.isArray(schema.anyOf) ? schema.anyOf : null;
  if (titled) {
    const choices: ElicitationChoice[] = [];
    for (const entry of titled) {
      const option = record(entry);
      if (!option || (typeof option.const !== "string" && typeof option.const !== "number")) return null;
      const value = String(option.const);
      const preview = text(record(record(option._meta)?.[OPTION_META])?.preview);
      choices.push({
        value,
        label: text(option.title) ?? value,
        ...(text(option.description) ? { description: option.description as string } : {}),
        ...(preview ? { preview } : {}),
      });
    }
    return choices.length > 0 ? choices : null;
  }
  if (Array.isArray(schema.enum)) {
    const names = Array.isArray(schema.enumNames) ? schema.enumNames : [];
    const choices = schema.enum
      .filter((value): value is string | number => typeof value === "string" || typeof value === "number")
      .map((value, index) => ({ value: String(value), label: text(names[index]) ?? String(value) }));
    return choices.length > 0 ? choices : null;
  }
  return null;
}

function inputTypeFor(format: unknown): "text" | "email" | "url" | "date" | "datetime-local" {
  switch (format) {
    case "email":
      return "email";
    case "uri":
      return "url";
    case "date":
      return "date";
    case "date-time":
      return "datetime-local";
    default:
      return "text";
  }
}

/** The key a custom-answer field belongs to, if it is one. */
function customOwner(schema: Record<string, unknown>): string | undefined {
  const marker = record(record(schema._meta)?.[CUSTOM_ANSWER_META]);
  return marker?.isCustomAnswer === true ? text(marker.questionId) : undefined;
}

export function parseElicitationForm(params: unknown, fallbackMessage = ""): ElicitationForm {
  const request = record(params) ?? {};
  const schema = record(request.requestedSchema) ?? {};
  const properties = record(schema.properties) ?? {};
  const required = new Set(Array.isArray(schema.required) ? schema.required.filter((k) => typeof k === "string") : []);
  const message = text(request.message) ?? fallbackMessage;

  // Custom-answer companions first, so their owners can claim them.
  const companions = new Map<string, string>();
  for (const [key, raw] of Object.entries(properties)) {
    const owner = customOwner(record(raw) ?? {});
    if (owner && owner !== key && record(properties[owner])) companions.set(owner, key);
  }
  const claimed = new Set(companions.values());

  const fields: ElicitationField[] = [];
  for (const [key, raw] of Object.entries(properties)) {
    if (claimed.has(key)) continue;
    const property = record(raw) ?? {};
    const title = text(property.title);
    const description = text(property.description);
    const base: FieldBase = {
      key,
      label: description ?? title ?? key,
      ...(description && title ? { header: title } : {}),
      required: required.has(key),
    };
    const customKey = companions.get(key);

    switch (property.type) {
      case "array": {
        const choices = readChoices(record(property.items) ?? {});
        fields.push(choices ? { ...base, kind: "multi", choices, ...(customKey ? { customKey } : {}) } : { ...base, kind: "unsupported" });
        break;
      }
      case "string": {
        const choices = readChoices(property);
        if (choices) fields.push({ ...base, kind: "single", choices, ...(customKey ? { customKey } : {}) });
        else fields.push({ ...base, kind: "text", inputType: inputTypeFor(property.format) });
        break;
      }
      case "number":
      case "integer":
        fields.push({ ...base, kind: "number", integer: property.type === "integer" });
        break;
      case "boolean":
        fields.push({ ...base, kind: "boolean" });
        break;
      default:
        fields.push({ ...base, kind: "unsupported" });
    }
  }

  const flavor: ElicitationForm["flavor"] = companions.size > 0
    ? "question"
    : fields.length === 1 && fields[0].kind === "single" && fields[0].choices.some((c) => c.value === FALLBACK_RETRY)
      ? "fallback"
      : "generic";
  return { message, flavor, fields };
}

/** The choices a boolean field is answered with. */
export const BOOLEAN_CHOICES: ElicitationChoice[] = [
  { value: "true", label: "Yes" },
  { value: "false", label: "No" },
];

/**
 * The accept content for what the person answered. A field left unanswered is
 * left out — the agent reads a missing key as "no answer", which is what a
 * skipped question is.
 *
 * A choice field's controls share its key, the free-text "Other" box included,
 * so its values are sorted back out here: a value that is one of the choices is
 * a selection, anything else is typed text. Both are sent, each under its own
 * key. claude-agent-acp 0.78 reads them together: a pick stays the answer and
 * the text becomes its note, a multi-select gets the text appended, and text
 * with no pick is the answer. (0.74 let the text override the pick, which is
 * why this once sent the text alone.)
 */
export function buildElicitationContent(form: ElicitationForm, answers: ElicitationAnswers): ElicitationContent {
  const content: ElicitationContent = {};
  for (const field of form.fields) {
    const values = answers[field.key] ?? [];
    switch (field.kind) {
      case "single":
      case "multi": {
        const known = new Set(field.choices.map((c) => c.value));
        const picked = values.filter((v) => known.has(v));
        const typed = values.filter((v) => !known.has(v) && v.trim()).map((v) => v.trim()).join(", ");
        if (field.kind === "single" && picked.length > 0) {
          content[field.key] = picked[0];
        } else if (field.kind === "multi" && picked.length > 0) {
          content[field.key] = [...new Set(picked)];
        }
        if (typed && field.customKey) {
          content[field.customKey] = typed;
        }
        break;
      }
      case "text": {
        const value = values.find((v) => v.trim());
        if (value !== undefined) content[field.key] = value;
        break;
      }
      case "number": {
        const raw = values.find((v) => v.trim());
        const value = raw === undefined ? NaN : Number(raw);
        if (Number.isFinite(value) && (!field.integer || Number.isInteger(value))) content[field.key] = value;
        break;
      }
      case "boolean": {
        const value = values.find((v) => v === "true" || v === "false");
        if (value !== undefined) content[field.key] = value === "true";
        break;
      }
      case "unsupported":
        break;
    }
  }
  return content;
}

/** Every value a form's controls would submit, by name. */
export function answersFromForms(forms: Iterable<HTMLFormElement | null>): ElicitationAnswers {
  const answers: ElicitationAnswers = {};
  for (const form of forms) {
    if (!form) continue;
    new FormData(form).forEach((value, name) => {
      if (typeof value !== "string") return;
      (answers[name] ??= []).push(value);
    });
  }
  return answers;
}

/** "1 question", "3 questions" — what a toast says instead of showing the form. */
export function questionCountLabel(form: ElicitationForm): string {
  const n = Math.max(form.fields.length, 1);
  return n === 1 ? "1 question" : `${n} questions`;
}
