import { MAX_OPEN_STREAMS, ORIGIN_CONNECTION_BUDGET, SubscriptionHub, type EventSourceLike } from "./hub";

/**
 * Мультиплексор потока изменений.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЭТО ПРОБЫ ХАБА, А НЕ ХУКА
 *
 * Предмет здесь — решения, от которых зависит, снимет ли консоль опрос:
 * ОДИН ли поток на владельца, покрыт ли вид ПО ПРОВОДУ, и что происходит,
 * когда потока нет. Все три наблюдаемы без React и без браузера — значит
 * проверяются там, где их видно, а не через три слоя отрисовки.
 *
 * ПОЧЕМУ ПРИЁМНИК СОБЫТИЙ ПОДСТАВНОЙ, И ЧЕГО ЭТО НЕ ДОКАЗЫВАЕТ
 *
 * Подставной приёмник доказывает РАЗБОР и УЧЁТ, и не доказывает, что браузер
 * вообще откроет поток: печенье сессии, тип ответа, возобновление заголовком —
 * ничего этого здесь нет by construction. Это утверждает проба браузером
 * (`ui-future/e2e/specs/subscription-stream.spec.ts`), и она исполняется: её
 * условие создано поставкой, объявившей владельцев журнала.
 */

class FakeSource implements EventSourceLike {
  static opened: string[] = [];
  readyState = 0;
  closed = false;
  private handlers = new Map<string, ((ev: MessageEvent<string>) => void)[]>();
  onerror: ((ev: Event) => void) | null = null;

  constructor(readonly url: string) {
    FakeSource.opened.push(url);
  }
  addEventListener(name: string, fn: (ev: MessageEvent<string>) => void): void {
    const list = this.handlers.get(name) ?? [];
    list.push(fn);
    this.handlers.set(name, list);
  }
  close(): void {
    this.closed = true;
    this.readyState = 2;
  }
  emit(name: string, data: unknown, id = ""): void {
    this.readyState = 1;
    for (const fn of this.handlers.get(name) ?? []) {
      fn({ data: JSON.stringify(data), lastEventId: id } as MessageEvent<string>);
    }
  }
  fail(): void {
    this.readyState = 2;
    this.onerror?.(new Event("error"));
  }
}

const opened = (kinds: string[], position = "p0") => ({
  opened: { position, caughtUp: true, honoredFilters: ["kinds", "project_id"], knownKinds: kinds, retainsEverything: true },
});
const event = (kind: string, resourceId: string, change = "UPDATED") => ({
  event: { position: "p1", kind, resourceId, projectId: "prj-1", change },
});

function makeHub(sources: FakeSource[], maxOpenStreams?: number) {
  return new SubscriptionHub({
    maxOpenStreams,
    open: (url) => {
      const s = new FakeSource(url);
      sources.push(s);
      return s;
    },
    // Разбор отказа — отдельный вход: приёмник событий браузера кода ответа не
    // отдаёт вовсе, и без него «владелец не объявлен» неотличимо от «край лёг».
    // Подставной разбор отказа отвечает ГОТОВЫМ обещанием: ждать ему нечего, а
    // `async` без единого `await` объявлял ожидание, которого в теле нет.
    diagnose: () =>
      Promise.resolve({ status: 501, contentType: "application/json", body: "no journal owner is declared for this edge" }),
    log: () => undefined,
  });
}

beforeEach(() => {
  FakeSource.opened = [];
});

