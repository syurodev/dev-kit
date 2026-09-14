import { agentHubSetConfigOption, agentHubSetMode } from "../../bridge/agenthub";
import { updateTranscriptControls } from "./transcript";
import type { TranscriptModel } from "./types";

type UpdateControls = (sessionId: string, update: (model: TranscriptModel) => TranscriptModel) => unknown;

/*
 * Mode and config options change optimistically: the picker moves at once and
 * the agent confirms. A refusal puts back that one field. The transcript keeps
 * streaming while the call is out, so restoring a snapshot of the whole model
 * would erase what arrived in the meantime. And only while the field still
 * holds the value set here: a newer update from the agent is the truth, not
 * something to undo.
 *
 * Put back to the last value the agent confirmed, not the one the picker held
 * before: with A→B and B→C both out, B's refusal leaves C alone, and C's then
 * restoring "B" showed a mode the agent never took.
 */

type Change = { confirmed: unknown; lastSet: unknown; count: number };

/** Fields with a change out, by session and field. */
const changes = new Map<string, Change>();

async function changeOptimistically<T>(
  sessionId: string,
  key: string,
  value: T,
  read: (model: TranscriptModel) => T | undefined,
  write: (model: TranscriptModel, value: T) => TranscriptModel,
  apply: () => Promise<void>,
  update: UpdateControls,
): Promise<void> {
  const change = changes.get(key) ?? { confirmed: undefined, lastSet: undefined, count: 0 };
  change.count += 1;
  changes.set(key, change);
  update(sessionId, (model) => {
    const current = read(model);
    // Anything but the value set here by a change still out came from the agent.
    if (change.count === 1 || current !== change.lastSet) change.confirmed = current;
    change.lastSet = value;
    return write(model, value);
  });
  try {
    await apply();
    change.confirmed = value;
  } catch (error) {
    const confirmed = change.confirmed as T;
    update(sessionId, (model) => (read(model) === value ? write(model, confirmed) : model));
    throw error;
  } finally {
    change.count -= 1;
    if (change.count === 0) changes.delete(key);
  }
}

export function setSessionMode(
  sessionId: string,
  mode: string,
  apply: (sessionId: string, mode: string) => Promise<void> = agentHubSetMode,
  update: UpdateControls = updateTranscriptControls,
): Promise<void> {
  return changeOptimistically<string | null>(
    sessionId,
    `${sessionId}:mode`,
    mode,
    (model) => model.mode,
    (model, value) => ({ ...model, mode: value }),
    () => apply(sessionId, mode),
    update,
  );
}

export function setSessionConfigOption(
  sessionId: string,
  optionId: string,
  value: string,
  apply: (sessionId: string, optionId: string, value: string) => Promise<void> = agentHubSetConfigOption,
  update: UpdateControls = updateTranscriptControls,
): Promise<void> {
  return changeOptimistically<string | boolean>(
    sessionId,
    `${sessionId}:option:${optionId}`,
    value,
    (model) => model.configOptions.find((option) => option.id === optionId)?.currentValue,
    (model, next) => ({
      ...model,
      configOptions: model.configOptions.map((option) =>
        option.id === optionId ? { ...option, currentValue: next } : option,
      ),
    }),
    () => apply(sessionId, optionId, value),
    update,
  );
}
