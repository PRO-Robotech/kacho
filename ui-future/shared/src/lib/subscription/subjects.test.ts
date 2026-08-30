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
  it("владельцы названы доменами контракта, а не сегментами REST", () => {
    // `loadbalancer` против `nlb` — тот самый разнобой, о котором предупреждает
    // страница подписки: REST-путь `/nlb/v1/`, каталог в дереве `services/nlb`,
    // а владелец `loadbalancer`. Возьми здесь сегмент пути или имя каталога —
    // ручка ответила бы `400 unknown owner`.
    //
    // ЭТА ПРОБА ЗАКРЕПЛЯЛА ДЕФЕКТ (kacho#1440): она ждала `nlb` и была зелена,
    // пока страница списка балансировщиков давала два отказа `400` на каждом
    // открытии. Список копию перечня и не мог опровергнуть — обе стороны были
    // тут, а край в этом файле не участвует. Согласие с краем держит теперь
    // гейт `gateway/deploy/console_subscription_owner_test.go`: он берёт
    // множество принимаемых имён ВЫЗОВОМ той функции, которой пользуется край.
    //
    // ЗДЕСЬ СТОЯЛИ ТРИ ИМЕНИ, а край объявлял пять: блочное хранение и реестр
    // журналы ведут, а карта их не называла — и четыре места опроса объясняли
    // себя утверждением «журнала у этого домена нет». Обратную сторону — что
    // объявленный краю владелец обязан быть здесь назван — держит гейт
    // `ui-future/deploy/console_stream_owner_coverage_test.go`.
    expect([...JOURNAL_OWNERS].sort()).toEqual([
      "compute",
      "loadbalancer",
      "registry",
      "storage",
      "vpc",
    ]);
  });

  it("названы ровно те виды, что объявляют журналы владельцев", () => {
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
        "storage_volume",
        "storage_snapshot",
        "storage_image",
        "registry_registry",
      ].sort(),
    );
  });

  it("каждый вид назван при своём владельце", () => {
    // Вид, приписанный чужому владельцу, отвергается на открытии `400`, и
    // список молчал бы навсегда. Сверяется поимённо, а не количеством.
    //
    // ПРЕФИКС ВИДА НЕ ПРОИЗВОДИТСЯ ОТ ИМЕНИ ВЛАДЕЛЬЦА, и прежняя редакция
    // выводила его именно так (`kind.startsWith(owner + "_")`). Вывод держался
    // ровно потому, что у compute и vpc обе величины совпадают случайно; у
    // балансировщика они разные по построению — владелец `loadbalancer` (домен
    // контракта), вид `nlb_*` (тип объекта модели прав). То есть проверка
    // требовала бы здесь ровно того написания владельца, которое край
    // отвергает, — и толкала бы починку в сторону дефекта.
    //
    // Соответствие названо ЯВНО и повторяет первую колонку таблицы владельцев
    // клиентской страницы подписки.
    const kindPrefix: Record<string, string> = {
      compute: "compute_",
      loadbalancer: "nlb_",
      registry: "registry_",
      storage: "storage_",
      vpc: "vpc_",
    };
    const mismatched = Object.entries(STREAM_SUBJECTS)
      .filter(([, s]) => !s.kind.startsWith(kindPrefix[s.owner] ?? "\u0000"))
      .map(([specId, s]) => `${specId}: вид ${s.kind} при владельце ${s.owner}`);
    expect(mismatched).toEqual([]);
  });

  it("покрытые спеки называют свой предмет", () => {
    expect(streamSubject("networks")).toEqual({ owner: "vpc", kind: "vpc_network" });
    expect(streamSubject("compute-instances")).toEqual({ owner: "compute", kind: "compute_instance" });
    expect(streamSubject("load-balancers")).toEqual({
      owner: "loadbalancer",
      kind: "nlb_network_load_balancer",
    });
    expect(streamSubject("volumes")).toEqual({ owner: "storage", kind: "storage_volume" });
    expect(streamSubject("registries")).toEqual({ owner: "registry", kind: "registry_registry" });
  });

  it("непокрытая спека отвечает null, а не догадкой", () => {
    // ОТРИЦАНИЕ СТОИТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ ВЫШЕ. Само по себе оно зеленело бы
    // на карте, отвечающей `null` вообще всем.
    //
    // Каждое имя ниже — не выдумка, а живая спека дерева, чей ВЛАДЕЛЕЦ этого
    // вида не объявляет. Три разных основания, и все три реальны: у iam журнала
    // нет вовсе (`users`, `projects`); у блочного хранения и реестра журнал ЕСТЬ,
    // но ведёт не всякий свой предмет (`disk-types`, `repositories`, `tags`);
    // у compute журнал пишет один вид, машины (`placement-groups`,
    // `machine-types`). Догадка «раз домен покрыт, значит покрыт и этот вид»
    // дала бы снятый опрос при молчащем потоке — то есть список, замерший
    // навсегда.
    for (const specId of [
      "users",
      "projects",
      "disk-types",
      "repositories",
      "tags",
      "placement-groups",
      "machine-types",
      "zones",
    ]) {
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
