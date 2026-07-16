// KAC-246: дуал-тема Kachō (Linear/Vercel premium-dark + светлая).
// buildTheme(mode) → AntD ThemeConfig с algorithm + token + components-override.
//
// Значения токенов синхронизированы с CSS-vars в index.css (:root[data-theme=...])
// — Tailwind/CSS-компоненты (StatusBadge/Toaster) читают те же цвета через var(--…).
// Меняешь токен здесь → меняй соответствующую CSS-var.

import { theme as antdTheme, type ThemeConfig } from "antd";

export type ThemeMode = "dark" | "light";

/** Палитра одной темы — единый источник для AntD token и (зеркально) для CSS-vars. */
interface Palette {
  page: string; // bgBase / фон страницы и layout
  container: string; // bgContainer (карточки, header таблицы)
  elevated: string; // bgElevated (модалки, dropdown)
  border: string;
  borderSecondary: string;
  text: string;
  textSecondary: string;
  textTertiary: string;
  hoverFill: string;
  /** Фон закрытых инпутов/селектов: в dark «утопает» в page, в light — container. */
  controlBg: string;
  shadowSm: string;
  shadowMd: string;
  shadowLg: string;
}

const DARK: Palette = {
  page: "#0d0e12",
  container: "#15161d",
  elevated: "#1b1d25",
  border: "#272a33",
  borderSecondary: "#1d1f27",
  text: "#e7e9ef",
  textSecondary: "#9aa0ac",
  textTertiary: "#6a7080",
  hoverFill: "rgba(255,255,255,0.045)",
  controlBg: "#0d0e12",
  shadowSm: "0 1px 2px rgba(0,0,0,.4)",
  shadowMd: "0 6px 20px rgba(0,0,0,.5)",
  shadowLg: "0 16px 48px rgba(0,0,0,.55)",
};

const LIGHT: Palette = {
  page: "#f6f7f9",
  container: "#ffffff",
  elevated: "#ffffff",
  border: "#e3e6ea",
  borderSecondary: "#eef0f3",
  text: "#14171c",
  textSecondary: "#5a616e",
  textTertiary: "#8b929e",
  hoverFill: "rgba(0,0,0,0.03)",
  controlBg: "#ffffff",
  shadowSm: "0 1px 2px rgba(16,24,40,.06)",
  shadowMd: "0 6px 20px rgba(16,24,40,.1)",
  shadowLg: "0 16px 48px rgba(16,24,40,.16)",
};

// Общие (theme-agnostic) бренд-цвета.
export const BRAND = {
  primary: "#3D8DF5",
  gradient: "linear-gradient(135deg,#3D8DF5 0%,#6E56CF 100%)",
  gradientFrom: "#3D8DF5",
  gradientTo: "#6E56CF",
  success: "#2bb877",
  warning: "#e0a338",
  error: "#e5484d",
  focusRing: "0 0 0 3px rgba(61,141,245,.25)",
  focusRingColor: "rgba(61,141,245,.25)",
} as const;

export function paletteFor(mode: ThemeMode): Palette {
  return mode === "light" ? LIGHT : DARK;
}

export function buildTheme(mode: ThemeMode): ThemeConfig {
  const p = paletteFor(mode);
  const algorithm = mode === "light" ? antdTheme.defaultAlgorithm : antdTheme.darkAlgorithm;

  return {
    algorithm,
    token: {
      colorPrimary: BRAND.primary,
      colorInfo: BRAND.primary,
      colorSuccess: BRAND.success,
      colorWarning: BRAND.warning,
      colorError: BRAND.error,

      colorBgBase: p.page,
      colorBgLayout: p.page,
      colorBgContainer: p.container,
      colorBgElevated: p.elevated,
      colorBorder: p.border,
      colorBorderSecondary: p.borderSecondary,
      colorText: p.text,
      colorTextSecondary: p.textSecondary,
      colorTextTertiary: p.textTertiary,

      fontFamily: "Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
      fontSize: 13,
      borderRadius: 8,
      borderRadiusLG: 12,
      borderRadiusSM: 6,

      boxShadow: p.shadowSm,
      boxShadowSecondary: p.shadowMd,
    },
    components: {
      Layout: {
        headerBg: p.page,
        headerHeight: 48,
        headerPadding: "0 12px",
        siderBg: p.page,
        bodyBg: p.page,
      },
      Menu: {
        itemBg: "transparent",
        itemSelectedBg: p.hoverFill,
        itemActiveBg: p.hoverFill,
        itemHoverBg: p.hoverFill,
        itemSelectedColor: BRAND.primary,
      },
      Table: {
        headerBg: p.container,
        rowHoverBg: p.hoverFill,
        borderColor: p.borderSecondary,
        headerColor: p.textSecondary,
      },
      // KAC-246: горизонтальные табы (admin/iam) — чёткий active-цвет + ink-bar
      // в accent, читаемый разделитель снизу, чтобы таб-полоса не сливалась.
      Tabs: {
        itemColor: p.textSecondary,
        itemHoverColor: p.text,
        itemSelectedColor: BRAND.primary,
        itemActiveColor: BRAND.primary,
        inkBarColor: BRAND.primary,
        titleFontSize: 13,
      },
      Modal: {
        contentBg: p.elevated,
        headerBg: p.elevated,
        footerBg: p.elevated,
        boxShadow: p.shadowLg,
      },
      Card: {
        colorBgContainer: p.container,
      },
      Select: {
        colorBgContainer: p.controlBg,
        colorBgElevated: p.elevated,
        optionSelectedBg: p.hoverFill,
        optionActiveBg: p.hoverFill,
        // Select использует activeOutlineColor (цвет «ореола» фокуса), не activeShadow.
        activeOutlineColor: BRAND.focusRingColor,
      },
      Input: {
        colorBgContainer: p.controlBg,
        activeShadow: BRAND.focusRing,
      },
      InputNumber: {
        colorBgContainer: p.controlBg,
        // InputNumber наследует focus-стиль внутреннего Input (activeShadow выше).
      },
      DatePicker: {
        colorBgContainer: p.controlBg,
      },
      Button: {
        primaryShadow: "none",
      },
    },
  };
}
