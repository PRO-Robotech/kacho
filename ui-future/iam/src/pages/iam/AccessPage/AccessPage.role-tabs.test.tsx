// Выбор ролей при выдаче доступа — то, ЧТО ВИДИТ ОПЕРАТОР: какие наборы ролей
// ему предложены и какой он сейчас читает.
//
// ПРЕДМЕТ (#625). Вкладки приходят ПРОПОМ (`items`), а не детьми. Пока общий
// заменитель подменял `Tabs` пустым `<div>{children}</div>`, ни подписи вкладок,
// ни содержимое активной не рисовались вовсе — и утверждение о том, что
// выдающему предложены обе группы ролей, было бы истинным при любом составе,
// включая пустой.
//
// Почему это важно именно здесь. Своих ролей у организации может не быть, и
// тогда вкладка «Свои роли» — единственное место, где это сказано словами.
// Пустая вкладка без подписи неотличима от «страница не догрузилась», а выдающий
// доступ уходит выдавать системную роль шире нужного.
//
// Утверждается наблюдаемое: подписи ВСЕХ вкладок, содержимое ТОЛЬКО активной и
// смена активной по клику — то, что видит и делает оператор, а не разметка.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Role, User } from "@shared/api/iam";
import { contextApi } from "@shared/lib/context-store";

const roles: Role[] = [
  { id: "rol-1", name: "vpc.network.admin", is_system: true } as Role,
  { id: "rol-2", name: "vpc.subnet.view", is_system: true } as Role,
];

jest.unstable_mockModule("@shared/api/iam", () => ({
  IAM: { accessBindings: "/iam/v1/accessBindings", users: "/iam/v1/users" },
  SUBJECT_TYPE_ENUM: { USER: "USER", EMAIL: "EMAIL" },
  buildCreateAccessBindingBody: () => ({}),
  iamApi: {
    listRoles: () => Promise.resolve({ roles }),
    listUsers: () => Promise.resolve({ users: [] as User[] }),
  },
}));

jest.unstable_mockModule("@shared/components/molecules/PageHeaderSlot", () => ({
  useBreadcrumb: () => undefined,
  useHeaderRight: () => undefined,
  usePageTitle: () => undefined,
}));

const { AccessGrantPage } = await import("./AccessPage");

function renderGrant() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/iam/access/grant?scope=cloud"]}>
        <AccessGrantPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  contextApi.setProject({ id: "prj-1", name: "проект", accountId: "acc-1" });
});

afterEach(() => {
  contextApi.setProject(null);
});

describe("AccessGrantPage — наборы ролей объявлены вкладками", () => {
  it("оператору названы ОБА набора и сказано, сколько в каждом", async () => {
    renderGrant();

    // Ждём ответа края: до него счётчик в подписи равен нулю, и утверждение
    // «наборы названы» прошло бы на странице, которая ролей ещё не видела.
    await screen.findByRole("tab", { name: "Системные (1)" });
    const tabList = screen.getByRole("tablist");
    expect(
      within(tabList)
        .getAllByRole("tab")
        .map((t) => t.textContent),
    ).toEqual(["Системные (1)", "Свои роли (0)"]);
  });

  it("читается содержимое ТОЛЬКО активной вкладки", async () => {
    renderGrant();

    await screen.findByRole("tab", { name: "Системные (1)" });
    // Своих ролей нет — и вкладка «Свои роли» говорит об этом словами. Пока она
    // не выбрана, этой фразы на экране быть не должно, иначе оператор прочтёт
    // её как утверждение о системных ролях.
    expect(screen.queryByText("У вашей организации пока нет своих ролей.")).not.toBeInTheDocument();
    // Активная вкладка показывает выбор системных ролей, и в нём — модуль из
    // ответа края: «панель есть» без содержимого зеленело бы на пустой панели.
    // Выбор ролей — множественный, поэтому наружу он объявлен списком, а не
    // полем со значением; внутри — модуль из ответа края. «Панель есть» без
    // содержимого зеленело бы на пустой панели.
    const panel = screen.getByRole("tabpanel");
    expect(within(panel).getByRole("listbox")).toBeInTheDocument();
    expect(within(panel).getByText("vpc")).toBeInTheDocument();
  });

  it("выбор вкладки меняет и пометку выбранного, и содержимое", async () => {
    renderGrant();

    await screen.findByRole("tab", { name: "Системные (1)" });
    fireEvent.click(screen.getByRole("tab", { name: "Свои роли (0)" }));

    expect(screen.getByRole("tab", { name: "Свои роли (0)" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Системные (1)" })).toHaveAttribute("aria-selected", "false");
    const panel = screen.getByRole("tabpanel");
    expect(within(panel).getByText("У вашей организации пока нет своих ролей.")).toBeInTheDocument();
    // Парный контроль: содержимое прежней вкладки ушло, а не осталось под новой.
    expect(within(panel).queryByRole("listbox")).not.toBeInTheDocument();
  });
});
