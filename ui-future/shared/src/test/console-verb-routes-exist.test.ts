import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Гейт: ни одна кнопка консоли не адресует действие-глагол, которого край не
 * обслуживает.
 *
 * Класс. Действие-глагол (`…/{id}:verb`) — единственная поверхность консоли, у
 * которой НЕТ статической сверки. Тела запросов сверяет гейт края
 * (`gateway/internal/restmux/console_body_contract_test.go`), но его детектор
 * знает `api.create|update|post|patch|put` и сырой `fetch` — и НЕ знает
 * `api.action`, которым отправляются именно глаголы. Поэтому переименованный или
 * снятый с контракта глагол остаётся в консоли живой кнопкой: пользователь
 * жмёт, край отвечает 404, и ни один тест этого не видит.
 *
 * Что утверждается. Каждое выражение пути, оканчивающееся на `:verb`, после
 * подстановки констант обязано совпасть с каким-нибудь `google.api.http`
 * связыванием из `proto/`. Источник истины — САМ контракт, а не список,
 * переписанный руками рядом с тестом: список разъезжается с контрактом молча,
 * ровно так же, как разъехалась консоль.
 *
 * Вердикт — по СОДЕРЖИМОМУ. Отдельно утверждается количество осмотренного: «ноль
 * находок» обязано быть отличимо от «ноль прочитанных файлов». И отдельно
 * проверяется сам сопоставитель — на заведомо несуществующем пути он обязан
 * дать находку, на заведомо существующем — не дать.
 */

// ui-future/ — корень консоли (файл лежит в shared/src/test/).
const consoleRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);
// proto/ — дом ВСЕХ .proto (polyrepo.md); контракт края лежит только там.
const protoRoot = path.resolve(consoleRoot, "..", "proto");

// Приложения консоли. Перечислены поимённо, а не обходом корня: новое
// приложение обязано быть внесено сюда осознанно, иначе оно молча выпадет
// из-под сверки — тот же класс, ради которого гейт и написан.
const CONSOLE_APPS = [
  "host",
  "dashboard",
  "shared",
  "vpc",
  "compute",
  "storage",
  "nlb",
  "registry",
  "iam",
  "system",
];

// ───────────────────────────── контракт ─────────────────────────────

// httpBinding — `get: "/vpc/v1/subnets/{subnet_id}"` в любом .proto.
const httpBinding = /\b(?:get|post|put|patch|delete)\s*:\s*"([^"]+)"/g;

function walk(dir: string, out: string[], exts: string[]): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === "node_modules" || entry === "dist" || entry === ".git")
      continue;
    const abs = path.join(dir, entry);
    if (statSync(abs).isDirectory()) {
      walk(abs, out, exts);
    } else if (exts.some((e) => entry.endsWith(e))) {
      out.push(abs);
    }
  }
  return out;
}

// contractRoutes — все REST-пути, объявленные аннотациями google.api.http.
function contractRoutes(): string[] {
  const routes = new Set<string>();
  for (const file of walk(protoRoot, [], [".proto"])) {
    // google/api/http.proto — сама аннотация; её примеры в комментариях путями
    // Kachō не являются.
    if (path.relative(protoRoot, file).startsWith("google" + path.sep))
      continue;
    const src = readFileSync(file, "utf8");
    for (const m of src.matchAll(httpBinding)) routes.add(m[1]);
  }
  return [...routes].sort();
}

// routeMatcher — связывание пути в регулярное выражение. `{x}` — один сегмент,
// `{x=**}` — остаток пути (repository реестра).
//
// Двоеточие исключено из ОБОИХ классов намеренно: значение параметра его не
// содержит, а действие им отделяется. Без этого запрета `[^/]+` проглатывал бы
// `:verb` целиком — и `…/{id}:teleport` совпадал бы с обычным чтением `…/{id}`,
// то есть гейт зеленел бы ровно на том, что обязан ловить.
function routeMatcher(route: string): RegExp {
  const body = route
    .split(/(\{[^}]*\})/)
    .map((part) => {
      if (!part.startsWith("{"))
        return part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      return part.includes("=**") ? "[^:]+" : "[^/:]+";
    })
    .join("");
  return new RegExp(`^${body}$`);
}

// ───────────────────────────── консоль ─────────────────────────────

