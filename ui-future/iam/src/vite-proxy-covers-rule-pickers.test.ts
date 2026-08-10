// Dev-прокси iam обязан покрывать домены, в которые iam-консоль реально ходит.
//
// Редактор правил роли рендерит picker реальных инстансов и для НЕ-iam типов:
// каталог `(модуль, ресурс) → спека реестра` включает vpc-, compute- и
// nlb-ресурсы, и их List идёт по apiPath спеки, то есть по чужому префиксу.
//
// Почему это тест, а не «мелочь конфигурации». Промах прокси в dev выглядит НЕ
// как ошибка: запрос гасится обработчиком отказа в редакторе и деградирует до
// свободного ввода id — picker просто пуст. Отказ, который ничего не печатает и
// ни на что не влияет визуально, живёт годами; а автор правила молча теряет
// подсказку и вводит идентификаторы руками.
//
// Обе стороны берутся ЗНАЧЕНИЯМИ: каталог токенов — экспорт модуля, правила
// прокси — `server.proxy` загруженного `vite.config.ts`. Прежняя редакция
// разбирала оба файла как ТЕКСТ и потому утверждала о форме записи: смена
// литерала на константу, перенос ключа, сокращённая запись — и разбор молча
// возвращал пустой набор, а «все домены покрыты» становилось истинным даром.

import { jest } from "@jest/globals";
import { TOKEN_TO_REGISTRY_ID, instanceFetcherFor } from "@shared/lib/resourceInstanceFetchers";

const covered = (apiPath: string, prefixes: string[]) => prefixes.some((p) => apiPath.startsWith(p));

let prefixes: string[] = [];

beforeAll(async () => {
  jest.unstable_mockModule("vite", () => ({ defineConfig: (c: unknown) => c }));
  jest.unstable_mockModule("@originjs/vite-plugin-federation", () => ({ default: () => ({ name: "federation" }) }));
  jest.unstable_mockModule("@vitejs/plugin-react", () => ({ default: () => ({ name: "react" }) }));
  (globalThis as unknown as { __dirname: string }).__dirname = new URL("..", import.meta.url).pathname;
  const configModule = "../vite.config";
  const mod = (await import(configModule)) as unknown as {
    default: { server?: { proxy?: Record<string, unknown> } };
  };
  prefixes = Object.keys(mod.default.server?.proxy ?? {});
});

describe("iam dev-прокси против путей, по которым ходит консоль", () => {
  const tokens = Object.keys(TOKEN_TO_REGISTRY_ID).map((t) => {
    const [module, resource] = t.split(".");
    return { module, resource };
  });
  const apiPaths = [
    ...new Set(tokens.map((t) => instanceFetcherFor(t.module, t.resource)?.spec.apiPath ?? "")),
  ].filter(Boolean);

  it("объём осмотренного назван: сколько токенов, путей и прокси прочитано", () => {
    expect(tokens.length).toBeGreaterThanOrEqual(15);
    expect(apiPaths.length).toBeGreaterThanOrEqual(15);
    expect(prefixes.length).toBeGreaterThanOrEqual(2);
    // Перечень обязан выходить за пределы своего домена — иначе утверждение ниже
    // проверяло бы только /iam и было бы тождественно истинным.
    expect(apiPaths.some((p) => !p.startsWith("/iam/"))).toBe(true);
  });

  it("каждый путь picker'а правил резолвится через прокси", () => {
    const unreachable = apiPaths.filter((p) => !covered(p, prefixes)).sort();
    expect(unreachable).toEqual([]);
  });

  it("непокрытый префикс предикат считает недостижимым (контроль в обратную сторону)", () => {
    expect(covered("/iam/v1/roles", prefixes)).toBe(true);
    expect(covered("/nothing/v1/here", prefixes)).toBe(false);
  });
});
