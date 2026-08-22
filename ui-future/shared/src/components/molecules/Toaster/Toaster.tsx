// Toaster — всплывающие уведомления. KAC-246: theme-aware через CSS-vars
// (--toast-bg/-border/-fg + per-variant accent). В dark — тёмный фон + accent-
// текст иконки/полосы; в light — светлый фон. Аккуратная тень (--kc-shadow-md).
// Раньше палитра была хардкод-светлая (bg-emerald-50) — в dark выглядела инородно.
//
// ЕДИНСТВЕННАЯ реализация на всё дерево; копии в модулях — ре-экспорт отсюда.
// Тот же довод, по которому единой сделана сама очередь (`lib/toast.ts`): очередь
// — модульный синглтон, и разъехавшийся показ означал бы, что сигнал уходит в
// очередь, которую никто не читает. Это не гипотеза: модуль `system` не нёс
// показа ВОВСЕ, и потому отказ на форме создания региона не был виден никак —
// находка владельца 2026-08-15, из которой выведено правило про сигнал об исходе.
//
// Показ обязан быть примонтирован в КАЖДОМ модуле, который мутирует. Держится
// пробой `Toaster.mounted.test.ts` рядом с этим файлом.
//
// ФОРМА. Целевое оформление даёт уведомлению одну строку высотой 42, радиус 9,
// усиленную линию и точку 7×7 слева в тоне исхода. Тон несёт ТОЧКА, а не
// полоса-обрезок и не крупная иконка: уведомление всплывает над страницей, где
// насыщенного цвета почти нет, и семи пикселей хватает, чтобы исход читался с
// края экрана. Тень здесь остаётся — это одно из немногих мест, которое
// действительно всплывает над страницей, а не является ею.

import type { CSSProperties } from "react";
import { Loader2, X } from "lucide-react";
import { useToasts, toast as toastApi } from "@shared/lib/toast";

const VARIANT_ACCENT = {
  success: "var(--toast-success-accent)",
  error: "var(--toast-error-accent)",
  info: "var(--toast-info-accent)",
  loading: "var(--toast-loading-accent)",
} as const;

const shell: CSSProperties = {
  pointerEvents: "auto",
  display: "flex",
  alignItems: "center",
  gap: 9,
  minHeight: 42,
  padding: "9px 14px",
  borderRadius: 9,
  border: "1px solid var(--toast-border)",
  background: "var(--toast-bg)",
  color: "var(--toast-fg)",
  boxShadow: "var(--kc-shadow-md)",
  fontSize: 11,
};

export function Toaster() {
  const items = useToasts();
  if (items.length === 0) return null;

  return (
    <div className="fixed bottom-[22px] right-[22px] z-[100] flex flex-col gap-2 max-w-md pointer-events-none">
      {items.map((t) => {
        const accent = VARIANT_ACCENT[t.variant];
        return (
          <div
            key={t.id}
            className="animate-in slide-in-from-right-4 fade-in-0"
            style={shell}
            role={t.variant === "error" ? "alert" : "status"}
          >
            {t.variant === "loading" ? (
              // У ожидания тона нет: оно длится. Статичная точка утверждала бы,
              // что операция уже чем-то кончилась, поэтому здесь остаётся
              // вращающийся глиф — единственное исключение из формы «точка».
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" style={{ color: accent }} aria-hidden />
            ) : (
              <span
                aria-hidden
                style={{ width: 7, height: 7, borderRadius: "50%", background: accent, flex: "0 0 auto" }}
              />
            )}
            <div style={{ flex: 1, minWidth: 0, lineHeight: 1.45 }}>{t.message}</div>
            <button
              onClick={() => toastApi.dismiss(t.id)}
              className="shrink-0 opacity-60 hover:opacity-100 transition-opacity"
              style={{ color: "var(--toast-fg)" }}
              aria-label="Закрыть"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
