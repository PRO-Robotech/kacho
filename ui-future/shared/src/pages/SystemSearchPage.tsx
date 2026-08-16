// SystemSearchPage — поиск по всем ресурсам сразу, по имени и идентификатору.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕТЫРЕ ВЕЩИ, КОТОРЫЕ ЗДЕСЬ СДЕЛАНЫ ИНАЧЕ, И ПОЧЕМУ (#373, #465)
//
// 1. НИЧЕГО НЕ СПРАШИВАЕТСЯ, ПОКА НЕ НАБРАН ЗАПРОС. Прежде страница на открытии
//    тянула девять списков по 500 строк — безусловно, ещё до того как читатель
//    что-нибудь набрал. Эту стоимость платил каждый заглянувший, и платил зря.
//
// 2. ГДЕ ВЛАДЕЛЕЦ УМЕЕТ — СПРАШИВАЕТСЯ СЕРВЕР. Совпадение искалось подстрокой в
//    браузере поверх первых 500 строк, поэтому за этим пределом ресурс был
//    невидим НАВСЕГДА, а «ничего не найдено» выглядело так же уверенно, как
//    настоящий ответ. Право спросить сервер берётся из спеки ресурса
//    (`serverSearchField`) — одного места, где оно объявлено и проверено против
//    дерева владельца (`lib/list-server-search-parity.test.ts`).
//
//    Отправлять выражение всем нельзя: владелец, который его не разбирает,
//    молча игнорирует незнакомое, и список вернулся бы ПОЛНЫМ под видом
//    отфильтрованного — строго хуже прежнего среза.
//
// 3. НЕПОЛНОТА НАЗЫВАЕТСЯ. Области, где поиск остаётся клиентским, перечислены
//    на странице поимённо. Молчание сделало бы неполный ответ неотличимым от
//    полного.
//
// 4. PROJECT-SCOPED ОБЛАСТЬ СПРАШИВАЕТСЯ ТОЛЬКО С `project_id`, И ТОЛЬКО КОГДА
//    ОН ЕСТЬ. Прежде запрос уходил без него всегда — край резолвит отсутствие
//    в `project:*` и отвечает отказом в правах (#465), а не пустым списком:
//    поиск по сети/подсети/машине падал целиком отказом сервера. Область
//    видимости берётся из той же спеки реестра (`scope`), что и у
//    `RefNameLink`. Без выбранного проекта такая область не спрашивается
//    вовсе (не отказ — отсутствие вопроса) и названа поимённо тем же приёмом,
//    что и клиентская неполнота в пункте 3: молчаливое «Найдено: 0» здесь
//    означало бы «ресурса нет», а не «эти области не смотрели».
//
// Плюс машины и тома, которых в области поиска не было вовсе: без них самый
// частый запрос администратора («найди эту машину») не решался в принципе.

import { useState, useMemo, useEffect } from "react";
import { Link } from "react-router";
import { useQueries } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api } from "@shared/api/client";
import { REGISTRY } from "@shared/lib/resource-registry";
import { searchFilterExpression } from "@shared/lib/list-search-filter";
import { displayText } from "@shared/lib/display-text";
import { useProjectStore } from "@shared/lib/context-store";

/** Значение, отставшее от ввода на `ms` спокойных миллисекунд. */
function useDebounced(value: string, ms: number): string {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    if (value === settled) return;
    const t = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, ms]);
  return settled;
}

interface Hit {
  resource: string;
  id: string;
  name: string;
  project_id?: string;
  account_id?: string;
  link: string;
  extras?: Record<string, string>;
}

/**
 * Область поиска. `specId` — ключ РЕЕСТРА: адрес, ключ ответа и право спросить
 * сервер берутся оттуда, а не выписываются здесь вторым списком (расходятся
 * такие списки молча, и первым узнаёт об этом пользователь).
 */
interface SearchDomain {
  specId: string;
  resource: string;
  path: string;
  key: string;
  linkBase: string;
  /** Поле серверного поиска либо `undefined` — владелец выражения не разбирает. */
  serverSearchField?: string;
  /** Область видимости — из спеки реестра, не второй список: `project` требует
   *  `project_id` в запросе, `global`/`account` его не несут вовсе (край
   *  резолвит такой параметр в `project:*` и отвергает — инцидент #465). */
  scope: "global" | "project" | "account";
}

