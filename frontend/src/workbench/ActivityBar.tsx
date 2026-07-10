type ActivityBarProps = {
  activeModuleId: string;
  modules: Array<{ id: string; title: string }>;
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
            module.id === activeModuleId
              ? "activity-item is-active"
              : "activity-item"
          }
          onClick={() => onSelectModule(module.id)}
        >
          {module.title}
        </button>
      ))}
    </nav>
  );
}
