// Приложение не может дозвониться до домена, которого нет в его dev-прокси.
//
// В dev браузер стучится по относительному пути (`/geo/v1/zones`), и до
// api-gateway его доводит `server.proxy` этого приложения. Домен, которого в
// прокси нет, отдаёт index.html вместо JSON: запрос формально успешен, а список
// пуст — отказ выглядит как «ресурсов нет». Поэтому набор проксируемых доменов
// утверждается против того, куда приложение реально ходит.
//
// Требуемый набор ВЫВОДИТСЯ из общего реестра, а не выписывается: реестр —
// единственное место, где живут apiPath спек, и он же переезжает вслед за proto
// ствола. Выписанный список разошёлся бы с ним молча — ровно так /geo и остался
// непроксированным, когда Region/Zone стали адресоваться /geo/v1/*.
//
// Берётся ВЕСЬ реестр, а не только маршрутизированные разделы: RefSelect,
// GlobalResourceFormModal и общие detail-страницы читают спеки, на которые в
// этом приложении нет ни одного маршрута (ref-цели), — их запросы уходят из того
// же браузера.
//
// Читается ОБЪЯВЛЕНИЕ (сам vite.config.ts), а не отрендеренный dev-сервер:
// проверка, которой нужен поднятый vite, пропускалась бы там, где её и надо
// исполнять.

import { jest } from "@jest/globals";
import { REGISTRY } from "@shared/lib/resource-registry";

/** Первый сегмент пути — домен, по которому выбирается правило прокси. */
function domainOf(apiPath: string): string | null {
  const m = /^\/([a-z][a-z0-9-]*)\//.exec(apiPath);
  return m ? `/${m[1]}` : null;
}

function registryDomains(): string[] {
  const out = new Set<string>();
  for (const spec of Object.values(REGISTRY)) {
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
  // `defineConfig` — тождество, а плагины к объявлению прокси отношения не имеют:
  // подменяем их, чтобы не тянуть в jsdom бандлер (esbuild требует node-овый
  // TextEncoder и в этой среде падает). Подменён ТРАНСПОРТ загрузки, а не предмет
  // проверки: `server.proxy` читается из настоящего vite.config.ts.
  jest.unstable_mockModule("vite", () => ({ defineConfig: (c: unknown) => c }));
  jest.unstable_mockModule("@originjs/vite-plugin-federation", () => ({ default: () => ({ name: "federation" }) }));
  jest.unstable_mockModule("@vitejs/plugin-react", () => ({ default: () => ({ name: "react" }) }));
  // vite.config.ts написан как CJS-модуль (использует __dirname), а ts-jest
  // грузит его как ESM. Подставляем каталог приложения — тот же, что видит vite.
  (globalThis as unknown as { __dirname: string }).__dirname = new URL("../..", import.meta.url).pathname;
  // Спецификатор — переменная намеренно: vite.config.ts принадлежит проекту
  // tsconfig.node.json (ему нужны типы node), а этот тест компилируется в
  // tsconfig.app.json. Литеральный импорт затянул бы файл во второй проект
  // (TS6307). Резолвит его jest в рантайме — читается тот же самый файл.
  const configModule = "../../vite.config";
  const mod = (await import(configModule)) as unknown as {
    default: { server?: { proxy?: Record<string, unknown> } };
  };
  prefixes = Object.keys(mod.default.server?.proxy ?? {});
});

describe("dev-прокси vpc покрывает домены, к которым приложение обращается", () => {
  it("предмет проверки непуст — реестр прочитан, правила прокси прочитаны", () => {
    expect(registryDomains().length).toBeGreaterThan(3);
    expect(prefixes.length).toBeGreaterThan(3);
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
