import { cn } from "@/lib/utils";
import { Chip } from "@/components/ui/chip";
import { MariaDBMark, MySQLMark, PostgresMark, SQLiteMark } from "../icons/engineMarks";

export type EngineType = "postgres" | "mysql" | "mariadb" | "sqlite" | string;

export function getEngineMeta(engine?: string) {
  const norm = (engine || "postgres").toLowerCase();
  if (norm === "mysql") {
    return { id: norm, name: "MySQL", placeholder: "?", Mark: MySQLMark };
  }
  if (norm === "mariadb") {
    return { id: norm, name: "MariaDB", placeholder: "?", Mark: MariaDBMark };
  }
  if (norm === "sqlite") {
    return { id: norm, name: "SQLite", placeholder: "?", Mark: SQLiteMark };
  }
  return { id: "postgres", name: "PostgreSQL", placeholder: "$1, $2", Mark: PostgresMark };
}

/**
 * The 16px brand mark Figma puts at the head of every profile row (741:20).
 * A vendor's mark in the vendor's colours — the one place raw hex is right.
 */
export function EngineMark({ engine, className }: { engine?: string; className?: string }) {
  const { Mark } = getEngineMeta(engine);
  return <Mark className={cn("size-4 shrink-0", className)} />;
}

export { Chip } from "@/components/ui/chip";

export function EngineBadge({
  engine,
  showPlaceholder = false,
  className = "",
}: {
  engine?: string;
  showPlaceholder?: boolean;
  className?: string;
}) {
  const meta = getEngineMeta(engine);
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <Chip>{meta.id}</Chip>
      {showPlaceholder ? (
        <Chip className="text-text-tertiary">{meta.placeholder}</Chip>
      ) : null}
    </span>
  );
}