function domain(specId: string, linkBase: string, resource?: string): SearchDomain {
  const spec = REGISTRY[specId];
  if (!spec) throw new Error(`область поиска «${specId}» не найдена в реестре`);
  return {
    specId,
    resource: resource ?? specId,
    path: spec.apiPath,
    key: spec.payloadKey,
    linkBase,
    serverSearchField: spec.serverSearchField,
    scope: spec.scope,
  };
}

const PRJ = "/projects/:project_id";

export const SEARCH_DOMAINS: SearchDomain[] = [
  domain("accounts", "/iam/accounts/:id"),
  domain("projects", "/projects/:id"),
  domain("networks", `${PRJ}/vpc/networks/:id`),
  domain("subnets", `${PRJ}/vpc/subnets/:id`),
  domain("addresses", `${PRJ}/vpc/addresses/:id`),
  domain("security-groups", `${PRJ}/vpc/security-groups/:id`),
  domain("route-tables", `${PRJ}/vpc/route-tables/:id`),
  domain("gateways", `${PRJ}/vpc/gateways/:id`),
  domain("cidr-groups", `${PRJ}/vpc/cidr-groups/:id`),
  domain("network-interfaces", `${PRJ}/vpc/network-interfaces/:id`),
  domain("address-pools", "/system/address-pools/:id"),
  // Машины и тома — ради них поиск и переписан: без них «найди эту машину» не
  // решалось вовсе.
  domain("compute-instances", `${PRJ}/compute/instances/:id`, "instances"),
  domain("volumes", `${PRJ}/storage/volumes/:id`),
  // Каталог размещения принадлежит geo и всегда принадлежал ему.
  domain("regions", "/system/regions/:id"),
  domain("zones", "/system/zones/:id"),
];

/** Области, где совпадение ищется в браузере поверх прочитанной страницы. */
export const PARTIAL_DOMAINS = SEARCH_DOMAINS.filter((d) => !d.serverSearchField);

/** Области, которым для запроса нужен `project_id` выбранного проекта. */
export const PROJECT_SCOPED_DOMAINS = SEARCH_DOMAINS.filter((d) => d.scope === "project");

/**
 * Сколько строк просить у области, которая сервером не сужается.
 *
 * Это НЕ «достаточно»: это цена компромисса, названная числом. Полный ответ у
 * такой области получить нечем, пока её владелец не научится разбирать
 * выражение, — и страница об этом говорит вслух, а не делает вид.
 */
const PARTIAL_PAGE = "200";
/** Сужено сервером — страница нужна только чтобы ограничить ответ сверху. */
const SERVER_PAGE = "100";

