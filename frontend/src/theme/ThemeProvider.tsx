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
  getThemePreference,
  saveThemePreference,
  type ThemePreference,
} from "../bridge/preferences";

export type ResolvedTheme = "light" | "dark";

type ThemeContextValue = {
  theme: ThemePreference;
  resolvedTheme: ResolvedTheme;
  setTheme: (theme: ThemePreference) => Promise<void>;
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
  const [systemDark, setSystemDark] = useState(false);
  const themeRef = useRef<ThemePreference | null>(null);

  useEffect(() => {
    let active = true;
    void Promise.all([getThemePreference().catch(() => "dark" as const), systemDarkMode()]).then(
      ([savedTheme, isSystemDark]) => {
        if (!active) {
          return;
        }
        themeRef.current = savedTheme;
        setSystemDark(isSystemDark);
        setThemeState(savedTheme);
      },
    );
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

  const value = useMemo<ThemeContextValue | null>(
    () =>
      theme && resolvedTheme
        ? {
            theme,
            resolvedTheme,
            setTheme,
          }
        : null,
    [resolvedTheme, setTheme, theme],
  );

  // Avoid painting the default CSS palette before the persisted preference is
  // known. The native window already has the correct appearance from Go.
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
