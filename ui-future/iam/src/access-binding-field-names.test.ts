// Контракт-замок AccessBinding: консоль читает у привязки доступа РОВНО те имена,
// которые сообщение ствола несёт, — и обосновывает это тем, что в стволе правда.
//
// Ground truth — proto/kacho/cloud/iam/v1/access_binding.proto. У снятого имени
// ДВА разных класса, и предикат, знающий только про один, пропускает второй:
//
//   1. ЗАХОРОНЕНО (`reserved` вместе с именем) — `condition_id`/`builtin_condition`
//      (теги 9/14) и `scope`/`scope_ref`/`target_ref`/`selector` (теги 15-18).
//      Имя выведено из оборота навсегда, номер не переиспользуется.
//   2. ПЕРЕИМЕНОВАНО — координата анкера была `resource_type`/`resource_id`, стала
//      `scope_type` (тег 5) / `scope_id` (тег 6) (redesign-2026 F7). Прежние имена
//      в `reserved` НЕ попали: в самом сообщении их просто нет, а на соседних RPC
//      того же домена (`ListAccessBindingsByScopeRequest`, `SubjectPrivilege`,
//      `ListAssignableRolesRequest`) они ЖИВЫ, помечены DEPRECATED и заполняются.
//
// Отсюда форма проверки: перечень запретного выводится не из `reserved`, а из
// НАБОРА ОБЪЯВЛЕННЫХ ПОЛЕЙ сообщения — тогда оба класса ловятся одним предикатом.
// Проверка на `reserved` остаётся дополнительным, более узким утверждением: она
// стережёт ОБОСНОВАНИЕ (комментарий не вправе называть захороненным то, что стволом
// не захоронено).
//
// Почему это тест, а не вкусовщина. Имя, которого в сообщении нет, сервер не
// заполняет никогда: чтение возвращает undefined молча, поэтому запасная ветка на
// нём не «подстрахует старый сервер» — она мертва по построению и при этом выглядит
// страховкой. Ровно так же читается заголовок формы, объявляющий тело запроса: он
// остаётся инструкцией для следующего контрибьютора, и если он называет снятое имя
// или лжёт про умолчание, следующий приведёт РАБОЧИЙ код к нерабочему описанию.
//
// Область: блок registerDetailExtension("access-bindings", …) — именно он читает
// сам ресурс AccessBinding. Таблицы привилегий выше читают SubjectPrivilege, где
// `scope`/`resource_type`/`resource_id` живы, — совпадение имён, разные референты,
// поэтому файл целиком под запрет не попадает.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const protoPath = path.resolve(here, "../../../proto/kacho/cloud/iam/v1/access_binding.proto");
const proto = readFileSync(protoPath, "utf8");

/** Тело `message <name> { … }` со счётом скобок (вложенные enum/oneof не рвут). */
const messageBody = (name: string): string => {
  const at = proto.indexOf(`message ${name} {`);
  expect(at).toBeGreaterThan(-1);
  const open = proto.indexOf("{", at);
  let depth = 0;
  for (let i = open; i < proto.length; i += 1) {
    if (proto[i] === "{") depth += 1;
    else if (proto[i] === "}") {
      depth -= 1;
      if (depth === 0) return proto.slice(open + 1, i);
    }
  }
  throw new Error(`message ${name}: не закрыт`);
};

const accessBindingBody = messageBody("AccessBinding");

/** Имена ОБЪЯВЛЕННЫХ полей сообщения (значения enum — SCREAMING, не совпадут). */
const declaredFields = (body: string): string[] => [
  ...new Set(
    [
      ...body.matchAll(
        /^[ \t]*(?:repeated[ \t]+)?[A-Za-z][A-Za-z0-9_.]*(?:<[^>]*>)?[ \t]+([a-z][a-z0-9_]*)[ \t]*=[ \t]*\d+[ \t]*[;[]/gm,
      ),
    ].map((m) => m[1]),
  ),
];

/** Имена из `reserved "a", "b";` — захоронения (первый класс). */
const retiredNames = (body: string): string[] => {
  const out = new Set<string>();
  for (const m of body.matchAll(/reserved\s+((?:"[a-z_]+"\s*,?\s*)+);/g)) {
    for (const n of m[1].matchAll(/"([a-z_]+)"/g)) out.add(n[1]);
  }
  return [...out];
};

const snakeToCamel = (s: string): string => s.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());

const extSource = readFileSync(path.join(here, "registerExtensions.tsx"), "utf8");

