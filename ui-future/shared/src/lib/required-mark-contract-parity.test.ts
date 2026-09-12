// Звёздочка обязательности обязана сходиться с КОНТРАКТОМ владельца (#608).
//
// ПРЕДМЕТ. Значок «*» у подписи сообщает арендатору: без этого поля запрос не
// примут. Форма, ставящая значок там, где сервер поле не требует, утверждает
// неправду; хуже того, она обесценивает значок вообще: если он стоит
// безотносительно обязательности, по нему нельзя понять, что заполнить обязан, —
// а это единственное, ради чего он нарисован (решение владельца #562).
//
// ОТКУДА БЕРЁТСЯ ОБЯЗАТЕЛЬНОСТЬ — ПЕРЕОСНОВАНО (kacho#1255). Прежде гейт читал
// её из КОНТРАКТА: обходил дерево `.proto`, снимал комментарии и извлекал у поля
// опцию `(required)`. Семейство расширений, которому эта опция принадлежала,
// снято с контрактов целиком — исполнителя на пути запроса у него не было ни
// одного, и объявление ничего не ограничивало.
//
// Это НЕ равноценная замена, а строгое улучшение, и вот почему. Прежнее
// основание бывало ОБРАТНЫМ действительному: на двух полях выдачи удостоверений
// контракт объявлял обязательным ровно то, что край подставляет сам и присланным
// отвергает. Ошибка такого рода транслировалась бы арендатору НА ЭКРАН — гейт
// требовал бы звёздочку там, где поле слать не надо.
//
// Теперь источник — ПОВЕДЕНИЕ СЕРВЕРА: поле обязательно ровно тогда, когда
// запрос без него отвергается. Перечень назван ниже (`REQUIRED_BY_SERVER`) и
// ДОКАЗЫВАЕТСЯ, а не объявляется: у каждой записи назван отказывающий код
// продукта, и его существование проверяется отдельной пробой предпосылки.
//
// ПОЧЕМУ ЭТО ГЕЙТ, А НЕ ПРОБА ОДНОЙ ФОРМЫ. Класс найден переписью, а не диффом:
// пять форм арендатора объявляли «Имя» обязательным, тогда как контракт vpc не
// требует `name` НИ У ОДНОГО ресурса, а `submit()` этих форм имени не проверяет
// вовсе — то есть значок не подпирался даже самой формой. Проба браузером
// (`e2e/specs/findings.spec.ts`, `verifies #562`) видит одну форму из пяти:
// остальные четыре остались бы без держащего артефакта.
//
// ПОЧЕМУ ЧТЕНИЕМ ДЕРЕВА (сторона КОНСОЛИ — читается по-прежнему деревом).
// Общий стенд проб подменяет `antd` (`test/antd-stub.ts`):
// `Form.Item` рисуется без разметки библиотеки, звёздочки в jsdom нет как узла —
// утверждать о ней рендером здесь нельзя ни при каком написании пробы. Поэтому
// сверяются ОБЪЯВЛЕНИЯ: что объявила форма и что объявил контракт. Это перепись
// состава дерева, а не «проба, подтверждающая себя своим же текстом»: она обходит
// дерево и читает файлы, которых сама не называет (гейт продукта
// `TestUITestsDoNotReadTheirOwnSourceAsText` исключает обход именно по этой
// причине). Разбор идёт по исполняемой части — комментарии сняты общим
// разборщиком `stripComments`, иначе гейт нашёл бы `required` в абзаце, который
// сам же и объясняет запрет.
//
// ОБЛАСТЬ — поле `name`, и это сказано прямо. Общее отображение «подпись формы →
// поле контракта» в дереве не объявлено, поэтому гейт судит ровно то поле, чья
// подпись выводится из реестра (`REGISTRY[specId].fields`, элемент `name`), и
// печатает, сколько объявлений он рассудить не смог. Расширять область — работа
// со своим предметом, а не молчаливое умолчание этой.
//
// ЧЕМ ОГРАНИЧЕН ВЕРДИКТ (названо, чтобы не читалось шире). Обязательность
// берётся объединением по ВСЕМ путям записи ресурса (`post`/`put`/`patch` его
// REST-адреса): если контракт требует `name` на создании, гейт молчит и о форме
// правки. Это делает его консервативным — он пропускает, но не выдумывает.
//
// СПОСОБНОСТЬ УПАСТЬ доказана инъекцией в обе стороны (describe ниже): дефект
// краснеет с координатой, а законный близнец ТОЙ ЖЕ ФОРМЫ — подпись «Имя» со
// звёздочкой у ресурса, чей контракт её требует (группа IAM), — молчит.
// Дискриминатор держится одним именем с обеих сторон.
//
// СЛУЖБА ДОСТУПА ВЫНЕСЕНА ОТДЕЛЬНЫМ ПРОДУКТОМ — и ШЕСТЬ IAM-ЗАПИСЕЙ СНЯТЫ ВМЕСТЕ
// С ДОКАЗАТЕЛЬСТВОМ. `services/iam/` в этом дереве не осталось ни одним файлом;
// её собственные `newman`-кейсы уехали с ней же. Единственным доказательством
// того, что `/iam/v1/accounts`…`/iam/v1/internal/interactiveClients` требуют
// `name`, был `validateResourceName` в `services/iam/internal/domain/types.go` —
// файл, который сравнивать больше не с чем: другого производителя этого факта в
// дереве нет (контракт эту опцию не несёт с kacho#1255, страница документации
// сервиса уехала вместе с сервисом). «Доказывается, а не объявляется» — здесь
// именно оно и работает: не имея доказательства, запись держать нельзя, даже
// если поведение сервера не менялось ни на бит.
//
// Шесть путей поэтому НЕ в `REQUIRED_BY_SERVER` — не потому что имя стало
// необязательным, а потому что у платформы нет способа это утверждать.
// `contractRequires` вернёт для них `null` («не знаю»), а не `false` («не
// требует»): гейт больше не судит звёздочку «Имя» этих шести форм — ни в одну,
// ни в другую сторону. Тот же приём уже применён в этом дереве для того же
// расставания: `internal/repohygiene/nestedquotasuitereclaim_test.go` снял
// двустороннюю сверку с каталогом `countableKinds` ровно по этой причине и
// оставил её незаменённой, а не подменённой выдумкой.
//
// Восстановить проверяемость может только НОВЫЙ производитель этого факта — в
// дереве службы доступа (её собственный гейт формы) либо явной странице
// документации, которую эта проба сможет прочитать. До тех пор шесть форм
// проверяют себя чем угодно, кроме этого гейта.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";

