// usePermissionCatalog — react-query hook + pure derivation helpers поверх
// backend-driven permission-каталога (RBAC rules-model).
//
// Источник опций module/resource/verb + wildcard-политики — живой RPC
// GET /iam/v1/permissionCatalog, а не bundled-константа. Каталог платформенно-
// статичен (immutable-в-рантайме) → staleTime щедрый, ретраи не нужны; loading/
// error/empty состояния обрабатываются в RulesEditor.

import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { iamApi, type CatalogResource, type PermissionCatalog, type WildcardPolicy } from "./iam";

/** Wildcard-сегмент. verb-`*` разрешён в custom; module/resource-`*` — system-only
 *  (политика приходит из каталога — wildcard_policy). */
export const WILDCARD = "*";

/** react-query ключ каталога — стабильный (одна платформенная таксономия). */
export const PERMISSION_CATALOG_QUERY_KEY = ["iam", "permissionCatalog"] as const;

/**
 * Грузит permission-каталог из живого RPC. Каталог платформенно-статичен —
 * staleTime/gcTime щедрые (час), без поллинга. Один кэш на всё приложение.
 */
export function usePermissionCatalog(): UseQueryResult<PermissionCatalog> {
  return useQuery({
    queryKey: PERMISSION_CATALOG_QUERY_KEY,
    queryFn: () => iamApi.fetchPermissionCatalog(),
    staleTime: 60 * 60_000, // 1 час — каталог immutable-в-рантайме
    gcTime: 60 * 60_000,
  });
}

// ── Pure-хелперы поверх данных каталога (без bundled-константы) ───────────────

/** Упорядоченный список module-токенов из каталога. */
export function catalogModules(catalog: PermissionCatalog | undefined): string[] {
  return (catalog?.modules ?? []).map((m) => m.module);
}

/** True если module-токен присутствует в каталоге (grantable). */
export function isKnownModule(catalog: PermissionCatalog | undefined, module: string): boolean {
  return (catalog?.modules ?? []).some((m) => m.module === module);
}

/**
 * Объединённый dedup'ленный список resource-токенов для выбранных модулей
 * (cascade module → resources). Пустой `modules` → []. Неизвестный модуль
 * игнорируется (fail-safe на legacy-токене из существующего rule).
 */
export function resourcesForModules(catalog: PermissionCatalog | undefined, modules: string[]): string[] {
  const byModule = new Map((catalog?.modules ?? []).map((m) => [m.module, m.resources ?? []]));
  const seen = new Set<string>();
  const out: string[] = [];
  for (const m of modules) {
    for (const r of byModule.get(m) ?? []) {
      if (!seen.has(r.resource)) {
        seen.add(r.resource);
        out.push(r.resource);
      }
    }
  }
  return out;
}

/**
 * Ресурсы ОДНОГО модуля (scalar). Пустой / неизвестный module → []. Порядок — как
 * в каталоге (правило несёт один модуль, cascade `module → resources` по скаляру).
 */
export function resourcesForModule(catalog: PermissionCatalog | undefined, module: string): string[] {
  if (!module) return [];
  return (catalog?.modules ?? []).find((m) => m.module === module)?.resources?.map((r) => r.resource) ?? [];
}

/**
 * Тип label-selectable (есть resource-feed для match_labels). undefined /
 * неизвестный (module,resource) → false (fail-safe: не предлагаем и блокируем
 * match_labels на типе, чей selectability неизвестен). Используется RulesEditor
 * для labels-арм gating (фильтр опций + submit-блок).
 */
export function isLabelSelectable(catalog: PermissionCatalog | undefined, module: string, resource: string): boolean {
  return catalogResource(catalog, module, resource)?.label_selectable === true;
}

