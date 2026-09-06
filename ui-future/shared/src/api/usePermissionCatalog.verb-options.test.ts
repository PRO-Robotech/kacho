// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// usePermissionCatalog.verb-options.test.ts — выпадающий список глаголов берётся
// У РЕСУРСА, а не из общего пересечения (#1128).
//
// # Предмет
//
// `closed_verbs` каталога — ПЕРЕСЕЧЕНИЕ наборов всех типов, и список строился из
// него. Пока набор глаголов был платформенной константой, различие не
// наблюдалось; с набором, ставшим атрибутом ТИПА, оно наблюдается в обе стороны:
//
//   - расширение у одного типа не доходило до него самого — глагол энфорсился,
//     но не предлагался (`addTargets`/`removeTargets` у групп целей, `create` у
//     реестров);
//   - сужение у одного типа вынимало глагол из списка у ВСЕХ остальных.
//
// # Что здесь утверждается
//
// Обе стороны, плюс поведение на нескольких выбранных ресурсах: правило несёт
// ОДИН модуль и НЕСКОЛЬКО типов, поэтому предлагать надо то, что даёт каждый из
// них, — иначе глагол, выбранный человеком, молча не материализуется на части
// названных типов.
//
// # Откуда берутся наборы фикстуры (#1291)
//
// Прежняя редакция ВЫПИСЫВАЛА их литералом и обещала комментарием, что «наборы
// взяты такими же, какими их отдаёт backend». Обещание не было ничем подкреплено:
// фикстура сверялась сама с собой, поэтому расхождение с краем было невидимо
// by construction — и накопилось оно ДВУМЯ независимыми ходами, ни один из
// которых проба не заметила:
//
//   - #1189 снял `v_delete` с `iam_user`. Ложными стали сразу два утверждения:
//     набор `iam.user` (стал `[get list]`) и пересечение `closed_verbs`
//     (тоже `[get list]` — `delete` держался последним типом);
//   - раньше того rbac-2026 P3/D-6 сделал `project` ГЛАГОЛЬНЫМ, отчего строка
//     `iam.project` с `has_verb_relations: false` перестала описывать хоть
//     что-то живое.
//
// Два расхождения из разных линий, и проба оставалась зелёной на обоих: она
// сверяла литерал с самим собой.
//
// Теперь набор каждой строки СПРАШИВАЕТСЯ у канонической модели прав по имени
// типа, который эта строка моделирует, — то есть у того же источника, из которого
// его берёт край. Цепочка источника названа, а не подразумевается:
//
//   fga_model.fga  ──(безусловный гейт дрейфа
//                    services/iam/internal/authzmap/fga_model_drift_test.go)──▶
//   authzmap.typeVerbRelations ──▶ VerbsOfType ──▶ CatalogResource.verbs ──▶ край
//
// Модель — канон, таблица типов iam — её зеркало, поэтому спрашивать надо модель.
// Читать её с диска приходится по той же причине, по какой это уже делает
// ui-future/iam/src/access-binding-field-names.test.tsx с `.proto`: контракт, который проба
// исполнить не может, и другого способа спросить у него нет.
//
// Ожидаемые значения ниже остаются ЛИТЕРАЛАМИ. Это не забывчивость, а условие
// работы: сверка фикстуры с её собственным источником зеленела бы при любом
// сужении — форма проверки без содержания. Разделение ролей даёт то, ради чего
// правка делалась: тип сменил набор ⇒ фикстура поехала за ним ⇒ литеральное
// ожидание покраснело С КООРДИНАТОЙ.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { verbOptions, WILDCARD } from "./usePermissionCatalog";
import type { PermissionCatalog } from "./iam";

const here = path.dirname(fileURLToPath(import.meta.url));
const MODEL_PATH = path.resolve(here, "../../../../proto/kaname/cloud/iam/v1/fga_model.fga");
const model = readFileSync(MODEL_PATH, "utf8").split("\n");

