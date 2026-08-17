// Карточка ресурса, АДРЕСУЕМОГО ПУТЁМ: пока сегмент пути закрыть нечем, запрос
// не уходит.
//
// Предмет. У ресурса под родителем адрес несёт подстановку:
// `/registry/v1/registries/{registryId}/repositories`. Список такого ребёнка
// оболочка уже сужает адресом и охраняет незакрытую подстановку
// (`resolveListPath` → `resolved:false` ⇒ запрос не уходит вовсе). Детальное
// чтение той же оболочки собиралось СКЛЕЙКОЙ — `${spec.apiPath}/${uid}` — и
// охраны не имело: подстановка уезжала в адрес ЛИТЕРАЛОМ, а `enabled: !!uid`
// разрешал запрос.
//
// То есть два чтения ОДНОГО ресурса расходились в дисциплине, и верно было одно.
// Со стороны это не отличить от обычного промаха: край отвечает отказом на
// адрес, которого не объявлял, а карточка показывает ошибку — как если бы
// ресурса не было. Отправленный литерал при этом виден только в журнале сети.
//
// Что утверждается ниже:
//   1. пока подстановка не закрыта, к адресу с литералом `{…}` НЕ уходит ни
//      одного запроса — утверждается сам запрос, а не картинка на экране;
//   2. пользователю сказано, что списка НЕ БЫЛО, а не что ресурс не найден:
//      «не спрашивали» и «спросили и не нашли» — разные утверждения о мире;
//   3. положительный контроль: ресурс с обычным адресом читается как прежде.
//      Без него отрицание зеленело бы и на оболочке, которая не запрашивает
//      НИЧЕГО и никогда.
//
// Фикстура не выдумана: адрес взят дословно у `repositories` реестра модуля
// registry (`registry/src/lib/resource-registry.tsx`), а спека передаётся
// оболочке пропом — общий реестр ресурсов, адресуемых путём, пока не несёт, и
// поставить туда запись ради пробы значило бы проверять собственную выдумку.

import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import type { ResourceSpec } from "@shared/lib/resource-spec";
import { ResourceShell } from "./ResourceShell";

const realFetch = globalThis.fetch;

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

/** Стенд отвечает 404 на путь, которого не объявляли: непокрытый запрос обязан
 *  быть виден отказом, а не молча превращаться в пустой ответ. */
function jsonMiss(path: string): Promise<Response> {
  return Promise.resolve({
    ok: false,
    status: 404,
    statusText: "Not Found",
    text: () => Promise.resolve(JSON.stringify({ code: 5, message: `no stub for ${path}` })),
  } as Response);
}

/** Адрес запроса из любой формы, которую принимает настоящий `fetch`: на
 *  `URL`/`Request` приведение по умолчанию дало бы `[object Object]`, то есть
 *  один и тот же путь для любого запроса — и утверждения об адресе стали бы
 *  истинными сразу. */
function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/** Записывает КАЖДЫЙ ушедший адрес — включая тот, который уходить не должен.
 *  Стенд намеренно не «понимает» литеральный путь: если бы он на него отвечал,
 *  проба зеленела бы на собственной снисходительности. */
function stubApi(detail: Record<string, unknown>): string[] {
  const calls: string[] = [];
  const stub: typeof globalThis.fetch = (input) => {
    const u = new URL(requestUrl(input), "http://console.test");
    // Путь берём СЫРЫМ, не через `u.pathname`: `URL` процентно кодирует фигурные
    // скобки (`%7BregistryId%7D`), и утверждение «в адресе нет `{`» стало бы
    // истинным при уехавшем литерале.
    calls.push(decodeURIComponent(u.pathname));
    const body = detail[decodeURIComponent(u.pathname)];
    return body ? jsonOk(body) : jsonMiss(u.pathname);
  };
  globalThis.fetch = stub;
  return calls;
}

const CREATED = "2026-08-01T10:00:00Z";

/** Общая часть спеки — то, что к предмету пробы отношения не имеет. */
const BASE: Omit<ResourceSpec, "id" | "route" | "apiPath" | "payloadKey"> = {
  singular: "Репозиторий",
  plural: "Репозитории",
  genitive: "Репозитория",
  accusative: "репозиторий",
  scope: "project",
  ops: { create: false, update: false, delete: false },
  columns: [{ header: "Имя", path: "name", format: "text" }],
  template: () => ({}),
};

/** Ресурс, адресуемый путём: подстановку `{registryId}` закрыть в этом маршруте
 *  нечем — родителя URL карточки не называет. */
const PATH_ADDRESSED: ResourceSpec = {
  ...BASE,
  id: "probe-repositories",
  route: "repositories",
  apiPath: "/registry/v1/registries/{registryId}/repositories",
  payloadKey: "repositories",
};

