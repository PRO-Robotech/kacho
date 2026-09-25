import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { orderedTransport } from "@shared/api/carrier-order";
import { SubscriptionHub, type EventSourceLike } from "./hub";
import { useResourceStream } from "./use-resource-stream";

/**
 * Провязка потока с чтением списка.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ
 *
 * Ровно два решения, от которых зависит поведение страницы: КОГДА опрос
 * выключается (только на доказанном покрытии) и ЧТО происходит по событию
 * (ровно одно перечитывание того ключа, который назвал вызывающий).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ПЕРЕЧИТЫВАНИЕ, А НЕ ПРИМЕНЕНИЕ СОСТОЯНИЯ ИЗ СОБЫТИЯ
 *
 * Соблазн велик — событие несёт полное состояние, — но состояние несут НЕ ВСЕ:
 * у балансировщика его нет ни у одного вида и ни у одного рода изменения, а у
 * снятия предмета его нет ни у кого. Значит применение состояния закрыло бы
 * девять видов из двенадцати и ни одного удаления, а рядом жил бы второй путь
 * для остальных — два источника одного состояния, которые разойдутся молча.
 *
 * Плюс список читается КУРСОРОМ и сужается сервером (`filter`, `listFilters`):
 * строка, вставленная в него клиентом, разошлась бы с тем, что владелец считает
 * страницей. Перечитывание оставляет истину у владельца и стоит одного запроса
 * НА ИЗМЕНЕНИЕ вместо одного запроса в три секунды НА ВСЯКИЙ СЛУЧАЙ.
 */

class FakeSource implements EventSourceLike {
  readyState = 0;
  private handlers = new Map<string, ((ev: MessageEvent<string>) => void)[]>();
  onerror: ((ev: Event) => void) | null = null;
  constructor(readonly url: string) {}
  addEventListener(name: string, fn: (ev: MessageEvent<string>) => void): void {
    const list = this.handlers.get(name) ?? [];
    list.push(fn);
    this.handlers.set(name, list);
  }
  close(): void {
    this.readyState = 2;
  }
  /** Терминальный отказ: приёмник закрыт, повтора не будет. */
  fail(): void {
    this.readyState = 2;
    this.onerror?.(new Event("error"));
  }
  emit(name: string, data: unknown): void {
    this.readyState = 1;
    for (const fn of this.handlers.get(name) ?? []) fn({ data: JSON.stringify(data), lastEventId: "p1" } as MessageEvent<string>);
  }
}

function setup(specId: string) {
  const sources: FakeSource[] = [];
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
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidated: unknown[][] = [];
  client.invalidateQueries = ((args: { queryKey?: unknown[] }) => {
    invalidated.push(args.queryKey ?? []);
    return Promise.resolve();
  }) as QueryClient["invalidateQueries"];

  let streamed = false;
  function Probe(): ReactNode {
    const r = useResourceStream({ specId, projectId: "prj-1", invalidate: [specId, "list"] }, hub);
    streamed = r.streamed;
    return null;
  }
  const view = render(
    <QueryClientProvider client={client}>
      <Probe />
    </QueryClientProvider>,
  );
  return { sources, invalidated, view, read: () => streamed };
}

const opened = (kinds: string[]) => ({
  opened: { position: "p0", caughtUp: true, honoredFilters: ["kinds", "project_id"], knownKinds: kinds, retainsEverything: true },
});

