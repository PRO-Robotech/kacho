// iam-target-types — read-парсеры формы AccessBinding.
//
// Thin binding несёт `subjects[]` + `scope_ref` (селекция объектов целиком в
// role.rules). Здесь — устойчивые read-парсеры биндинга: scope (canonical
// scope_ref + legacy fallback), subjects[] (canonical subjects[] + legacy
// single-subject fallback) и признак защиты от удаления. Аргумент — весь
// binding-объект (wire-shape после camelToSnake, ключи snake_case).

/** Один грантополучатель thin-биндинга. */
export interface ParsedSubject {
  type: string;
  id: string;
}

/**
 * Разбирает субъектов биндинга в нормализованный subjects[]. Предпочитает
 * canonical `subjects[]` (полный набор, 1..32); fallback на legacy single
 * `subject_type`/`subject_id` (= subjects[0]). Не падает на отсутствии — пустой
 * массив. enum SubjectType в subjects[].type нормализуется к нижнему регистру и
 * srvc-форме (service_account).
 */
export function parseSubjects(binding: unknown): ParsedSubject[] {
  if (!binding || typeof binding !== "object") return [];
  const b = binding as {
    subjects?: Array<{ type?: unknown; id?: unknown }>;
    subject_type?: unknown;
    subject_id?: unknown;
  };
  const norm = (t: string): string => {
    const lc = t.trim().toLowerCase();
    // enum-имена (SUBJECT_TYPE_USER / SERVICE_ACCOUNT / GROUP) → канон строки.
    if (lc.includes("service_account") || lc.includes("service account")) return "service_account";
    if (lc.includes("group")) return "group";
    if (lc.includes("user")) return "user";
    return lc;
  };
  if (Array.isArray(b.subjects) && b.subjects.length > 0) {
    return b.subjects
      .filter((s) => s && typeof s === "object")
      .map((s) => ({ type: norm(String(s.type ?? "")), id: String(s.id ?? "") }))
      .filter((s) => s.id);
  }
  // legacy single fallback.
  const t = String(b.subject_type ?? "");
  const id = String(b.subject_id ?? "");
  if (id) return [{ type: norm(t), id }];
  return [];
}

/** Разобранный scope-anchor binding'а: `tier` (CLUSTER/ACCOUNT/PROJECT) + anchor `id`. */
export interface ParsedScope {
  tier: string;
  id: string;
}

/** Scope-tier, derive'нутый из legacy `resource_type` (fallback). */
function tierFromResourceType(resourceType: string): string {
  switch (resourceType) {
    case "cluster":
      return "CLUSTER";
    case "account":
      return "ACCOUNT";
    case "project":
      return "PROJECT";
    default:
      return "";
  }
}

/**
 * Разбирает scope binding'а в канонический `{tier, id}`. Предпочитает canonical
 * `scope_ref` ({tier, id}), fallback на legacy тройку `scope`/`resource_type`/
 * `resource_id`. Не падает на отсутствии — пустой `{tier:"", id:""}`.
 */
export function parseScope(binding: unknown): ParsedScope {
  if (!binding || typeof binding !== "object") return { tier: "", id: "" };
  const b = binding as {
    scope_ref?: { tier?: unknown; id?: unknown };
    scope?: unknown;
    resource_type?: unknown;
    resource_id?: unknown;
  };
  // canonical scope_ref имеет приоритет.
  if (b.scope_ref && typeof b.scope_ref === "object") {
    const tier = String(b.scope_ref.tier ?? "");
    const id = String(b.scope_ref.id ?? "");
    if (tier || id) return { tier, id };
  }
  // legacy fallback: tier из enum `scope`, иначе derive из resource_type.
  const resourceType = String(b.resource_type ?? "");
  const scope = String(b.scope ?? "");
  const tier = scope && scope !== "SCOPE_UNSPECIFIED" ? scope : tierFromResourceType(resourceType);
  const id = String(b.resource_id ?? "");
  return { tier, id };
}

/**
 * Защищён ли binding от удаления. owner-auto-binding (Account.Create) несёт
 * `deletion_protection=true`; такой binding нельзя удалить из UI, пока защита не
 * снята (Update). Не падает на отсутствии поля (undefined → false).
 */
export function isProtectedBinding(binding: unknown): boolean {
  if (!binding || typeof binding !== "object") return false;
  return (binding as { deletion_protection?: unknown }).deletion_protection === true;
}
