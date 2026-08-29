import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import type { ReactNode } from "react";
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

  it("спека без владельца журнала потока НЕ открывает вовсе", () => {
    // iam журнала не объявляет: открывать поток к несуществующему владельцу
    // значит получать `400` на каждой перерисовке страницы.
    const { sources, read } = setup("users");
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
});
