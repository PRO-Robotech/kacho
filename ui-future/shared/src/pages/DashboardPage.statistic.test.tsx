// Сводка на плашке сервиса — то, ЧТО ВИДИТ ОПЕРАТОР, открывая консоль.
//
// ПРЕДМЕТ (#625). Подпись и значение приходят `Statistic`-у ПРОПАМИ (`title`,
// `value`), а не детьми. Пока общий заменитель подменял это имя пустым
// `<div>{children}</div>`, оба уезжали в АТРИБУТЫ DOM — настоящий antd таких
// атрибутов не производит ни одного, — и плашка была наблюдаема ровно как
// пустой прямоугольник: проба «дашборд открылся» зеленела бы при любых числах,
// включая ни одного.
//
// Почему это важнее вида. Плашка — первое, что видит вошедший, и единственное
// место, где «в проекте есть ресурсы» отличимо от «проект пуст» без обхода
// разделов. Подпись без значения и значение без подписи одинаково бесполезны,
// поэтому утверждается ПАРА.
//
// Утверждается наблюдаемое: текст внутри плашки своего модуля. Привязка к
// плашке несущая — числа трёх модулей на одной странице, и утверждение «где-то
// на экране есть 2» прошло бы на чужом счётчике.

import { render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { contextApi } from "@shared/lib/context-store";
import { requestUrl } from "@shared/test/fetch-capture";
import { DashboardPage } from "./DashboardPage";

const realFetch = globalThis.fetch;

/**
 * Край, отвечающий РАЗНЫМ числом на разные вопросы.
 *
 * Один ответ на все запросы был бы дублёром снисходительнее настоящего края: он
 * дал бы одно и то же число всем счётчикам, и проба зеленела бы на консоли,
 * которая перепутала счётчики местами (регресс KAC-171, из-за которого lookup в
 * продукте ведётся ПО КЛЮЧУ, а не по индексу).
 */
const SIZES: Record<string, { key: string; n: number }> = {
  "/vpc/v1/networks": { key: "networks", n: 2 },
  "/vpc/v1/subnets": { key: "subnets", n: 5 },
  "/vpc/v1/securityGroups": { key: "security_groups", n: 1 },
};

/** `pending` — адреса, на которые край НЕ ОТВЕЧАЕТ: состояние «неизвестно»
 *  иначе неотличимо от гонки с ещё не пришедшим ответом, и утверждение о
 *  прочерке было бы вакуумным (оно проходило бы до прихода любого числа). */
function stub(pending: string[] = []) {
  globalThis.fetch = (input: RequestInfo | URL) => {
    const url = requestUrl(input);
    if (pending.some((p) => url.includes(p))) return new Promise<Response>(() => undefined);
    const hit = Object.entries(SIZES).find(([path]) => url.includes(path));
    const body = hit ? { [hit[1].key]: Array.from({ length: hit[1].n }, (_, i) => ({ id: `x-${i}` })) } : {};
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(body)),
    } as Response);
  };
}

function renderDashboard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/dashboard"]}>
        <PageHeaderSlotProvider>
          <DashboardPage />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  // Счётчики VPC читаются под выбранным проектом: без него плашка объявлена
  // недоступной и чисел не показывает вовсе.
  contextApi.setProject({ id: "prj-1", name: "проект", accountId: "acc-1" });
  stub();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  contextApi.setProject(null);
});

const tile = (key: string) => screen.getByTestId(`dashboard-tile-${key}`);

describe("DashboardPage — сводка на плашке сервиса", () => {
  it("показывает ПАРУ «подпись — значение» по каждой метрике модуля", async () => {
    renderDashboard();

    const vpc = tile("vpc");
    // Ждём первого числа: до ответа края у метрик стоит прочерк.
    expect(await within(vpc).findByText("2")).toBeInTheDocument();
    for (const caption of ["Сетей", "Подсетей", "Групп безопасности"]) {
      expect(within(vpc).getByText(caption)).toBeInTheDocument();
    }
    for (const value of ["2", "5", "1"]) {
      expect(within(vpc).getByText(value)).toBeInTheDocument();
    }
  });

  it("числа стоят у СВОЕЙ плашки, а не где-нибудь на экране", async () => {
    // Парный контроль к утверждению выше: без него оно прошло бы на консоли,
    // которая свела все счётчики в один модуль (ровно тот регресс, ради
    // которого продукт ищет счётчики по ключу, а не по индексу).
    renderDashboard();

    const vpc = tile("vpc");
    await within(vpc).findByText("2");
    expect(within(tile("compute")).queryByText("Сетей")).not.toBeInTheDocument();
    expect(within(tile("iam")).queryByText("Подсетей")).not.toBeInTheDocument();
  });

  it("метрика без ответа края подписана ПРОЧЕРКОМ, а не нулём", async () => {
    // Ноль — утверждение «ресурсов нет»; его край не делал. Прочерк говорит
    // «неизвестно», и это разные вещи для того, кто по числу принимает решение.
    //
    // Край молчит ТОЛЬКО про машины, а про сети отвечает: без этого утверждение
    // было бы вакуумным — прочерк стоит и в первый миг после монтирования, до
    // прихода любого ответа, и проба зеленела бы, ничего не дождавшись.
    stub(["/compute/v1/instances"]);
    renderDashboard();

    await within(tile("vpc")).findByText("2");
    const compute = tile("compute");
    expect(within(compute).getByText("Машин")).toBeInTheDocument();
    expect(within(compute).getByText("—")).toBeInTheDocument();
    expect(within(compute).queryByText("0")).not.toBeInTheDocument();
  });

  it("ответивший край показывает ЧИСЛО там, где молчащий показывал прочерк", async () => {
    // Контроль в обратную сторону к утверждению выше: без него «прочерк» было
    // бы верно и на консоли, которая не показывает чисел никогда.
    SIZES["/compute/v1/instances"] = { key: "instances", n: 7 };
    try {
      stub();
      renderDashboard();

      const compute = tile("compute");
      expect(await within(compute).findByText("7")).toBeInTheDocument();
      expect(within(compute).queryByText("—")).not.toBeInTheDocument();
    } finally {
      delete SIZES["/compute/v1/instances"];
    }
  });
});