import { isCorelibCoordinate, readCorelibFile } from "@shared/test/corelib-source";
import { stripComments } from "@shared/test/strip-comments";
import { REGISTRY } from "./resource-registry";

const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const UI_ROOT = resolve(APP_DIR, "..");

// ── разбор исходного текста: строки, теги, атрибуты ──────────────────────────

/** Индекс ПОСЛЕ закрывающей кавычки литерала, начинающегося в `i`. */
function skipString(src: string, i: number): number {
  const quote = src[i];
  i++;
  while (i < src.length) {
    if (src[i] === "\\") {
      i += 2;
      continue;
    }
    if (src[i] === quote) return i + 1;
    i++;
  }
  return i;
}

/**
 * Текст ОТКРЫВАЮЩЕГО тега — от `<` до его собственного `>`.
 *
 * Скобки считаются: `label={<Space size={4}>…</Space>}` несёт `>` внутри
 * выражения, и наивный поиск первого `>` обрезал бы тег на нём, потеряв всё, что
 * стоит правее, — в том числе `required`.
 */
function readOpeningTag(src: string, start: number): string | null {
  let i = start + 1;
  let depth = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`") {
      i = skipString(src, i);
      continue;
    }
    if (c === "{") depth++;
    else if (c === "}") depth--;
    else if (c === ">" && depth === 0) return src.slice(start, i + 1);
    i++;
  }
  return null;
}

/** Значение атрибута тега: `"…"` → его содержимое, `{…}` → выражение целиком. */
function attrValue(tag: string, attr: string): string | null {
  const m = new RegExp(`(?:^|\\s)${attr}\\s*=\\s*`).exec(tag);
  if (!m) return null;
  const i = m.index + m[0].length;
  const c = tag[i];
  if (c === '"' || c === "'" || c === "`") return tag.slice(i + 1, skipString(tag, i) - 1);
  if (c !== "{") return null;
  let depth = 0;
  let j = i;
  while (j < tag.length) {
    const ch = tag[j];
    if (ch === '"' || ch === "'" || ch === "`") {
      j = skipString(tag, j);
      continue;
    }
    if (ch === "{") depth++;
    else if (ch === "}" && --depth === 0) return tag.slice(i + 1, j);
    j++;
  }
  return null;
}

