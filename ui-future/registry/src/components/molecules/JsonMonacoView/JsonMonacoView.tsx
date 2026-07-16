// JsonMonacoView — read-only JSON viewer на Monaco editor с terminal-стилем
// (vs-dark тема, моно-шрифт, line-numbers, минимальный chrome).
//
// Используется в JSON-табе detail-страниц вместо <pre>-блока.

import { useMemo } from "react";
import Editor from "@monaco-editor/react";
import { theme } from "antd";
import { useThemeMode } from "@/lib/theme-context";

interface Props {
  data: unknown;
  /** Высота редактора. Default 60vh — занимает основную часть tab-area. */
  height?: string | number;
}

export function JsonMonacoView({ data, height = "60vh" }: Props) {
  const { token } = theme.useToken();
  const { mode } = useThemeMode();
  // Мемоизируем сериализацию: detail-query поллит каждые 3-5с и заново передаёт
  // data — без memo Monaco перекормливался бы новой строкой на каждый рефетч.
  const json = useMemo(() => JSON.stringify(data, null, 2), [data]);
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
