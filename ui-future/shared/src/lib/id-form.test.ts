// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { STREAM_SUBJECTS } from "./subscription/subjects";
import { isPlatformId } from "./id-form";

/**
 * Проверка сегмента адреса, пришедшего из ленты уведомлений (приёмка NTF-6 Р7,
 * замысел З12). Предмет — ОБЕ стороны алфавита сразу: законный id каждого вида,
 * который лента может назвать, принимается; вход с разметкой пути, запроса,
 * якоря, кодирования, с заглавной, пустой и длиннее предела — отвергается.
 * Односторонняя проба зеленела бы на функции `() => true` (лента уводила бы
 * человека по любому адресу) либо на `() => false` (ни одной ссылки в ленте).
 *
 * Законные id выписаны ПО ФОРМЕ ПЛАТФОРМЫ (`api-conventions.md` `api-id-newid`,
 * `api-hyphen-prefix`): приставка вида, дефис, 17 знаков crockford-base32 в
 * нижнем регистре; у реестра — слитная прежняя форма, которой он чеканит id
 * сегодня. Перечень сверяется с `STREAM_SUBJECTS` в обе стороны: вид, который
 * словарь назвал, а проба нет, роняет её — новый вид не проходит мимо проверки
 * молча.
 */

const BODY = "0123456789abcdefg"; // 17 знаков алфавита crockford-base32

const LEGAL_ID_BY_SPEC: Readonly<Record<string, string>> = {
  networks: `net-${BODY}`,
  subnets: `sub-${BODY}`,
  addresses: `adr-${BODY}`,
  gateways: `gtw-${BODY}`,
  "route-tables": `rtb-${BODY}`,
  "security-groups": `sgr-${BODY}`,
  "network-interfaces": `nic-${BODY}`,
  "cidr-groups": `cdg-${BODY}`,
  // Пул адресов чеканится слитной формой (`ids.NewID(ids.PrefixAddressPool)`).
  "address-pools": `apl${BODY}`,
  "compute-instances": `ins-${BODY}`,
  "placement-groups": `plg-${BODY}`,
  "guest-access-keys": `gak-${BODY}`,
  "load-balancers": `nlb-${BODY}`,
  listeners: `lst-${BODY}`,
  "target-groups": `tgr-${BODY}`,
  volumes: `vol-${BODY}`,
  snapshots: `snp-${BODY}`,
  images: `img-${BODY}`,
  registries: `reg${BODY}`,
  accounts: `acc-${BODY}`,
  projects: `prj-${BODY}`,
  users: `usr-${BODY}`,
  "service-accounts": `sva-${BODY}`,
  groups: `grp-${BODY}`,
  roles: `rol-${BODY}`,
  "access-bindings": `acb-${BODY}`,
};

/**
 * Виды ленты, чей идентификатор в журнале — НЕ id платформы, и форма, которой
 * владелец его пишет.
 *
 * У репозитория поля `id` нет вовсе (`registry.proto`, message Repository): он
 * адресуется парой «реестр + имя», и журнал реестра пишет идентификатор строки
 * `<registry_id>/<repo>` (`services/registry/internal/subscriptionjournal`,
 * шапка). Коса в нём законна — грамматика имени OCI её допускает, — поэтому
 * одним сегментом адреса карточки он не является, и строка ленты обязана
 * показать его текстом без ссылки. Выписать репозиторию «законный id» в
 * перечень выше значило бы утверждать форму, которой владелец не чеканит.
 *
 * Ведомость сверяется с `STREAM_SUBJECTS` вместе с перечнем выше: вид обязан
 * стоять ровно в одном из двух.
 */
const NON_SEGMENT_ID_BY_SPEC: Readonly<Record<string, string>> = {
  repositories: `reg${BODY}/library/app`,
};

describe("isPlatformId — законный id каждого вида ленты принимается", () => {
  test("перечень законных id покрывает ровно виды STREAM_SUBJECTS", () => {
    const specs = Object.keys(STREAM_SUBJECTS).sort();
    // Пустой словарь сделал бы пробу ниже пустой и зелёной.
    expect(specs.length).toBeGreaterThan(0);
    const legal = Object.keys(LEGAL_ID_BY_SPEC);
    const nonSegment = Object.keys(NON_SEGMENT_ID_BY_SPEC);
    // Вид — ровно в одном из двух перечней: в обоих сразу он утверждал бы
    // противоположное о себе же.
    expect(legal.filter((spec) => nonSegment.includes(spec))).toEqual([]);
    expect([...legal, ...nonSegment].sort()).toEqual(specs);
  });

  test.each(Object.entries(NON_SEGMENT_ID_BY_SPEC))(
    "%s: идентификатор журнала %s ссылкой не становится",
    (_spec, id) => {
      expect(isPlatformId(id)).toBe(false);
    },
  );

  test.each(Object.entries(LEGAL_ID_BY_SPEC))("%s: %s принимается", (_spec, id) => {
    expect(isPlatformId(id)).toBe(true);
  });

  test("граница длины: 64 знака принимается, первый знак — цифра принимается", () => {
    expect(isPlatformId("a".repeat(64))).toBe(true);
    expect(isPlatformId(`0${"b".repeat(63)}`)).toBe(true);
  });
});

describe("isPlatformId — вход вне алфавита id отвергается", () => {
  // Каждый отвергаемый вход меняет РОВНО ОДИН факт против законного близнеца
  // `net-0123456789abcdefg` (приёмка §6): знак разметки, регистр, пустота, длина.
  const twin = `net-${BODY}`;
  const rejected: ReadonlyArray<readonly [string, string]> = [
    ["косая черта (разметка пути)", `net-0123/456789abcdefg`],
    ["«..» (выход из сегмента)", `..`],
    ["«?» (начало запроса)", `net-0123?456789abcdefg`],
    ["«#» (начало якоря)", `net-0123#456789abcdefg`],
    ["«%» (кодирование)", `net-0123%2f456789abcde`],
    ["заглавная буква", `net-0123456789ABCDEFG`],
    ["пустая строка", ``],
    ["65 знаков", "a".repeat(65)],
  ];

  test("законный близнец принимается", () => {
    expect(isPlatformId(twin)).toBe(true);
  });

  test("отвергаемых входов ровно восемь (приёмка §8, NTF6-16)", () => {
    expect(rejected).toHaveLength(8);
  });

  test.each(rejected)("%s: %j отвергается", (_why, input) => {
    expect(isPlatformId(input)).toBe(false);
  });

  test("дефис первым знаком отвергается, как и знак вне ASCII", () => {
    expect(isPlatformId(`-${BODY}`)).toBe(false);
    expect(isPlatformId(`net-${BODY.slice(0, 16)}ё`)).toBe(false);
  });

  test("не-строка отвергается, а не падает", () => {
    expect(isPlatformId(undefined as unknown as string)).toBe(false);
    expect(isPlatformId(null as unknown as string)).toBe(false);
  });
});