/**
 * Тег объявляет поле обязательным.
 *
 * Обе формы засчитываются, потому что библиотека рисует звёздочку по обеим:
 * свойство `required` и правило `rules={[{ required: true }]}`. `required={false}`
 * под предикат не подпадает — за `required` там стоит `=`, а не конец имени.
 */
const CLAIMS_REQUIRED = /(?:^|[\s{,])required(?:\s*[:=]\s*\{?\s*true\s*\}?)?(?=[\s>/,}\]])/;

function lineOf(src: string, index: number): number {
  return src.slice(0, index).split("\n").length;
}

// ── сторона контракта: что владелец объявил обязательным ────────────────────

/**
 * REQUIRED_BY_SERVER — поле обязательно ровно тогда, когда СЕРВЕР отвергает
 * запрос без него. Ключ — REST-адрес записи, значение — имена таких полей.
 *
 * Это ЗАМЕНА чтения контракта, а не его копия. Прежде обязательность бралась из
 * опции `(required)` у поля запроса; семейство, которому опция принадлежала,
 * снято (kacho#1255), и — что важнее — оно расходилось с поведением.
 *
 * У КАЖДОЙ ЗАПИСИ НАЗВАН ОТКАЗЫВАЮЩИЙ КОД, и его существование проверяется
 * пробой предпосылки ниже: перечень, который нельзя опровергнуть, был бы
 * утверждением о себе самом.
 *
 * ОБЛАСТЬ — поле `name`, ровно как и прежде (см. шапку). Адрес, которого здесь
 * нет, означает «не знаю», а не «не требует»: молчание и незнание обязаны быть
 * различимы, и `contractRequires` возвращает для такого адреса `null`.
 */
const REQUIRED_BY_SERVER: ReadonlyMap<string, ReadonlySet<string>> = new Map([
  // iam: ШЕСТЬ путей СНЯТЫ (см. шапку файла, «СЛУЖБА ДОСТУПА ВЫНЕСЕНА
  // ОТДЕЛЬНЫМ ПРОДУКТОМ») — `services/iam/internal/domain/types.go` уехал с
  // сервисом, и предпосылка `validateResourceName` больше не читается ничем в
  // этом дереве. Отсутствие записи здесь означает «не знаю» (`contractRequires`
  // вернёт `null`), а не «не требует»: снятие записи — не смена контракта.
  // storage: административный ресурс, имя обязательно (домен отвергает пустое).
  ["/storage/v1/storageBackends", new Set(["name"])],
  // vpc и compute: имя НЕ обязательно — сервер производит его от `id`
  // (`NameOrDefault`), поэтому пустое имя законный вход, а звёздочка была бы
  // ложью. Пустой набор — это УТВЕРЖДЕНИЕ «не требует», а не «не знаю».
  ["/vpc/v1/networks", new Set<string>()],
  ["/vpc/v1/subnets", new Set<string>()],
  ["/vpc/v1/securityGroups", new Set<string>()],
  ["/vpc/v1/routeTables", new Set<string>()],
  ["/vpc/v1/addresses", new Set<string>()],
  ["/vpc/v1/gateways", new Set<string>()],
  ["/vpc/v1/networkInterfaces", new Set<string>()],
  ["/vpc/v1/cidrGroups", new Set<string>()],
  ["/compute/v1/instances", new Set<string>()],
  ["/compute/v1/placementGroups", new Set<string>()],
]);

/**
 * Отказывающий (либо, наоборот, подставляющий имя) код продукта — предпосылка
 * каждой записи. Исчезнет он — перечень станет описанием вчерашнего дерева, и
 * проба предпосылки краснеет по имени файла.
 *
 * Координата бывает ДВУХ видов (см. `@shared/test/corelib-source`): путь дерева
 * читается напрямую, координата `corelib/…` — из кэша модулей Go, версией,
 * закреплённой корневым `go.mod`. `pkg/validate/nameform` вынесен отдельным
 * опубликованным модулем (`github.com/PRO-Robotech/corelib`) — дерево больше не
 * несёт этого файла, поэтому его предпосылка читается туда, куда он переехал.
 *
 * Предпосылки шести IAM-путей здесь нет — она СНЯТА вместе с записями
 * `REQUIRED_BY_SERVER`, а не потеряна молча; причина — шапка файла.
 */
