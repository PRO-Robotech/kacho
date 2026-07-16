// Единый формат даты-времени в UI: «04.06.2026, в 04:32» (ru-RU, без секунд).
// Использовать ВЕЗДЕ, где показывается дата/время (таблицы, обзор, операции,
// auth) — вместо разнобойного new Date(...).toLocaleString().
export function formatDateTime(iso?: string | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const date = d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric" });
  const time = d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
  return `${date}, в ${time}`;
}
