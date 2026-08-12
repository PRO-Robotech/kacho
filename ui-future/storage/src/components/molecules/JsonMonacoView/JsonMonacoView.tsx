// JsonMonacoView — read-only JSON viewer на Monaco editor с terminal-стилем
// (vs-dark тема, моно-шрифт, line-numbers, минимальный chrome).
//
// Используется в JSON-табе detail-страниц вместо <pre>-блока.

import Editor, { loader } from "@monaco-editor/react";
import { theme } from "antd";
import { useThemeMode } from "@/lib/theme-context";

interface Props {
  data: unknown;
  /** Высота редактора. Default 60vh — занимает основную часть tab-area. */
  height?: string | number;
}

// Редактор берётся со СВОЕГО origin. По умолчанию `@monaco-editor/react` грузит
// его с внешнего CDN, а CSP консоли разрешает только `script-src 'self'` —
// браузер блокирует загрузку, и вкладка навсегда остаётся в «Loading…»
// (наблюдалось на живом стенде 2026-08-12: JSON не работал ни у одного ресурса).
// Ослаблять CSP ради стороннего домена нельзя, поэтому файлы едут в образе:
// `scripts/copy-monaco.mjs` кладёт их в `public/monaco/vs` перед сборкой.
//
// Путь — от базы САМОГО модуля (`/vpc-remote/`, `/nlb-remote/`, …), потому что
// каждый федеративный модуль отдаётся под своим префиксом, а оболочка редактора
// не несёт: у неё нет и зависимости на него.
// `import.meta.env` даёт Vite; в jest его нет вовсе, и обращение к полю роняло
// бы МОДУЛЬ на импорте — то есть пробы падали бы не на предмете, а на среде
// (так и случилось: шесть проб маршрутов легли на `BASE_URL` undefined).
const monacoBase = (import.meta as { env?: { BASE_URL?: string } }).env?.BASE_URL ?? "/";
loader.config({ paths: { vs: `${monacoBase}monaco/vs` } });

export function JsonMonacoView({ data, height = "60vh" }: Props) {
  const { token } = theme.useToken();
  const { mode } = useThemeMode();
  const json = JSON.stringify(data, null, 2);
  // KAC-246: Monaco-тема следует теме приложения (в светлой раньше оставался тёмным).
  const monacoTheme = mode === "dark" ? "vs-dark" : "vs";

  return (
    <div
      style={{
        border: `1px solid ${token.colorBorderSecondary}`,
        borderRadius: token.borderRadius,
        overflow: "hidden",
        background: mode === "dark" ? "#1e1e1e" : "#ffffff",
      }}
    >
      <Editor
        height={height}
        defaultLanguage="json"
        value={json}
        theme={monacoTheme}
        options={{
          readOnly: true,
          domReadOnly: true,
          minimap: { enabled: false },
          fontSize: 12,
          fontFamily: "ui-monospace, 'Cascadia Code', 'Source Code Pro', Menlo, Consolas, monospace",
          lineNumbers: "on",
          renderLineHighlight: "none",
          scrollBeyondLastLine: false,
          smoothScrolling: true,
          wordWrap: "off",
          padding: { top: 8, bottom: 8 },
          overviewRulerLanes: 0,
          overviewRulerBorder: false,
          hideCursorInOverviewRuler: true,
          folding: true,
          foldingStrategy: "indentation",
          automaticLayout: true,
        }}
      />
    </div>
  );
}
