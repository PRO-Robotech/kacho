import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Гейт: консоль не показывает координаты инфраструктуры хранилища.
 *
 * # Решение, которое этот гейт держит
 *
 * Производственный контракт storage завёл два АДМИНСКИХ ресурса —
 * `StorageBackend` (`/storage/v1/storageBackends`) и `DiskTypeBinding`
 * (`/storage/v1/diskTypeBindings`). Оба живут ТОЛЬКО на cluster-internal
 * слушателе. В консоль они НЕ выводятся, и решение это осознанное:
 *
 *   (1) они несут ровно то, что `security.md` §«Инфра-чувствительные данные»
 *       запрещает на любой поверхности, кроме `Internal*`: адрес кластера
 *       хранилища, ссылку на учётный материал, координату размещения объекта
 *       (пул, шаблон пространства имён), идентификатор бэкенда. Показать ресурс
 *       и НЕ показать этих полей нельзя: вкладка JSON карточки отдаёт ответ
 *       целиком;
 *   (2) арендатору они не нужны: всё, что ему следует знать о привязках,
 *       контракт уже вывел на публичную поверхность типа диска — `capabilities`
 *       (ПЕРЕСЕЧЕНИЕ действующих ревизий) и `limits`. Показ ревизий завёл бы
 *       второй источник того же факта;
 *   (3) админская плоскость консоли (`/system/*`) принадлежит другому
 *       приложению и другому реестру; заводить её для storage — отдельная
 *       работа с отдельным решением, а не следствие правки контракта.
 *
 * # Что утверждается
 *
 * Ни один реестр консоли не адресует ресурс storage полем-координатой
 * инфраструктуры. Проверяются ОБЕ стороны показа: `columns[].path` (что видно в
 * списке) и `fields[].name` (что предлагается ввести).
 *
 * Область — только спеки `/storage/v1/*`. Сужение намеренное и названо: имя
 * `endpoint` законно у соседних доменов (слушатель балансировщика), и запрет по
 * одному лишь слову дал бы ложные срабатывания, после которых гейт снимут.
 *
 * # Чего гейт НЕ видит — названо, а не умолчано
 *
 * Он читает РЕЕСТРЫ. Строка карточки, собранная в доменном расширении мимо
 * реестра, в область не попадает; её удерживает обзор кода, а не эта проба.
 * Изоляцию самих маршрутов держит край (`gateway/internal/restmux`
 * external-isolation), а не консоль: здесь речь только о том, что консоль
 * ПОКАЗЫВАЕТ.
 */

const consoleRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);

/**
 * Имена полей-координат инфраструктуры хранилища.
 *
 * Список закрытый и выведен из контракта админских сообщений: `StorageBackend`
 * (`endpoint`, `credentials_ref`, `kind`) и `DiskTypeBinding` (`backend_id`,
 * `locator`, `revision`, `qos`). Он не «на всякий случай»: каждое имя
 * существует в дереве proto, и появление любого из них у публичной спеки
 * означает, что кто-то вывел внутреннюю проекцию наружу.
 */
const INFRA_FIELD_NAMES = [
  "endpoint",
  "credentials_ref",
  "backend_id",
  "locator",
  "pool",
  "pool_name",
  "namespace",
  "namespace_template",
  "qos",
];

/** Адреса админских ресурсов storage — их в реестрах консоли быть не должно. */
const ADMIN_API_PATHS = [
  "/storage/v1/storageBackends",
  "/storage/v1/diskTypeBindings",
];

interface SpecSlice {
  file: string;
  apiPath: string;
  /** Тело объявления спеки — от её `apiPath` до следующего. */
  body: string;
}

/** Реестры ресурсов всех приложений дерева. */
function registryFiles(): string[] {
  const NOT_APPS = new Set([
    "node_modules",
    "deploy",
    "docs",
    "scripts",
    ".git",
    "e2e",
  ]);
  const out: string[] = [];
  for (const name of readdirSync(consoleRoot)) {
    if (NOT_APPS.has(name)) continue;
    const dir = path.join(consoleRoot, name);
    if (!statSync(dir).isDirectory()) continue;
    const file = path.join(dir, "src/lib/resource-registry.tsx");
    try {
      if (statSync(file).isFile()) out.push(file);
    } catch {
      // приложение без собственного реестра
    }
  }
  // shared лежит вне перечня приложений, но реестр несёт — и он самый крупный.
  const sharedRegistry = path.join(
    consoleRoot,
    "shared/src/lib/resource-registry.tsx",
  );
  if (statSync(sharedRegistry).isFile()) out.push(sharedRegistry);
  return out.sort();
}

