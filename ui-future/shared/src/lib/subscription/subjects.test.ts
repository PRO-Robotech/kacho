import {
  NOTIFY_SOURCE_BY_OWNER,
  STREAM_SUBJECTS,
  notifyModuleOf,
  specOfKind,
  streamSubject,
} from "./subjects";

/**
 * Мост между спекой консоли и её предметом потока: `streamSubject(specId)`.
 *
 * Предмет пробы — ДВЕ стороны карты сразу: покрытая спека обязана назвать пару
 * «владелец + вид», непокрытая обязана ответить `null`. Односторонняя проба
 * зеленела бы на карте, объявившей покрытым всё, — и консоль сняла бы опрос там,
 * где потока нет вовсе, то есть заморозила бы список навсегда.
 *
 * # ЧЕГО ЭТА ПРОБА НЕ УТВЕРЖДАЕТ, И ЧЕМ ЭТО ДЕРЖИТСЯ
 *
 * Согласия консоли со словарями владельцев журнала она НЕ утверждает и утверждать
 * не может: объявления владельцев — Go, по файлу `journal.go` в каталоге
 * `internal/subscriptionjournal` каждого сервиса-владельца, и отсюда они не читаются
 * ни при какой аккуратности. Согласие держат гейты дерева, и все они читают ЭТОТ
 * файл карты против настоящих объявлений:
 *
 *  - вид, названный консолью, объявлен своим владельцем —
 *    `TestEveryKindTheConsoleNamesIsDeclaredByItsOwner`
 *    (`ui-future/deploy/console_stream_kind_dictionary_test.go`, #1546);
 *  - вид, объявленный владельцем, назван консолью либо объявлен непоказанным —
 *    `TestEveryKindTheOwnerDeclaresIsNamedOrDeclaredUnshown`
 *    (`ui-future/deploy/console_stream_kind_coverage_test.go`, #1558);
 *  - имя владельца объявлено профилем края —
 *    `TestConsoleNamesTheOwnerTheEdgeAccepts`
 *    (`gateway/deploy/console_subscription_owner_test.go`, #1633).
 *
 * # ЗДЕСЬ СТОЯЛА ПРОБА «названы ровно те виды, что объявляют журналы владельцев»
 *
 * Снята задачей #1571: её заголовок обещал согласие с журналами, а тело сверяло
 * карту с выписанным тут же перечнем из шестнадцати видов — то есть с её же копией.
 * Читатель, искавший, чем держится согласие консоли со словарями владельцев,
 * приходил сюда и находил утверждение, которого проба не делала.
 *
 * СНЯТА, А НЕ ПЕРЕИМЕНОВАНА, и довод замерен, а не выбран по вкусу: состав карты
 * судят оба гейта выше В ОБЕ СТОРОНЫ, поэтому перечень не ловил НИ ОДНОГО
 * состояния, которое иначе прошло бы молча. Он ловил лишь сам ФАКТ изменения
 * состава — и брал за это вторую точку правки при покрытии нового вида, ничего не
 * добавляя к вердикту. Отдельно он был третьим написанием словаря видов (после
 * объявлений владельцев и таблицы клиентской страницы) и единственным, которое не
 * сверялось с деревом ничем: страницу держит `internal/repohygiene`
 * `TestSubscriptionKindVocabularyHasOneWriting`, но её обход — proto, Go и
 * клиентская страница; файлов `.ts` в нём нет.
 *
 * # ЗДЕСЬ СТОЯЛА ПРОБА «владельцы названы доменами контракта, а не сегментами REST»
 *
 * Снята задачей #1578 вместе с экспортом `JOURNAL_OWNERS`, который она сверяла.
 * Её заголовок был честен — предметом было НАПИСАНИЕ имени, — но утверждала она
 * РАВЕНСТВО МНОЖЕСТВ выписанному тут же перечню из пяти имён, то есть держала
 * вторую точку правки при появлении шестого владельца.
 *
 * СНЯТА, А НЕ ОСЛАБЛЕНА, и довод ЗАМЕРЕН, а не взят у соседней задачи. Четыре
 * инъекции — каждая меняет множество владельцев так, как его меняет ошибка, — и
 * ни в одной перечень не был единственным, кто краснеет:
 *
 *  · шестой владелец `geo` с видом `geo_zone`     → `…KindTheConsoleNamesIsDeclaredByItsOwner`
 *  · шестой владелец `geo` с чужим видом          → он же
 *  · снят последний предмет владельца `registry`  → `…OwnerDeclaresIsNamedOrDeclaredUnshown`
 *                                                   + `…DeclaredStreamOwnerIsMappedByTheConsole`
 *  · владелец `storage` переименован в `geo`      → первый + третий
 *
 * (первые два гейта — `ui-future/deploy/console_stream_kind_dictionary_test.go` и
 * `console_stream_kind_coverage_test.go`, третий — `console_stream_owner_coverage_test.go`.)
 *
 * ОСТОРОЖНО С ЧЕТВЁРТЫМ ГЕЙТОМ: `gateway/deploy/console_subscription_owner_test.go`
 * держит НАПИСАНИЕ (`nlb` вместо `loadbalancer` он ловит), но СОСТАВ — нет. Он
 * сверяет имена с множеством, которое край ПРИНИМАЕТ, а принимает край всякий
 * домен с внутренним адресом: их семь (`compute geo iam loadbalancer registry
 * storage vpc`), тогда как ОБЪЯВЛЕНО посадкой пять. Во всех четырёх инъекциях он
 * молчал. Состав держат гейты `ui-future/deploy`, и ссылаться за составом надо на
 * них.
 *
 * ЧЕМ ЭТА ПРОБА БЫЛА ПОУЧИТЕЛЬНА: она ЗАКРЕПЛЯЛА ДЕФЕКТ (#1440). Перечень ждал
 * `nlb`, проба была зелена — а страница списка балансировщиков давала два отказа
 * `400` на каждом открытии. Опровергнуть копию перечня список и не мог: обе
 * стороны сверки лежали в этом файле, а край в нём не участвует вовсе. Разбор
 * самого дефекта живёт у гейта, который его теперь держит, и здесь не
 * пересказывается.
 *
 * # ЗДЕСЬ СТОЯЛИ ЕЩЁ ДВЕ КОПИИ — ПЕРЕЧЕНЬ ВЛАДЕЛЬЦЕВ И КАРТА ПРЕФИКСОВ
 *
 * Сняты задачей #1635, и довод ЗАМЕРЕН, а не выбран по вкусу. Обе были ВТОРОЙ
 * ТОЧКОЙ ПРАВКИ: шестой владелец краснил бы их при совершенно верном дереве.
 *
 * Замер карты префиксов — четыре оси, по каждой прогнаны обе стороны:
 *
 * | ось | проба `kindPrefix` | гейты дерева |
 * |---|---|---|
 * | вид приписан чужому владельцу (`storage_volume` при `compute`) | красная | красны |
 * | перестановка владельцев целиком (`vpc` ↔ `compute`) | красная | красны |
 * | опечатка в виде при верном префиксе (`vpc_netwrok`) | ЗЕЛЁНАЯ | красны |
 * | снята запись из самой карты, дерево цело | КРАСНАЯ | зелены |
 *
 * То есть верные её находки все до одной воспроизводит гейт дерева (он сверяет
 * ТОЧНЫЙ вид с ТОЧНЫМ словарём владельца, а префикс — огрубление этой сверки);
 * там, где гейт видит, она слепа; а единственное, что она находит одна, — своя
 * собственная неполнота.
 *
 * Починить её «одним источником» было нельзя: карту «владелец → префикс» не
 * читает НИ ОДНА строка прода (предикат:
 * `grep -rn 'startsWith\|[Pp]refix' ui-future/shared/src/lib/subscription/*.ts`
 * вне проб — ноль; единственное попадание есть проза про другую функцию).
 * Источник, «из которого читают и проба, и код», требует кода, который читает, —
 * а его нет. Объявить такую карту в `subjects.ts` ради одной пробы значило бы
 * получить сверку карты с её же копией в том же файле, то есть ровно то, что
 * #1571 отсюда уже вынесла.
 *
 * Перечень владельцев снят по тому же замеру: равенство названного объявленному
 * держит ПАРА гейтов дерева (объявленное ⊆ названное — гейт покрытия; названное ⊆
 * объявленного — гейт края), а литерал добавлял к вердикту лишь свою неполноту.
 * Отдельно: он однажды УЖЕ был зелен на дефекте — ждал `nlb`, пока страница
 * балансировщиков давала два отказа `400` на каждом открытии (kacho#1440).
 *
 * Заводя здесь новое утверждение, спроси то же самое: назван ли в нём артефакт,
 * которого этот файл прочитать не может. Если да — утверждение принадлежит гейту
 * дерева, а не пробе консоли. И спроси второе: краснеет ли оно на РОСТЕ дерева.
 * Краснеет — это вторая точка правки, а не проверка.
 */