/**
 * Опции глаголов для verb-dropdown — набор ВЫБРАННЫХ РЕСУРСОВ, а не общий
 * словарь каталога (#1128).
 *
 * ПОЧЕМУ НЕ `closed_verbs`. Оно есть ПЕРЕСЕЧЕНИЕ наборов всех типов. Пока набор
 * глаголов был платформенной константой, пересечение равнялось ему и различие не
 * наблюдалось; с набором, ставшим атрибутом ТИПА, оно наблюдается в обе стороны:
 * глагол, который объявляет один тип, в пересечение не попадает и не
 * предлагается ему самому (`addTargets`/`removeTargets` у групп целей, `create` у
 * реестров), а снятие глагола у одного типа вынимает его из списка у всех
 * остальных.
 *
 * ПОЧЕМУ ПЕРЕСЕЧЕНИЕ ВЫБРАННЫХ, а не объединение. Правило несёт один модуль и
 * НЕСКОЛЬКО типов. Глагол, которого нет у одного из названных типов, на нём не
 * материализуется — материализация сверяется с набором типа и молча его
 * отбрасывает. Предложить такой глагол значило бы обещать право, которого правило
 * не даст.
 *
 * ПОРЯДОК берётся у края и не пересортировывается: он канонический и есть часть
 * контракта поля.
 *
 * СОВМЕСТИМОСТЬ СО СТАРЫМ КРАЕМ названа, а не подразумевается: proto3 опускает
 * пустой repeated, поэтому «набора нет» и «набор пуст» на проводе неразличимы.
 * Различает их `has_verb_relations` — глагольный ресурс без набора означает
 * старый край, и тогда для него берётся общий словарь. Пустой список здесь
 * означал бы «правило нельзя дописать», то есть поломку редактора.
 *
 * verb-`*` добавляется, если политика каталога разрешает его в custom; в
 * system-роли он доступен всегда (seed-path).
 */
export function verbOptions(
  catalog: PermissionCatalog | undefined,
  isSystem: boolean,
  module: string,
  resources: string[],
): string[] {
  const common = catalog?.closed_verbs ?? [];
  const verbs = [...verbsForSelection(catalog, module, resources, common)];
  const verbWildcardAllowed = isSystem || !!catalog?.wildcard_policy?.verb_wildcard_allowed_custom;
  if (verbWildcardAllowed) verbs.push(WILDCARD);
  return verbs;
}

/** Набор глаголов выбранной пары (module, resources) — см. verbOptions. */
function verbsForSelection(
  catalog: PermissionCatalog | undefined,
  module: string,
  resources: string[],
  common: string[],
): string[] {
  if (!catalog) return [];
  const entries = (catalog.modules ?? []).find((m) => m.module === module)?.resources ?? [];
  if (!module || module === WILDCARD || entries.length === 0) return common;

  // Названный `*` и пустой выбор означают одно: тип ещё не назван, поэтому
  // предлагается то, что даёт ЛЮБОЙ ресурс этого модуля.
  const named = resources.filter((r) => r && r !== WILDCARD);
  const selected = named.length > 0 ? entries.filter((e) => named.includes(e.resource)) : entries;
  if (selected.length === 0) return common;

  const setsOf = (e: CatalogResource): string[] =>
    e.verbs && e.verbs.length > 0 ? e.verbs : e.has_verb_relations ? common : [];

  // Порядок — из первого набора; членство — во всех остальных.
  const [first, ...rest] = selected.map(setsOf);
  return first.filter((v) => rest.every((s) => s.includes(v)));
}

/**
 * Находит CatalogResource по (module, resource) — нужен для per-ресурсных флагов
 * (has_list_endpoint решает picker-vs-free-text для resource_names).
 */
export function catalogResource(
  catalog: PermissionCatalog | undefined,
  module: string,
  resource: string,
): CatalogResource | undefined {
  return (catalog?.modules ?? []).find((m) => m.module === module)?.resources?.find((r) => r.resource === resource);
}

/**
 * Wildcard-политика каталога с дефолтами на случай отсутствующего поля (старый/
 * частичный ответ): module/resource-`*` считаем system-only по дефолту (fail-safe
 * — не предлагаем `*` в custom, если каталог промолчал).
 */
export function wildcardPolicyOf(catalog: PermissionCatalog | undefined): Required<WildcardPolicy> {
  const wp = catalog?.wildcard_policy ?? {};
  return {
    verb_wildcard_allowed_custom: wp.verb_wildcard_allowed_custom ?? false,
    module_resource_wildcard_system_only: wp.module_resource_wildcard_system_only ?? true,
  };
}