/** Блок одного registerDetailExtension — от его открытия до следующего. */
const extensionBlock = (specId: string): string => {
  const start = extSource.indexOf(`registerDetailExtension("${specId}", {`);
  expect(start).toBeGreaterThan(-1);
  const rest = extSource.slice(start + 1);
  const next = rest.indexOf("registerDetailExtension(");
  return next === -1 ? rest : rest.slice(0, next);
};

/**
 * Исполняемая часть блока: строчные комментарии вырезаны. Гейт обязан читать код,
 * а не текст, — иначе он краснеет на комментарии, который САМ ЖЕ и объясняет запрет
 * (и зеленеет, если запрет снят, но объяснение осталось).
 */
const codeOnly = (src: string): string => src.replace(/^[ \t]*\/\/.*$/gm, "");

/** Комментарии блока — отдельный предмет: они несут ОБОСНОВАНИЕ. */
const commentsOnly = (src: string): string => [...src.matchAll(/^[ \t]*\/\/(.*)$/gm)].map((m) => m[1].trim()).join(" ");

/** Ключи, которые код блока реально спрашивает у ответа сервера. */
const readKeys = (src: string): string[] => [
  ...new Set([...src.matchAll(/getByPath<[^>]*>\(\s*data\s*,\s*"([^"]+)"\s*\)/g)].map((m) => m[1])),
];

const formSource = readFileSync(
  path.join(here, "components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx"),
  "utf8",
);
/** Шапка файла — всё до первого import: она и есть объявление модели для читателя. */
const formHeader = formSource.slice(0, formSource.indexOf("\nimport "));

/** Ключевые слова proto — в обратных кавычках встречаются как слова, не как поля. */
const PROTO_KEYWORDS = new Set(["reserved", "message", "enum", "oneof", "repeated", "map", "string", "bool"]);

/**
 * Имена, объявленные комментарием ЗАХОРОНЕННЫМИ. Разбор по предложениям: слово
 * «захорон…» относится к именам своего предложения, а не ко всему абзацу.
 *
 * Отрицание («не захоронено, а переименовано») — не утверждение о захоронении, и
 * предложение с ним пропускается: иначе гейт запрещал бы РАЗБИРАТЬ разницу двух
 * классов, то есть ровно то объяснение, ради которого он и заведён.
 */
const claimedRetired = (comments: string): string[] => {
  const out = new Set<string>();
  for (const sentence of comments.split(/(?<=[.;])\s+/)) {
    if (!/захорон/i.test(sentence)) continue;
    // `\b` здесь неприменим: в JS граница слова — ASCII, и перед кириллическим
    // «не» она не срабатывает никогда (проба такого предиката была бы вечно
    // зелёной). Отсюда явная проверка предыдущего символа.
    if (/(^|[^а-яёa-z])не\s+захорон/i.test(sentence)) continue;
    for (const m of sentence.matchAll(/`([a-z][a-z0-9_]*)`/g)) {
      if (!PROTO_KEYWORDS.has(m[1])) out.add(m[1]);
    }
  }
  return [...out];
};

describe("AccessBinding — имена полей, которые консоль вправе читать", () => {
  const DECLARED = declaredFields(accessBindingBody);
  const RETIRED = retiredNames(accessBindingBody);
  const block = extensionBlock("access-bindings");

  it("объём осмотренного назван: сколько полей и захоронений прочитано из proto", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: если любой из
    // двух парсеров замолчит, все утверждения ниже станут вакуумными.
    expect(DECLARED).toEqual(
      expect.arrayContaining(["id", "role_id", "scope_type", "scope_id", "status", "target", "deletion_protection"]),
    );
    expect(DECLARED.length).toBeGreaterThanOrEqual(15);
    expect(RETIRED).toEqual(
      expect.arrayContaining(["condition_id", "builtin_condition", "scope", "scope_ref", "target_ref", "selector"]),
    );
  });

  it("два класса снятого имени различимы: захоронённое и переименованное", () => {
    // Захоронение — имя есть в `reserved` и нет среди полей.
    for (const name of RETIRED) expect(DECLARED).not.toContain(name);
    // Переименование — имени нет НИ среди полей, НИ в `reserved`. Именно поэтому
    // список запретного нельзя выводить из одного лишь `reserved`.
    for (const renamed of ["resource_type", "resource_id"]) {
      expect(DECLARED).not.toContain(renamed);
      expect(RETIRED).not.toContain(renamed);
    }
    // И контроль в обратную сторону: имя, в которое координата переехала, живо.
    expect(DECLARED).toContain("scope_type");
    expect(DECLARED).toContain("scope_id");
  });

  it("деталь привязки не спрашивает ни одного имени, которого в сообщении нет", () => {
    // Утверждение на ОБА класса разом: ключ обязан быть объявленным полем (в
    // snake- либо camel-проекции края). Возврат запасной ветки на `resource_id`,
    // `resource_type` или `scope` краснит именно здесь.
    const allowed = new Set([...DECLARED, ...DECLARED.map(snakeToCamel)]);
    const keys = readKeys(codeOnly(block));
    expect(keys.length).toBeGreaterThanOrEqual(8);
    expect(keys.filter((k) => !allowed.has(k))).toEqual([]);
  });

  it("предикат помечает чужое имя недопустимым (контроль в обратную сторону)", () => {
    // Без этого предыдущее утверждение зеленело бы на предикате «всё допустимо».
    const allowed = new Set([...DECLARED, ...DECLARED.map(snakeToCamel)]);
    for (const gone of ["resource_type", "resource_id", "scope", "scope_ref", "condition_id"]) {
      expect(allowed.has(gone)).toBe(false);
    }
    expect(readKeys('getByPath<string>(data, "resource_id") ?? ""')).toEqual(["resource_id"]);
    // И предикат обоснования: утверждение о захоронении ловится, отрицание — нет.
    expect(claimedRetired("`scope` у AccessBinding захоронено.")).toEqual(["scope"]);
    expect(claimedRetired("`resource_type` не захоронено, а переименовано.")).toEqual([]);
  });

  it("деталь привязки читает живые координаты анкера и цели", () => {
    const code = codeOnly(block);
    expect(code).toContain(`"scope_type"`);
    expect(code).toContain(`"scope_id"`);
    expect(code).toContain(`"target"`);
  });

  it("обоснование не называет захоронённым то, что ствол не захоранивал", () => {
    // Комментарий — утверждение о стволе наравне с кодом. Прежняя редакция
    // объявляла захоронённой всю прежнюю координату анкера; по стволу захоронено
    // из неё только `scope`, а пара имён переименована и на соседних RPC жива.
    const claims = claimedRetired(commentsOnly(block));
    expect(claims.length).toBeGreaterThanOrEqual(1);
    expect(claims.filter((n) => !RETIRED.includes(n))).toEqual([]);
  });

  it("таблицы привилегий субъекта продолжают читать живые поля SubjectPrivilege", () => {
    // Контроль по референту: у SubjectPrivilege scope/resource_type/resource_id
    // существуют и заполняются на каждом чтении, поэтому запрет их не касается.
    expect(extSource).toContain(`dataIndex: "scope"`);
    expect(extSource).toContain("row.resource_type");
  });
});

describe("AccessBindingCreateForm — шапка против собственного кода", () => {
  const RETIRED = retiredNames(accessBindingBody);

  it("шапка не называет ни одного захоронённого составного имени", () => {
    // Проверяются только СОСТАВНЫЕ имена (`scope_ref`, `condition_id`, …): голое
    // `scope` — вдобавок обычное слово прозы («пока scope не выбран»), и запрет на
    // него ловил бы русский текст, а не координату.
    const compound = RETIRED.filter((n) => n.includes("_"));
    expect(compound.length).toBeGreaterThan(0);
    for (const name of compound) expect(formHeader).not.toContain(name);
  });

  it("шапка не объявляет захоронённым то, что ствол не захоранивал", () => {
    const claims = claimedRetired(formHeader);
    expect(claims.filter((n) => !RETIRED.includes(n))).toEqual([]);
  });

  it("шапка называет каноничные имена тела запроса", () => {
    expect(formHeader).toContain("scope_type");
    expect(formHeader).toContain("scope_id");
    expect(formHeader).toContain("target");
  });

  it("шапка называет умолчание, которое форма преднабирает сама", () => {
    // Предпосылка читается из кода: форма действительно преднабирает арм target'а.
    // Сервер умолчания НЕ имеет (Create без target отвергается синхронно —
    // services/iam/internal/apps/kacho/api/access_binding/delta_input.go), поэтому
    // шапка обязана РАЗЛИЧИТЬ две разные вещи, а не отрицать одну из них.
    const preset = /_target_kind:\s*"([A-Za-z]+)"/.exec(formSource);
    expect(preset).not.toBeNull();
    expect(formHeader).toContain("_target_kind");
    expect(formHeader).toContain(preset![1]);
  });
});
