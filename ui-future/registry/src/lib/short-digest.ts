// shortDigest — hex-часть OCI-digest'а (после `sha256:`), обрезанная до DIGEST_LEN
// символов. Чистая функция (без UI-импортов) — используется панелью тегов для
// компактного показа digest'а; полное значение доступно по копированию.

export const DIGEST_LEN = 9;

export function shortDigest(value: unknown): string {
  const d = typeof value === "string" ? value : "";
  if (!d) return "";
  const hex = d.includes(":") ? d.slice(d.indexOf(":") + 1) : d;
  return hex.slice(0, DIGEST_LEN);
}
