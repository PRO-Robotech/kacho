// KAC-246: ThemeProvider + useThemeMode — runtime-переключатель dark/light.
// In the federated VPC remote this provider must not mutate <html>; the host
// owns global page theme. VPC CSS defaults to light unless a host-level
// data-theme is explicitly present.

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ConfigProvider } from "antd";
import { buildTheme, type ThemeMode } from "@/lib/theme";

const STORAGE_KEY = "kacho-theme";

interface ThemeContextValue {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  toggle: () => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

function htmlTheme(): ThemeMode | null {
  if (typeof document === "undefined") return null;
  const value = document.documentElement.dataset.theme;
  return value === "dark" || value === "light" ? value : null;
}

/** Резолвит начальный режим: host html[data-theme] → localStorage → prefers-color-scheme → light. */
function resolveInitialMode(): ThemeMode {
  const hostTheme = htmlTheme();
  if (hostTheme) return hostTheme;

  if (typeof window !== "undefined") {
    try {
      const stored = window.localStorage.getItem(STORAGE_KEY);
      if (stored === "dark" || stored === "light") return stored;
    } catch {
      // localStorage недоступен (private mode / SSR) — игнорируем.
    }
    if (typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: light)").matches) {
      return "light";
    }
  }
  return "light";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(resolveInitialMode);

  useEffect(() => {
    const applyHostTheme = () => {
      const next = htmlTheme();
      if (next) setModeState(next);
    };

    applyHostTheme();
    if (typeof MutationObserver === "undefined") return undefined;

    const observer = new MutationObserver(applyHostTheme);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // localStorage недоступен.
    }
  }, []);

  const toggle = useCallback(() => {
    setModeState((cur) => {
      const next: ThemeMode = cur === "dark" ? "light" : "dark";
      try {
        window.localStorage.setItem(STORAGE_KEY, next);
      } catch {
        /* ignore */
      }
      return next;
    });
  }, []);

  const value = useMemo<ThemeContextValue>(() => ({ mode, setMode, toggle }), [mode, setMode, toggle]);

  // ConfigProvider с полной темой (buildTheme: fontSize 13 + component-токены) —
  // host-ConfigProvider задаёт лишь colorPrimary/fontFamily без fontSize, поэтому
  // без этого remote-контент рендерился бы в дефолтном AntD-размере (14), а не в
  // эталонном 13 kacho-ui. Nested ConfigProvider переопределяет тему для remote.
  return (
    <ThemeContext.Provider value={value}>
      <ConfigProvider theme={buildTheme(mode)}>{children}</ConfigProvider>
    </ThemeContext.Provider>
  );
}

export function useThemeMode(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error("useThemeMode must be used within <ThemeProvider>");
  }
  return ctx;
}
