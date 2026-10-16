export type SchemaChoice = "auto" | "none" | "liquibase" | "compose" | "kubernetes";
export const SCHEMA_LABELS: Record<SchemaChoice, string> = { auto: "Auto", none: "Syntax only", liquibase: "Liquibase · SchemaStore", compose: "Docker Compose", kubernetes: "Kubernetes 1.35" };
export type YamlPath = (string | number)[];
export type Problem = { from: number; to: number; severity: "error" | "warning"; message: string; source: string };
export type SqlBlock = { document: number; path: YamlPath; from: number; to: number; raw: string; value: string; label: string };
export type Analysis = { problems: Problem[]; path: YamlPath; active: { from: number; to: number } | null; sql: SqlBlock | null; schema: string; notice?: string };
export type Suggestion = { label: string; detail?: string; info?: string; insert: string; kind: "property" | "constant"; cursor?: number };
export type Suggestions = { from: number; to: number; options: Suggestion[]; filter?: boolean } | null;
export type Edit = { from: number; to: number; insert: string };
export type Request = { text: string; file: string; schema: SchemaChoice; indentSize?: number } & (
  | { type: "analyze"; position: number }
  | { type: "complete"; position: number; explicit: boolean }
  | { type: "format"; selection?: { from: number; to: number } }
  | { type: "applySql"; block: SqlBlock; value: string }
);
export type Response = Analysis | Suggestions | Edit;
export const YAML_LIMIT = 512_000;
