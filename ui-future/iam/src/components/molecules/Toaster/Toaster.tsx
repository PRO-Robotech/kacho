// Toaster — всплывающие уведомления. KAC-246: theme-aware через CSS-vars
// (--toast-bg/-border/-fg + per-variant accent). В dark — тёмный фон + accent-
// текст иконки/полосы; в light — светлый фон. Аккуратная тень (--kc-shadow-md).
// Раньше палитра была хардкод-светлая (bg-emerald-50) — в dark выглядела инородно.

import type { CSSProperties } from "react";
import { CheckCircle2, XCircle, Info, Loader2, X } from "lucide-react";
import { useToasts, toast as toastApi } from "@shared/lib/toast";
import { cn } from "@shared/lib/utils";

const VARIANT_ICONS = {
  success: CheckCircle2,
  error: XCircle,
  info: Info,
  loading: Loader2,
} as const;

const VARIANT_ACCENT: Record<keyof typeof VARIANT_ICONS, string> = {
  success: "var(--toast-success-accent)",
  error: "var(--toast-error-accent)",
  info: "var(--toast-info-accent)",
  loading: "var(--toast-loading-accent)",
};

export function Toaster() {
  const items = useToasts();
  if (items.length === 0) return null;

  return (
    <div className="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 max-w-md pointer-events-none">
      {items.map((t) => {
        const Icon = VARIANT_ICONS[t.variant];
        const accent = VARIANT_ACCENT[t.variant];
        const containerStyle: CSSProperties = {
          background: "var(--toast-bg)",
          color: "var(--toast-fg)",
          borderColor: "var(--toast-border)",
          boxShadow: "var(--kc-shadow-md)",
          // Тонкая accent-полоса слева по тону уведомления.
          borderLeft: `3px solid ${accent}`,
        };
        return (
          <div
            key={t.id}
            className="pointer-events-auto flex items-start gap-2 rounded-lg border px-3 py-2.5 animate-in slide-in-from-right-4 fade-in-0"
            style={containerStyle}
            role={t.variant === "error" ? "alert" : "status"}
          >
            <Icon
              className={cn("h-4 w-4 shrink-0 mt-0.5", t.variant === "loading" && "animate-spin")}
              style={{ color: accent }}
            />
            <div className="text-sm flex-1 leading-snug">{t.message}</div>
            <button
              onClick={() => toastApi.dismiss(t.id)}
              className="opacity-60 hover:opacity-100 shrink-0"
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
