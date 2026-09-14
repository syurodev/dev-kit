import { createContext, useContext } from "react";

const ModuleVisibilityContext = createContext(true);

export function ModuleVisibilityProvider({
  visible,
  children,
}: {
  visible: boolean;
  children: React.ReactNode;
}) {
  return <ModuleVisibilityContext.Provider value={visible}>{children}</ModuleVisibilityContext.Provider>;
}

export function useModuleVisibility(): boolean {
  return useContext(ModuleVisibilityContext);
}
