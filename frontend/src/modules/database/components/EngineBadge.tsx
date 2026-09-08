export type EngineType = "postgres" | "mysql" | "mariadb" | string;

export function getEngineMeta(engine?: string) {
  const norm = (engine || "postgres").toLowerCase();
  if (norm === "mysql") {
    return {
      name: "MySQL",
      placeholder: "?",
      badgeVariant: "outline" as const,
      colorClass: "text-amber-500 border-amber-500/30 bg-amber-500/10",
      accentDot: "bg-amber-500",
    };
  }
  if (norm === "mariadb") {
    return {
      name: "MariaDB",
      placeholder: "?",
      badgeVariant: "outline" as const,
      colorClass: "text-teal-400 border-teal-400/30 bg-teal-400/10",
      accentDot: "bg-teal-400",
    };
  }
  return {
    name: "PostgreSQL",
    placeholder: "$1, $2",
    badgeVariant: "outline" as const,
    colorClass: "text-sky-400 border-sky-400/30 bg-sky-400/10",
    accentDot: "bg-sky-400",
  };
}

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
    <div className={`inline-flex items-center gap-1.5 ${className}`}>
      <span
        className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-caption-11-medium border ${meta.colorClass}`}
      >
        <span className={`w-1.5 h-1.5 rounded-full ${meta.accentDot}`} />
        <span>{meta.name}</span>
      </span>
      {showPlaceholder ? (
        <span className="text-caption-11 text-text-tertiary font-mono bg-surface-base px-1.5 py-0.5 rounded border border-border">
          {meta.placeholder}
        </span>
      ) : null}
    </div>
  );
}
