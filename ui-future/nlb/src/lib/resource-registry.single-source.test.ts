// Реестр домена держит ТО, чем домен владеет, — и ничего сверх.
//
// Ресурсы, которых модуль не показывает и не создаёт, стоят в его реестре
// только ради резолва ссылки: `RefNameLink` и `RefSelect` читают из спеки
// `apiPath`, `payloadKey`, `plural` и адрес карточки. Заводить ради этого
// СВОЮ запись значит держать вторую проекцию чужого ресурса: правка у владельца
// (общий реестр) до неё не доезжает, и человек читает разницу как «другое место
// продукта» (правило 3 `ui.md`).
//
// Замер, из которого выведено (2026-08-28, `origin/release/deforking-2`):
// разность ключей «реестр nlb минус общий реестр» — ПУСТА, все девять спек
// модуля общий реестр несёт. При этом шесть из девяти были у домена
// заглушками, а у владельца — полными: `compute-instances` 22 строки против
// 376, `addresses` 22 против 243, `zones` 21 против 181.
//
// СУДИТ ИДЕНТИЧНОСТЬ ОБЪЕКТА, а не совпадение полей. Копия, сегодня совпадающая
// с оригиналом до буквы, завтра расходится молча — и проба на равенство полей
// останется зелёной ровно до первой правки у владельца, то есть будет молчать
// именно тогда, когда должна говорить.

import { REGISTRY, getResource } from "./resource-registry";
import { REGISTRY as SHARED } from "@shared/lib/resource-registry";

/**
 * Ссылочные цели: ресурсы чужих доменов, которые модуль только резолвит.
 * Перечень назван поимённо, потому что это РЕШЕНИЕ («мы их не владеем»), а не
 * вывод из дерева: выведи его из разности реестров — и он схлопнется в тавтологию.
 */
const REF_TARGETS = [
  "compute-regions",
  "compute-instances",
  "network-interfaces",
  "zones",
  "subnets",
  "addresses",
] as const;

/** Ресурсы домена: их проекция у nlb богаче общей и остаётся здесь. */
const OWNED = ["load-balancers", "listeners", "target-groups"] as const;

describe("реестр nlb: ссылочные цели — от владельца, доменные — свои", () => {
  it(`перепись: спек в реестре ${Object.keys(REGISTRY).length}, ссылочных целей ${REF_TARGETS.length}, доменных ${OWNED.length}`, () => {
    // Пустой обход сделал бы всё ниже вакуумно истинным.
    expect(Object.keys(REGISTRY).length).toBe(REF_TARGETS.length + OWNED.length);
    expect(Object.keys(SHARED).length).toBeGreaterThan(0);
  });

  it.each(REF_TARGETS)("%s — ТОТ ЖЕ объект, что у владельца", (id) => {
    expect(REGISTRY[id]).toBe(SHARED[id]);
    // getResource обязан отдавать то же самое: два входа в один реестр.
    expect(getResource(id)).toBe(SHARED[id]);
  });

  it.each(OWNED)("%s — проекция домена, а не общая", (id) => {
    // Контроль в обратную сторону: если бы проба умела только «совпадает»,
    // она была бы зелёной и на реестре, целиком равном общему, — то есть на
    // состоянии, при котором домен потерял бы всё, ради чего держит реестр.
    expect(REGISTRY[id]).not.toBe(SHARED[id]);
    expect(REGISTRY[id]).toBeDefined();
  });
});

describe("возможности домена сохранены поимённо", () => {
  // Перечни дословные: сведение реестра не вправе унести ни колонку, ни поле
  // формы. Общая проекция балансировщика беднее доменной на «Схему», «Адрес»,
  // зоны без анонса, привязку сессий и защиту от удаления — молчаливая замена
  // на неё и есть та потеря возможности, которую этот файл стережёт.
  const cols = (id: string) => (REGISTRY[id].columns ?? []).map((c) => c.path);
  const fields = (id: string) => (REGISTRY[id].fields ?? []).map((f) => f.name);

  it("балансировщик: колонки", () => {
    expect(cols("load-balancers")).toEqual([
      "name",
      "id",
      "region_id",
      "type",
      "v4_address_id",
      "status",
      "created_at",
      "labels",
    ]);
  });

  it("балансировщик: поля формы", () => {
    expect(fields("load-balancers")).toEqual([
      "placement",
      "name",
      "description",
      "region_id",
      "vip_source",
      "disabled_announce_zones",
      "session_affinity",
      "deletion_protection",
      "labels",
      "project_id",
    ]);
  });

  it("балансировщик: связанный дочерний ресурс объявлен", () => {
    expect(REGISTRY["load-balancers"].related).toEqual([
      { childId: "listeners", filterField: "load_balancer_id", label: "Листенеры" },
    ]);
  });

  it("обработчик: колонки и поля", () => {
    expect(cols("listeners")).toEqual([
      "name",
      "id",
      "load_balancer_id",
      "protocol",
      "port",
      "resolved_backend_port",
      "status",
      "created_at",
    ]);
    expect(fields("listeners")).toContain("default_target_group_id");
  });

  it("группа целей: ветви проверки живости остаются выразимыми", () => {
    const names = fields("target-groups");
    for (const branch of ["tcp", "http", "https", "grpc"]) {
      expect(names.some((n) => n.startsWith(`health_check.${branch}`))).toBe(true);
    }
  });
});
