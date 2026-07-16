// formatBytes — размер в байтах в человекочитаемый вид (B/KB/MB/GB/TB).
// proto3 int64 приходит строкой, поэтому принимаем unknown. Пусто / 0 / не число
// → «—» (никогда «0 B»). Единый хелпер для колонки «Размер» и карточек тегов.
export function formatBytes(v: unknown): string {
  const n = typeof v === "string" ? Number.parseInt(v, 10) : typeof v === "number" ? v : Number.NaN;
  if (!Number.isFinite(n) || n <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let x = n;
  let i = 0;
  while (x >= 1024 && i < units.length - 1) {
    x /= 1024;
    i += 1;
  }
  return `${i ? x.toFixed(1) : x} ${units[i]}`;
}