// verbTail — путь-действие: `:verb` ПРИКЛЕЕН к предыдущему сегменту
// (`…/{id}:start`). Обязательный не-слэш перед двоеточием отделяет действие от
// параметра маршрута браузера (`/projects/:projectId`), который выглядит так же
// и REST-путём не является вовсе.
const verbTail = /[^/:]:[a-zA-Z][A-Za-z0-9-]*$/;
// stringLiteral — строковые и шаблонные литералы (без вложенных backtick'ов).
const stringLiteral = /`([^`\\]*)`|"([^"\\]*)"/g;
// stringConst — `const NAME = "…";` / `export const NAME = "…";`
const stringConst =
  /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*"([^"]*)"\s*;/g;
// objectStringEntry — `key: "…"` внутри объектной константы путей.
const objectConst =
  /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*\{([^}]*)\}\s*as const;/g;
const objectEntry = /([A-Za-z_$][\w$]*)\s*:\s*"([^"]*)"/g;
// registryAlias — `const SPEC = REGISTRY["compute-instances"];`
const registryAlias =
  /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*REGISTRY\[\s*"([^"]+)"\s*\]/g;
// registrySpec — `id: "x"` … `apiPath: "/y"` в файле реестра ресурсов.
const registrySpecId = /\n\s*id:\s*"([^"]+)",\n\s*route:/g;
const registryApiPath = /\n\s*apiPath:\s*"([^"]+)"/;

// appOf — приложение консоли, которому принадлежит файл (первый сегмент пути
// от корня консоли).
function appOf(file: string): string {
  return path.relative(consoleRoot, file).split(path.sep)[0];
}

// specApiPaths — приложение → (id ресурса → apiPath). Копия реестра у каждого
// приложения СВОЯ и намеренно расходится (например `disk-types` живёт и в
// compute, и в storage, пока раскол блочного хранения не доведён), поэтому
// склеивать их в один словарь нельзя — путь резолвится реестром СВОЕГО
// приложения.
function specApiPaths(files: string[]): Map<string, Map<string, string>> {
  const out = new Map<string, Map<string, string>>();
  for (const file of files) {
    if (path.basename(file) !== "resource-registry.tsx") continue;
    const app = appOf(file);
    const byID = out.get(app) ?? new Map<string, string>();
    out.set(app, byID);
    const src = readFileSync(file, "utf8");
    for (const m of src.matchAll(registrySpecId)) {
      const tail = src.slice(m.index ?? 0, (m.index ?? 0) + 2000);
      const api = registryApiPath.exec(tail);
      if (!api) continue;
      byID.set(m[1], api[1]);
    }
  }
  return out;
}

// objectPathConsts — `IAM.accessBindings` и подобные, собранные по всему дереву
// (объявлены в одном модуле, используются из другого).
function objectPathConsts(files: string[]): Map<string, string> {
  const out = new Map<string, string>();
  for (const file of files) {
    const src = readFileSync(file, "utf8");
    for (const m of src.matchAll(objectConst)) {
      for (const e of m[2].matchAll(objectEntry)) {
        if (!e[2].startsWith("/")) continue;
        out.set(`${m[1]}.${e[1]}`, e[2]);
      }
    }
  }
  return out;
}

export interface VerbRouteUse {
  file: string;
  literal: string;
  resolved: string;
}

/**
 * Плейсхолдер неразрешённого `${…}`-сегмента пути.
 *
 * Символ выбран так, чтобы не столкнуться ни с одним настоящим сегментом URL.
 * Записан ЭКРАНИРОВАННО и ровно в одном месте: сырой управляющий байт в
 * исходнике невидим — его не показывает ни diff, ни обзор, и он молча теряется
 * при копировании строки, после чего сверка маршрутов начинает сравнивать не то,
 * что думает.
 */
const UNRESOLVED = "\u0000";

// resolveLiteral — подстановка констант в выражение пути. Неразрешённая
// интерполяция становится ОДНИМ сегментом: это всегда подставленный id.
function resolveLiteral(
  literal: string,
  locals: Map<string, string>,
  objects: Map<string, string>,
): string | null {
  const resolved = literal.replace(/\$\{([^}]*)\}/g, (_all, expr: string) => {
    const key = expr.trim();
    const local = locals.get(key);
    if (local !== undefined) return local;
    const obj = objects.get(key);
    if (obj !== undefined) return obj;
    return UNRESOLVED;
  });
  return resolved.startsWith("/") ? resolved : null;
}

// collectVerbRoutes — все места консоли, адресующие действие-глагол.
function collectVerbRoutes(
  files: string[],
  objects: Map<string, string>,
  specs: Map<string, Map<string, string>>,
): VerbRouteUse[] {
  const uses: VerbRouteUse[] = [];
  for (const file of files) {
    if (file.includes(".test.")) continue;
    const src = readFileSync(file, "utf8");
    const ownSpecs = specs.get(appOf(file));

    const locals = new Map<string, string>();
    for (const m of src.matchAll(stringConst)) locals.set(m[1], m[2]);
    for (const m of src.matchAll(registryAlias)) {
      const api = ownSpecs?.get(m[2]);
      if (api !== undefined) locals.set(`${m[1]}.apiPath`, api);
    }

    for (const m of src.matchAll(stringLiteral)) {
      const literal = m[1] ?? m[2];
      if (!literal || !/:[a-zA-Z][A-Za-z0-9-]*$/.test(literal)) continue;
      if (!literal.includes("/") && !literal.startsWith("${")) continue;
      const resolved = resolveLiteral(literal, locals, objects);
      // Проверка `verbTail` идёт по РАЗРЕШЁННОМУ пути: до подстановки перед
      // двоеточием стоит `}` у обеих форм, и действие от параметра маршрута
      // так не отличить.
      if (resolved === null || !verbTail.test(resolved)) continue;
      uses.push({ file: path.relative(consoleRoot, file), literal, resolved });
    }
  }
  return uses;
}

// unmatched — выражения пути, не совпавшие ни с одним связыванием контракта.
function unmatched(uses: VerbRouteUse[], matchers: RegExp[]): VerbRouteUse[] {
  return uses.filter((u) => {
    const probe = u.resolved.split(UNRESOLVED).join("x");
    return !matchers.some((re) => re.test(probe));
  });
}

// ───────────────────────────── проверки ─────────────────────────────

const consoleFiles = CONSOLE_APPS.flatMap((app) =>
  walk(path.join(consoleRoot, app, "src"), [], [".ts", ".tsx"]),
);
const routes = contractRoutes();
const matchers = routes.map(routeMatcher);
const objects = objectPathConsts(consoleFiles);
const specs = specApiPaths(consoleFiles);
const uses = collectVerbRoutes(consoleFiles, objects, specs);

describe("console addresses only verb-routes the contract serves", () => {
  it("reads the contract and the console (a silent zero is not a pass)", () => {
    expect(consoleFiles.length).toBeGreaterThan(500);
    expect(routes.length).toBeGreaterThan(150);
    expect(routes.filter((r) => verbTail.test(r)).length).toBeGreaterThan(40);
    // Столько мест консоли адресуют действие-глагол. Число обязано меняться
    // вместе с кодом — молча уехавший в ноль разбор виден именно здесь.
    // Правится ТОЛЬКО вместе с зелёным утверждением ниже: оно и решает, законна
    // ли прибавка (новое место совпало со связыванием контракта) или консоль
    // завела кнопку в маршрут, которого край не подаёт.
    //
    // Было 44. Прибавка законна и ровно одна: `internalGetPath` сети в реестре
    // переписан со слэшевой формы `/vpc/v1/networks/{id}/internal` на
    // глагольную `…/{id}:internal` — так связывание объявлено в
    // `InternalNetworkService.GetNetwork`, и это единственная :internal-
    // аннотация vpc. То есть место в консоли не появилось, а ВОШЛО под этот
    // разбор, впервые став глаголом; утверждение ниже подтверждает совпадение с
    // контрактом. Вторая правка того же диффа ушла в минус и здесь не видна:
    // у сетевого интерфейса поле снято целиком (у
    // `InternalNetworkInterfaceService` нет ни одной http-аннотации), а
    // слэшевая форма этим разбором и не считалась.
    //
    // Было 45. Прибавка законна и ровно три — производственный контракт storage
    // завёл три действия-глагола, и консоль их адресует из `api/resources.ts`
    // этого приложения (владелец ресурса строит путь сам, чтобы голова литерала
    // резолвилась в константу файла и место оставалось под этим надзором):
    //   · `/storage/v1/volumes/{id}:changeDiskType` — перевод тома на другой тип
    //     диска; отдельный глагол, потому что это перемещение данных, а не
    //     правка поля;
    //   · `/storage/v1/snapshots/{id}:copy` — копия снимка в другую зону;
    //   · `/storage/v1/images/{id}:copy`   — копия образа в другой регион.
    // Совпадение каждого со связыванием контракта подтверждает утверждение ниже.
    expect(uses.length).toBe(48);
  });

  it("every verb-route the console addresses exists in the contract", () => {
    const bad = unmatched(uses, matchers);
    const report = bad.map(
      (u) =>
        `  ${u.file}\n      ${u.literal}\n      → ${u.resolved.split(UNRESOLVED).join("…")}`,
    );
    expect(
      `${bad.length} console call(s) address a route the contract does not serve:\n${report.join("\n")}`,
    ).toBe("0 console call(s) address a route the contract does not serve:\n");
  });

  it("the matcher itself reddens on a route the contract does not serve", () => {
    const invented: VerbRouteUse[] = [
      {
        file: "probe.ts",
        literal: "/vpc/v1/subnets/${id}:teleport",
        resolved: `/vpc/v1/subnets/${UNRESOLVED}:teleport`,
      },
    ];
    expect(unmatched(invented, matchers)).toHaveLength(1);
  });

  it("the matcher accepts a route the contract does serve", () => {
    const real: VerbRouteUse[] = [
      {
        file: "probe.ts",
        literal: "/vpc/v1/subnets/${id}:add-cidr-blocks",
        resolved: `/vpc/v1/subnets/${UNRESOLVED}:add-cidr-blocks`,
      },
    ];
    expect(unmatched(real, matchers)).toHaveLength(0);
  });
});
