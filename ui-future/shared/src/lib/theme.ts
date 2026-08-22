// KAC-246: дуал-тема Kachō (холодная «операторская» тёмная + светлая).
// buildTheme(mode) → AntD ThemeConfig с algorithm + token + components-override.
//
// Значения токенов синхронизированы с CSS-vars в index.css (:root[data-theme=...])
// — Tailwind/CSS-компоненты (StatusBadge/Toaster) читают те же цвета через var(--…).
// Меняешь токен здесь → меняй соответствующую CSS-var.
//
// Требование связности не декоративное: часть ролей палитры (фон поля ввода,
// усиленная линия, ореол фокуса) видна пользователю ТОЛЬКО через AntD-компоненты,
// а часть — только через CSS. Разъехавшись, они дадут поле одного тона в рамке
// другого, и заметит это не сборка, а глаз.

import { theme as antdTheme, type ThemeConfig } from "antd";

export type ThemeMode = "dark" | "light";

/** Палитра одной темы — единый источник для AntD token и (зеркально) для CSS-vars. */
interface Palette {
  page: string; // bgBase / фон страницы и layout
  rail: string; // фон рейла модулей (--kc-rail)
  container: string; // bgContainer (карточки, header таблицы)
  elevated: string; // bgElevated (модалки, dropdown) — единственное, что всплывает
  border: string; // основная линия (--kc-border)
  borderSecondary: string; // внутренние разделители (--kc-border-secondary)
  /** Усиленная линия: наведение на элемент управления (--kc-line-strong). */
  lineStrong: string;
  text: string;
  textSecondary: string;
  textTertiary: string;
  hoverFill: string;
  /** Фон закрытых инпутов/селектов (--kc-field): поле «утоплено» в поверхность. */
  field: string;
  /** Фон поля при наведении (--kc-field-hover). */
  fieldHover: string;
  /** Сигнал: действие, фокус, активный пункт (--kc-primary). */
  signal: string;
  /** Заливка под сигналом — тинт, а не сплошной цвет (--kc-primary-bg). */
  signalFill: string;
  /** Заливка сигнала при наведении на основную кнопку. */
  signalFillHover: string;
  /** Цвет текста на сигнальной заливке (кнопка «основное действие»). */
  signalText: string;
  cyan: string; // телеметрия (--kc-cyan)
  healthy: string;
  warning: string;
  danger: string;
  /** Цвет текста опасного действия — светлее самой опасности, чтобы читался. */
  dangerText: string;
  dangerFill: string;
  shadowSm: string;
  shadowMd: string;
  shadowLg: string;
  /** Ореол фокуса: 3px тинта сигнала (--kc-focus-ring). */
  focusRing: string;
  focusRingColor: string;
}

const DARK: Palette = {
  page: "#050914",
  rail: "#070d18",
  container: "#09111f",
  elevated: "#111c2e",
  border: "rgba(205,222,255,0.11)",
  borderSecondary: "rgba(205,222,255,0.09)",
  lineStrong: "rgba(205,222,255,0.2)",
  text: "#e8f0ff",
  textSecondary: "#8c9ab3",
  textTertiary: "#65738c",
  hoverFill: "rgba(232,240,255,0.04)",
  field: "#0d1728",
  fieldHover: "#111d31",
  signal: "#6d8eff",
  signalFill: "rgba(109,142,255,0.12)",
  signalFillHover: "rgba(109,142,255,0.19)",
  signalText: "#9eb4ff",
  cyan: "#65d7ee",
  healthy: "#57d59a",
  warning: "#e8b765",
  danger: "#ff6b78",
  dangerText: "#ff8792",
  dangerFill: "rgba(255,107,120,0.08)",
  shadowSm: "0 1px 2px rgba(0,0,0,.5)",
  shadowMd: "0 12px 32px rgba(0,0,0,.55)",
  shadowLg: "0 22px 60px rgba(0,0,0,.62)",
  focusRing: "0 0 0 3px rgba(109,142,255,.12)",
  focusRingColor: "rgba(109,142,255,.12)",
};

