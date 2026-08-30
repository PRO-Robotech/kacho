// Карточка блочного хранения перестаёт ОПРАШИВАТЬ, когда владелец назвал вид.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#1021)
//
// Эта оболочка — форк общей: у общей опрос снимается признаком покрытия, у
// копии стоял литерал `5_000` с причиной «журнала у storage нет». Журнал у
// блочного хранения ЕСТЬ — три вида, — и край объявляет его владельцем; то есть
// причина была выведена из собственного умолчания консоли, а не из дерева.
//
// ЧТО УТВЕРЖДАЕТСЯ. Наблюдаемое: уходит ли повторный запрос карточки. Ни один
// assert не читает исходник и не заглядывает в пропсы — свойство «опрос снят»
// проверяется тем, что запросов больше не становится, когда время идёт.
//
// ОТРИЦАНИЕ СТОИТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ, и пар здесь две:
//
//  · до кадра открытия карточка опрашивается — иначе «не опрашивается» зеленело
//    бы на странице, которая не читает вовсе;
//  · вид, которого владелец НЕ назвал (тип диска), остаётся на опросе при том же
//    открытом потоке — иначе утверждение зеленело бы на снятии опроса всюду,
//    то есть на списке, замирающем навсегда.

import { jest } from "@jest/globals";
import { render, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import type { EventSourceLike } from "@shared/lib/subscription/hub";
import { REGISTRY } from "@/lib/resource-registry";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;
const realEventSource = (globalThis as { EventSource?: unknown }).EventSource;

const CREATED = "2026-08-01T10:00:00Z";

/** Приёмник событий, которым хаб пользуется вместо браузерного. */
class FakeSource implements EventSourceLike {
  static live: FakeSource[] = [];
  readyState = 0;
  onerror: ((ev: Event) => void) | null = null;
  private handlers = new Map<string, ((ev: MessageEvent<string>) => void)[]>();

  constructor(readonly url: string) {
    FakeSource.live.push(this);
  }
  addEventListener(name: string, fn: (ev: MessageEvent<string>) => void): void {
    const list = this.handlers.get(name) ?? [];
    list.push(fn);
    this.handlers.set(name, list);
  }
  close(): void {
    this.readyState = 2;
  }
  /** Кадр открытия: владелец называет свой словарь видов. */
  announce(kinds: string[]): void {
    this.readyState = 1;
    const frame = { opened: { position: "p0", caughtUp: true, knownKinds: kinds } };
    for (const fn of this.handlers.get("opened") ?? []) {
      fn({ data: JSON.stringify(frame) } as MessageEvent<string>);
    }
  }
}

/** Сколько раз ушёл запрос карточки этого ресурса. */
let detailReads = 0;

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

function stubEdge(detailPath: string, row: Record<string, unknown>): void {
  globalThis.fetch = (input: RequestInfo | URL) => {
    const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    const u = new URL(raw, "http://console.test");
    if (u.pathname === detailPath) {
      detailReads += 1;
      return jsonOk(row);
    }
    // Непокрытый путь отвечает ОТКАЗОМ, а не пустотой: молчаливый пустой ответ
    // на неожиданный адрес зеленил бы утверждения о числе чтений.
    return Promise.resolve({
      ok: false,
      status: 404,
      statusText: "Not Found",
      text: () => Promise.resolve(JSON.stringify({ code: 5, message: `нет заглушки для ${u.pathname}` })),
    } as Response);
  };
}

function showCard(route: string, uid: string, specId: "volumes" | "disk-types") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/storage/${route}/${uid}`]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route
              path="/projects/:projectId/storage/:route/:uid/*"
              element={<ResourceShell spec={REGISTRY[specId]} />}
            />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Проходит объявленный интервал опроса и отдаёт очередь микрозадач. */
async function letThePollFire(): Promise<void> {
  await act(async () => {
    // Асинхронный прогон времени, а не `advanceTimersByTime`: перечитывание
    // ставится обещанием, и синхронный прогон возвращает управление до того,
    // как оно исполнится, — счётчик чтений тогда не двигается ни при каком
    // интервале, и положительный контроль был бы красным на исправном коде.
    await jest.advanceTimersByTimeAsync(12_000);
  });
}

beforeEach(() => {
  detailReads = 0;
  FakeSource.live = [];
  (globalThis as { EventSource?: unknown }).EventSource = FakeSource as unknown;
  jest.useFakeTimers({ doNotFake: ["queueMicrotask", "nextTick"] });
});

afterEach(() => {
  jest.useRealTimers();
  globalThis.fetch = realFetch;
  (globalThis as { EventSource?: unknown }).EventSource = realEventSource;
  jest.resetModules();
});

describe("карточка блочного хранения: опрос снимается покрытием, а не догадкой", () => {
  it("покрытый вид перестаёт опрашиваться, непокрытый — нет", async () => {
    // verifies #1021
    stubEdge("/storage/v1/volumes/vol-1", {
      id: "vol-1",
      name: "том",
      description: "",
      created_at: CREATED,
      labels: {},
      zone_id: "z-a",
      size_bytes: "1073741824",
      status: "READY",
    });
    showCard("volumes", "vol-1", "volumes");
    await waitFor(() => expect(detailReads).toBeGreaterThan(0));

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: пока владелец не назвал словарь, карточка
    // опрашивается. Без него утверждение ниже зеленело бы на странице, которая
    // не читает вовсе.
    const beforeAnnounce = detailReads;
    await letThePollFire();
    expect(detailReads).toBeGreaterThan(beforeAnnounce);

    // Поток открыт хабом на владельца `storage` — значит карта предметов его
    // называет. Пустой перечень означал бы, что карта молчит, и утверждение
    // ниже стало бы вакуумным.
    expect(FakeSource.live.map((s) => s.url)).toEqual(["/subscription/v1/events?owner=storage&projectId=prj-1"]);

    await act(async () => {
      FakeSource.live[0].announce(["storage_volume", "storage_snapshot", "storage_image"]);
      await Promise.resolve();
    });

    const afterAnnounce = detailReads;
    await letThePollFire();
    expect(detailReads).toBe(afterAnnounce);
  });

  it("вид, которого владелец не назвал, остаётся на опросе при том же потоке", async () => {
    // verifies #1021
    //
    // ЗАКОННЫЙ БЛИЗНЕЦ. Тип диска — предмет того же домена и той же оболочки, но
    // журнал его не ведёт: у его строк нет ни проекта, ни типа объекта модели
    // прав, поэтому вопрос «вправе ли вызывающий это видеть» задать нечем.
    // Снимись опрос и здесь — карточка замерла бы навсегда, а выглядело бы это
    // как «изменений не было».
    stubEdge("/storage/v1/diskTypes/dt-1", {
      id: "dt-1",
      name: "network-ssd",
      description: "",
      created_at: CREATED,
      labels: {},
    });
    showCard("disk-types", "dt-1", "disk-types");
    await waitFor(() => expect(detailReads).toBeGreaterThan(0));

    await act(async () => {
      for (const src of FakeSource.live) src.announce(["storage_volume", "storage_snapshot", "storage_image"]);
      await Promise.resolve();
    });

    const afterAnnounce = detailReads;
    await letThePollFire();
    expect(detailReads).toBeGreaterThan(afterAnnounce);
  });
});

// Ссылка на предмет стоит ВНУТРИ каждой пробы, а не над файлом: над файлом она
// пережила бы пробу, к которой относится.
export {};