describe("страница снимает опрос только на ДОКАЗАННОМ покрытии", () => {
  // verifies #1021
  it("до служебного сообщения открытия опрос не снят", () => {
    const { read } = setup("networks");
    expect(read()).toBe(false);
  });

  it("вид назван словарём владельца — опрос снят", () => {
    const { sources, read } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network", "vpc_subnet"])));
    expect(read()).toBe(true);
  });

  it("вид словарём НЕ назван — опрос остаётся", () => {
    // Положительный контроль стоит строкой выше. Без него это отрицание
    // зеленело бы на хуке, который не снимает опрос никогда.
    const { sources, read } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_subnet"])));
    expect(read()).toBe(false);
  });

  it("поток отказал — ОПРОС ВОЗВРАЩАЕТСЯ, а не остаётся снятым навсегда", () => {
    // Это половина предмета, которую легко упустить: покрытие СНИМАЕТ опрос, и
    // если оно не вернётся в «нет», список замрёт молча — ни ошибки, ни пустого
    // ответа, просто ничего не меняется. Со стороны такой список неотличим от
    // верного, и заметить это можно только по тому, что он не меняется никогда.
    //
    // Положительный контроль стоит ПЕРВЫМ утверждением: без него «опрос
    // включён» зеленело бы на хуке, который не снимает его никогда.
    const { sources, read } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network"])));
    expect(read()).toBe(true);

    act(() => sources[0].fail());
    expect(read()).toBe(false);
  });

  it("спека без владельца журнала потока НЕ открывает вовсе", () => {
    // Тип машины журналом не ведётся ни одним владельцем: открывать поток к
    // предмету, которого никто не объявляет, значит получать `400` на каждой
    // перерисовке страницы.
    //
    // Здесь стояла спека `users` с доводом «iam журнала не объявляет» — довод
    // истёк вместе с предметом: журнал у службы доступа есть, и `users` теперь
    // ПОКРЫТА. Предмет пробы при этом не изменился, поэтому она не снята, а
    // переведена на спеку, которая и сегодня владельца не имеет.
    const { sources, read } = setup("machine-types");
    expect(sources).toHaveLength(0);
    expect(read()).toBe(false);
  });

  it("событие своего вида перечитывает НАЗВАННЫЙ ключ, и ровно его", () => {
    const { sources, invalidated } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network"])));
    expect(invalidated).toEqual([]);
    act(() =>
      sources[0].emit("event", {
        event: { position: "p1", kind: "vpc_network", resourceId: "net-1", projectId: "prj-1", change: "CREATED" },
      }),
    );
    expect(invalidated).toEqual([["networks", "list"]]);
  });

  it("событие ЧУЖОГО вида ничего не перечитывает", () => {
    const { sources, invalidated } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network", "vpc_subnet"])));
    act(() =>
      sources[0].emit("event", {
        event: { position: "p2", kind: "vpc_subnet", resourceId: "sub-1", projectId: "prj-1", change: "CREATED" },
      }),
    );
    expect(invalidated).toEqual([]);
  });

  it("снятие предмета перечитывает так же, как создание", () => {
    // У снятия состояния нет ни у одного владельца, и именно оно сообщает, что
    // строки больше нет. Отбрось его — удалённая строка осталась бы в списке.
    const { sources, invalidated } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network"])));
    act(() =>
      sources[0].emit("event", {
        event: { position: "p3", kind: "vpc_network", resourceId: "net-1", projectId: "prj-1", change: "DELETED" },
      }),
    );
    expect(invalidated).toEqual([["networks", "list"]]);
  });

  it("уход страницы закрывает поток", () => {
    const { sources, view } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network"])));
    view.unmount();
    expect(sources[0].readyState).toBe(2);
  });

  it("Р10 · поток, закрытый вокруг глагола, ставящего носитель, открыт после исхода — список перечитан ОДИН раз", async () => {
    // Пока потока нет, событий не приходит, а новый приёмник позиции прежнего не
    // несёт: без перечитывания изменение, случившееся в промежутке, до страницы
    // не дошло бы. Первое покрытие перечитывания не стоит — положительная и
    // отрицательная стороны в одной пробе.
    const { sources, invalidated, read } = setup("networks");
    act(() => sources[0].emit("opened", opened(["vpc_network"])));
    expect(read()).toBe(true);
    expect(invalidated).toEqual([]);

    const original = globalThis.fetch;
    let answer: (r: Response) => void = () => undefined;
    globalThis.fetch = () =>
      new Promise<Response>((resolve) => {
        answer = resolve;
      });
    try {
      let verb: Promise<Response> = Promise.resolve({} as Response);
      await act(async () => {
        verb = orderedTransport.fetch("/iam/v1/auth/password", { method: "POST" }, { setsCarrier: true });
        await Promise.resolve();
      });
      // До выпуска глагола поток закрыт, покрытия нет — список на опросе.
      expect(sources[0].readyState).toBe(2);
      expect(read()).toBe(false);
      expect(sources).toHaveLength(1);
      await act(async () => {
        answer({ ok: true, status: 200 } as Response);
        await verb;
      });
      // После исхода поток открыт снова; покрытие вернулось — одно перечитывание.
      expect(sources).toHaveLength(2);
      act(() => sources[1].emit("opened", opened(["vpc_network"])));
      expect(read()).toBe(true);
      expect(invalidated).toEqual([["networks", "list"]]);
    } finally {
      globalThis.fetch = original;
    }
  });
});
