type ActivityBarProps = {
  activeModuleId: string;
  modules: Array<{ id: string; title: string; unavailable?: boolean }>;
  onSelectModule: (moduleId: string) => void;
};

export function ActivityBar({
  activeModuleId,
  modules,
  onSelectModule,
}: ActivityBarProps) {
  return (
    <nav className="activity-bar" aria-label="Modules">
      {modules.map((module) => (
        <button
          key={module.id}
          type="button"
          className={
            module.unavailable
              ? "activity-item is-unavailable"
              : module.id === activeModuleId
              ? "activity-item is-active"
              : "activity-item"
          }
          disabled={module.unavailable}
          aria-disabled={module.unavailable}
          onClick={() => onSelectModule(module.id)}
        >
          {module.title}
          {module.unavailable ? " (Unavailable)" : null}
        </button>
      ))}
    </nav>
  );
}