const REQUIRED_BY_SERVER_SOURCES: ReadonlyArray<readonly [string, string]> = [
  ["corelib/validate/nameform/nameform.go", "func OK("],
  ["services/vpc/internal/domain/types.go", "NameOrDefault"],
  ["services/compute/internal/apps/kacho/api/instance/instance.go", "NameOrDefault"],
];

interface Contract {
  /** Адрес записи → поля, без которых сервер запрос отвергает. */
  requiredByPath: ReadonlyMap<string, ReadonlySet<string>>;
  /** Сколько адресов перечень называет ВООБЩЕ. */
  pathsKnown: number;
  /** Сколько из них несут хотя бы одно обязательное поле. */
  pathsWithRequired: number;
}

function readContract(): Contract {
  let withRequired = 0;
  for (const fields of REQUIRED_BY_SERVER.values()) if (fields.size > 0) withRequired += 1;
  return {
    requiredByPath: REQUIRED_BY_SERVER,
    pathsKnown: REQUIRED_BY_SERVER.size,
    pathsWithRequired: withRequired,
  };
}

const contract = readContract();

/**
 * Контракт требует поле `field` у ресурса с REST-адресом `apiPath`?
 *
 * `null` — сопоставить не удалось: ни один путь записи не отвечает адресу. Это
 * не «не требует»: молчание и незнание обязаны быть различимы.
 *
 * `requiredByPath` — необязательный, по умолчанию РЕАЛЬНЫЙ (`contract.requiredByPath`).
 * Параметр существует ради инъекции: с уходом службы доступа в дереве не
 * осталось РЕАЛЬНОГО ресурса, у которого контракт требует «Имя» и который несёт
 * форму в консоли (см. шапку файла) — самопроверку дискриминатора «true / false
 * / null» держит теперь синтетическая карта, а не факт о живом ресурсе, который
 * может измениться сам по себе.
 */
function contractRequires(
  apiPath: string,
  field: string,
  requiredByPath: ReadonlyMap<string, ReadonlySet<string>> = contract.requiredByPath,
): boolean | null {
  let known = false;
  let required = false;
  for (const [path, fields] of requiredByPath) {
    if (path === apiPath || apiPath.startsWith(`${path}/`) || path.startsWith(`${apiPath}/`)) {
      known = true;
      if (fields.has(field)) required = true;
    }
  }
  if (!known) return null;
  return required;
}

// ── сторона консоли: что объявила форма ─────────────────────────────────────

interface Claim {
  rel: string;
  line: number;
  specIds: string[];
  label: string;
}

interface FormsRead {
  filesRead: number;
  itemsParsed: number;
  claims: Claim[];
}

/** Все `Form.Item`, объявившие обязательность, вместе с их подписью и ресурсом. */
function readForms(sources: Array<{ rel: string; raw: string }>): FormsRead {
  let itemsParsed = 0;
  const claims: Claim[] = [];

  for (const { rel, raw } of sources) {
    const src = stripComments(raw, { keepLines: true });
    const specIds = [
      ...new Set(
        [...src.matchAll(/<FormShell\b/g)]
          .map((m) => readOpeningTag(src, m.index))
          .map((tag) => (tag === null ? null : attrValue(tag, "specId")))
          .filter((v): v is string => v !== null),
      ),
    ];
    for (const m of src.matchAll(/<Form\.Item\b/g)) {
      const tag = readOpeningTag(src, m.index);
      if (tag === null) continue;
      itemsParsed++;
      if (!CLAIMS_REQUIRED.test(tag)) continue;
      const label = attrValue(tag, "label");
      if (label === null) continue;
      claims.push({ rel, line: lineOf(src, m.index), specIds, label });
    }
  }

  return { filesRead: sources.length, itemsParsed, claims };
}

/** Форма записи реестра, нужная этому файлу — не весь `ResourceSpec`. */
type NameRegistry = Record<string, Pick<(typeof REGISTRY)[string], "fields" | "apiPath"> | undefined>;

