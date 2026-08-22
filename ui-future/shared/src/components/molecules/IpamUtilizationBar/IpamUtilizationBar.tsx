// IpamUtilizationBar — NetBox-style визуализация утилизации IP-блока.
//
// Используется на:
//   - AddressPoolDetailPage  (admin: utilization pool + per-CIDR breakdown)
//   - SubnetDetailPage       (admin: utilization subnet)
//
// Цвет заполнения — тон СОСТОЯНИЯ, а не отдельная палитра полосы. Прежде здесь
// стояли имена палитры Tailwind (`bg-red-500`, `bg-emerald-500`), и они не
// участвовали ни в одной теме продукта: полоса светилась насыщенным зелёным на
// светлом фоне и им же на тёмном, тогда как остальная консоль в обеих темах
// меняет тон. Теперь берутся те же `--status-*`, которыми окрашен значок
// состояния, — «пул кончается» и «ресурс в ошибке» читаются одним цветом.

import type { CSSProperties } from "react";

/**
 * Порог заполнения → тон состояния.
 *
 * Пороги 30/70/90 — контракт этой полосы: по ним администратор узнаёт, что пора
 * расширять пул. Меняя их, меняешь смысл, а не оформление.
 */
function toneOfPercent(pct: number): string {
  if (pct >= 90) return "var(--status-error-fg)";
  if (pct >= 70) return "var(--status-warn-fg)";
  if (pct >= 30) return "var(--status-info-fg)";
  return "var(--status-ok-fg)";
}

/**
 * Дорожка полосы: заливка поля и линия — те же, что у всякой рамки продукта.
 *
 * Ширина сюда НЕ пишется, она задаётся классом: полосой заполнения проба
 * считает первый узел, у которого ширина стоит в инлайн-стиле, — и дорожка со
 * своими «100%» отобрала бы у неё это имя.
 */
const track: CSSProperties = {
  position: "relative",
  height: 24,
  overflow: "hidden",
  borderRadius: 6,
  border: "1px solid var(--kc-border)",
  background: "var(--kc-field)",
};

interface Props {
  total: number | string;
  used: number | string;
  free?: number | string;
  percent?: number;
  label?: string;
  className?: string;
}

export function IpamUtilizationBar({ total, used, free, percent, label, className }: Props) {
  const totalN = Number(total);
  const usedN = Number(used);
  const freeN = free !== undefined ? Number(free) : Math.max(0, totalN - usedN);
  const pct =
    percent !== undefined ? Math.max(0, Math.min(100, percent)) : totalN > 0 ? Math.floor((usedN * 100) / totalN) : 0;

  return (
    <div className={className}>
      {label && <div className="t-label text-muted-foreground mb-1.5">{label}</div>}
      <div className="w-full" style={track}>
        {/* Заполнение приглушено до .55: поверх него читается число, и полная
            насыщенность съедала бы его — а число здесь и есть содержание.
            Свечения (тени, размытия) у полосы нет и не заводится: цвет здесь
            уже несёт порог, а второй сигнал поверх первого его обесценивает.

            Движется ТОЛЬКО ширина, и токенами продукта (160 мс, общая кривая).
            Прежде стоял набор `transition-all`: он анимирует и цвет тоже, из-за
            чего при переходе через порог полоса перекрашивалась постепенно —
            и мгновение показывала оттенок, которого ни один порог не означает. */}
        <div
          className="absolute left-0 top-0 h-full"
          style={{
            width: `${pct}%`,
            background: toneOfPercent(pct),
            opacity: 0.55,
            transition: "width var(--kc-duration) var(--kc-ease)",
          }}
        />
        <div className="absolute inset-0 flex items-center justify-center t-mono">
          {usedN.toLocaleString()} / {totalN.toLocaleString()} ({pct}%)
        </div>
      </div>
      <div className="flex gap-4 t-small text-muted-foreground mt-1.5">
        <span>
          занято: <span className="t-mono text-foreground">{usedN}</span>
        </span>
        <span>
          свободно: <span className="t-mono text-foreground">{freeN}</span>
        </span>
        <span>
          всего: <span className="t-mono text-foreground">{totalN}</span>
        </span>
      </div>
    </div>
  );
}

// CIDRBreakdown — компактная таблица per-CIDR usage.
interface CIDRRow {
  cidr: string;
  total: number | string;
  used: number | string;
}

export function CIDRBreakdown({ cidrs }: { cidrs: CIDRRow[] }) {
  if (!cidrs || cidrs.length === 0) return null;
  return (
    <div style={{ border: "1px solid var(--kc-border)", borderRadius: 8, overflow: "hidden" }}>
      <table className="w-full">
        <thead>
          <tr>
            <th className="text-left px-3 py-2 t-label text-muted-foreground">CIDR</th>
            <th className="text-right px-3 py-2 t-label text-muted-foreground">занято / всего</th>
            <th className="px-3 py-2 w-1/2 t-label text-muted-foreground">заполненность</th>
          </tr>
        </thead>
        <tbody>
          {cidrs.map((c) => {
            const totalN = Number(c.total);
            const usedN = Number(c.used);
            const pct = totalN > 0 ? Math.floor((usedN * 100) / totalN) : 0;
            return (
              <tr key={c.cidr} style={{ borderTop: "1px solid var(--kc-border)" }}>
                <td className="px-3 py-2 t-mono">{c.cidr}</td>
                <td className="px-3 py-2 text-right t-mono">
                  {usedN}/{totalN}
                </td>
                <td className="px-3 py-2">
                  <div
                    className="relative h-2 w-full overflow-hidden"
                    style={{ borderRadius: 5, background: "var(--kc-field)" }}
                  >
                    <div
                      className="absolute left-0 top-0 h-full"
                      style={{ width: `${pct}%`, background: toneOfPercent(pct), opacity: 0.55 }}
                    />
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