/**
 * Глаголы, которые тип ОБЪЯВЛЯЕТ в канонической модели, в порядке объявления.
 *
 * Разбор судит ОБЪЯВЛЕНИЕ, а не текст: строка-комментарий (`#`) пропускается —
 * иначе разбор нашёл бы глагол в объяснении того, что этот глагол СНЯТ, и снятое
 * продолжало бы «объявляться». Блок типа кончается следующей конструкцией
 * верхнего уровня (`type` либо `condition`).
 *
 * Пустой набор и неизвестный тип — ОТКАЗ, а не пустой список: молчаливый ноль
 * здесь неотличим от «тип не объявляет глаголов», и опечатка в имени тихо
 * обнулила бы строку фикстуры вместо того, чтобы её уронить.
 */
function declaredVerbs(fgaType: string): string[] {
  const at = model.findIndex((l) => l.trimEnd() === `type ${fgaType}`);
  if (at < 0) {
    throw new Error(`fga_model.fga: типа '${fgaType}' нет — фикстура называет тип, которого модель не объявляет`);
  }
  const verbs: string[] = [];
  for (let i = at + 1; i < model.length; i += 1) {
    if (/^(type|condition)\s/.test(model[i])) break;
    const body = model[i].trim();
    if (body.startsWith("#")) continue;
    const m = /^define\s+v_([a-z0-9]+)\s*:/.exec(body);
    if (m) verbs.push(m[1]);
  }
  if (verbs.length === 0) {
    throw new Error(`fga_model.fga: тип '${fgaType}' не объявляет ни одного v_* — у фикстуры нет источника набора`);
  }
  return verbs;
}

/** Все глагольные типы модели: имя → набор. Основание для пересечения. */
function allVerbBearingTypes(): Map<string, string[]> {
  const out = new Map<string, string[]>();
  let current: string | null = null;
  for (const line of model) {
    const t = /^type (\S+)\s*$/.exec(line);
    if (t) {
      current = t[1];
      continue;
    }
    if (/^condition\s/.test(line)) {
      current = null;
      continue;
    }
    if (!current) continue;
    const body = line.trim();
    if (body.startsWith("#")) continue;
    const m = /^define\s+v_([a-z0-9]+)\s*:/.exec(body);
    if (m) out.set(current, [...(out.get(current) ?? []), m[1]]);
  }
  return out;
}

/**
 * `closed_verbs` — ПЕРЕСЕЧЕНИЕ наборов всех глагольных типов, ровно та величина,
 * которую край проецирует этим полем (`authzmap.CommonVerbVocabulary`).
 *
 * Пересечение считается по МОДЕЛИ, и это законно потому, что множество глагольных
 * типов модели и множество ключей таблицы каталога совпадают ТОЧНО — их держит тот
 * же безусловный гейт дрейфа. Порядок берётся из объявления первого типа: он
 * канонический (`get`/`list`/… — старшинство показа), и переписывать его здесь
 * второй копией значило бы завести два места об одном предмете.
 */
function commonVerbs(): string[] {
  const sets = [...allVerbBearingTypes().values()];
  if (sets.length === 0) throw new Error("fga_model.fga: глагольных типов не найдено — разбор модели пуст");
  const [first, ...rest] = sets;
  return first.filter((v) => rest.every((s) => s.includes(v)));
}

