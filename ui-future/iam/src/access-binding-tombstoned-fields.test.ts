// Контракт-замок AccessBinding: имена, которые ствол ЗАХОРОНИЛ, не читаются и не
// обещаются на поверхности iam-консоли.
//
// Ground truth — proto/kacho/cloud/iam/v1/access_binding.proto: сообщение несёт
// `reserved "condition_id", "builtin_condition"` (теги 9/14) и `reserved "scope",
// "scope_ref", "target_ref", "selector"` (теги 15-18). Анкер гранта выражен парой
// `scope_type` (dotted `iam.cluster|iam.account|iam.project`) + `scope_id`, а на
// какие объекты под анкером грант распространяется — `target` (тег 22).
//
// Почему это тест, а не вкусовщина. Захороненное имя сервер не заполняет НИКОГДА:
// чтение возвращает undefined молча, поэтому запасная ветка на нём не «подстрахует
// старый сервер» — она мертва по построению и при этом выглядит рабочей. Ровно так
// же читается заголовок формы, объявляющий тело запроса: он остаётся инструкцией
// для следующего контрибьютора, и если он называет снятое имя, следующий приведёт
// РАБОЧИЙ код к нерабочему описанию.
//
// Предикат разбирает попадания ПО РЕФЕРЕНТУ: `scope`/`resource_type`/`resource_id`
// живы у SubjectPrivilege (access_binding_service.proto, поля 4/5/6 — DEPRECATED,
// но заполняются на каждом чтении), поэтому таблицы привилегий их читать вправе.
// Захоронены они только у самого AccessBinding — значит проверяется РОВНО блок
// registerDetailExtension("access-bindings", …), а не файл целиком.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const protoPath = path.resolve(here, "../../../proto/kacho/cloud/iam/v1/access_binding.proto");
const proto = readFileSync(protoPath, "utf8");

/** Имена из `reserved "a", "b";` сообщения AccessBinding. */
const retiredNames = (): string[] => {
  const body = proto.slice(proto.indexOf("message AccessBinding {"));
  const out = new Set<string>();
  for (const m of body.matchAll(/reserved\s+((?:"[a-z_]+"\s*,?\s*)+);/g)) {
    for (const n of m[1].matchAll(/"([a-z_]+)"/g)) out.add(n[1]);
  }
  return [...out];
};

const extSource = readFileSync(path.join(here, "registerExtensions.tsx"), "utf8");

/** Блок одного registerDetailExtension — от его открытия до следующего. */
const extensionBlock = (specId: string): string => {
  const start = extSource.indexOf(`registerDetailExtension("${specId}", {`);
  expect(start).toBeGreaterThan(-1);
  const rest = extSource.slice(start + 1);
  const next = rest.indexOf("registerDetailExtension(");
  return next === -1 ? rest : rest.slice(0, next);
};

const formSource = readFileSync(
  path.join(here, "components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx"),
  "utf8",
);
/** Шапка файла — всё до первого import: она и есть объявление модели для читателя. */
const formHeader = formSource.slice(0, formSource.indexOf("\nimport "));

describe("AccessBinding — захороненные имена ствола", () => {
  const RETIRED = retiredNames();

  it("читает список захоронений из proto, а не из памяти", () => {
    // Объём осмотренного: если парсер перестанет находить `reserved`, «ноль
    // находок» ниже станет неотличим от «ноль прочитанного» — поэтому предикат
    // сначала доказывает, что он вообще что-то распарсил.
    expect(RETIRED).toEqual(
      expect.arrayContaining(["condition_id", "builtin_condition", "scope", "scope_ref", "target_ref", "selector"]),
    );
    expect(proto).toContain("string scope_type = 5;");
    expect(proto).toContain("string scope_id = 6;");
    expect(proto).toContain("AccessTarget target = 22;");
  });

  it("деталь привязки не читает ни одного захороненного имени", () => {
    const block = extensionBlock("access-bindings");
    for (const name of RETIRED) {
      expect(block).not.toContain(`"${name}"`);
    }
  });

  it("деталь привязки читает живые координаты (контроль в обратную сторону)", () => {
    // Без этого утверждения предыдущее зеленело бы и на пустом блоке.
    const block = extensionBlock("access-bindings");
    expect(block).toContain(`"scope_type"`);
    expect(block).toContain(`"scope_id"`);
    expect(block).toContain(`"target"`);
  });

  it("таблицы привилегий субъекта продолжают читать живые поля SubjectPrivilege", () => {
    // Контроль по референту: у SubjectPrivilege scope/resource_type/resource_id
    // существуют и заполняются, поэтому запрет их не касается.
    expect(extSource).toContain(`dataIndex: "scope"`);
    expect(extSource).toContain("row.resource_type");
  });

  it("шапка формы создания описывает то тело запроса, которое форма шлёт", () => {
    // В шапке проверяются только СОСТАВНЫЕ захороненные имена (`scope_ref`,
    // `condition_id`, …): голое `scope` — вдобавок обычное слово прозы («пока
    // scope не выбран»), и запрет на него ловил бы русский текст, а не координату.
    // Составное имя с подчёркиванием координатой быть больше нечем.
    const compound = RETIRED.filter((n) => n.includes("_"));
    expect(compound.length).toBeGreaterThan(0);
    for (const name of compound) {
      expect(formHeader).not.toContain(name);
    }
    expect(formHeader).toContain("scope_type");
    expect(formHeader).toContain("scope_id");
    expect(formHeader).toContain("target");
  });
});
