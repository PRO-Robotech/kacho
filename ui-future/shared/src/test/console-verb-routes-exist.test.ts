import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  UNRESOLVED,
  collectVerbRouteUses,
  verbTail,
  type ConsoleSource,
  type VerbRouteUse,
} from "./console-verb-literals";

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
 * Вердикт — по СОДЕРЖИМОМУ. Отдельно утверждается количество осмотренного, и
 * отдельно — по файлам И по литералам: «ноль находок» обязано быть отличимо не
 * только от «ноль прочитанных файлов», но и от «файлы прочитаны, а литералы в
 * них разобраны не все». Второе — предмет #559: разбор ЛИТЕРАЛОВ шёл по сырому
 * тексту парным счётом обратных кавычек, слеп после нечётной кавычки в
 * комментарии и одновременно принимал за вызов путь, стоящий В комментарии.
 * Теперь литералы берутся у компилятора TypeScript
 * (`./console-verb-literals.ts`), а способность разбора не ослепнуть доказана
 * инъекцией в `./console-verb-literals.test.ts`.
 *
 * И отдельно проверяется сам сопоставитель — на заведомо несуществующем пути он
 * обязан дать находку, на заведомо существующем — не дать.
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

// appOf — приложение консоли, которому принадлежит файл (первый сегмент пути
// от корня консоли).
function appOf(file: string): string {
  return path.relative(consoleRoot, file).split(path.sep)[0];
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
).filter((f) => !f.includes(".test."));

const sources: ConsoleSource[] = consoleFiles.map((file) => ({
  file: path.relative(consoleRoot, file),
  app: appOf(file),
  isResourceRegistry: path.basename(file) === "resource-registry.tsx",
  source: readFileSync(file, "utf8"),
}));

const routes = contractRoutes();
const matchers = routes.map(routeMatcher);
const scan = collectVerbRouteUses(sources);
const uses = scan.uses;

describe("console addresses only verb-routes the contract serves", () => {
  it("reads the contract and the console (a silent zero is not a pass)", () => {
    // eslint-disable-next-line no-console
    console.log(
      `перепись: исходников консоли ${scan.filesParsed}, литералов разобрано ` +
        `${scan.literalsParsed}, связываний контракта ${routes.length} ` +
        `(из них действий-глаголов ${routes.filter((r) => verbTail.test(r)).length}), ` +
        `мест консоли ${uses.length} по ${new Set(uses.map((u) => u.resolved)).size} маршрутам`,
    );

    expect(scan.filesParsed).toBeGreaterThan(500);
    // Литералы — ОТДЕЛЬНАЯ перепись, а не следствие первой. Пока её не было,
    // «ноль находок в этом файле» было неотличимо от «этот файл не разбирался»:
    // разбор молча терял всё после нечётной обратной кавычки, и число мест это
    // не показывало (см. #559 и шапку ./console-verb-literals.ts).
    expect(scan.literalsParsed).toBeGreaterThan(15000);
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
    //
    // Было 48. Убыль законна и ровно две: `/iam/v1/users/{id}:block` и
    // `:unblock` адресовались ТОЛЬКО из `UsersPage` — страницы, которую не
    // рендерил ни один маршрут (`/iam/users` ведёт в `IamUsersListShell`).
    // Страница снята вместе с находкой #421, и вместе с ней с этого надзора
    // ушли два глагола. Связывания в контракте остались: с консоли пропало
    // обращение, а не возможность. Отсутствие запрета участия на живой странице
    // заведено предметом (#440), а не оставлено молчаливым.
    // Было 46. Убыль законна и ровно шестнадцать: справочник iam скопирован в
    // четырёх модулях, и один и тот же литерал считался ПЯТЬ раз. Копии сведены
    // к `@shared/api/iam` (#405) — с надзора ушли повторы, а не обращения.
    //
    // Именно поэтому здесь пинится ДВА числа. Счёт мест зависит от того, во
    // скольких копиях лежит один литерал, и при сведении форков падает сам собой —
    // на нём одном «сведено» неотличимо от «разбор молча уехал в ноль». Счёт
    // РАЗЛИЧНЫХ маршрутов от дублирования не зависит: он обязан остаться прежним,
    // и он остался — 27 до сведения и 27 после, множества совпали дословно
    // (проверено прогоном на восстановленных копиях).
    //
    // Было 30/27. Прибавка законна и ровно два маршрута с одного места каждый:
    // `/iam/v1/users/{id}:block` и `:unblock` вернулись на живую страницу —
    // теперь действием-глаголом строки списка, объявленным в спеке ресурса
    // (#440). Это ровно те два, что ушли отсюда вместе со снятой страницей
    // (абзац выше): возможность край подавал всё это время, а обращения к ней
    // из консоли не было.
    //
    // Было 32/29, и осталось 32/29 — но СОСТАВ изменился на одно место (#559).
    // Разбор литералов переведён с парного счёта обратных кавычек по сырому
    // тексту на синтаксическое дерево, и это сняло ДВЕ ошибки сразу, которые
    // до сих пор гасили друг друга в счёте:
    //   · ушло `shared/src/lib/resource-spec.ts` — путь `…/{id}:internal` стоит
    //     там в объясняющем комментарии («Пример: …»), то есть считался вызов,
    //     которого нет;
    //   · прибавилось `shared/src/lib/resource-registry.tsx` — тот же путь, но
    //     НАСТОЯЩИМ литералом (`internalGetPath`), которого прежний разбор не
    //     видел вовсе: до места объявления в файле стоит нечётное число
    //     обратных кавычек.
    // То есть «прибавка законна и ровно одна» из абзаца про `internalGetPath`
    // выше относилась к КОММЕНТАРИЮ, а живое обращение всё это время не
    // считалось. Числа совпали случайно; проверять состав, а не только счёт.
    //
    // Оба глагола пользователя (`:block`/`:unblock`) строятся в
    // `@shared/api/iam` (`userBlockPath`/`userUnblockPath`), а не в реестре.
    // Прежде это было ОБХОДОМ слепого пятна (в реестре литерал не читался);
    // слепого пятна больше нет, а место остаётся — поверхность API домена и без
    // того законный дом такого литерала (так же устроены глаголы машины в
    // compute).
    // 29→30 распознанных адресов и 32→33 использования: заведено исключение
    // человека из аккаунта (`:removeFromAccount`, #1127) — третий глагол
    // пользователя, и строится он там же, в поверхности API домена.
    const distinct = new Set(uses.map((u) => u.resolved));
    expect(distinct.size).toBe(30);
    expect(uses.length).toBe(33);
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
