// Приложение не может дозвониться до домена, которого нет в его dev-прокси.
//
// В dev браузер стучится по относительному пути (`/geo/v1/zones`), и до
// api-gateway его доводит `server.proxy` этого приложения. Домен, которого в
// прокси нет, отдаёт index.html вместо JSON: запрос формально успешен, а список
// пуст — отказ выглядит как «ресурсов нет».
//
// Требуемый набор ВЫВОДИТСЯ из реестра ресурсов, а не выписывается: реестр —
// единственное место, где живут apiPath спек, и он же переезжает вслед за proto
// ствола. Выписанный список разошёлся бы с ним молча.
//
// Выводится он из того, что монтирует ЭТОТ модуль (`MODULE_SPEC_IDS`), плюс цели
// ссылок его полей и колонок, — а НЕ из всего реестра. Разница появилась вместе
// с общим реестром (#409): прежде реестр был доменным, и «все спеки» совпадало
// с «спеки этого приложения» by construction. После переезда в реестре лежит вся
// платформа, и обход по нему потребовал бы правил прокси для /compute, /nlb,
// /storage и /vpc — доменов, к которым это приложение не обращается ни одним
// запросом.
//
// То есть предпосылка гейта была верна ОТНОСИТЕЛЬНО ПОПУЛЯЦИИ, на которой он
// писался, и молчала об этом. Расширился охват — перепроверяется предпосылка, а
// не только её новые элементы. Тот же порядок и та же причина, что у storage
// (#1466).
//
// Замыкание по ссылкам — ровно ОДИН шаг, и это не экономия: `RefSelect` и
// `RefNameLink` разрешают ссылку запросом к домену цели, а цель цели запросом
// уже не разрешают. Больший шаг вернул бы обход всей платформы через первую же
// спеку, которая ссылается на соседний домен.
//
// Правила прокси берутся ЗАГРУЗКОЙ `vite.config.ts` и чтением `server.proxy`
// как значения, а не разбором его текста: текстовый разбор утверждает о
// символах файла и переживает любую смену формы записи (сокращённая запись,
// вынесенная константа, вычисляемый ключ) — то есть перестаёт что-либо мерить,
// не покраснев.

import { jest } from "@jest/globals";
import { REGISTRY } from "./lib/resource-registry";
import { MODULE_SPEC_IDS } from "./lib/module-specs";

/** Первый сегмент пути — домен, по которому выбирается правило прокси. */
function domainOf(apiPath: string): string | null {
  const m = /^\/([a-z][a-z0-9-]*)\//.exec(apiPath);
  return m ? `/${m[1]}` : null;
}

/** Спеки, к которым приложение обращается: смонтированные + цели их ссылок. */
function reachedSpecIds(): string[] {
  const out = new Set<string>(MODULE_SPEC_IDS);
  for (const id of MODULE_SPEC_IDS) {
    const spec = REGISTRY[id];
    if (!spec) continue;
    const refs = [...(spec.fields ?? []), ...(spec.columns ?? [])] as { refResource?: string }[];
    for (const r of refs) if (r.refResource) out.add(r.refResource);
  }
  return [...out];
}

function registryDomains(): string[] {
  const out = new Set<string>();
  for (const id of reachedSpecIds()) {
    const spec = REGISTRY[id];
    if (!spec) continue;
    for (const p of [spec.apiPath, spec.internalGetPath, spec.admin?.basePath]) {
      const d = p ? domainOf(p) : null;
      if (d) out.add(d);
    }
  }
  return [...out].sort();
}

/** Правило `/iam/v1` покрывает `/iam/...`: сравниваем по префиксу, не по равенству. */
function isProxied(domain: string, prefixes: string[]): boolean {
  return prefixes.some((p) => p === domain || p.startsWith(`${domain}/`));
}

let prefixes: string[] = [];

beforeAll(async () => {
  // `defineConfig` — тождество, а плагины к объявлению прокси отношения не
  // имеют: подменяем их, чтобы не тянуть в jsdom бандлер. Подменён ТРАНСПОРТ
  // загрузки, а не предмет проверки: `server.proxy` читается из настоящего
  // vite.config.ts.
  jest.unstable_mockModule("vite", () => ({ defineConfig: (c: unknown) => c }));
  jest.unstable_mockModule("@originjs/vite-plugin-federation", () => ({ default: () => ({ name: "federation" }) }));
  jest.unstable_mockModule("@vitejs/plugin-react", () => ({ default: () => ({ name: "react" }) }));
  // vite.config.ts написан как CJS-модуль (использует __dirname), а ts-jest
  // грузит его как ESM. Подставляем каталог приложения — тот же, что видит vite.
  (globalThis as unknown as { __dirname: string }).__dirname = new URL("..", import.meta.url).pathname;
  // Спецификатор — переменная намеренно: vite.config.ts принадлежит проекту
  // tsconfig.node.json, а этот тест компилируется в tsconfig.app.json.
  // Литеральный импорт затянул бы файл во второй проект (TS6307).
  const configModule = "../vite.config";
  const mod = (await import(configModule)) as unknown as {
    default: { server?: { proxy?: Record<string, unknown> } };
  };
  prefixes = Object.keys(mod.default.server?.proxy ?? {});
});

describe("dev-прокси покрывает домены, к которым приложение обращается", () => {
  it("предмет проверки непуст — реестр прочитан, правила прокси прочитаны", () => {
    expect(registryDomains().length).toBeGreaterThan(1);
    expect(prefixes.length).toBeGreaterThan(1);
  });

  it("перечень модуля резолвится в общем реестре — иначе обход пуст и всё ниже вакуумно", () => {
    // Опечатка в идентификаторе или снятая запись оставили бы `reachedSpecIds`
    // без спек, и перечисление ниже прошло бы, ничего не измерив.
    expect(MODULE_SPEC_IDS.length).toBeGreaterThan(0);
    expect(MODULE_SPEC_IDS.filter((id) => !REGISTRY[id])).toEqual([]);
  });

  it.each(registryDomains().map((d) => [d]))("%s проксируется", (domain) => {
    expect(isProxied(domain, prefixes)).toBe(true);
  });

  it("Operation-поллинг проксируется", () => {
    expect(isProxied("/operations", prefixes)).toBe(true);
  });

  // Положительный контроль: предикат обязан уметь ответить «нет», иначе
  // перечисление выше зеленело бы при любом наборе правил.
  it("домен, которого в прокси нет, проверку НЕ проходит", () => {
    expect(isProxied("/domain-that-is-not-proxied", prefixes)).toBe(false);
  });
});
