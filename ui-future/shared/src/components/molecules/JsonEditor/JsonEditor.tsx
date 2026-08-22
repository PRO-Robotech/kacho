import { useState } from "react";
import { cn } from "@shared/lib/utils";

interface Props {
  value: string;
  onChange: (v: string) => void;
  rows?: number;
  className?: string;
  placeholder?: string;
}

export function JsonEditor({ value, onChange, rows = 18, className, placeholder }: Props) {
  const [parseErr, setParseErr] = useState<string | null>(null);

  const handle = (s: string) => {
    onChange(s);
    if (!s.trim()) {
      setParseErr(null);
      return;
    }
    try {
      JSON.parse(s);
      setParseErr(null);
    } catch (e) {
      setParseErr((e as Error).message);
    }
  };

  return (
    <div className="space-y-1">
      {/* Поле ввода — та же форма, что у всякого поля продукта: заливка
          `--kc-field`, линия `--kc-border`, радиус 8. Прежде здесь стояли
          `bg-zinc-950 text-zinc-100` — чёрное поле со светлым текстом
          НЕЗАВИСИМО от темы: на светлой консоли редактор оставался чёрным
          прямоугольником. Отказ формата виден границей и подписью в тоне
          ошибки продукта. */}
      <textarea
        rows={rows}
        spellCheck={false}
        style={{
          background: "var(--kc-field)",
          color: "var(--kc-text)",
          borderRadius: 8,
          border: `1px solid ${parseErr ? "var(--kc-danger)" : "var(--kc-border)"}`,
        }}
        className={cn("w-full t-mono p-3", "focus:outline-none focus:ring-1 focus:ring-primary", className)}
        value={value}
        onChange={(e) => handle(e.target.value)}
        placeholder={placeholder}
      />
      {parseErr && (
        <div className="t-small" style={{ color: "var(--kc-danger)" }}>
          JSON: {parseErr}
        </div>
      )}
    </div>
  );
}
