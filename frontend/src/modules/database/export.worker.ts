import { serializeCSV, serializeJSON } from "./exportSerialization";
self.onmessage = (e) => {
  try {
    self.postMessage({
      content:
        e.data.format === "csv"
          ? serializeCSV(e.data.result)
          : serializeJSON(e.data.result),
    });
  } catch (err) {
    self.postMessage({
      error: err instanceof Error ? err.message : "Could not prepare export.",
    });
  }
};
