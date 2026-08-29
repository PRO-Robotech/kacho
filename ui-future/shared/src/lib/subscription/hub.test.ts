import { SubscriptionHub, type EventSourceLike } from "./hub";

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

function makeHub(sources: FakeSource[]) {
  return new SubscriptionHub({
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
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-1" }, () => undefined);
    hub.subscribe({ owner: "vpc", kind: "vpc_network", projectId: "prj-2" }, () => undefined);
    hub.subscribe({ owner: "compute", kind: "compute_instance", projectId: "prj-1" }, () => undefined);
    expect(sources).toHaveLength(3);
  });

  it("поток закрывается, когда ушёл последний подписчик", () => {
    // Потолок потоков на вызывающего — восемь; поток, переживший свою страницу,
    // занимает место, которого потом не хватит соседней вкладке.
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
    hub.subscribe({ owner: "nlb", kind: "nlb_listener", projectId: "prj-1" }, (e) => seen.push(e.change));
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