// catalog — форма ответа края, урезанная до предмета пробы: пять строк вместо
// всего каталога. Наборы и пересечение — из модели (см. шапку); из литералов
// осталось лишь то, что фикстура выбирает САМА: какие типы назвать.
const catalog: PermissionCatalog = {
  closed_verbs: commonVerbs(),
  wildcard_policy: { verb_wildcard_allowed_custom: true, module_resource_wildcard_system_only: true },
  modules: [
    {
      module: "vpc",
      resources: [
        { resource: "network", has_verb_relations: true, verbs: declaredVerbs("vpc_network") },
        { resource: "subnet", has_verb_relations: true, verbs: declaredVerbs("vpc_subnet") },
      ],
    },
    {
      module: "loadbalancer",
      resources: [
        { resource: "targetGroups", has_verb_relations: true, verbs: declaredVerbs("nlb_target_group") },
      ],
    },
    {
      module: "iam",
      resources: [
        { resource: "user", has_verb_relations: true, verbs: declaredVerbs("iam_user") },
        // ЭТА строка модель НЕ зеркалит, и это сказано вслух. Неглагольный ресурс
        // контрактом представим (`has_verb_relations` — поле ответа, `false` —
        // его законное значение), но ни один из 27 типов каталога сегодня им не
        // является: `iam.project` в жизни глагольный. Обещать здесь соответствие
        // краю было бы ровно тем, что чинит #1291, — поэтому строка объявлена
        // ФОРМОЙ, которую разбирает `verbOptions`, а не снимком чужого состояния.
        { resource: "project", has_verb_relations: false, verbs: [] },
      ],
    },
  ],
};

// без `*` — сравнивать состав удобнее без хвоста политики подстановки.
const plain = (module: string, resources: string[]): string[] =>
  verbOptions(catalog, false, module, resources).filter((v) => v !== WILDCARD);

describe("источник наборов фикстуры — каноническая модель, а не сама фикстура", () => {
  it("модель прочитана: глагольных типов много, наборы непусты", () => {
    // Перепись объёма: «ноль находок» обязано быть отличимо от «ноль
    // прочитанного». Пустой разбор означал бы, что все наборы ниже — пустые
    // списки, и КАЖДОЕ утверждение пробы стало бы вакуумным.
    const types = allVerbBearingTypes();
    expect(types.size).toBeGreaterThan(20);
    expect(declaredVerbs("vpc_network").length).toBeGreaterThan(0);
  });

  it("неизвестный тип — ОТКАЗ, а не пустой набор", () => {
    // Опечатка в имени типа обязана ронять пробу, а не тихо обнулять строку
    // фикстуры: обнулённая строка зеленеет на «ресурс ничего не предлагает».
    expect(() => declaredVerbs("vpc_netwrok")).toThrow(/типа 'vpc_netwrok' нет/);
  });

  it("разбор судит объявление, а не текст: комментарий глаголом не считается", () => {
    // У `iam_user` глаголы СНЯТЫ (#1128, #1189), и модель объясняет это
    // комментариями, где `v_update` и `v_delete` названы поимённо. Разбор по
    // подстроке вернул бы их обратно — и фикстура снова разошлась бы с краем,
    // на этот раз молча и в обратную сторону.
    expect(declaredVerbs("iam_user")).not.toContain("update");
    expect(declaredVerbs("iam_user")).not.toContain("delete");
  });
});

