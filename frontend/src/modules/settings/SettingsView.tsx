import { useEffect, useState } from "react";
import {
  createEnvTag,
  deleteEnvTag,
  listEnvTags,
  updateEnvTag,
  type EnvTag,
} from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";

export function SettingsView() {
  const [tags, setTags] = useState<EnvTag[]>([]);
  const [newName, setNewName] = useState("");
  const [newRequiresConfirmation, setNewRequiresConfirmation] = useState(false);
  const [error, setError] = useState("");

  async function refresh() {
    try {
      setTags(await listEnvTags());
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreate() {
    setError("");
    try {
      await createEnvTag(newName, newRequiresConfirmation);
      setNewName("");
      setNewRequiresConfirmation(false);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onToggleRequiresConfirmation(tag: EnvTag) {
    setError("");
    try {
      await updateEnvTag(tag.id, !tag.requiresConfirmation);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onDelete(tag: EnvTag) {
    setError("");
    try {
      await deleteEnvTag(tag.id);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  return (
    <section className="settings-view">
      <h2>Environments</h2>

      <div className="env-create">
        <input
          aria-label="env-name"
          placeholder="Tag name"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
        />
        <label>
          <input
            aria-label="env-new-requires-confirmation"
            type="checkbox"
            checked={newRequiresConfirmation}
            onChange={(e) => setNewRequiresConfirmation(e.target.checked)}
          />
          Requires confirmation
        </label>
        <button type="button" onClick={onCreate}>
          Create tag
        </button>
      </div>

      <table aria-label="env-tags-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Requires confirmation</th>
            <th>Builtin</th>
            <th>Locked</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {tags.map((tag) => (
            <tr key={tag.id}>
              <td>{tag.name}</td>
              <td>
                <input
                  aria-label={`env-requires-confirmation-${tag.name}`}
                  type="checkbox"
                  checked={tag.requiresConfirmation}
                  disabled={tag.locked}
                  onChange={() => onToggleRequiresConfirmation(tag)}
                />
              </td>
              <td>{tag.isBuiltin ? "yes" : "no"}</td>
              <td>{tag.locked ? "yes" : "no"}</td>
              <td>
                <button
                  type="button"
                  aria-label={`env-delete-${tag.name}`}
                  disabled={tag.isBuiltin}
                  onClick={() => onDelete(tag)}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