const LIGHT: Palette = {
  page: "#f4f6fb",
  rail: "#eef1f7",
  container: "#ffffff",
  elevated: "#ffffff",
  border: "rgba(16,28,56,0.1)",
  borderSecondary: "rgba(16,28,56,0.07)",
  lineStrong: "rgba(16,28,56,0.2)",
  text: "#0d1526",
  textSecondary: "#5c6a85",
  textTertiary: "#8492aa",
  hoverFill: "rgba(16,28,56,0.04)",
  field: "#f7f9fd",
  fieldHover: "#eff3fa",
  signal: "#3a5bd9",
  signalFill: "rgba(58,91,217,0.1)",
  signalFillHover: "rgba(58,91,217,0.16)",
  signalText: "#2f4dc4",
  cyan: "#0d7f99",
  healthy: "#0f7a52",
  warning: "#8d5d12",
  danger: "#d02f3d",
  dangerText: "#b32633",
  dangerFill: "rgba(208,47,61,0.07)",
  shadowSm: "0 1px 2px rgba(13,21,38,.06)",
  shadowMd: "0 10px 28px rgba(13,21,38,.1)",
  shadowLg: "0 22px 56px rgba(13,21,38,.16)",
  focusRing: "0 0 0 3px rgba(58,91,217,.14)",
  focusRingColor: "rgba(58,91,217,.14)",
};

/**
 * Геометрия — одна на обе темы: тема меняет тон, а не форму. Числа взяты у
 * эталона (высота кнопки 36, поля 38, радиус элемента управления 8, радиус
 * поверхности 11, компактного — 5).
 */
const SHAPE = {
  controlHeight: 36,
  fieldHeight: 38,
  radius: 8,
  radiusSurface: 11,
  radiusCompact: 5,
  fontSize: 13,
  railWidth: 62,
  headerHeight: 54,
} as const;

