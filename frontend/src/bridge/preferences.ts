import { AppService } from "../../bindings/devkit/internal/bridge";
import { Theme as NativeTheme } from "../../bindings/devkit/internal/preferences/models";
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
