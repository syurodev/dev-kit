import { useState } from "react";
import { executeCommand } from "../../bridge/commands";
import { errorMessage } from "../../bridge/errors";

type HashResult = {
  Algorithm: string;
  Output: string;
};

export function ToolsView() {
  const [input, setInput] = useState("");
  const [algorithm, setAlgorithm] = useState("sha256");
  const [output, setOutput] = useState("");
  const [error, setError] = useState("");

  async function runHash() {
    setError("");
    try {
      const result = await executeCommand<HashResult>({
        id: "tools.hash",
        payload: { algorithm, input },
      });
      setOutput(result.Output);
    } catch (error) {
      setOutput("");
      setError(errorMessage(error));
    }
  }

  return (
    <section className="tools-view">
      <h2>Hash</h2>
      <label className="tools-field">
        Algorithm
        <select
          value={algorithm}
          onChange={(event) => setAlgorithm(event.target.value)}
        >
          <option value="sha256">SHA-256</option>
          <option value="sha512">SHA-512</option>
          <option value="md5">MD5</option>
        </select>
      </label>
      <label className="tools-field">
        Input
        <textarea
          aria-label="hash-input"
          value={input}
          onChange={(event) => setInput(event.target.value)}
        />
      </label>
      <button type="button" onClick={runHash}>
        Hash
      </button>
      {output ? <pre aria-label="hash-output">{output}</pre> : null}
      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
