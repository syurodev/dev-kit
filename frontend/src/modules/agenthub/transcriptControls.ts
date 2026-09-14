import { agentHubSetConfigOption, agentHubSetMode } from "../../bridge/agenthub";
import { updateTranscriptControls } from "./transcript";
import type { ConfigOption, TranscriptModel } from "./types";

type UpdateControls = (sessionId: string, update: (model: TranscriptModel) => TranscriptModel) => unknown;

/*
 * Mode and config options change optimistically: the picker moves at once and
 * the agent confirms. A refusal puts back that one field. The transcript keeps
 * streaming while the call is out, so restoring a snapshot of the whole model
 * would erase what arrived in the meantime. And only while the field still
 * holds the value set here: a newer update from the agent is the truth, not
 * something to undo.
 */

export async function setSessionMode(
  sessionId: string,
  mode: string,
  apply: (sessionId: string, mode: string) => Promise<void> = agentHubSetMode,
  update: UpdateControls = updateTranscriptControls,
): Promise<void> {
  let previous: string | null = null;
  update(sessionId, (model) => {
    previous = model.mode;
    return { ...model, mode };
  });
  try {
    await apply(sessionId, mode);
  } catch (error) {
    update(sessionId, (model) => (model.mode === mode ? { ...model, mode: previous } : model));
    throw error;
  }
}

export async function setSessionConfigOption(
  sessionId: string,
  optionId: string,
  value: string,
  apply: (sessionId: string, optionId: string, value: string) => Promise<void> = agentHubSetConfigOption,
  update: UpdateControls = updateTranscriptControls,
): Promise<void> {
  let previous: ConfigOption["currentValue"];
  update(sessionId, (model) => ({
    ...model,
    configOptions: model.configOptions.map((option) => {
      if (option.id !== optionId) return option;
      previous = option.currentValue;
      return { ...option, currentValue: value };
    }),
  }));
  try {
    await apply(sessionId, optionId, value);
  } catch (error) {
    update(sessionId, (model) =>
      model.configOptions.some((option) => option.id === optionId && option.currentValue === value)
        ? {
            ...model,
            configOptions: model.configOptions.map((option) =>
              option.id === optionId ? { ...option, currentValue: previous } : option,
            ),
          }
        : model,
    );
    throw error;
  }
}
