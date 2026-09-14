import { AppService } from "../../bindings/devkit/internal/bridge";
import {
  Theme as NativeTheme,
  ThemeFamily as NativeThemeFamily,
} from "../../bindings/devkit/internal/preferences/models";
import { callBridge } from "./client";

export const themePreferences = ["system", "light", "dark"] as const;

export type ThemePreference = (typeof themePreferences)[number];

function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === "string" && themePreferences.includes(value as ThemePreference);
}

export function getThemePreference(): Promise<ThemePreference> {
  return callBridge(async () => {
    const value = await AppService.PreferencesGetTheme();
    return isThemePreference(value) ? value : "dark";
  });
}

export function saveThemePreference(theme: ThemePreference): Promise<void> {
  const nativeTheme = {
    system: NativeTheme.ThemeSystem,
    light: NativeTheme.ThemeLight,
    dark: NativeTheme.ThemeDark,
  }[theme];
  return callBridge(() => AppService.PreferencesSetTheme(nativeTheme));
}

/**
 * Which palette the UI is painted with — a second axis from the mode above.
 *
 * "devkit" is the palette in index.css, and it is the absence of an override
 * rather than a set of values, which is why it is the default and why it has
 * no token map on the frontend either.
 */
export const themeFamilies = ["devkit", "catppuccin", "dracula", "ayu", "github"] as const;

export type ThemeFamilyPreference = (typeof themeFamilies)[number];

function isThemeFamily(value: unknown): value is ThemeFamilyPreference {
  return typeof value === "string" && themeFamilies.includes(value as ThemeFamilyPreference);
}

export function getThemeFamily(): Promise<ThemeFamilyPreference> {
  return callBridge(async () => {
    const value = await AppService.PreferencesGetThemeFamily();
    return isThemeFamily(value) ? value : "devkit";
  });
}

export function saveThemeFamily(family: ThemeFamilyPreference): Promise<void> {
  const native = {
    devkit: NativeThemeFamily.ThemeFamilyDevKit,
    catppuccin: NativeThemeFamily.ThemeFamilyCatppuccin,
    dracula: NativeThemeFamily.ThemeFamilyDracula,
    ayu: NativeThemeFamily.ThemeFamilyAyu,
    github: NativeThemeFamily.ThemeFamilyGitHub,
  }[family];
  return callBridge(() => AppService.PreferencesSetThemeFamily(native));
}

/**
 * Whether Agent Hub raises an OS notification when an agent needs you or
 * finishes. On by default; turning it on asks the OS for permission to notify.
 */
export function getAgentNotifications(): Promise<boolean> {
  return callBridge(() => AppService.PreferencesGetAgentNotifications());
}

export function saveAgentNotifications(enabled: boolean): Promise<void> {
  return callBridge(() => AppService.PreferencesSetAgentNotifications(enabled));
}
