// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { ENTITIES, type EntityKey } from "@shared/lib/entity-names";
import { KNOWN_KINDS, kindLabel } from "@shared/lib/quota-view";

/**
 * Гейт: ВИТРИНА КВОТ НАЗЫВАЕТ РЕСУРС ТЕМ ЖЕ СЛОВОМ, ЧТО ВЕСЬ ОСТАЛЬНОЙ ПРОДУКТ (#1703).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ — ТРИ ПОЛОСЫ ОДНОГО МЕХАНИЗМА, КОТОРЫЕ НИКТО НЕ СВЕРЯЛ МЕЖДУ СОБОЙ
 *
 *  A. каталог видов у владельца величин (`services/iam/internal/domain/limit.go`,
 *     `countableKinds`) — что край вообще может назвать в ответе и в отказе;
 *  B. витрина квот консоли (`lib/quota-view.ts`, `KIND_LABELS`) — как это
 *     показано арендатору;
 *  C. словарь подписей сущностей (`lib/entity-names.ts`) — как тот же ресурс
 *     назван в меню, крошке, шапке карточки и сигнале об исходе мутации.
 *
 * У каждой полосы был свой гейт, и каждый был зелёным: #1605 держит, что дом
 * словаря B один и что отказ ходит именно в него; #1609/#1610 держат, что ни
 * один литерал консоли не называет сущность мимо C. Ни один не сравнивал B с C,
 * и расхождение приземлилось молча — ровно тот класс, о котором
 * `architecture.md` §«Параллельные полосы одного механизма обязаны сверяться
 * МЕЖДУ СОБОЙ»: обе полосы валидны по отдельности, неверна их РАЗНИЦА, и
 * решал её не человек, а случай.
 *
 * Замер на ветке до правки (предикаты — в теле задачи #1703): видов в каталоге
 * владельца 33, в витрине 31; подписей, разошедшихся со словарём, — 8; видов,
 * которые витрина не называет вовсе и потому показывает машинным именем, — 3;
 * подписей, у которых вида на сервере больше нет, — 1.
 *
 * Самый выразительный из восьми: витрина звала обработчик балансировщика
 * «Слушателями», при том что шапка словаря подписей перечисляет это самое слово
 * СРЕДИ ПРЕЖНИХ ЖЕРТВ, ради которых словарь и заведён, а шапка гейта #1605
 * приводит пару «на витрине „Обработчики“, в отказе „Слушатели“» как
 * иллюстрацию предотвращаемого класса. В дереве было наоборот.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЦЕНА ДЛЯ КЛИЕНТА — В ШАГАХ, А НЕ В ОЩУЩЕНИИ
 *
 * Витрина квот и отказ по пределу — единственное место, где арендатор узнаёт,
 * во что он упёрся. Прочитав «Слушатели» или «Служебные учётные записи», он
 * идёт искать раздел с таким именем: такого раздела в консоли нет ни одного, и
 * поиск по консоли по этому написанию не находит НИЧЕГО. Машинное имя
 * (`iam.user.credential`) не адресует даже к домену.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ — ЧЕТЫРЕ РАЗНЫЕ ВЕЩИ, НИ ОДНА НЕ ВЫВОДИТСЯ ИЗ ДРУГОЙ
 *
 *  1. B ⊇ A — каждый вид каталога витрина называет. Иначе клиент читает
 *     машинное имя, а `kindLabel` отдаёт незнакомое имя как есть НАМЕРЕННО (это
 *     страховка от вида, заведённого позже консоли), и потому молчит.
 *  2. B ⊆ A — у каждой подписи витрины есть вид на сервере. Подпись, которой
 *     больше нечего называть, — находка: она унаследует следующую слепую зону.
 *  3. B = C по слову — там, где у вида есть сущность в консоли, подпись взята
 *     из словаря дословно (вложенный вид — словарное слово плюс уточнение).
 *  4. B без машинных слов — ни одна подпись не содержит точечного имени вида.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ СВЕРКА, А НЕ ВЫВОД ПОДПИСИ ИЗ СЛОВАРЯ
 *
 * Вывод был бы сильнее (расхождение невозможно by construction) и отвергнут по
 * двум причинам, обе измеримые. Первая: не у каждого вида есть сущность в
 * консоли — удостоверения принципала показываются панелью, а не разделом, и
 * ресурсом реестра не являются; вывод потребовал бы завести сущность ради
 * подписи. Вторая: гейт #1605 требует, чтобы отображение «вид → русская
 * подпись» существовало объектным литералом в единственном доме и проверяет
 * ЕГО ЖЕ непустоту; вывод оставил бы там латинские значения, обесценив соседний
 * гейт и сузив его перепись с 33 видов до горстки. Сверка обеих полос дешевле и
 * не трогает уже посаженный контроль.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ГРАНИЦА НАЗВАНА ЧЕСТНО
 *
 * Гейт судит ОБЪЯВЛЕНИЯ трёх файлов, а не отрисованный экран. Что арендатор
 * действительно читает на витрине — предмет сквозной пробы браузером
 * (`e2e/specs/quotas.spec.ts`, `ui.md` правило 12): она берёт ответ края и
 * требует, чтобы КАЖДЫЙ названный им вид был показан человеческим словом, то
 * есть держит утверждение 1 на живом каталоге, а не на его копии в дереве.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/**
 * Каталог видов живёт у владельца величин и НЕ выводится из дерева консоли.
 *
 * Координата выписана, и она единственная выписанная. Вывести её поиском по
 * имени переменной значило бы сделать предикат равным предмету; тот же довод и
 * та же координата стоят у соседнего гейта продукта
 * (`internal/repohygiene/quotakindproducer_test.go`). Пропажа файла — отказ, а
 * не молчаливый ноль: перепись ниже требует непустого каталога.
 */
