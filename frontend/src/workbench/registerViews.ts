import { registerMemoryResultViews } from "./memoryResultViews";

/**
 * Result views for tools the workbench itself owns — what a module's
 * manifest does for its own tools. Imported once by ./Workbench.tsx, beside
 * the module registrations. Workspace memory has no module of its own.
 */
registerMemoryResultViews();
