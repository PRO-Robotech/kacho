// Опция `enabled` у `useResourceList` (kacho#2925, полоса G4; замысел NTF-6 З13,
// CX6-31, условие (а) пересверки `0fff30f0`, M32).
//
// Поток изменений открывается ВНУТРИ хука (`useResourceStream`), поэтому
// выключенное чтение обязано гасить и его, а не только запрос: иначе страница с
// источником извне держала бы открытым поток, по которому ей нечего
// перечитывать. «Ни потока» держит счёт открытий через подменённый транспорт
// (приёмник событий браузера, который строит клиент потока), а не чтение кода.
//
// Близнец — то же без опции: запрос уходит и поток открывается. Без него «0»
// зеленело бы на подмене, не считающей ничего.

import { jest } from "@jest/globals";
import { act, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { REGISTRY } from "./resource-registry";
import { useResourceList } from "./use-resource-list";

const realFetch = globalThis.fetch;
const realEventSource = (globalThis as { EventSource?: unknown }).EventSource;

// Вид из словаря потока, без серверного сужения.
const spec = REGISTRY["network-interfaces"];

let requested: string[] = [];
let streamOpens: string[] = [];

class CountingEventSource {
  readyState = 0;
  onerror: ((ev: Event) => void) | null = null;
  constructor(readonly url: string) {
    streamOpens.push(url);
  }
  addEventListener(): void {}
  close(): void {
    this.readyState = 2;
  }
}

beforeEach(() => {
  jest.useFakeTimers();
  requested = [];
  streamOpens = [];
  (globalThis as { EventSource?: unknown }).EventSource = CountingEventSource;
  globalThis.fetch = (input: RequestInfo | URL) => {
    requested.push(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      headers: new Headers({ "content-type": "application/json" }),
      text: () => Promise.resolve(JSON.stringify({ [spec.payloadKey]: [] })),
    } as Response);
  };
});

afterEach(() => {
  jest.useRealTimers();
  globalThis.fetch = realFetch;
  (globalThis as { EventSource?: unknown }).EventSource = realEventSource;
});

function mount(enabled: boolean | undefined) {
  function Probe(): ReactNode {
    useResourceList(spec, "project_id", "prj-1", undefined, undefined, enabled === undefined ? undefined : { enabled });
    return null;
  }
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <Probe />
    </QueryClientProvider>,
  );
}

async function advance(ms: number): Promise<void> {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
}

const listRequests = () => requested.filter((u) => new URL(u, "http://console.test").pathname === spec.apiPath);

describe("useResourceList: enabled гасит и запрос, и поток", () => {
  it("enabled: false — запросов 0 и открытий потока 0 за 10 с", async () => {
    // verifies #2925
    mount(false);
    await advance(10_000);
    process.stdout.write(`[G4] enabled:false — запросов ${listRequests().length}, открытий потока ${streamOpens.length} за 10 с\n`);
    expect(listRequests()).toEqual([]);
    expect(streamOpens).toEqual([]);
  });

  it("близнец без опции: запрос уходит, опрос идёт, поток открывается", async () => {
    // verifies #2925
    mount(undefined);
    await advance(3_000);
    process.stdout.write(`[G4] без опции — запросов ${listRequests().length} за 3 с, открытий потока ${streamOpens.length}\n`);
    expect(listRequests().length).toBeGreaterThanOrEqual(2);
    expect(streamOpens.length).toBeGreaterThanOrEqual(1);
  });
});