describe("verbOptions — словарь глаголов по ресурсу", () => {
  it("ресурс с ОБЫЧНЫМ набором предлагает свои четыре глагола", () => {
    expect(plain("vpc", ["network"])).toEqual(["get", "list", "update", "delete"]);
  });

  it("ресурс с РАСШИРЕННЫМ набором предлагает и свои собственные глаголы", () => {
    // Прежде их не предлагал никто: в пересечение они не входили by construction.
    expect(plain("loadbalancer", ["targetGroups"])).toEqual([
      "get",
      "list",
      "update",
      "delete",
      "addtargets",
      "removetargets",
    ]);
  });

  it("СУЖЕННЫЙ ресурс не предлагает снятых глаголов", () => {
    // `iam.user` — единственный осознанно суженный ресурс каталога: у строки
    // личности сняты ОБА распоряжающихся глагола (`v_update` #1128,
    // `v_delete` #1189), осталось чтение.
    expect(plain("iam", ["user"])).toEqual(["get", "list"]);
  });

  it("сужение у одного ресурса НЕ отнимает глаголы у соседа", () => {
    // Предмет #1128 целиком: та же правка, тот же каталог, соседний тип.
    expect(plain("vpc", ["subnet"])).toContain("update");
    expect(plain("vpc", ["subnet"])).toContain("delete");
    expect(plain("iam", ["user"])).not.toContain("update");
    expect(plain("iam", ["user"])).not.toContain("delete");
  });

  it("несколько выбранных типов дают ПЕРЕСЕЧЕНИЕ их наборов", () => {
    // Глагол, которого нет у одного из названных типов, на нём не материализуется
    // — предлагать его значило бы обещать право, которого правило не даст.
    expect(plain("vpc", ["network", "subnet"])).toEqual(["get", "list", "update", "delete"]);
    expect(plain("loadbalancer", ["targetGroups"])).toContain("addtargets");
  });

  it("порядок — тот, в котором глаголы пришли от края", () => {
    // Порядок показа — часть контракта поля; UI его не пересортировывает.
    // По алфавиту вышло бы `addtargets` первым — то есть утверждение различает.
    expect(plain("loadbalancer", ["targetGroups"])[0]).toBe("get");
    expect(plain("loadbalancer", ["targetGroups"]).slice(-2)).toEqual(["addtargets", "removetargets"]);
  });

  it("тип без глаголов (ярусный предок) не предлагает ничего", () => {
    expect(plain("iam", ["project"])).toEqual([]);
  });

  it("ресурс не выбран — предлагается общий набор модуля", () => {
    // Пока тип не назван, обещать глагол конкретного типа не на чем; общее —
    // единственный честный ответ.
    expect(plain("vpc", [])).toEqual(["get", "list", "update", "delete"]);
  });

  it("модуль не выбран — общий словарь каталога", () => {
    // Пересечение наборов ВСЕХ типов платформы. Оно сузилось до чтения, когда
    // `iam_user` лишился `v_delete` (#1189), — и это верное поведение поля:
    // оно объявлено общим для всех ресурсов, а не перечнем всех глаголов.
    expect(plain("", [])).toEqual(["get", "list"]);
  });

  it("verb-`*` добавляется по политике каталога и в system-роли", () => {
    expect(verbOptions(catalog, false, "vpc", ["network"])).toContain(WILDCARD);
    expect(verbOptions(catalog, true, "vpc", ["network"])).toContain(WILDCARD);
    const noWildcard: PermissionCatalog = {
      ...catalog,
      wildcard_policy: { verb_wildcard_allowed_custom: false, module_resource_wildcard_system_only: true },
    };
    expect(verbOptions(noWildcard, false, "vpc", ["network"])).not.toContain(WILDCARD);
    // В system-роли `*` доступен всегда (seed-path).
    expect(verbOptions(noWildcard, true, "vpc", ["network"])).toContain(WILDCARD);
  });

  it("старый край без поля `verbs` не оставляет редактор пустым", () => {
    // Совместимость названа, а не подразумевается: глагольный ресурс, чей набор
    // край не прислал, откатывается на общий словарь. Пустой список здесь означал
    // бы «правило нельзя дописать», то есть поломку редактора на старом крае.
    //
    // Каталог здесь СВОЙ и намеренно НЕ из модели: он моделирует край ПРОШЛОЙ
    // версии, у которого поля `verbs` не было вовсе. Спрашивать сегодняшнюю
    // модель о вчерашнем крае не у чего.
    const legacy: PermissionCatalog = {
      closed_verbs: ["get", "list", "update", "delete"],
      wildcard_policy: { verb_wildcard_allowed_custom: true, module_resource_wildcard_system_only: true },
      modules: [{ module: "vpc", resources: [{ resource: "network", has_verb_relations: true }] }],
    };
    expect(verbOptions(legacy, false, "vpc", ["network"]).filter((v) => v !== WILDCARD)).toEqual([
      "get",
      "list",
      "update",
      "delete",
    ]);
  });

  it("каталога нет вовсе — список пуст, а не выдуман", () => {
    expect(verbOptions(undefined, false, "vpc", ["network"]).filter((v) => v !== WILDCARD)).toEqual([]);
  });
});
