// Отбор списка ПО МЕТКАМ — переход ЧЛЕНСТВА, а не ответ предиката (#1021, п.3).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Оси подписки объявлены неизменяемыми (`kinds` × `project` × `ids`), а метка
// мутабельна: ресурс входит в выборку и выходит из неё правкой метки. Сервер
// сказать «вышел из выборки» не может иначе как синтетическим снятием, которое
// подписчик прочитал бы как «ресурс удалён». Поэтому отбор по меткам делает
// КЛИЕНТ — тот, у кого есть оба состояния строки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТА ПРОБА ОТЛИЧАЕТСЯ ОТ `lib/list-label-filter.test.ts`
//
// Та проверяет ПРЕДИКАТ: подходит ли строка под условие. Предикат был написан,
// покрыт девятью случаями — и не имел в дереве НИ ОДНОГО вызывающего: перепись
// `rowMatchesLabels|parseLabelQuery|labelFilterActive` по не-тестовому дереву
// консоли давала ноль. То есть возможность была объявлена и неисполнима: строка,
// потерявшая метку, оставалась в списке навсегда, потому что списка, суженного
// метками, не существовало вовсе.
//
// Здесь утверждается ЧЛЕНСТВО: строка ушла из показанного списка и вернулась в
// него. Ответ предиката такого утверждения не делает и делать не может.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕЧИТЫВАНИЕ ЗДЕСЬ ВЫРАЖЕНО `invalidateQueries`
//
// Ровно этим оно выражено и в продукте: обработчик события потока
// (`useResourceStream`) не применяет состояние из события, а помечает ключ
// устаревшим. Дёргая тот же рычаг, проба идёт тем же путём, что и поток, — и не
// заводит второго способа перечитать список, который разошёлся бы с первым молча.
//
// Поток при этом здесь НЕ ОТКРЫТ и открыться не может: `EventSource` в jsdom нет,
// хаб отвечает «принимать нечем», признак покрытия остаётся ложным. Значит проба
// говорит о клиентском отборе, а не о потоке, — предмет потока живёт в
// `e2e/specs/subscription-list.spec.ts`, где есть браузер и край.

import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { requestUrl } from "@shared/test/fetch-capture";
import { ResourceListPage } from "./ResourceListPage";

const realFetch = globalThis.fetch;

/** Строки, которые край отдаёт ПРЯМО СЕЙЧАС: проба их подменяет между чтениями. */
let served: Record<string, unknown>[] = [];
let urls: string[] = [];

function stubList(payloadKey: string) {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    urls.push(requestUrl(input));
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [payloadKey]: served, next_page_token: "" })),
    } as Response);
  };
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderList(spec: (typeof REGISTRY)[string], at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={spec} panelForms />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, qc };
}

/** Поле отбора по меткам. Ищется ПО ПЛЕЙСХОЛДЕРУ, а не по роли: строк ввода на
 *  панели несколько, и роль их не различает. */
function labelBox(): HTMLElement {
  return screen.getByPlaceholderText(/метк/i);
}

function typeLabels(value: string) {
  fireEvent.change(labelBox(), { target: { value } });
}

/** Перечитать список так же, как это делает обработчик события потока. */
async function restreamWith(qc: QueryClient, rows: Record<string, unknown>[]) {
  served = rows;
  const before = urls.length;
  await qc.invalidateQueries();
  await waitFor(() => expect(urls.length).toBeGreaterThan(before));
}

const NET = "net-lbl-1";