export function SystemSearchPage() {
  const [q, setQ] = useState("");
  // Пауза перед запросом: без неё каждое нажатие клавиши — пятнадцать запросов.
  const term = useDebounced(q, 300);
  const active = term.trim();

  // Область видимости решает, чем спрашивать (тот же предикат, что у
  // `RefNameLink`): `project`-scoped ресурс без `project_id` в запросе край
  // резолвит в `project:*` и отвергает отказом в правах, а не пустым списком
  // — молчаливый провал в 403, а не в «ничего не найдено» (#465). Глобальный
  // каталог и account-scoped ресурс `project_id` не несут вовсе — для них он
  // чужой параметр.
  const project = useProjectStore((s) => s.project);
  const projectId = project?.id ?? null;

  const queries = useQueries({
    queries: SEARCH_DOMAINS.map((d) => {
      const expr = searchFilterExpression(d.serverSearchField, active);
      const needsProject = d.scope === "project";
      return {
        // Запрос — ЧАСТЬ ключа: без него ответ на «прод» переиспользовался бы
        // как ответ на «тест». `project_id` — тоже часть ключа там, где он
        // влияет на ответ: смена проекта не должна отдавать чужой кэш.
        queryKey: ["search", d.specId, expr ?? null, needsProject ? projectId : null],
        queryFn: () =>
          api.list<Record<string, unknown>>(d.path, {
            pageSize: expr ? SERVER_PAGE : PARTIAL_PAGE,
            ...(expr ? { filter: expr } : {}),
            ...(needsProject ? { project_id: projectId! } : {}),
          }),
        // Ничего не спрашиваем, пока не набран запрос, и project-scoped
        // область не спрашиваем, пока нет выбранного проекта — иначе запрос
        // уходит без `project_id` и край отвечает отказом, а не пустым ответом.
        enabled: active.length > 0 && (!needsProject || !!projectId),
        staleTime: 10_000,
      };
    }),
  });

  const hits: Hit[] = useMemo(() => {
    const term = active.toLowerCase();
    if (!term) return [];

    const out: Hit[] = [];
    queries.forEach((qry, i) => {
      const d = SEARCH_DOMAINS[i];
      if (!qry.data) return;
      const list = (qry.data[d.key] as Record<string, unknown>[] | undefined) ?? [];
      list.forEach((r) => {
        const id = displayText(r.id);
        const name = displayText(r.name);
        // Строку, которую сузил СЕРВЕР, клиент не пересевает: он судил бы своим
        // правилом сравнения о том, что владелец уже признал подходящим, и
        // отбрасывал бы часть ответа — тот же дефект этажом выше.
        if (!d.serverSearchField) {
          const matchesId = id.toLowerCase().includes(term);
          const matchesName = name.toLowerCase().includes(term);
          if (!matchesId && !matchesName) return;
        }
        out.push({
          resource: d.resource,
          id,
          name,
          project_id: r.project_id as string | undefined,
          account_id: r.account_id as string | undefined,
          link: d.linkBase.replace(":project_id", displayText(r.project_id)).replace(":id", id),
          extras: extractExtras(d.resource, r),
        });
      });
    });
    return out.slice(0, 100);
  }, [active, queries]);

  const loading = queries.some((q) => q.isLoading);

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-semibold">Поиск</h1>
        <p className="text-sm text-muted-foreground">
          Поиск по всем ресурсам сразу — по имени и идентификатору.
        </p>
      </div>

      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Поиск: имя или идентификатор ресурса"
          className="w-full pl-10 pr-4 py-2 bg-secondary border border-border rounded text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      {loading && <div className="text-xs text-muted-foreground">Ищем…</div>}
      <div className="text-xs text-muted-foreground">
        {active ? `Найдено: ${hits.length}` : "Введите имя или идентификатор"}
      </div>

      {/* Без выбранного проекта project-scoped области вообще не спрашиваются
          (иначе запрос уходит без `project_id` и край отвечает отказом, а не
          пустым ответом) — и «Найдено: 0» без этой строки читалось бы как факт
          об отсутствии ресурса, а не как «эти области не смотрели вовсе». */}
      {active && !projectId && PROJECT_SCOPED_DOMAINS.length > 0 && (
        <div className="text-xs text-muted-foreground">
          Выберите проект — без него не смотрены {PROJECT_SCOPED_DOMAINS.length} из {SEARCH_DOMAINS.length}{" "}
          областей:{" "}
          {PROJECT_SCOPED_DOMAINS.map((d) => REGISTRY[d.specId].plural).join(", ")}.
        </div>
      )}

      {/* Неполноту нельзя выдавать за полноту: области, где совпадение ищется в
          браузере поверх прочитанной страницы, названы поимённо. Иначе «ничего
          не найдено» читалось бы как утверждение об отсутствии ресурса. */}
      {active && PARTIAL_DOMAINS.length > 0 && (
        <div className="text-xs text-muted-foreground">
          Поиск неполон в {PARTIAL_DOMAINS.length} из {SEARCH_DOMAINS.length} областей — там сравниваются только
          загруженные строки (по {PARTIAL_PAGE}):{" "}
          {PARTIAL_DOMAINS.map((d) => REGISTRY[d.specId].plural).join(", ")}.
        </div>
      )}

      {hits.length > 0 && (
        <div className="border border-border rounded overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-xs uppercase tracking-wide">
              <tr>
                <th className="text-left px-3 py-2">Ресурс</th>
                <th className="text-left px-3 py-2">Имя / идентификатор</th>
                <th className="text-left px-3 py-2">Проект / аккаунт</th>
                <th className="text-left px-3 py-2">Дополнительно</th>
              </tr>
            </thead>
            <tbody>
              {hits.map((h) => (
                <tr key={`${h.resource}-${h.id}`} className="border-t border-border hover:bg-muted/20">
                  <td className="px-3 py-2 text-xs uppercase">{h.resource}</td>
                  <td className="px-3 py-2">
                    <Link to={h.link} className="text-blue-400 hover:underline font-medium">
                      {h.name || "(unnamed)"}
                    </Link>
                    <div className="text-[10px] font-mono text-muted-foreground">{h.id}</div>
                  </td>
                  <td className="px-3 py-2 text-xs font-mono">
                    {h.project_id && <div>P: {h.project_id.slice(0, 12)}…</div>}
                    {h.account_id && <div>A: {h.account_id.slice(0, 12)}…</div>}
                    {!h.project_id && !h.account_id && <span className="text-muted-foreground">—</span>}
                  </td>
                  <td className="px-3 py-2 text-xs">
                    {Object.entries(h.extras ?? {}).map(([k, v]) => (
                      <div key={k}>
                        <span className="text-muted-foreground">{k}:</span> <span className="font-mono">{v}</span>
                      </div>
                    ))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

/** Экспортируется ради пробы: приписка читает имена полей ответа напрямую, и
 *  снятое имя молча пустеет — проверять это можно только вызовом. */
export function extractExtras(resource: string, r: Record<string, unknown>): Record<string, string> {
  const e: Record<string, string> = {};
  switch (resource) {
    case "addresses": {
      const ext = r.external_ipv4_address as Record<string, unknown> | undefined;
      const intn = r.internal_ipv4_address as Record<string, unknown> | undefined;
      if (ext?.address) e.ext = displayText(ext.address);
      if (intn?.address) e.int = displayText(intn.address);
      if (r.type) e.type = displayText(r.type);
      break;
    }
    case "subnets": {
      // VPC-1: placement anchor is zone_id (ZONAL) or region_id (REGIONAL);
      // CIDR is ipv4_cidr_primary + additional ranges (v4_cidr_blocks retired).
      if (r.zone_id) e.zone = displayText(r.zone_id);
      else if (r.region_id) e.region = displayText(r.region_id);
      const cidrs = [
        typeof r.ipv4_cidr_primary === "string" ? r.ipv4_cidr_primary : "",
        typeof r.ipv6_cidr_primary === "string" ? r.ipv6_cidr_primary : "",
        ...(Array.isArray(r.ipv4_cidr_blocks) ? (r.ipv4_cidr_blocks as string[]) : []),
        ...(Array.isArray(r.ipv6_cidr_blocks) ? (r.ipv6_cidr_blocks as string[]) : []),
      ].filter(Boolean);
      if (cidrs.length > 0) e.cidrs = cidrs.join(",");
      break;
    }
    case "address-pools": {
      if (r.zone_id) e.zone = displayText(r.zone_id);
      if (r.kind) e.kind = displayText(r.kind);
      // Слитное `cidr_blocks` (тег 7) у AddressPool зарезервировано и расщеплено
      // на v4 (13) + v6 (14). Чтение снятого имени возвращало undefined молча —
      // строка находилась, приписка диапазонов не появлялась никогда. Второй
      // экземпляр того же класса, что чинился в подписи RefSelect.
      const cidrs = [
        ...(Array.isArray(r.v4_cidr_blocks) ? (r.v4_cidr_blocks as string[]) : []),
        ...(Array.isArray(r.v6_cidr_blocks) ? (r.v6_cidr_blocks as string[]) : []),
      ].filter(Boolean);
      if (cidrs.length > 0) e.cidrs = cidrs.join(",");
      break;
    }
    case "zones":
      if (r.region_id) e.region = displayText(r.region_id);
      break;
  }
  return e;
}
