import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Events, System } from "@wailsio/runtime";
import {
  getThemeFamily,
  getThemePreference,
  saveThemeFamily,
  saveThemePreference,
  type ThemeFamilyPreference,
  type ThemePreference,
} from "../bridge/preferences";
import { applyThemeFamily } from "./applyTheme";
import { familyOf } from "./families";

export type ResolvedTheme = "light" | "dark";

type ThemeContextValue = {
  theme: ThemePreference;
  resolvedTheme: ResolvedTheme;
  setTheme: (theme: ThemePreference) => Promise<void>;
  /** Which palette. Orthogonal to the mode — see preferences.ThemeFamily. */
  family: ThemeFamilyPreference;
  setFamily: (family: ThemeFamilyPreference) => Promise<void>;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

function browserDarkMode(): boolean {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

async function systemDarkMode(): Promise<boolean> {
  try {
    return await System.IsDarkMode();
  } catch {
    return browserDarkMode();
  }
}

function resolveTheme(theme: ThemePreference, systemDark: boolean): ResolvedTheme {
  if (theme === "system") {
    return systemDark ? "dark" : "light";
  }
  return theme;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemePreference | null>(null);
  const [family, setFamilyState] = useState<ThemeFamilyPreference | null>(null);
  const [systemDark, setSystemDark] = useState(false);
  const themeRef = useRef<ThemePreference | null>(null);
  const familyRef = useRef<ThemeFamilyPreference | null>(null);

  useEffect(() => {
    let active = true;
    // One Promise.all, not three awaits: these are three independent reads and
    // the gate below waits for all of them anyway, so serialising them would
    // add latency to the one thing that blocks the first paint.
    void Promise.all([
      getThemePreference().catch(() => "dark" as const),
      getThemeFamily().catch(() => "devkit" as const),
      systemDarkMode(),
    ]).then(([savedTheme, savedFamily, isSystemDark]) => {
      if (!active) {
        return;
      }
      themeRef.current = savedTheme;
      familyRef.current = savedFamily;
      setSystemDark(isSystemDark);
      setThemeState(savedTheme);
      setFamilyState(savedFamily);
    });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const refreshSystemTheme = () => {
      if (themeRef.current !== "system") {
        return;
      }
      void systemDarkMode().then(setSystemDark);
    };

    const offThemeChanged = Events.On(Events.Types.Common.ThemeChanged, refreshSystemTheme);
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener("change", refreshSystemTheme);
    return () => {
      offThemeChanged();
      media?.removeEventListener("change", refreshSystemTheme);
    };
  }, []);

  const resolvedTheme = theme ? resolveTheme(theme, systemDark) : null;

  // The palette is written before the mode attribute flips, and both in a
  // layout effect, so the browser never gets a chance to paint a frame with
  // one applied and not the other.
  useLayoutEffect(() => {
    if (!family) {
      return;
    }
    const definition = familyOf(family);
    applyThemeFamily(family, { dark: definition.dark, light: definition.light });
  }, [family]);

  useLayoutEffect(() => {
    if (!resolvedTheme) {
      return;
    }
    const root = document.documentElement;
    root.classList.toggle("dark", resolvedTheme === "dark");
    root.dataset.theme = resolvedTheme;
    root.style.colorScheme = resolvedTheme;
  }, [resolvedTheme]);

  const setTheme = useCallback(
    async (nextTheme: ThemePreference) => {
      const previousTheme = themeRef.current ?? "dark";
      if (nextTheme === "system") {
        setSystemDark(await systemDarkMode());
      }
      themeRef.current = nextTheme;
      setThemeState(nextTheme);
      try {
        await saveThemePreference(nextTheme);
      } catch (error) {
        themeRef.current = previousTheme;
        setThemeState(previousTheme);
        throw error;
      }
    },
    [],
  );

  const setFamily = useCallback(async (next: ThemeFamilyPreference) => {
    const previous = familyRef.current ?? "devkit";
    familyRef.current = next;
    setFamilyState(next);
    try {
      await saveThemeFamily(next);
    } catch (error) {
      familyRef.current = previous;
      setFamilyState(previous);
      throw error;
    }
  }, []);

  const value = useMemo<ThemeContextValue | null>(
    () =>
      theme && resolvedTheme && family
        ? {
            theme,
            resolvedTheme,
            setTheme,
            family,
            setFamily,
          }
        : null,
    [family, resolvedTheme, setFamily, setTheme, theme],
  );

  // Avoid painting the default CSS palette before the persisted preference is
  // known. The native window already has the correct appearance from Go. The
  // family is part of that gate now: a frame of DevKit lime before Dracula
  // arrives is the "phần tử đuổi theo theme" smoothness §8.4 rules out.
  if (!value) {
    return null;
  }

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useTheme must be used inside ThemeProvider");
  }
  return context;
}