// Общие (theme-agnostic) бренд-цвета. Градиент — единственное место, где два
// насыщенных цвета стоят рядом; он не участвует в разметке страниц.
export const BRAND = {
  primary: DARK.signal,
  gradient: "linear-gradient(135deg,#6D8EFF 0%,#9D79EF 100%)",
  gradientFrom: "#6D8EFF",
  gradientTo: "#9D79EF",
  success: DARK.healthy,
  warning: DARK.warning,
  error: DARK.danger,
  focusRing: DARK.focusRing,
  focusRingColor: DARK.focusRingColor,
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
      colorPrimary: p.signal,
      colorInfo: p.signal,
      colorSuccess: p.healthy,
      colorWarning: p.warning,
      colorError: p.danger,

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
      fontFamilyCode: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
      fontSize: SHAPE.fontSize,
      // Кнопка 36, поле 38 — разной высоты намеренно (эталон): поле выше, потому
      // что в него вводят, кнопка компактнее, потому что её только нажимают.
      controlHeight: SHAPE.controlHeight,
      borderRadius: SHAPE.radius,
      borderRadiusLG: SHAPE.radiusSurface,
      borderRadiusSM: 6,
      borderRadiusXS: 4,

      motionEaseInOut: "cubic-bezier(0.22, 1, 0.36, 1)",
      motionDurationMid: "0.16s",

      // Тень остаётся только у всплывающего (модалка, выпадающий список);
      // статичные панели держат глубину тоном — см. .kc-surface в index.css.
      boxShadow: p.shadowMd,
      boxShadowSecondary: p.shadowLg,
    },
    components: {
      Layout: {
        headerBg: p.page,
        headerHeight: SHAPE.headerHeight,
        headerPadding: "0 12px",
        siderBg: p.rail,
        bodyBg: p.page,
      },
      Menu: {
        itemBg: "transparent",
        itemSelectedBg: p.signalFill,
        itemActiveBg: p.hoverFill,
        itemHoverBg: p.hoverFill,
        itemSelectedColor: p.signal,
        itemColor: p.textSecondary,
        itemHoverColor: p.text,
        itemBorderRadius: SHAPE.radius,
        itemHeight: SHAPE.controlHeight,
      },
      Table: {
        headerBg: p.container,
        rowHoverBg: p.hoverFill,
        // Строки разделяет линия, а не заливка: полосатость снята (index.css).
        borderColor: p.border,
        headerSplitColor: "transparent",
        headerColor: p.textSecondary,
        rowSelectedBg: p.signalFill,
        rowSelectedHoverBg: p.signalFill,
        cellFontSize: 12,
      },
      // KAC-246: горизонтальные табы (admin/iam) — чёткий active-цвет + ink-bar
      // в accent, читаемый разделитель снизу, чтобы таб-полоса не сливалась.
      Tabs: {
        itemColor: p.textSecondary,
        itemHoverColor: p.text,
        itemSelectedColor: p.signal,
        itemActiveColor: p.signal,
        inkBarColor: p.signal,
        titleFontSize: SHAPE.fontSize,
      },
      Modal: {
        contentBg: p.elevated,
        headerBg: p.elevated,
        footerBg: p.elevated,
        titleFontSize: 15,
        // Модалка — единственное, что действительно всплывает над страницей,
        // поэтому тень у неё есть, и она сильная.
        boxShadow: p.shadowLg,
        borderRadiusLG: SHAPE.radiusSurface,
      },
      Card: {
        colorBgContainer: p.container,
        colorBorderSecondary: p.border,
        borderRadiusLG: SHAPE.radiusSurface,
      },
      Select: {
        colorBgContainer: p.field,
        colorBgElevated: p.elevated,
        optionSelectedBg: p.signalFill,
        optionActiveBg: p.hoverFill,
        controlHeight: SHAPE.fieldHeight,
        hoverBorderColor: p.lineStrong,
        activeBorderColor: p.signal,
        // Select использует activeOutlineColor (цвет «ореола» фокуса), не activeShadow.
        activeOutlineColor: p.focusRingColor,
        optionFontSize: SHAPE.fontSize,
      },
      Input: {
        colorBgContainer: p.field,
        hoverBg: p.fieldHover,
        activeBg: p.field,
        hoverBorderColor: p.lineStrong,
        activeBorderColor: p.signal,
        activeShadow: p.focusRing,
        controlHeight: SHAPE.fieldHeight,
        paddingInline: 11,
      },
      InputNumber: {
        colorBgContainer: p.field,
        hoverBg: p.fieldHover,
        activeBg: p.field,
        hoverBorderColor: p.lineStrong,
        activeBorderColor: p.signal,
        controlHeight: SHAPE.fieldHeight,
        // InputNumber наследует focus-стиль внутреннего Input (activeShadow выше).
        activeShadow: p.focusRing,
      },
      DatePicker: {
        colorBgContainer: p.field,
        hoverBorderColor: p.lineStrong,
        activeBorderColor: p.signal,
        activeShadow: p.focusRing,
        controlHeight: SHAPE.fieldHeight,
      },
      Button: {
        // Кнопка по умолчанию — прозрачная в линии; заливка появляется только
        // при наведении и у основного действия. Это и есть «скупой сигнал».
        defaultBg: "transparent",
        defaultColor: p.text,
        defaultBorderColor: p.border,
        defaultHoverBg: p.hoverFill,
        defaultHoverColor: p.text,
        defaultHoverBorderColor: p.lineStrong,
        defaultActiveBg: p.hoverFill,
        defaultActiveColor: p.text,
        defaultActiveBorderColor: p.lineStrong,
        dangerColor: p.dangerText,
        textHoverBg: p.hoverFill,
        contentFontSize: 12,
        fontWeight: 610,
        paddingInline: 13,
        controlHeight: SHAPE.controlHeight,
        borderRadius: SHAPE.radius,
        iconGap: 8,
        // Тени под кнопками нет ни у одной: глубина делается тоном.
        primaryShadow: "none",
        defaultShadow: "none",
        dangerShadow: "none",
      },
      Tag: {
        defaultBg: p.field,
        defaultColor: p.textSecondary,
        borderRadiusSM: SHAPE.radiusCompact,
        colorBorder: p.border,
        // Форму эталон задаёт полностью (радиус 5, фон поля, линия), а кегль —
        // нет: у эталона в теге лежит машинное значение (CIDR), и он там
        // моноширинный 10. У нас тег носит и слова тоже, поэтому кегль остаётся
        // базовым, а моноширинный ряд живёт отдельным классом .t-mono — иначе
        // русское слово в теге пришлось бы читать десятым кеглем.
      },
      Tooltip: {
        colorBgSpotlight: p.elevated,
        colorTextLightSolid: p.text,
      },
      Segmented: {
        itemSelectedBg: p.signalFill,
        itemSelectedColor: p.signal,
        trackBg: p.field,
      },
    },
  };
}