/** Тот же ресурс с обычным адресом — положительный контроль. */
const PLAIN: ResourceSpec = {
  ...BASE,
  id: "probe-plain",
  route: "repositories",
  apiPath: "/registry/v1/repositories",
  payloadKey: "repositories",
};

function showCard(spec: ResourceSpec, uid: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/registry/${spec.route}/${uid}`]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="/projects/:projectId/registry/:route/:uid/*" element={<ResourceShell spec={spec} />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Тот же ресурс, но открытый по адресу, который НАЗЫВАЕТ родителя. Имя
 *  параметра маршрута совпадает с именем подстановки — в этом всё правило
 *  связи. */
function showCardUnderParent(spec: ResourceSpec, registryId: string, uid: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/projects/prj-1/registry/registries/${registryId}/${spec.route}/${uid}`]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route
              path="/projects/:projectId/registry/registries/:registryId/:route/:uid/*"
              element={<ResourceShell spec={spec} />}
            />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

describe("карточка ресурса, адресуемого путём", () => {
  it("незакрытая подстановка — НИ ОДНОГО запроса с литералом в адресе", async () => {
    const calls = stubApi({});
    showCard(PATH_ADDRESSED, "nginx");

    // Ждём того же, чего ждал бы пользователь: оболочка отрисовала свой исход.
    await screen.findByText(/родител/i);
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
    // И ни одного запроса вообще — закрыть подстановку в этом маршруте нечем,
    // поэтому спрашивать не о чем. Утверждение сильнее предыдущего и нужно
    // отдельно: адрес БЕЗ литерала, собранный догадкой, был бы тем же дефектом
    // с другой стороны — запросом о ресурсе, которого никто не называл.
    expect(calls).toEqual([]);
  });

  it("сказано «списка не было», а не «ресурс не найден»", async () => {
    stubApi({});
    showCard(PATH_ADDRESSED, "nginx");

    // Текст — часть утверждения: «не найден» здесь был бы ложью о ресурсе,
    // которого никто не спрашивал.
    expect(await screen.findByText(/родител/i)).toBeInTheDocument();
    expect(screen.queryByText(/не найден/i)).not.toBeInTheDocument();
  });

  it("адрес, называющий родителя, закрывает подстановку и запрос уходит", async () => {
    // Тот же ресурс, что в отрицаниях выше, и та же спека — меняется ТОЛЬКО
    // адрес страницы. Значит утверждение здесь про источник сегмента, а не про
    // удачную фикстуру: маршрут назвал родителя, и запрос собрался.
    const calls = stubApi({
      "/registry/v1/registries/reg-1/repositories/nginx": {
        name: "nginx",
        registry_id: "reg-1",
        created_at: CREATED,
        labels: {},
      },
    });
    showCardUnderParent(PATH_ADDRESSED, "reg-1", "nginx");

    await waitFor(() => expect(calls).toContain("/registry/v1/registries/reg-1/repositories/nginx"));
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
    expect(screen.queryByText(/Адрес неполон/i)).not.toBeInTheDocument();
  });

  it("НЕ ТОТ параметр маршрута подстановку не закрывает", async () => {
    // Связь держится совпадением имён, а не порядком сегментов. Маршрут,
    // называющий родителя иначе, обязан оставить адрес неполным — иначе
    // «закрылось» означало бы «подставилось хоть что-нибудь».
    const calls = stubApi({});
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/projects/prj-1/registry/registries/reg-1/repositories/nginx"]}>
          <PageHeaderSlotProvider>
            <Routes>
              <Route
                path="/projects/:projectId/registry/registries/:someOtherName/:route/:uid/*"
                element={<ResourceShell spec={PATH_ADDRESSED} />}
              />
            </Routes>
          </PageHeaderSlotProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await screen.findByText(/родител/i);
    expect(calls).toEqual([]);
  });

  it("обычный адрес читается как прежде — положительный контроль", async () => {
    const calls = stubApi({
      "/registry/v1/repositories/nginx": { id: "nginx", name: "nginx", created_at: CREATED, labels: {} },
    });
    showCard(PLAIN, "nginx");

    await waitFor(() => expect(calls).toContain("/registry/v1/repositories/nginx"));
    // Карточка ПРОЧИТАЛАСЬ: на экране значение из ответа стенда, а не подпись
    // строки (подпись отрисовалась бы и на пустом ответе, если бы оболочка
    // вообще дошла до обзора).
    expect(await screen.findByText("01.08.2026, в 13:00")).toBeInTheDocument();
    // И блокирующая ветка на обычном адресе не срабатывает — иначе отрицания
    // выше зеленели бы на оболочке, которая блокирует всё подряд.
    expect(screen.queryByText(/Адрес неполон/i)).not.toBeInTheDocument();
  });
});
