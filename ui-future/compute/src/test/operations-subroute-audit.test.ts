// Гейт класса: вкладка «Операции» не обещает подмаршрута, которого в стволе нет.
//
// Дефект, ради которого гейт заведён, жил в ПЯТИ копиях сразу: `OperationsTab`
// каждого приложения склеивал адрес списка из `spec.apiPath` — а связывание
// `GET <apiPath>/{id}/operations` ствол несёт не у всякого ресурса. У каталожных
// и админских ресурсов (типы дисков и машин, каталог размещения geo, пулы
// адресов, вложенные ресурсы реестра) его нет вовсе: вкладка была видна,
// спрашивала несуществующий адрес и показывала отказ края там, где верный ответ
// — «у этого ресурса операций не бывает».
//
// Экземпляры чинятся в своих файлах; здесь закрывается КЛАСС, и закрывается он
// с ДВУХ сторон:
//
//   структурно — ни один исходник консоли не собирает этот путь сам, а каждый,
//   кто рендерит вкладку, берёт путь у общего перечня `operationsListPath` и
//   передаёт его пропом. Дальше работает система типов: перечень отвечает
//   `string | null`, проп объявлен `string`, поэтому «показать вкладку, не
//   убедившись, что путь есть» — ошибка компиляции, а не забытая проверка;
//
//   перечислительно — по КАЖДОМУ реестру дерева сверяется, что консоль считает
//   подмаршрут существующим ровно там, где его несёт proto. Реестры не
//   выписаны, а найдены обходом, поэтому приложение, заведённое завтра, попадает
//   под гейт само; их поимённый перечень утверждается, чтобы появление шестого
//   было замечено, а не поглощено молча.
//
// Предпосылка гейта проверяется им же. Разбор идёт по узлам синтаксиса, потому
// что комментарии в этом же дереве цитируют и `spec.apiPath`, и сам запрещённый
// шаблон — объясняя запрет; текстовый предикат зачёл бы объяснение за нарушение.
// Что дискриминатор различает код и комментарий, доказано инъекцией в обе
// стороны, а не объявлено. Второе допущение — что `apiPath` вообще прочитан:
// в реестре shared две спеки задают его импортированной константой, и предикат,
// умеющий только литералы, молчал бы на них, отчитываясь «ноль находок».
// Поэтому неразрешённый `apiPath` — находка, а не пропуск.

import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { OPERATIONS_LIST_PATHS, hasOperationsSubroute } from "@shared/lib/operations-subroute";

import {
  appOf,
  findRegistryFiles,
  protoFileCount,
  protoOperationBases,
  readRegistrySpecs,
  tabWiringAcrossTree,
  tabWiringOf,
  walk,
  type SpecEntry,
} from "./operations-subroute-audit";

const SRC_DIR = fileURLToPath(new URL("..", import.meta.url));
const UI_ROOT = resolve(SRC_DIR, "../..");
const PROTO_DIR = join(resolve(UI_ROOT, ".."), "proto");

const protoBases = protoOperationBases(PROTO_DIR);
const registryFiles = findRegistryFiles(UI_ROOT);
const specsByApp = new Map<string, SpecEntry[]>(
  registryFiles.map((f) => [appOf(UI_ROOT, f), readRegistrySpecs(f, UI_ROOT)]),
);
const allSpecs = [...specsByApp.values()].flat();
const wiring = tabWiringAcrossTree(UI_ROOT);

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("дерево proto прочитано и подмаршруты операций из него извлечены", () => {
    expect(protoFileCount(PROTO_DIR)).toBeGreaterThan(100);
    expect(protoBases.length).toBeGreaterThan(15);
  });

  it("реестры найдены обходом и названы поимённо", () => {
    // Перечень ВЫВЕДЕН из дерева, но утверждён: шестое приложение со своим
    // реестром обязано быть замечено здесь, а не молча попасть под гейт.
    expect([...specsByApp.keys()].sort()).toEqual(["compute", "nlb", "registry", "shared", "storage"]);
    // И каждый из них ПРОЧИТАН: пустой разбор дал бы «ноль спек без подмаршрута»
    // и выглядел бы как чистое дерево.
    const empty = [...specsByApp].filter(([, specs]) => specs.length < 4).map(([app]) => app);
    expect(empty).toEqual([]);
  });

  it("исходники, причастные к вкладке, осмотрены", () => {
    expect(wiring.filter((w) => w.rendersTab).length).toBeGreaterThanOrEqual(5);
  });

  it("у каждой спеки apiPath прочитан — нерезолвнутый был бы слепым пятном", () => {
    const blind = allSpecs.filter((s) => s.apiPath === null).map((s) => `${s.app}/${s.id} → ${s.rawApiPath}`);
    expect(blind).toEqual([]);
  });
});

describe("предпосылка гейта: разбор читает исполняемую часть, а не текст", () => {
  const probe = (source: string) =>
    tabWiringOf(join(UI_ROOT, "compute/src/probe.tsx"), UI_ROOT, source).buildsPathFromApiPath;

  it("тот же шаблон в КОДЕ — нарушение", () => {
    expect(probe("const u = `${spec.apiPath}/${id}/operations`;")).toBe(true);
  });

  it("тот же шаблон в КОММЕНТАРИИ — не нарушение", () => {
    // Иначе запрет краснел бы на собственном объяснении, и его сняли бы первым.
    expect(probe("// путь `${spec.apiPath}/${id}/operations` собирать здесь нельзя\nconst u = listPath;")).toBe(false);
  });

  it("законный близнец — путь из перечня, а не из apiPath — молчит", () => {
    expect(probe("const u = `${template}/${id}/operations`;")).toBe(false);
    expect(probe("const u = operationsListPath(spec.apiPath, id);")).toBe(false);
  });
});