describe("подписка: спека консоли → владелец журнала и вид предмета", () => {
  // verifies #1021, #1578, #1635
  it("виды карты названы при своих владельцах и перечень не выписан вторым списком", () => {
    // ЗДЕСЬ СТОЯЛИ ДВА УТВЕРЖДЕНИЯ, И ОБА ЛИШИЛИСЬ ПРЕДМЕТА — по-разному.
    //
    // Первое сверяло плоский перечень `JOURNAL_OWNERS` с картой. Его предмет снят
    // задачей #1578 СИЛЬНЕЕ, чем предлагала эта проба: экспорта больше нет вовсе,
    // поэтому второй точки правки не существует BY CONSTRUCTION, и сверять нечего.
    //
    // Второе сверяло карту «владелец → префикс вида». Снято задачей #1635 по
    // замеру (разбор в шапке): все её верные находки воспроизводит гейт дерева,
    // на опечатке при верном префиксе она слепа, а единственное, что она находит
    // одна, — своя собственная неполнота.
    //
    // ОСТАЛОСЬ то, что этот файл утверждать ВПРАВЕ и что не становится второй
    // точкой правки: карта непуста и каждый её вид назван ровно один раз.
    const owners = Object.values(STREAM_SUBJECTS).map((s) => s.owner);
    const kinds = Object.values(STREAM_SUBJECTS).map((s) => s.kind);

    // ПРЕМИСА, а не вежливость: на пустой карте обходы ниже вакуумны, и
    // «нарушений нет» означало бы «нечего было обходить».
    expect(owners.length).toBeGreaterThan(0);

    // Вид назван один раз: дубль дал бы два потока об одном предмете.
    expect(kinds.length).toBe(new Set(kinds).size);
  });

  it("покрытые спеки называют свой предмет", () => {
    expect(streamSubject("networks")).toEqual({
      owner: "vpc",
      kind: "vpc_network",
    });
    expect(streamSubject("compute-instances")).toEqual({
      owner: "compute",
      kind: "compute_instance",
    });
    expect(streamSubject("load-balancers")).toEqual({
      owner: "loadbalancer",
      kind: "nlb_network_load_balancer",
    });
    expect(streamSubject("volumes")).toEqual({
      owner: "storage",
      kind: "storage_volume",
    });
    expect(streamSubject("registries")).toEqual({
      owner: "registry",
      kind: "registry_registry",
    });
    // Служба доступа: написание вида НЕОДНОРОДНО, и проба закрепляет именно это.
    // Аккаунт и проект зовутся без приставки домена, остальные пять — с ней;
    // выведи их «как у соседей» — получишь `iam_account`, и спека молча осталась
    // бы на опросе: словарь такого вида не принесёт, ошибки не будет нигде.
    expect(streamSubject("accounts")).toEqual({ owner: "iam", kind: "account" });
    expect(streamSubject("projects")).toEqual({ owner: "iam", kind: "project" });
    expect(streamSubject("users")).toEqual({ owner: "iam", kind: "iam_user" });
    expect(streamSubject("service-accounts")).toEqual({
      owner: "iam",
      kind: "iam_service_account",
    });
    expect(streamSubject("groups")).toEqual({ owner: "iam", kind: "iam_group" });
    expect(streamSubject("roles")).toEqual({ owner: "iam", kind: "iam_role" });
    expect(streamSubject("access-bindings")).toEqual({
      owner: "iam",
      kind: "iam_access_binding",
    });
  });

  it("непокрытая спека отвечает null, а не догадкой", () => {
    // ОТРИЦАНИЕ СТОИТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ ВЫШЕ. Само по себе оно зеленело бы
    // на карте, отвечающей `null` вообще всем.
    //
    // Каждое имя ниже — не выдумка, а живая спека дерева, чей ВЛАДЕЛЕЦ этого
    // вида не объявляет. Оснований ДВА, и оба реальны: у блочного хранения и
    // реестра журнал ЕСТЬ, но ведёт не всякий свой предмет (`disk-types`,
    // `repositories`, `tags`); у compute журнал пишет один вид, машины
    // (`placement-groups`, `machine-types`). Догадка «раз домен покрыт, значит
    // покрыт и этот вид» дала бы снятый опрос при молчащем потоке — то есть
    // список, замерший навсегда.
    //
    // ТРЕТЬЕ ОСНОВАНИЕ ОТСЮДА СНЯТО ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ: здесь стояли
    // `users` и `projects` с доводом «у iam журнала нет вовсе». Журнал у службы
    // доступа появился, семь её видов названы картой, и обе спеки переехали в
    // положительную пробу выше — ослабить это отрицание, оставив их здесь,
    // значило бы закрепить пробой состояние, которого больше нет.
    for (const specId of [
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
      expect({ specId, subject: streamSubject(specId) }).toEqual({
        specId,
        subject: null,
      });
    }
  });

  it("выдуманная спека отвечает null, а не падает", () => {
    expect(streamSubject("нет-такой-спеки")).toBeNull();
  });

  it("имя свойства прототипа спекой не является", () => {
    // Карта — литерал объекта, и чтение по ключу без проверки собственности
    // отдало бы на `toString` функцию прототипа вместо `null`: вызывающий принял
    // бы её за предмет потока, а `notifyModuleOf` — за владельца без записи.
    for (const specId of ["toString", "constructor", "__proto__", "hasOwnProperty"]) {
      expect({ specId, subject: streamSubject(specId) }).toEqual({ specId, subject: null });
    }
  });
});

/**
 * Переход «спека → ключ модуля каталога `notify`» (NTF-6 Р10а, З11, CX6-01) и
 * обратный поиск «тип объекта → спека» (Р7, Р10).
 *
 * Ключ модуля каталога — ДРУГОЙ словарь, чем владелец журнала: у балансировщика
 * владелец `loadbalancer`, а модуль каталога `nlb`; у службы доступа — `iam` и
 * `kaname`. Поиск модуля каталога по `owner` промахивался бы ровно на этих
 * десяти спеках из двадцати трёх, и действие подписки на их карточках молча
 * исчезало бы.
 *
 * Каталог ниже — форма `GET /notify/v1/catalog` (NTF-3 Р29, NTF3-149): ключи
 * модулей в порядке таблицы сборки. Ячейка `(RESOURCE_CHANGE, EMAIL: SWITCHABLE)`
 * дана КАЖДОМУ модулю намеренно — предмет пробы переход, а не состав каталога,
 * и отсутствие ячейки у настоящего `kaname` не должно прятать промах ключа.
 * Согласие отображения с журналами дерева держит гейт
 * `ui-future/deploy/console_notify_source_key_test.go`, а не эта проба.
 */
describe("уведомления: спека консоли → ключ модуля каталога notify", () => {
  interface CatalogCell {
    kind: string;
    channel: string;
    policy: string;
  }
  interface CatalogModule {
    key: string;
    cells: CatalogCell[];
  }

  const subscribable: CatalogCell = {
    kind: "RESOURCE_CHANGE",
    channel: "EMAIL",
    policy: "SWITCHABLE",
  };
  const catalogKeys = ["compute", "vpc", "nlb", "registry", "storage", "notify", "kaname"];
  const catalog: CatalogModule[] = catalogKeys.map((key) => ({ key, cells: [subscribable] }));

  // Спеки, для которых ячейка подписки в каталоге НЕ нашлась, — пустой ответ и
  // есть цель. Функция одна для положительной ветки и её близнеца.
  function specsWithoutSubscribableCell(modules: readonly CatalogModule[]): string[] {
    return Object.keys(STREAM_SUBJECTS).filter((specId) => {
      const key = notifyModuleOf(specId);
      const module = modules.find((m) => m.key === key);
      return !module?.cells.some(
        (c) => c.kind === "RESOURCE_CHANGE" && c.channel === "EMAIL" && c.policy === "SWITCHABLE",
      );
    });
  }

  // verifies #2925
  it(`каждая запись STREAM_SUBJECTS (осмотрено ${Object.keys(STREAM_SUBJECTS).length}) находит в каталоге ячейку подписки своего модуля`, () => {
    const specIds = Object.keys(STREAM_SUBJECTS);
    // Объём осмотренного назван в имени пробы и утверждён: «нарушений нет» на пустом
    // обходе не значит ничего.
    expect(specIds.length).toBeGreaterThan(0);

    expect(specsWithoutSubscribableCell(catalog)).toEqual([]);
  });

  // verifies #2925
  it("близнец: каталог без модуля nlb — ячейки не находят ровно спеки балансировщика", () => {
    const withoutNlb = catalog.filter((m) => m.key !== "nlb");
    expect(specsWithoutSubscribableCell(withoutNlb).sort()).toEqual(
      ["listeners", "load-balancers", "target-groups"].sort(),
    );
  });

  // verifies #2925
  it("ключ модуля берётся переходом, а не написанием владельца", () => {
    // Два владельца, у которых написания расходятся, — ради них переход и заведён.
    expect(notifyModuleOf("target-groups")).toBe("nlb");
    expect(notifyModuleOf("load-balancers")).toBe("nlb");
    expect(notifyModuleOf("accounts")).toBe("kaname");
    expect(notifyModuleOf("access-bindings")).toBe("kaname");
    // Совпадающее написание — положительный контроль того же пути.
    expect(notifyModuleOf("networks")).toBe("vpc");
    expect(notifyModuleOf("compute-instances")).toBe("compute");
    expect(notifyModuleOf("volumes")).toBe("storage");
    expect(notifyModuleOf("registries")).toBe("registry");

    // Отображение исчерпывающее: у каждого владельца, встреченного картой, — запись.
    // Владельцы без записи названы поимённо; пустой перечень — цель.
    const owners = Object.values(STREAM_SUBJECTS).map(({ owner }) => owner);
    expect(owners.length).toBeGreaterThan(0);
    const ownersWithoutKey = owners.filter(
      (owner) => !/^[a-z]+$/.test(NOTIFY_SOURCE_BY_OWNER[owner] ?? ""),
    );
    expect(ownersWithoutKey).toEqual([]);
  });

  // verifies #2925
  it("спека вне STREAM_SUBJECTS отвечает null, а не догадкой по домену", () => {
    for (const specId of ["disk-types", "placement-groups", "нет-такой-спеки", "toString", ""]) {
      expect({ specId, key: notifyModuleOf(specId) }).toEqual({ specId, key: null });
    }
  });

  // verifies #2925
  it(`specOfKind — обратный поиск по STREAM_SUBJECTS (осмотрено ${Object.keys(STREAM_SUBJECTS).length}), по каждой записи туда и обратно`, () => {
    const entries = Object.entries(STREAM_SUBJECTS);
    expect(entries.length).toBeGreaterThan(0);
    for (const [specId, { kind }] of entries) {
      expect({ kind, specId: specOfKind(kind) }).toEqual({ kind, specId });
    }
  });

  // verifies #2925
  it("specOfKind: вид вне словаря консоли — null", () => {
    // Строка ленты несёт тип объекта от сервера; неизвестный тип — текст без
    // ссылки, а не угаданная спека. Написание с чужой приставкой (`iam_account`)
    // и владелец вместо вида — два реальных промаха, а не выдумка.
    for (const kind of ["iam_account", "vpc", "nlb_target_groups", "VPC_NETWORK", "toString", ""]) {
      expect({ kind, specId: specOfKind(kind) }).toEqual({ kind, specId: null });
    }
  });
});