/**
 * Подпись поля `name` у ресурса — берётся из реестра, а не выписывается.
 *
 * `registry` — по умолчанию РЕАЛЬНЫЙ (см. `contractRequires` — та же причина).
 */
function nameLabelOf(specId: string, registry: NameRegistry = REGISTRY): string | null {
  const spec = registry[specId];
  const field = spec?.fields?.find((f) => f.name === "name");
  return field?.label ?? null;
}

/** Подпись формы называет поле `name` этого ресурса? */
function labelNamesField(labelExpr: string, fieldLabel: string): boolean {
  return (
    labelExpr.trim() === fieldLabel ||
    labelExpr.includes(`"${fieldLabel}"`) ||
    labelExpr.includes(`'${fieldLabel}'`) ||
    labelExpr.includes(`>${fieldLabel}<`)
  );
}

interface Verdict {
  findings: string[];
  /** Объявлений про поле `name`, сопоставленных с контрактом. */
  judged: number;
  /** Объявлений, которые гейт рассудить не смог (ресурс или контракт не найден). */
  unresolved: number;
}

/**
 * Реестр и контракт, которыми судит `adjudicate` — по умолчанию РЕАЛЬНЫЕ.
 * Инъекции нужен свой пакет: см. `contractRequires`/`nameLabelOf`.
 */
interface AdjudicateDeps {
  registry: NameRegistry;
  requiredByPath: ReadonlyMap<string, ReadonlySet<string>>;
}

function adjudicate(forms: FormsRead, deps?: AdjudicateDeps): Verdict {
  const registry = deps?.registry ?? REGISTRY;
  const requiredByPath = deps?.requiredByPath ?? contract.requiredByPath;
  const findings: string[] = [];
  let judged = 0;
  let unresolved = 0;

  for (const claim of forms.claims) {
    for (const specId of claim.specIds) {
      const fieldLabel = nameLabelOf(specId, registry);
      if (fieldLabel === null || !labelNamesField(claim.label, fieldLabel)) continue;
      const apiPath = registry[specId]?.apiPath;
      const requires = apiPath ? contractRequires(apiPath, "name", requiredByPath) : null;
      if (requires === null) {
        unresolved++;
        continue;
      }
      judged++;
      if (requires === false) {
        findings.push(
          `${claim.rel}:${claim.line} — подпись «${fieldLabel}» объявлена обязательной, но контракт ` +
            `владельца (${apiPath}) поле «name» обязательным не делает. Значок утверждает обязанность, ` +
            `которой нет: арендатор перестаёт различать по нему, что заполнить обязан`,
        );
      }
    }
  }

  return { findings, judged, unresolved };
}

// ── перепись дерева ──────────────────────────────────────────────────────────

// Перечень берётся у git (он знает состав дерева), но ЧИТАЕТСЯ рабочая копия:
// это разные вещи. Файл, удалённый в рабочей копии и ещё числящийся в индексе, —
// законное переходное состояние; гейт, падающий на нём, обвиняет автора в том,
// что тот снял мёртвый компонент, и заставляет коммитить ради зелёного прогона.
// Пропущенные считаются и НАЗЫВАЮТСЯ: «ноль находок» обязано быть отличимо от
// «ноль прочитанного».
const treeListed = execFileSync("git", ["ls-files", "*/src/**/*.tsx"], { cwd: UI_ROOT, encoding: "utf8" })
  .split("\n")
  .filter((rel) => rel.length > 0 && !rel.includes(".test."));

const treeMissing: string[] = [];
const treeSources = treeListed.flatMap((rel) => {
  const abs = join(UI_ROOT, rel);
  if (!existsSync(abs)) {
    treeMissing.push(rel);
    return [];
  }
  return [{ rel, raw: readFileSync(abs, "utf8") }];
});