describe("предпосылка гейта: apiPath прослеживается до значения", () => {
  const probe = (source: string) =>
    readRegistrySpecs(join(UI_ROOT, "compute/src/lib/resource-registry.tsx"), UI_ROOT, source);

  it("литерал читается", () => {
    expect(probe('const R = { x: { id: "a", apiPath: "/compute/v1/instances" } };')[0].apiPath).toBe(
      "/compute/v1/instances",
    );
  });

  it("своя константа читается", () => {
    const src = 'const P = "/geo/v1/zones";\nconst R = { x: { id: "a", apiPath: P } };';
    expect(probe(src)[0].apiPath).toBe("/geo/v1/zones");
  });

  it("импортированная константа читается — на РЕАЛЬНОМ реестре, а не на выдумке", () => {
    // shared задаёт эти два apiPath через `@shared/api/geo`. Предикат, умеющий
    // только литералы, отдал бы здесь null и пропустил бы целый вид предмета.
    const shared = specsByApp.get("shared") ?? [];
    const byId = (id: string) => shared.find((s) => s.id === id);
    expect(byId("regions")?.rawApiPath).toBe("GEO_REGIONS_PATH");
    expect(byId("regions")?.apiPath).toBe("/geo/v1/regions");
    expect(byId("zones")?.rawApiPath).toBe("GEO_ZONES_PATH");
    expect(byId("zones")?.apiPath).toBe("/geo/v1/zones");
  });

  it("непрослеживаемое значение возвращается как null, а не как «нечего смотреть»", () => {
    const src = 'const R = { x: { id: "a", apiPath: somewhereElse } };';
    expect(probe(src)[0].apiPath).toBeNull();
  });
});

describe("класс: путь вкладки берётся у общего перечня, а не собирается на месте", () => {
  it("ни один исходник консоли не склеивает путь операций из apiPath", () => {
    const offenders = wiring.filter((w) => w.buildsPathFromApiPath).map((w) => w.file.slice(UI_ROOT.length + 1));
    expect(offenders).toEqual([]);
  });

  it("каждый, кто рендерит вкладку, спрашивает перечень и передаёт готовый путь", () => {
    const offenders = wiring
      .filter((w) => w.rendersTab && !(w.callsListPathHelper && w.passesListPathProp))
      .map((w) => w.file.slice(UI_ROOT.length + 1));
    expect(offenders).toEqual([]);
  });

  it("перечень подмаршрутов существует в ЕДИНСТВЕННОМ экземпляре", () => {
    // Вторая копия таблицы разъехалась бы с proto незамеченной: сверка с деревом
    // (`shared/src/lib/operations-subroute.test.ts`) читает только эту.
    // Ищется по всему дереву, а не среди причастных к вкладке файлов: копия
    // как раз и не была бы к ней причастна, и такой отбор ничего не искал бы.
    const tables = walk(UI_ROOT, /operations-subroute\.ts$/).map((f) => f.slice(UI_ROOT.length + 1));
    expect(tables).toEqual(["shared/src/lib/operations-subroute.ts"]);
    // Единственность найдена обходом; НАПОЛНЕННОСТЬ утверждается по значению,
    // которое модуль отдаёт. Прежняя редакция читала его исходник и искала имя
    // константы: такое утверждение выживает при пустой таблице, потому что имя
    // остаётся на месте.
    expect(OPERATIONS_LIST_PATHS.length).toBeGreaterThan(15);
    expect(OPERATIONS_LIST_PATHS.every((p) => p.endsWith("/operations"))).toBe(true);
  });
});

describe("согласие каждого реестра со стволом", () => {
  it("консоль считает подмаршрут существующим ровно там, где его несёт proto", () => {
    const disagree = allSpecs
      .filter((s) => s.apiPath !== null && hasOperationsSubroute(s.apiPath) !== protoBases.includes(s.apiPath))
      .map((s) => `${s.app}/${s.id} (${s.apiPath ?? "?"})`);
    expect(disagree).toEqual([]);
  });

  it("спеки без вкладки названы поимённо по приложениям — это утверждение, а не умолчание", () => {
    const without: Record<string, string[]> = {};
    for (const [app, specs] of [...specsByApp].sort((a, b) => a[0].localeCompare(b[0]))) {
      without[app] = specs
        .filter((s) => s.apiPath !== null && !protoBases.includes(s.apiPath))
        .map((s) => s.id)
        .sort();
    }
    expect(without).toEqual({
      compute: ["machine-types", "zones"],
      nlb: ["compute-regions", "zones"],
      // Вложенные ресурсы реестра адресуются внутри `registries/{registryId}/…`,
      // и подмаршрута операций ствол им не объявляет — как и каталогу geo.
      registry: ["regions", "repositories", "tags"],
      shared: ["address-pools", "compute-regions", "compute-zones", "disk-types", "machine-types", "regions", "zones"],
      storage: ["disk-types", "regions", "snapshots", "zones"],
    });
  });
});