/** Спеки домена storage: адрес + тело объявления. */
export function storageSpecs(file: string, source: string): SpecSlice[] {
  const out: SpecSlice[] = [];
  const marks = [...source.matchAll(/apiPath:\s*"([^"]+)"/g)];
  for (let i = 0; i < marks.length; i += 1) {
    const apiPath = marks[i][1];
    if (!apiPath.startsWith("/storage/v1/")) continue;
    const start = marks[i].index ?? 0;
    const end =
      i + 1 < marks.length
        ? (marks[i + 1].index ?? source.length)
        : source.length;
    out.push({ file, apiPath, body: source.slice(start, end) });
  }
  return out;
}

/** Координаты инфраструктуры, показанные спекой. */
export function infraLeaks(spec: SpecSlice): string[] {
  const shown = [
    ...[...spec.body.matchAll(/\bpath:\s*"([^"]+)"/g)].map((m) => m[1]),
    ...[...spec.body.matchAll(/\bname:\s*"([^"]+)"/g)].map((m) => m[1]),
  ];
  // Читается ГОЛОВА пути: `locator.pool` показывает `locator` так же, как сам
  // `locator`, и точечная запись не должна обходить запрет.
  return shown
    .filter((p) => INFRA_FIELD_NAMES.includes(p.split(".")[0]))
    .map((p) => `${spec.apiPath} → ${p}`);
}

const FILES = registryFiles();
const SPECS = FILES.flatMap((f) => storageSpecs(f, readFileSync(f, "utf8")));

describe("консоль не показывает координаты инфраструктуры хранилища", () => {
  it(`объём осмотренного: реестры прочитаны, спеки storage найдены (files=${FILES.length}, specs=${SPECS.length})`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного».
    expect(FILES.length).toBeGreaterThan(0);
    expect(SPECS.length).toBeGreaterThan(0);
    const apps = [
      ...new Set(
        FILES.map((f) => path.relative(consoleRoot, f).split(path.sep)[0]),
      ),
    ].sort();
    expect(apps).toEqual(
      expect.arrayContaining(["compute", "shared", "storage"]),
    );
  });

  it("собственная предпосылка: детектор ловит утечку и молчит на законных именах", () => {
    const leaky: SpecSlice = {
      file: "synthetic",
      apiPath: "/storage/v1/diskTypes",
      body: 'apiPath: "/storage/v1/diskTypes", columns: [{ header: "Пул", path: "locator.pool" }]',
    };
    // (а) верни дефект — гейт краснеет и называет предмет.
    expect(infraLeaks(leaky)).toEqual(["/storage/v1/diskTypes → locator.pool"]);
    // (б) законная спека той же формы — гейт молчит. Без этого контроля он ловил
    // бы форму, а не существо, и первый ложный срабат его отключил бы.
    const clean: SpecSlice = {
      file: "synthetic",
      apiPath: "/storage/v1/volumes",
      body: 'apiPath: "/storage/v1/volumes", columns: [{ path: "zone_id" }, { path: "disk_type_id" }, { path: "used_by" }]',
    };
    expect(infraLeaks(clean)).toEqual([]);
  });

  it("ни один реестр не показывает координату инфраструктуры", () => {
    const leaks = SPECS.flatMap((s) =>
      infraLeaks(s).map((l) => `${path.relative(consoleRoot, s.file)}: ${l}`),
    );
    // Координаты, а не счётчик: красный гейт обязан сказать, где и что.
    expect(leaks).toEqual([]);
  });

  it("админские ресурсы storage в реестрах консоли не заведены", () => {
    // Решение записано в шапке файла. Если оно когда-нибудь изменится, менять
    // придётся здесь — то есть осознанно, а не побочным эффектом правки реестра.
    const found = SPECS.filter((s) => ADMIN_API_PATHS.includes(s.apiPath)).map(
      (s) => `${path.relative(consoleRoot, s.file)}: ${s.apiPath}`,
    );
    expect(found).toEqual([]);
  });

  it("предпосылка запрета жива: админские адреса существуют в контракте", () => {
    // Исключение живёт, пока у него есть предмет. Если эти маршруты исчезнут из
    // ствола, запрет станет записью, которой нечего запрещать, — и об этом надо
    // узнать здесь, а не унаследовать слепую зону.
    const protoRoot = path.resolve(
      consoleRoot,
      "..",
      "proto/kacho/cloud/storage/v1",
    );
    const sources = readdirSync(protoRoot)
      .filter((f) => f.endsWith(".proto"))
      .map((f) => readFileSync(path.join(protoRoot, f), "utf8"))
      .join("\n");
    expect(sources.length).toBeGreaterThan(1000);
    for (const p of ADMIN_API_PATHS)
      expect({ path: p, declared: sources.includes(`"${p}"`) }).toEqual({
        path: p,
        declared: true,
      });
  });
});