const treeForms = readForms(treeSources);
const treeVerdict = adjudicate(treeForms);

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("источник обязательности непуст, и обязательность в нём вообще встречается", () => {
    // Пустой источник дал бы «ничего не обязательно» и объявил находкой КАЖДУЮ
    // звёздочку — то есть гейт лгал бы уверенно. Премиса ловит это ПЕРВОЙ, до
    // вердикта, и потому ослаблять её ради зелёного нельзя.
    expect(contract.pathsKnown).toBeGreaterThan(10);
    expect(contract.pathsWithRequired).toBeGreaterThan(0);
  });

  it("у перечня есть ПРЕДПОСЫЛКА: отказывающий код продукта на месте", () => {
    // Перечень выведен из поведения, и поведение это живёт в названных файлах.
    // Исчезнет любой — перечень станет описанием вчерашнего дерева, и краснеет
    // ИМЕННО эта проба, а не вердикт: незнание и молчание обязаны быть
    // различимы.
    expect(REQUIRED_BY_SERVER_SOURCES.length).toBeGreaterThan(0);
    // Находки собираются в перечень, а не утверждаются по одной: `expect` этого
    // прогонщика второго довода не принимает, и падение без имени файла
    // заставило бы читателя искать предмет вручную.
    const stale: string[] = [];
    for (const [rel, marker] of REQUIRED_BY_SERVER_SOURCES) {
      let text: string;
      if (isCorelibCoordinate(rel)) {
        // Координата общего фундамента: файл не прочитан → `readCorelibFile`
        // сам бросает отказ с названной координатой (модуль не извлечён в кэш
        // либо переехал внутри модуля) — тот же смысл, что у «файла нет», но
        // средствами кэша модулей, а не рабочей копии.
        try {
          text = readCorelibFile(REPO_ROOT, rel);
        } catch (err) {
          stale.push(err instanceof Error ? err.message : `${rel} — файл не прочитан`);
          continue;
        }
      } else {
        const abs = join(REPO_ROOT, rel);
        if (!existsSync(abs)) {
          stale.push(`${rel} — файла нет`);
          continue;
        }
        text = readFileSync(abs, "utf8");
      }
      if (!text.includes(marker)) stale.push(`${rel} — нет «${marker}»`);
    }
    expect(stale).toEqual([]);
  });

  it("формы консоли прочитаны, и объявления обязательности в них найдены", () => {
    expect(treeForms.filesRead).toBeGreaterThan(100);
    expect(treeForms.itemsParsed).toBeGreaterThan(50);
    expect(treeForms.claims.length).toBeGreaterThan(5);
  });

  it("перепись напечатана: рассужено, не рассужено, найдено", () => {
    // Число здесь не утверждается — оно печатается. Утверждение о нём было бы
    // ориентиром, который устаревает молча.
    // eslint-disable-next-line no-console
    console.log(
      `[#608] форм прочитано ${treeForms.filesRead} · Form.Item разобрано ${treeForms.itemsParsed} · ` +
        `объявили обязательность ${treeForms.claims.length} · из них про поле «name» рассужено ` +
        `${treeVerdict.judged}, не рассужено ${treeVerdict.unresolved} · находок ${treeVerdict.findings.length}` +
        // Пропущенные называются числом И поимённо: молчаливый пропуск сделал бы
        // «ноль находок» неотличимым от «ноль прочитанного».
        (treeMissing.length
          ? ` · числятся в индексе, но в рабочей копии их нет: ${treeMissing.length} (${treeMissing.join(", ")})`
          : " · пропущенных нет"),
    );
    // Раньше здесь утверждалось `judged > 0` — со снятыми IAM-путями (шапка
    // файла) это стало ЛОЖНЫМ утверждением о живом дереве, а не забытой
    // строкой: единственный путь, требующий «Имя» (`storageBackends`), формы
    // не несёт, а vpc/compute корректно НЕ отмечают «Имя» звёздочкой там, где
    // контракт этого не требует, — резолвить в «true» или «false» сегодня
    // нечего, и это ПРАВИЛЬНОЕ состояние, а не пробел разбора. Сигнал о том,
    // что пайплайн всё ещё встречает живые заявления консоли (а не молчит
    // из-за сломанного разбора), даёт `unresolved`: iam-формы («Группы»,
    // «Роли») дают его ненулевым именно потому, что гейт их ВИДИТ и честно не
    // берётся судить.
    expect(treeVerdict.unresolved).toBeGreaterThan(0);
  });
});

describe("звёздочка обязательности сходится с контрактом владельца", () => {
  it("ни одна форма не объявляет «Имя» обязательным вопреки контракту", () => {
    expect(treeVerdict.findings).toEqual([]);
  });
});

// ── способность упасть: инъекция в обе стороны ──────────────────────────────