const CATALOGUE_FILE = path.resolve(consoleRoot, "..", "services/iam/internal/domain/limit.go");

/** Запись каталога: `{"vpc.network", CarrierProject}` либо `{"vpc.network.subnet", "vpc.network"}`. */
const CATALOGUE_ENTRY = /^\s*\{"([a-zA-Z][a-zA-Z.]*)",/gm;

export function kindsInCatalogue(source: string): string[] {
  const block = source.slice(source.indexOf("var countableKinds = []CountableKind{"));
  const end = block.indexOf("\n}");
  return [...block.slice(0, end === -1 ? undefined : end).matchAll(CATALOGUE_ENTRY)].map((m) => m[1]);
}

/**
 * Вид → сущность консоли, которой он считает штуки. `null` — сущности НЕТ, и это
 * решение, а не пропуск: причина названа строкой рядом.
 *
 * Перечень обязан покрывать каталог целиком: вид без записи роняет гейт, поэтому
 * завести вид молча нельзя. Запись `null` САМОИСТЕКАЕТ — заведут сущность с этим
 * ключом, и утверждение ниже потребует объяснить, почему подпись не из словаря.
 */
const KIND_ENTITY: Record<string, EntityKey | null> = {
  "vpc.network": "networks",
  "vpc.subnet": "subnets",
  "vpc.address": "addresses",
  "vpc.networkInterface": "network-interfaces",
  "vpc.securityGroup": "security-groups",
  "vpc.routeTable": "route-tables",
  "vpc.gateway": "gateways",
  "vpc.cidrGroup": "cidr-groups",
  "vpc.network.subnet": "subnets",
  "vpc.network.routeTable": "route-tables",
  "vpc.network.securityGroup": "security-groups",
  "iam.account": "accounts",
  "iam.project": "projects",
  "iam.user": "users",
  "iam.serviceAccount": "service-accounts",
  "iam.group": "groups",
  "iam.role": "roles",
  "iam.accessBinding": "access-bindings",
  // Удостоверение принципала разделом консоли не является: его показывает панель
  // на карточке пользователя и сервисного аккаунта, у него нет ни сегмента
  // адреса, ни записи реестра ресурсов — то есть нет и ключа словаря подписей.
  // Слово берётся у той же панели, которая его показывает («Создать токен»,
  // «Токен создан»), а не изобретается здесь.
  "iam.user.credential": null,
  "iam.serviceAccount.credential": null,
  "compute.instance": "instances",
  "compute.guestAccessKey": "guest-access-keys",
  "compute.placementGroup": "placement-groups",
  "storage.volumes": "volumes",
  "storage.snapshots": "snapshots",
  "storage.images": "images",
  "loadbalancer.networkLoadBalancers": "load-balancers",
  "loadbalancer.targetGroups": "target-groups",
  "loadbalancer.listeners": "listeners",
  "loadbalancer.networkLoadBalancers.listeners": "listeners",
  "registry.registries": "registries",
  "registry.repositories": "repositories",
  "registry.registries.repositories": "repositories",
};

/** Машинное имя вида: два и более сегмента через точку, латиницей. */
const MACHINE_NAME = /[a-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*)+/;

/** Вложенный вид — три и более сегмента; его подпись несёт уточнение носителя. */
const isNested = (kind: string): boolean => kind.split(".").length >= 3;

const catalogue = kindsInCatalogue(readFileSync(CATALOGUE_FILE, "utf8"));
const shown = [...KNOWN_KINDS];

process.stdout.write(
  `\n  словарь видов квот: в каталоге владельца ${catalogue.length}, на витрине ${shown.length}, ` +
    `сущностей в словаре подписей ${Object.keys(ENTITIES).length}, ` +
    `соответствий объявлено ${Object.keys(KIND_ENTITY).length} ` +
    `(из них «сущности нет» ${Object.values(KIND_ENTITY).filter((v) => v === null).length})\n\n`,
);

describe("витрина квот называет ресурс словом продукта (#1703)", () => {
  it("перепись непуста: пустой обход — отказ, а не зелёное", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного». Пропал файл
    // каталога, переехало объявление, опустел словарь — здесь это отказ.
    expect(catalogue.length).toBeGreaterThan(0);
    expect(shown.length).toBeGreaterThan(0);
    expect(Object.keys(ENTITIES).length).toBeGreaterThan(0);
  });

  it("каждый вид каталога витрина называет — иначе клиент читает машинное имя", () => {
    expect(catalogue.filter((k) => !shown.includes(k))).toEqual([]);
  });

  it("у каждой подписи витрины есть вид на сервере — подпись без предмета самоистекает", () => {
    expect(shown.filter((k) => !catalogue.includes(k))).toEqual([]);
  });

  it("соответствие объявлено для каждого вида — вид не заводится молча", () => {
    expect(catalogue.filter((k) => !(k in KIND_ENTITY))).toEqual([]);
  });

  it("«сущности нет» истекает само: появился ключ в словаре — запись стала находкой", () => {
    const stale = Object.entries(KIND_ENTITY)
      .filter(([kind, key]) => key === null && (kind.split(".").at(-1) ?? "") in ENTITIES)
      .map(([kind]) => kind);
    expect(stale).toEqual([]);
  });

  it("подпись взята из словаря сущностей дословно", () => {
    const wrong: string[] = [];
    for (const kind of shown) {
      const key = KIND_ENTITY[kind];
      if (key == null) continue;
      const dictionary = ENTITIES[key].plural;
      const label = kindLabel(kind);
      const ok = isNested(kind) ? label.startsWith(`${dictionary} `) : label === dictionary;
      if (!ok) wrong.push(`${kind}: витрина «${label}» · словарь «${dictionary}»`);
    }
    expect(wrong).toEqual([]);
  });

  it("ни одна подпись не показывает машинное имя вида", () => {
    // Положительный контроль к утверждению выше: подпись, СОВПАВШАЯ со словарём,
    // ещё не доказывает, что словарь не пуст, а подпись у вида без сущности не
    // проверяется первым утверждением вовсе — её держит только это.
    expect(shown.filter((k) => MACHINE_NAME.test(kindLabel(k)))).toEqual([]);
  });

  it("незнакомый вид по-прежнему назван своим именем — каталог растёт на сервере", () => {
    // Утверждения выше не должны превратить страховку в запрет: вид, заведённый
    // на сервере позже консоли, обязан быть показан, а не спрятан.
    expect(kindLabel("future.widget")).toBe("future.widget");
  });

  it("распознаватель каталога читает записи и молчит на прозе о них", () => {
    // ИНЪЕКЦИЯ — настоящим каталогом.
    expect(
      kindsInCatalogue(
        'var countableKinds = []CountableKind{\n\t{"vpc.network", CarrierProject},\n\t{"vpc.network.subnet", "vpc.network"},\n}\n',
      ),
    ).toEqual(["vpc.network", "vpc.network.subnet"]);

    // ЗАКОННЫЕ БЛИЗНЕЦЫ.
    // Снятая запись живёт в каталоге комментарием — и комментарий не запись.
    expect(
      kindsInCatalogue(
        'var countableKinds = []CountableKind{\n\t// Здесь стоял {"vpc.subnet.networkInterface", "vpc.subnet"} — снят.\n\t{"vpc.network", CarrierProject},\n}\n',
      ),
    ).toEqual(["vpc.network"]);
    // Соседний перечень той же формы за пределами объявления не читается.
    expect(
      kindsInCatalogue(
        'var countableKinds = []CountableKind{\n\t{"vpc.network", CarrierProject},\n}\n\nvar other = []Pair{\n\t{"vpc.ghost", X},\n}\n',
      ),
    ).toEqual(["vpc.network"]);
  });
});
