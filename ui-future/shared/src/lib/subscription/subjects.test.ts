import { JOURNAL_OWNERS, STREAM_SUBJECTS, streamSubject } from "./subjects";

/**
 * Мост между спекой консоли и словарём владельца журнала.
 *
 * Предмет пробы — не «карта непуста», а ДВЕ её стороны сразу: названное
 * покрытым обязано существовать у владельца, а не названное обязано отвечать
 * `null`. Односторонняя проба зеленела бы на карте, объявившей покрытым всё, —
 * и консоль сняла бы опрос там, где потока нет вовсе, то есть заморозила бы
 * список навсегда.
 */
describe("подписка: спека консоли → владелец журнала и вид предмета", () => {
  // verifies #1021
  it("владельцев ровно три, и это ключи бэкендов края, а не сегменты REST", () => {
    // `nlb` против `loadbalancer` — тот самый разнобой, о котором предупреждает
    // страница подписки: путь `/loadbalancer/v1/`, а ключ владельца `nlb`.
    // Возьми здесь сегмент пути — ручка ответила бы `400 unknown owner`.
    expect([...JOURNAL_OWNERS].sort()).toEqual(["compute", "nlb", "vpc"]);
  });

  it("названы ровно те двенадцать видов, что объявляют журналы трёх владельцев", () => {
    // Написание вида — тип объекта модели прав, одно на всё дерево.
    expect(
      Object.values(STREAM_SUBJECTS)
        .map((s) => s.kind)
        .sort(),
    ).toEqual(
      [
        "compute_instance",
        "nlb_listener",
        "nlb_network_load_balancer",
        "nlb_target_group",
        "vpc_address",
        "vpc_cidr_group",
        "vpc_gateway",
        "vpc_network",
        "vpc_network_interface",
        "vpc_route_table",
        "vpc_security_group",
        "vpc_subnet",
      ].sort(),
    );
  });

  it("каждый вид назван при своём владельце", () => {
    // Вид, приписанный чужому владельцу, отвергается на открытии `400`, и
    // список молчал бы навсегда. Сверяется поимённо, а не количеством.
    const mismatched = Object.entries(STREAM_SUBJECTS)
      .filter(([, s]) => !s.kind.startsWith(`${s.owner}_`))
      .map(([specId, s]) => `${specId}: вид ${s.kind} при владельце ${s.owner}`);
    expect(mismatched).toEqual([]);
  });

  it("покрытые спеки называют свой предмет", () => {
    expect(streamSubject("networks")).toEqual({ owner: "vpc", kind: "vpc_network" });
    expect(streamSubject("compute-instances")).toEqual({ owner: "compute", kind: "compute_instance" });
    expect(streamSubject("load-balancers")).toEqual({ owner: "nlb", kind: "nlb_network_load_balancer" });
  });

  it("непокрытая спека отвечает null, а не догадкой", () => {
    // ОТРИЦАНИЕ СТОИТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ ВЫШЕ. Само по себе оно зеленело бы
    // на карте, отвечающей `null` вообще всем.
    //
    // Каждое имя ниже — не выдумка, а живая спека дерева, чей домен журнала НЕ
    // объявляет: iam и storage и registry владельцами не значатся вовсе, а
    // группа размещения не значится в словаре compute (журнал пишет один вид —
    // `Instance`). Догадка «раз домен compute, значит покрыто» дала бы `400` на
    // открытии и молчащий список.
    for (const specId of ["users", "projects", "volumes", "registries", "tags", "placement-groups", "machine-types", "zones"]) {
      // Сверяется ЗНАЧЕНИЕ, а не его строка. Прежняя запись приводила ответ
      // `String()`-ом, чтобы назвать в отказе виновную спеку, — но объект
      // приводится к «[object Object]», то есть ЛЮБОЙ непустой ответ выглядел
      // бы в отказе одинаково, а два разных — одинаково же. Пара «спека +
      // ответ» называет виновника лучше и ничего не приводит.
      expect({ specId, subject: streamSubject(specId) }).toEqual({ specId, subject: null });
    }
  });

  it("выдуманная спека отвечает null, а не падает", () => {
    expect(streamSubject("нет-такой-спеки")).toBeNull();
  });
});