describe("отбор по меткам: переход ЧЛЕНСТВА в показанном списке", () => {
  it("метка ушла — строка ушла из списка; вернулась — строка вернулась", async () => {
    // Обе половины утверждаются вместе. Одно только исчезновение зеленело бы на
    // отборе, который выбрасывает всё подряд; одно только появление — на отборе,
    // который не выбрасывает ничего.
    const spec = REGISTRY.networks;
    served = [{ id: NET, name: "netto", labels: { env: "prod" } }];
    stubList(spec.payloadKey);
    const { qc } = renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("netto");

    typeLabels("env=prod");
    await waitFor(() => expect(screen.getAllByText("netto").length).toBeGreaterThan(0));

    // Другой клиент снял метку. Список перечитан — строка условию больше не
    // отвечает, и остаться в нём не вправе.
    await restreamWith(qc, [{ id: NET, name: "netto", labels: {} }]);
    await waitFor(() => expect(screen.queryByText("netto")).toBeNull());

    // Метку вернули — строка обязана вернуться. Без этой половины проба зеленела
    // бы на отборе, который, однажды спрятав строку, не показывает её никогда.
    await restreamWith(qc, [{ id: NET, name: "netto", labels: { env: "prod" } }]);
    await waitFor(() => expect(screen.getAllByText("netto").length).toBeGreaterThan(0));
  });

  it("положительный контроль: без запроса меток строка видна при любых метках", async () => {
    // Иначе утверждения выше выполнялись бы на странице, где строк нет вовсе.
    const spec = REGISTRY.networks;
    served = [{ id: NET, name: "netto", labels: {} }];
    stubList(spec.payloadKey);
    renderList(spec, "/projects/p1/vpc/networks");
    expect((await screen.findAllByText("netto")).length).toBeGreaterThan(0);
  });

  it("значение метки сравнивается ЦЕЛИКОМ, а не подстрокой", async () => {
    // Поиск по имени — подстрочный, отбор по метке — точный: это РАЗНЫЕ вопросы,
    // и свести их значило бы сделать точный незадаваемым.
    const spec = REGISTRY.networks;
    served = [{ id: NET, name: "netto", labels: { env: "production" } }];
    stubList(spec.payloadKey);
    renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("netto");

    typeLabels("env=prod");
    await waitFor(() => expect(screen.queryByText("netto")).toBeNull());
  });

  it("отбор по метке СЧИТАЕТСЯ СУЖЕНИЕМ: пустой результат не зовёт создавать первый", async () => {
    // Правило 13 `ui.md`: пустой экран означает пустой ОТВЕТ КРАЯ. Край ответил
    // «есть», строку спрятал отбор — значит приглашение «Создайте первый ресурс»
    // человеку не полагается: это утверждение о ресурсах арендатора, и оно ложно.
    //
    // Утверждается ОТСУТСТВИЕ ПРИГЛАШЕНИЯ, а не наличие текста «ничего не
    // найдено»: `ResourceListPage` состояние `no-matches` вычисляет, но читает у
    // него ровно одну ветку (`showWelcome`) и своего текста для него не рисует —
    // пустая таблица показывает умолчание antd. Требовать здесь текст значило бы
    // утверждать про соседний предмет (он назван находкой в отчёте задачи), а
    // проба краснела бы не на своём дефекте.
    const spec = REGISTRY.networks;
    served = [{ id: NET, name: "netto", labels: { env: "prod" } }];
    stubList(spec.payloadKey);
    renderList(spec, "/projects/p1/vpc/networks");
    await screen.findAllByText("netto");
    // Положительный контроль приглашения: пока строки есть, его нет и без отбора,
    // поэтому его отсутствие ниже само по себе ничего не доказывало бы. Проверяем
    // ОБРАТНОЕ — что приглашение вообще умеет появляться на этом ресурсе.
    expect(spec.ops.create).toBe(true);

    typeLabels("env=stage");
    await waitFor(() => expect(screen.queryByText("netto")).toBeNull());
    expect(screen.queryByText(/^Создайте /i)).toBeNull();
  });

  it("законный близнец сужения: край ответил ПУСТЫМ — приглашение на месте", async () => {
    // Без этой пары утверждение выше зеленело бы на странице, которая приглашения
    // не показывает НИКОГДА, — и признак сужения был бы ни при чём.
    //
    // Узнаётся по ГЛАГОЛУ, а не по полному тексту: приглашение у ресурса своё
    // («Создайте вашу первую облачную сеть» против общего «Создайте первый
    // ресурс…»), и проба, привязанная к тексту одного, краснела бы у соседа.
    const spec = REGISTRY.networks;
    served = [];
    stubList(spec.payloadKey);
    renderList(spec, "/projects/p1/vpc/networks");
    expect(await screen.findByText(/^Создайте /i)).toBeTruthy();
  });
});

describe("законный близнец: ресурс, у которого меток нет", () => {
  it("аккаунты поля отбора по меткам НЕ получают", async () => {
    // Поле без источника не показывается (правило 9 `ui.md`). Ручка, сужающая по
    // тому, чего у ресурса нет, отвечала бы «ничего не найдено» на любой ввод.
    //
    // Близнец обязателен: без него «поле есть у сетей» зеленело бы на поле,
    // приклеенном ко всем спискам подряд.
    const spec = REGISTRY.accounts;
    expect((spec.columns ?? []).some((c) => c.path === "labels")).toBe(false);
    served = [{ id: "acc-1", name: "alpha" }];
    stubList(spec.payloadKey);
    renderList(spec, "/iam/accounts");
    await screen.findAllByText("alpha");

    expect(screen.queryByPlaceholderText(/метк/i)).toBeNull();
  });
});