describe("инъекция: гейт краснеет на дефекте и молчит на законном близнеце", () => {
  const latch = (raw: string) => adjudicate(readForms([{ rel: "synthetic.tsx", raw }]));

  // Синтетический ресурс для «требует=true»: со снятыми IAM-путями (см.
  // шапку файла) в РЕАЛЬНОМ дереве не осталось ни одного ресурса, у которого
  // одновременно (а) контракт требует «Имя» и (б) есть форма в консоли —
  // `storageBackends` требует, но формы не несёт (`storage-console-surface.test.ts`
  // намеренно её не показывает). Самопроверку дискриминатора «true» держит
  // теперь пара, не привязанная к факту о живом ресурсе: она не переживёт
  // редизайна формы `storageBackends` и не устареет молча, если чей-то другой
  // контракт передумает.
  const syntheticRequiredByPath = new Map([["/synthetic/v1/widgets", new Set(["name"])]]);
  const syntheticRegistry: NameRegistry = {
    widgets: { apiPath: "/synthetic/v1/widgets", fields: [{ name: "name", label: "Имя", type: "string" }] },
  };
  const syntheticDeps: AdjudicateDeps = { registry: syntheticRegistry, requiredByPath: syntheticRequiredByPath };

  it("контракт различает два ресурса — иначе дискриминатору нечего различать", () => {
    // Положительный контроль самого разбора контракта: если бы обе стороны
    // читались одинаково, инъекция ниже проходила бы по причине, не имеющей
    // отношения к предмету.
    expect(contractRequires("/vpc/v1/subnets", "name")).toBe(false);
    expect(contractRequires("/synthetic/v1/widgets", "name", syntheticRequiredByPath)).toBe(true);
  });

  it("ДЕФЕКТ: «Имя» со звёздочкой у ресурса, чей контракт её не требует — находка с координатой", () => {
    const verdict = latch(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Имя" required>\n    <Input />\n  </Form.Item>\n` +
        `</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
    expect(verdict.findings[0]).toContain("synthetic.tsx:2");
  });

  it("ДЕФЕКТ той же формы через выражение подписи — тоже находка", () => {
    // Первая перепись по `label="Имя"` нашла три места; ещё два несли подпись
    // выражением (`labelWithInfo("Имя", …)`) и в неё не попали. Радиус берётся
    // по механизму, а не по форме записи.
    const verdict = latch(
      `<FormShell specId="network-interfaces" mode="create">\n` +
        `  <Form.Item label={labelWithInfo("Имя", "Имя интерфейса в пределах проекта.")} required>\n` +
        `    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
  });

  it("БЛИЗНЕЦ: та же подпись без звёздочки — молчание, и объявление рассужено", () => {
    const verdict = latch(
      `<FormShell specId="subnets" mode="create">\n  <Form.Item label="Имя">\n    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
  });

  it("БЛИЗНЕЦ: «Имя» со звёздочкой там, где контракт её ТРЕБУЕТ — молчание, но объявление рассужено", () => {
    // Ключевой близнец: форма выглядит ровно как дефект, и молчит гейт не
    // потому, что не разобрал её, а потому, что контракт синтетического
    // ресурса объявляет `name` обязательным. Без проверки `judged` молчание
    // означало бы «не прочитал».
    const verdict = adjudicate(
      readForms([
        {
          rel: "synthetic.tsx",
          raw:
            `<FormShell specId="widgets" mode="create">\n` +
            `  <Form.Item label="Имя" name="name" required rules={[{ required: true }]}>\n` +
            `    <Input />\n  </Form.Item>\n</FormShell>\n`,
        },
      ]),
      syntheticDeps,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(1);
  });

  it("БЛИЗНЕЦ: звёздочка у ДРУГОГО поля — вне области гейта, и он это не скрывает", () => {
    // Гейт судит поле `name`. Обязательность «Сети» он не рассматривает вовсе —
    // и не объявляет её законной: она просто не попадает в счёт рассуженного.
    const verdict = latch(
      `<FormShell specId="subnets" mode="create">\n  <Form.Item label="Сеть" required>\n    <Select />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(0);
  });

  it("разбор тега переживает `>` внутри выражения подписи", () => {
    // Наивный поиск первого `>` обрезал бы тег на `<Space size={4}>` и потерял
    // `required` — гейт молчал бы на дефекте, который обязан находить.
    const verdict = latch(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label={<Space size={4}>Имя<Tooltip title="x" /></Space>} required>\n` +
        `    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
  });
});
