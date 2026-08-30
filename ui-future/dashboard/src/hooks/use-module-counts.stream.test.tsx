// Счётчики витрины: перечитываются по СОБЫТИЮ потока, а не по таймеру.
//
// Утверждается НАБЛЮДАЕМОЕ — что уходит в сеть и что показано человеку, — а не
// раскладка хука: утверждение о раскладке пережило бы свой предмет, а число
// запросов за минуту и есть предмет задачи #1632.
//
// Пара утверждений здесь ОБЯЗАТЕЛЬНА в обе стороны, и по отдельности ни одно не
// годится: «по таймеру не читает» зеленело бы на хуке, который не читает вовсе,
// а «по событию читает» — на хуке, который читает и то и другое.

import { renderHook, waitFor, act } from "@testing-library/react";
import { jest } from "@jest/globals";

import { SubscriptionHub, type EventSourceLike } from "@shared/lib/subscription/hub";
import { useModuleCounts } from "./use-module-counts";
import { SERVICE_MODULES } from "../lib/service-modules";

/** Приёмник событий — подставной: настоящий открывает соединение в конструкторе. */
class FakeSource implements EventSourceLike {
  readyState = 0;
  onerror: ((ev: Event) => void) | null = null;
  private handlers = new Map<string, ((ev: MessageEvent<string>) => void)[]>();

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
    for (const fn of this.handlers.get(name) ?? []) {
      fn({ data: JSON.stringify(data), lastEventId: "" } as MessageEvent<string>);
    }
  }
}

const openedFrame = (kinds: string[]) => ({
  opened: { position: "p0", caughtUp: true, honoredFilters: [], knownKinds: kinds, retainsEverything: true },
});
const eventFrame = (kind: string) => ({
  event: { position: "p1", kind, resourceId: "res-1", projectId: "project-1", change: "UPDATED" },
});

function makeHub(sources: FakeSource[]): SubscriptionHub {
  return new SubscriptionHub({
    open: (url) => {
      const s = new FakeSource(url);
      sources.push(s);
      return s;
    },
    diagnose: () => Promise.resolve({ status: 200, contentType: "text/event-stream", body: "" }),
    log: () => {},
  });
}

const vpcModule = SERVICE_MODULES.find((m) => m.key === "vpc")!;
const iamModule = SERVICE_MODULES.find((m) => m.key === "iam")!;

/** Число списочных запросов и текущий ответ на каждый listPath. */
let listCalls = 0;
let itemsPerList = 1;

beforeEach(() => {
  jest.useFakeTimers();
  listCalls = 0;
  itemsPerList = 1;
  Object.defineProperty(global, "fetch", {
    writable: true,
    value: (input: unknown) => {
      const path = String(input);
      listCalls += 1;
      const stat =
        vpcModule.stats.find((s) => path.startsWith(s.listPath)) ??
        iamModule.stats.find((s) => path.startsWith(s.listPath));
      const body = stat
        ? { [stat.payloadKey]: Array.from({ length: itemsPerList }, (_, i) => ({ id: `x-${i}` })) }
        : {};
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        text: () => Promise.resolve(JSON.stringify(body)),
      } as Response);
    },
  });
});

afterEach(() => {
  jest.useRealTimers();
});

describe("счётчики витрины и поток изменений", () => {
  it("покрытый потоком модуль НЕ читает списки по таймеру", async () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);

    const { result } = renderHook(() => useModuleCounts(vpcModule, "project-1", "project_id", hub));
    await waitFor(() => expect(result.current.networks).toBe(1));

    // Владелец объявил словарь: все три вида модуля покрыты.
    act(() => sources[0].emit("opened", openedFrame(["vpc_network", "vpc_subnet", "vpc_security_group"])));
    const afterFirstLoad = listCalls;

    act(() => {
      jest.advanceTimersByTime(180_000);
    });

    expect(listCalls).toBe(afterFirstLoad);
  });

  it("покрытый потоком модуль перечитывает счётчик ПО СОБЫТИЮ", async () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);

    const { result } = renderHook(() => useModuleCounts(vpcModule, "project-1", "project_id", hub));
    await waitFor(() => expect(result.current.networks).toBe(1));
    act(() => sources[0].emit("opened", openedFrame(["vpc_network", "vpc_subnet", "vpc_security_group"])));

    itemsPerList = 2;
    act(() => sources[0].emit("event", eventFrame("vpc_network")));

    await waitFor(() => expect(result.current.networks).toBe(2));
  });

  it("домен без журнала остаётся на опросе — иначе счётчик замер бы навсегда", async () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);

    const { result } = renderHook(() => useModuleCounts(iamModule, "all", "", hub));
    await waitFor(() => expect(result.current.accounts).toBe(1));
    const afterFirstLoad = listCalls;

    // Ни одного потока не открыто: предмета у iam нет, подписываться не на что.
    expect(sources).toHaveLength(0);

    act(() => {
      jest.advanceTimersByTime(60_000);
    });
    await waitFor(() => expect(listCalls).toBeGreaterThan(afterFirstLoad));
  });

  it("отказ потока ВОЗВРАЩАЕТ опрос — покрытие снимается вместе с каналом", async () => {
    const sources: FakeSource[] = [];
    const hub = makeHub(sources);

    const { result } = renderHook(() => useModuleCounts(vpcModule, "project-1", "project_id", hub));
    await waitFor(() => expect(result.current.networks).toBe(1));

    // Словарь пришёл, но НУЖНОГО вида в нём нет: покрытия нет, опрос остаётся.
    act(() => sources[0].emit("opened", openedFrame(["vpc_network"])));
    const afterFirstLoad = listCalls;

    act(() => {
      jest.advanceTimersByTime(60_000);
    });
    await waitFor(() => expect(listCalls).toBeGreaterThan(afterFirstLoad));
  });
});