describe("хаб подписки: один поток на владельца, покрытие читается по проводу", () => {
  // verifies #1021
  it("два подписчика одного владельца делят ОДИН поток", () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.subscribe({ owner: "vpc", kind: "vpc_subnet", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(1);
  });

  it("разные проекты и разные владельцы — разные потоки", () => {
    // ПРЕДМЕТ ЗДЕСЬ — КЛЮЧ КАНАЛА, а не бюджет соединений: утверждается, что
    // пара «владелец + проект» не схлопывается в один поток. Потолок задаётся
    // явно и выше трёх ровно поэтому — иначе проба мерила бы потолок, о котором
    // ничего не говорит, и её заголовок стал бы шире её тела. Сам потолок
    // утверждается своим describe ниже, вместе с умолчанием.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources, 3);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-2" }, () => undefined);
    hub.subscribe({ owner: "compute", kind: "compute_instance", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(3);
  });

  it("поток закрывается, когда ушёл последний подписчик", () => {
    // Поток, переживший свою страницу, занимает место дважды: в потолке
    // вызывающего у края и в бюджете соединений браузера, где его не хватит
    // уже обычному запросу этой же вкладки.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    const off1 = hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    const off2 = hub.subscribe({ owner: "vpc", kind: "vpc_subnet", projectId: "prj-1" }, () => undefined);
    off1();
    expect(sources[0].closed).toBe(false);
    off2();
    expect(sources[0].closed).toBe(true);
  });

  it("адрес несёт владельца и проект и НЕ называет виды", () => {
    // Виды не называются намеренно: словарь принадлежит владельцу, приезжает
    // первым же кадром, и подбирать его против отказа не нужно. Назови их
    // здесь — и вид, которого владелец не знает, отверг бы поток целиком `400`.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    const url = new URL(FakeSource.opened[0], "http://stand");
    expect(url.pathname).toBe("/subscription/v1/events");
    expect(url.searchParams.get("owner")).toBe("vpc");
    expect(url.searchParams.get("projectId")).toBe("prj-1");
    expect(url.searchParams.getAll("kinds")).toEqual([]);
  });

  it("до служебного сообщения открытия вид НЕ считается покрытым", () => {
    // Опрос снимается только на доказанном покрытии: сними его на «поток вроде
    // бы открыт» — и список замрёт на владельце, который этого вида не знает.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    expect(hub.covers({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" })).toBe(false);
    sources[0].emit("opened", opened(["vpc_network", "vpc_subnet"]));
    expect(hub.covers({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" })).toBe(true);
  });

  it("вид вне словаря владельца покрытым НЕ становится", () => {
    // Положительный контроль стоит строкой выше: без него это отрицание
    // зеленело бы на хабе, который не покрывает вообще ничего.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    hub.subscribe({ owner: "compute", kind: "compute_instance", projectId: "prj-1" }, () => undefined);
    sources[0].emit("opened", opened(["compute_instance"]));
    expect(hub.covers({ owner: "compute", kind: "compute_instance", projectId: "prj-1" })).toBe(true);
    expect(hub.covers({ owner: "compute", kind: "compute_placement_group", projectId: "prj-1" })).toBe(false);
  });

  it("событие доходит до подписчика СВОЕГО вида и не доходит до чужого", () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    const nets: string[] = [];
    const subnets: string[] = [];
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, (e) => nets.push(e.resourceId));
    hub.subscribe({ owner: "vpc", kind: "vpc_subnet", projectId: "prj-1" }, (e) => subnets.push(e.resourceId));
    sources[0].emit("opened", opened(["vpc_network", "vpc_subnet"]));
    sources[0].emit("event", event("vpc_network", "net-1"));
    expect(nets).toEqual(["net-1"]);
    expect(subnets).toEqual([]);
  });

  it("снятие предмета доходит так же, как создание", () => {
    // Событие снятия состояния не несёт НИ У ОДНОГО владельца — и именно оно
    // единственное сообщает, что строки больше нет. Отбрось его как «нечего
    // применять» — и удалённая строка останется в списке навсегда.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    const seen: string[] = [];
    hub.subscribe({ owner: "loadbalancer", kind: "nlb_listener", projectId: "prj-1" }, (e) => seen.push(e.change));
    sources[0].emit("opened", opened(["nlb_listener"]));
    sources[0].emit("event", event("nlb_listener", "lsn-1", "DELETED"));
    expect(seen).toEqual(["DELETED"]);
  });

  it("отказ потока СНИМАЕТ покрытие — опрос обязан вернуться", () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    sources[0].emit("opened", opened(["vpc_network"]));
    expect(hub.covers({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" })).toBe(true);
    sources[0].fail();
    expect(hub.covers({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" })).toBe(false);
  });

  it("покрытие объявляется наблюдателям, а не спрашивается опросом", () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    let ticks = 0;
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.onCoverageChange(() => {
      ticks += 1;
    });
    sources[0].emit("opened", opened(["vpc_network"]));
    expect(ticks).toBeGreaterThan(0);
  });

  it("после отказа поток НЕ переоткрывается немедленно", () => {
    // Владелец не объявлен ни в одном профиле — край отвечает `501`, и приёмник
    // событий браузера соединение НЕ повторяет. Повторное открытие с каждой
    // перерисовки било бы по краю без единого шанса на успех.
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    const off = hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    sources[0].fail();
    off();
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(1);
  });

  it("окно молчания после отказа ИСТЕКАЕТ — включённая посадкой возможность подхватывается", () => {
    // Отрицание строкой выше («не переоткрывается немедленно») само по себе
    // зеленело бы на хабе, который не пробует НИКОГДА. Владельца объявляют
    // посадкой (#1388), и бессрочная память об отказе означала бы, что
    // включённый поток не берётся до перезагрузки вкладки.
    const sources: FakeSource[] = [];
    let clock = 1_000;
    const hub = new SubscriptionHub({
      open: (url) => {
        const s = new FakeSource(url);
        sources.push(s);
        return s;
      },
      // Подставной разбор отказа отвечает ГОТОВЫМ обещанием: ждать ему нечего, а
      // `async` без единого `await` объявлял ожидание, которого в теле нет.
      diagnose: () => Promise.resolve({ status: 501, contentType: "application/json", body: "" }),
      log: () => undefined,
      now: () => clock,
      reopenAfterMs: 60_000,
    });
    const off = hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    sources[0].fail();
    off();
    clock += 59_000;
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined)();
    expect(sources).toHaveLength(1);
    clock += 2_000;
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(2);
  });

  it("отказ разбирается ОДИН раз и называет причину словами края", async () => {
    // Приёмник событий браузера кода ответа не отдаёт: «владелец не объявлен»
    // (состояние поставки) и «край лёг» (дефект) выглядят одинаково — тишиной.
    // Один диагностический запрос делает их различимыми в журнале браузера.
    const sources: FakeSource[] = [];
    const said: string[] = [];
    let diagnosed = 0;
    const hub = new SubscriptionHub({
      open: (url) => {
        const s = new FakeSource(url);
        sources.push(s);
        return s;
      },
      diagnose: () => {
        diagnosed += 1;
        return Promise.resolve({ status: 501, contentType: "application/json", body: "no journal owner is declared for this edge" });
      },
      log: (m) => said.push(m),
    });
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    sources[0].fail();
    await Promise.resolve();
    await Promise.resolve();
    expect(diagnosed).toBe(1);
    expect(said.join("\n")).toContain("501");
  });
});

/**
 * ОБЪЯВЛЕННАЯ ПОСАДКА И СБОЙ — РАЗНЫЕ СОСТОЯНИЯ (#2016).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Перечень владельцев журналов у края ЗАКРЫТ, и пустой означает «никого»: край
 * отвечает «журналов не служу» ДО того, как прочтёт имя владельца. Значит этот
 * ответ относится к КРАЮ и одинаков для всех, а посадка без потока — законный
 * выбор оператора, а не поломка.
 *
 * Прежняя редакция хаба различие ВЫЧИСЛЯЛА и ВЫБРАСЫВАЛА: код ответа попадал в
 * строку журнала и больше никуда, поведение было одно на оба состояния. Цена
 * измерена браузером на подставном крае, отвечающем `501`: приёмник событий
 * даёт одну запись уровня `error` в журнале браузера, разбор отказа — вторую,
 * и платит их КАЖДЫЙ владелец на КАЖДОЙ странице.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * У КАЖДОГО ОТРИЦАНИЯ ЗДЕСЬ ЕСТЬ ПОЛОЖИТЕЛЬНЫЙ БЛИЗНЕЦ
 *
 * «Второй владелец потока не открыл» верно и для хаба, который не открывает
 * потока никогда, — то есть для хаба, сломанного целиком. Поэтому рядом с
 * каждым утверждением о молчании стоит тот же мир с ОДНИМ изменённым фактом
 * (код ответа края, состояние памяти, положение часов), в котором поток
 * открывается.
 */
describe("посадочный отказ края отличается от сбоя", () => {
  // Ссылки `verifies` здесь нет намеренно: своей задачи у предмета нет, а номер
  // соседней был бы ложной координатой — следующий читатель пошёл бы по ней и
  // нашёл другой предмет. Признак, по которому предмет найден, назван в шапке
  // выше: две записи уровня `error` в журнале браузера на подставном крае,
  // отвечающем «журналов не служу», и по паре на каждого следующего владельца.
  const WINDOW = 60_000;

  /** Дать осесть цепочке разбора: `then` → `catch` → `finally`. */
  const settle = async (): Promise<void> => {
    for (let i = 0; i < 8; i += 1) await Promise.resolve();
  };

  function hubWith(opts: {
    status?: number;
    /** Готовое обещание разбора; незаданное — отвечает `status` сразу. */
    pending?: Promise<{ status: number; contentType: string; body: string }>;
    available?: () => boolean;
    clock: { at: number };
    /** Общая память двух хабов — так выглядит переход между страницами. */
    cell?: { until: number };
    said?: string[];
    diagnosed?: { count: number };
  }) {
    const sources: FakeSource[] = [];
    const cell = opts.cell ?? { until: 0 };
    const hub = new SubscriptionHub({
      open: (url) => {
        const s = new FakeSource(url);
        sources.push(s);
        return s;
      },
      diagnose: () => {
        if (opts.diagnosed) opts.diagnosed.count += 1;
        return (
          opts.pending ??
          Promise.resolve({ status: opts.status ?? 501, contentType: "application/json", body: "" })
        );
      },
      log: (m) => opts.said?.push(m),
      now: () => opts.clock.at,
      reopenAfterMs: WINDOW,
      available: opts.available,
      recallEdgeSilence: () => cell.until,
      rememberEdgeSilence: (until) => {
        cell.until = until;
      },
    });
    return { hub, sources, cell };
  }

  const vpc = { owner: "vpc", kind: "vpc_network", projectId: "prj-1" };
  const compute = { owner: "compute", kind: "compute_instance", projectId: "prj-1" };

  it("«журналов не служу» гасит поток ВСЕМ владельцам и спрашивается ОДИН раз", async () => {
    const clock = { at: 1_000 };
    const diagnosed = { count: 0 };
    const { hub, sources } = hubWith({ status: 501, clock, diagnosed });
    hub.subscribe(vpc, () => undefined);
    sources[0].fail();
    await settle();

    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(1);
    expect(diagnosed.count).toBe(1);
  });

  it("а СБОЙ гасит только свой канал — соседний владелец открывается", async () => {
    // Положительный близнец утверждения выше. Мир отличается ОДНИМ фактом —
    // кодом ответа края; без него «второй поток не открылся» зеленело бы на
    // хабе, не открывающем потока никогда.
    const clock = { at: 1_000 };
    const { hub, sources } = hubWith({ status: 503, clock });
    hub.subscribe(vpc, () => undefined);
    sources[0].fail();
    await settle();

    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(2);
  });

  it("пока разбор идёт, нового потока не открывается — исход уже известен", async () => {
    const clock = { at: 1_000 };
    let release!: (v: { status: number; contentType: string; body: string }) => void;
    const pending = new Promise<{ status: number; contentType: string; body: string }>((res) => {
      release = res;
    });
    const { hub, sources } = hubWith({ pending, clock });
    hub.subscribe(vpc, () => undefined);
    sources[0].fail();

    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(1);

    // Разбор сказал «сбой» — значит молчал только канал, и сосед открывается.
    release({ status: 503, contentType: "application/json", body: "" });
    await settle();
    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(2);
  });

  it("память о посадке переживает переход между страницами", async () => {
    const clock = { at: 1_000 };
    const cell = { until: 0 };
    const first = hubWith({ status: 501, clock, cell });
    first.hub.subscribe(vpc, () => undefined);
    first.sources[0].fail();
    await settle();

    // Переход по разделу — полная перезагрузка: хаб новый, память та же.
    const second = hubWith({ status: 501, clock, cell });
    second.hub.subscribe(vpc, () => undefined);
    expect(second.sources).toHaveLength(0);
  });

  it("а с ПУСТОЙ памятью тот же хаб поток открывает", () => {
    // Положительный близнец: без него утверждение выше зеленело бы на хабе,
    // который не открывает потока ни при какой памяти.
    const clock = { at: 1_000 };
    const { hub, sources } = hubWith({ status: 501, clock, cell: { until: 0 } });
    hub.subscribe(vpc, () => undefined);
    expect(sources).toHaveLength(1);
  });

  it("окно посадочного молчания ИСТЕКАЕТ — включённая выкаткой возможность берётся", async () => {
    // Послабление обязано истекать само: посадку меняют выкаткой, и вечное
    // молчание означало бы, что поток не берётся до закрытия вкладки.
    const clock = { at: 1_000 };
    const cell = { until: 0 };
    const { hub, sources } = hubWith({ status: 501, clock, cell });
    hub.subscribe(vpc, () => undefined);
    sources[0].fail();
    await settle();

    clock.at += WINDOW - 1;
    hubWith({ status: 501, clock, cell }).hub.subscribe(vpc, () => undefined);
    expect(sources).toHaveLength(1);

    clock.at += 2;
    const after = hubWith({ status: 501, clock, cell });
    after.hub.subscribe(vpc, () => undefined);
    expect(after.sources).toHaveLength(1);
  });

  it("среда без приёмника событий молчит КРАЮ целиком и говорит об этом один раз", () => {
    const clock = { at: 1_000 };
    const said: string[] = [];
    const { hub, sources } = hubWith({ clock, said, available: () => false });
    hub.subscribe(vpc, () => undefined);
    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(0);
    expect(said).toHaveLength(1);
  });

  it("опрос остаётся в ОБОИХ состояниях: покрытия нет ни при посадке, ни при сбое", async () => {
    for (const status of [501, 503]) {
      const clock = { at: 1_000 };
      const { hub, sources } = hubWith({ status, clock });
      hub.subscribe(vpc, () => undefined);
      sources[0].emit("opened", opened(["vpc_network"]));
      expect(hub.covers(vpc)).toBe(true);
      sources[0].fail();
      await settle();
      expect(hub.covers(vpc)).toBe(false);
    }
  });

  it("разбор, который не ответил, НЕ запирает поток навсегда", () => {
    // «Не знаю» — третья категория, и выдавать её за «нет» нельзя. Край может
    // молчать (висящий запрос, оборванная связь), и признак «разбор идёт»,
    // снимаемый только ответом, оставил бы страницу на опросе до перезагрузки
    // вкладки — молча и навсегда. Ожидание получает ТО ЖЕ конечное окно.
    const clock = { at: 1_000 };
    // Обещание, которое не разрешается НИКОГДА: так выглядит молчащий край.
    const pending = new Promise<{ status: number; contentType: string; body: string }>(() => undefined);
    const { hub, sources } = hubWith({ pending, clock });
    hub.subscribe(vpc, () => undefined);
    sources[0].fail();

    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(1);

    clock.at += WINDOW + 1;
    hub.subscribe(compute, () => undefined);
    expect(sources).toHaveLength(2);
  });

  it("посадочный отказ назван ПОСАДКОЙ, а сбой — отказом владельца", async () => {
    const clock = { at: 1_000 };
    const posture: string[] = [];
    const broken: string[] = [];
    const a = hubWith({ status: 501, clock, said: posture });
    a.hub.subscribe(vpc, () => undefined);
    a.sources[0].fail();
    await settle();

    const b = hubWith({ status: 503, clock, said: broken, cell: { until: 0 } });
    b.hub.subscribe(vpc, () => undefined);
    b.sources[0].fail();
    await settle();

    expect(posture.join("\n")).toContain("ОБЪЯВЛЕННАЯ посадка");
    expect(broken.join("\n")).not.toContain("ОБЪЯВЛЕННАЯ посадка");
    expect(broken.join("\n")).toContain("vpc");
  });
});

/**
 * ПОТОЛОК ОДНОВРЕМЕННЫХ ПОТОКОВ: страница не занимает бюджет соединений
 * источника целиком.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ. Поток изменений живёт ДОЛГО и всё это время держит соединение
 * браузера. Соединений к одному источнику по http/1.1 браузер даёт шесть —
 * и это не запас, а весь бюджет: обычные запросы страницы берутся оттуда же.
 * Открой потоков ровно шесть — и ни один запрос к тому же источнику больше не
 * доедет НИКОГДА: ни чтение списка, ни загрузка модуля после перехода.
 *
 * ЧИСЛО ИЗМЕРЕНО, А НЕ ВЗЯТО ИЗ ПАМЯТИ. Одно-фактная лестница (то же дерево,
 * тот же источник, различие только в числе бесконечных потоков; обычный запрос
 * к тому же источнику спустя 1.5 с, окно ожидания 12 с):
 *
 *     потоков 0 → доехал, +1842 мс
 *     потоков 4 → доехал, +1846 мс
 *     потоков 5 → доехал, +1851 мс
 *     потоков 6 → НЕ доехал
 *     потоков 7 → НЕ доехал (седьмой поток и сам не открылся)
 *
 * ЧТО НАБЛЮДАЛОСЬ. Посадка объявила ШЕСТОГО владельца журнала, дашборд
 * подписывает по плитке на владельца — и страница заняла бюджет ровно целиком.
 * Две сквозные пробы консоли падали по 90 секунд с пустым экраном: переход по
 * прямому адресу отдавал документ за 2 мс, а его же связка не приезжала вовсе,
 * пока потоки не сняли при разборе прогона. Прочитано это было как «каркас не
 * отдал область арендатора» — то есть обвиняло страницу в том, что у неё
 * отобрали последнее соединение.
 *
 * ПОЧЕМУ ПОТОЛОК, А НЕ ОТКАЗ ОТ ПОТОКОВ. Опрос остаётся штатным путём и не
 * отзывается: владелец без потока покрытым не считается, и его список
 * перечитывается повторителем — медленнее, но верно. Потолок меняет ТОЛЬКО то,
 * скольким владельцам достаётся дешёвый путь.
 */
describe("потолок одновременных потоков", () => {
  const OWNERS = ["compute", "iam", "loadbalancer", "registry", "storage", "vpc"] as const;

  function hubCeiling(sources: FakeSource[], maxOpenStreams?: number) {
    return new SubscriptionHub({
      open: (url) => {
        const s = new FakeSource(url);
        sources.push(s);
        return s;
      },
      diagnose: () => Promise.resolve({ status: 503, contentType: "text/plain", body: "" }),
      log: () => undefined,
      maxOpenStreams,
    });
  }

  /** Подписать по одному виду на каждого объявленного посадкой владельца —
   *  ровно то, что делает дашборд: плитка на модуль, поток на владельца. */
  function subscribeEveryOwner(hub: SubscriptionHub): (() => void)[] {
    return OWNERS.map((owner) => hub.subscribe({ owner, kind: `${owner}_thing`, projectId: "prj-1" }, () => undefined));
  }

  it("шесть владельцев не дают шести потоков: бюджет источника не занимается целиком", () => {
    // Это и есть предмет. Без потолка здесь открывается ШЕСТЬ потоков — столько
    // же, сколько браузер даёт соединений, — и обычный запрос страницы к тому же
    // источнику не доезжает ни один.
    const sources: FakeSource[] = [];
    const hub = hubCeiling(sources);
    subscribeEveryOwner(hub);
    // Перепись парой: одно число не отличило бы «потолок сработал» от «никто не
    // подписался», а это ровно тот случай, ради которого проба и написана.
    expect({ ownersSubscribed: OWNERS.length, streamsOpened: sources.length }).toEqual({
      ownersSubscribed: 6,
      streamsOpened: 2,
    });
  });

  it("владелец без потока остаётся на опросе, а не молчит", () => {
    // Положительный контроль в той же пробе: покрытым обязан быть тот, кому
    // поток достался. Без него утверждение «не покрыт» зеленело бы на хабе,
    // который не покрывает вообще никого.
    const sources: FakeSource[] = [];
    const hub = hubCeiling(sources, 1);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.subscribe({ owner: "compute", kind: "compute_instance", projectId: "prj-1" }, () => undefined);
    // Кадр открытия отдаётся КАЖДОМУ заведённому потоку, а не первому: иначе
    // «не покрыт» было бы верно просто потому, что словаря никто не присылал, —
    // и проба зеленела бы на хабе без всякого потолка.
    for (const s of sources) s.emit("opened", opened(["vpc_network", "compute_instance"]));

    expect(hub.covers({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" })).toBe(true);
    expect(hub.covers({ owner: "compute", kind: "compute_instance", projectId: "prj-1" })).toBe(false);
  });

  it("освободившееся место достаётся ждущему владельцу", () => {
    // Потолок, который только запрещает, превратил бы первого подписчика в
    // вечного владельца места: ушёл его список — поток закрылся, а ждущий так и
    // остался бы на опросе до перезагрузки вкладки.
    const sources: FakeSource[] = [];
    const hub = hubCeiling(sources, 1);
    const offVpc = hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.subscribe({ owner: "compute", kind: "compute_instance", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(1);

    offVpc();
    expect(sources).toHaveLength(2);
    expect(new URL(sources[1].url, "http://stand").searchParams.get("owner")).toBe("compute");
  });

  it("потолок НЕ отбирает поток у того, кто уже подписан вторым видом того же владельца", () => {
    // Один поток на владельца — прежнее свойство, и потолок его не трогает:
    // второй вид того же владельца места не занимает вовсе.
    const sources: FakeSource[] = [];
    const hub = hubCeiling(sources, 1);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.subscribe({ owner: "vpc", kind: "vpc_subnet", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(1);
  });

  it("умолчание потолка строго ниже измеренного бюджета источника", () => {
    // Потолок, равный бюджету, — это отсутствие потолка: страница снова
    // занимает все соединения. Утверждается ПАРА, иначе «2 < 6» осталось бы
    // верным и при бюджете, выписанном от балды.
    expect({ budget: ORIGIN_CONNECTION_BUDGET, ceiling: MAX_OPEN_STREAMS }).toEqual({ budget: 6, ceiling: 2 });
    expect(MAX_OPEN_STREAMS).toBeLessThan(ORIGIN_CONNECTION_BUDGET);
  });
});
