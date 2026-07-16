// Recursive camelCase ↔ snake_case key transformer.
//
// Backend (api-gateway → grpc-gateway) даёт verbatim YC JSON в camelCase.
// UI код (registry, types, forms, columns) работает в snake_case (proto-style).
// api/client.ts применяет:
//   - request: snake_case → camelCase (UI → wire)
//   - response: camelCase → snake_case (wire → UI)
//
// Opaque-keys: ключи user-defined map'ов (labels, annotations) НЕ трансформируем —
// "team_lead" должен остаться "team_lead", а не превратиться в "teamLead".

const OPAQUE_FIELDS = new Set(["labels", "annotations"]);

function toCamel(s: string): string {
  // foo_bar → fooBar; не трогаем строки начинающиеся с "@" (Any-tag) или
  // strings без подчёркиваний.
  if (!s.includes("_")) return s;
  return s.replace(/_([a-z])/g, (_, c) => c.toUpperCase());
}

function toSnake(s: string): string {
  // fooBar → foo_bar; не трогаем "@type" и similar special.
  if (s.startsWith("@")) return s;
  return s.replace(/([A-Z])/g, "_$1").toLowerCase();
}

type Transform = (s: string) => string;

function deep(obj: unknown, fn: Transform, opaque = false): unknown {
  if (Array.isArray(obj)) {
    return obj.map((it) => deep(it, fn, opaque));
  }
  if (obj && typeof obj === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      const newKey = opaque ? k : fn(k);
      // Если ключ — opaque-field, не трансформируем дочерние keys.
      const childOpaque = opaque || OPAQUE_FIELDS.has(k);
      out[newKey] = deep(v, fn, childOpaque);
    }
    return out;
  }
  return obj;
}

/** Конвертирует все keys из snake_case → camelCase (для request body). */
export function snakeToCamel<T = unknown>(obj: unknown): T {
  return deep(obj, toCamel) as T;
}

/** Конвертирует все keys из camelCase → snake_case (для response). */
export function camelToSnake<T = unknown>(obj: unknown): T {
  return deep(obj, toSnake) as T;
}
