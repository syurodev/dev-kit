/**
 * A set of modules the title bar shows as one button.
 *
 * Presentational and frontend-only: the modules stay modules — their own tabs,
 * rail, work-folder pin and palette entries, keyed by their own ids — and the
 * backend has no idea a group exists. Only the switcher (workbench/switcherItems)
 * reads it.
 *
 * Members are the modules that name the group in their manifest; nothing here
 * lists them, so a module joins by saying so and cannot be left out of a list
 * that is kept somewhere else.
 */
export type ModuleGroup = {
  /** Also where the group falls in the title bar: slots sort by id, a module's or a group's. */
  id: string;
  title: string;
  /**
   * The member a group's button opens before any has been used — the one most
   * people arrive for. Falls back to the first member that can be opened.
   */
  primary?: string;
};

/**
 * Database, Kafka and SSH: each is a connection to a system elsewhere, made with
 * a saved profile, and the three are used together — the same accounts and
 * environment tags run through all of them.
 */
export const INFRA_GROUP: ModuleGroup = { id: "infra", title: "Infra", primary: "database" };

/** Notes and Tasks: where work is written down and where it is tracked. */
export const PLAN_GROUP: ModuleGroup = { id: "plan", title: "Plan", primary: "notes" };

/**
 * What is reached for now and then rather than all day: the API client, the
 * vault of accounts, and the small tools. It is last on the bar for that reason
 * (its id sorts after the others).
 */
export const UTILITIES_GROUP: ModuleGroup = { id: "utilities", title: "Utilities", primary: "apiclient" };
