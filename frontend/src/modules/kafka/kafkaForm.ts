/**
 * The pure edges of the Kafka module — everything a test can pin without a
 * broker. The view and the store both import from here so the rules live
 * once.
 */

export const SECURITY_PROTOCOLS = ["PLAINTEXT", "SSL", "SASL_PLAINTEXT", "SASL_SSL"] as const;
export const SASL_MECHANISMS = ["PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"] as const;

/** The branch the new-cluster form takes: only SASL asks for a mechanism and a user. */
export function isSaslProtocol(protocol: string): boolean {
  return protocol === "SASL_PLAINTEXT" || protocol === "SASL_SSL";
}

/**
 * "a:9092, b:9093," → ["a:9092", "b:9093"]. The field is typed as one line
 * because that is how every Kafka client prints it; the profile stores a list.
 */
export function parseBootstrap(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

/** "" → undefined (let the broker choose by key), "2" → 2. NaN is undefined too. */
export function parsePartition(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  return Number.isInteger(n) && n >= 0 ? n : undefined;
}

/** A broker timestamp (unix ms) as the table shows it — the wall clock, to the ms. */
export function formatClock(unixMs: number | undefined): string {
  if (unixMs == null || !Number.isFinite(unixMs)) return "";
  const d = new Date(unixMs);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}

/** Pretty JSON when the value parses, the raw bytes-as-text when it does not. */
export function prettyValue(value: string): { text: string; json: boolean } {
  try {
    return { text: JSON.stringify(JSON.parse(value), null, 2), json: true };
  } catch {
    return { text: value, json: false };
  }
}

/** "4 812 907" — offsets get long, and a thin space every three digits is
 *  the difference between reading one and counting it. */
export function formatOffset(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}
