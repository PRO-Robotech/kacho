import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { User } from "@shared/api/iam";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import { contextApi } from "@shared/lib/context-store";
import type { UsersPage as UsersPageExport } from "./UsersPage";
import { antdDouble } from "@/test/antd-double";

interface MutationOpts {
  method: string;
  path: (body: unknown) => string;
  successText?: string;
}

const listUsers = jest.fn<() => Promise<{ users?: User[] }>>();
const mutations: MutationOpts[] = [];
const runs: { method: string; body: unknown }[] = [];

// Свой дублёр antd — общий бросает на `rowKey="id"`, не подставляет значение
// ячейки и теряет подписи подтверждений; разбор — в шапке @/test/antd-double.
jest.unstable_mockModule("antd", () => antdDouble);

jest.unstable_mockModule("@shared/api/iam", () => ({
  IAM: { users: "/iam/v1/users" },
  iamApi: { listUsers },
}));

jest.unstable_mockModule("@shared/components/organisms/iam/IamCommon", () => ({
  useIamMutation: (opts: MutationOpts) => {
    mutations.push(opts);
    return {
      run: (body: unknown) => {
        runs.push({ method: opts.method, body: opts.path(body) });
        return Promise.resolve(undefined);
      },
      submitting: false,
    };
  },
  fmtTs: (v?: string) => v ?? "—",
  CopyableMonoId: ({ id }: { id?: string }) => <span>{id ?? ""}</span>,
  groupedRoleOptions: () => [],
}));

const auth = { whoami: null } as unknown as AuthContextValue;
jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({ useAuth: () => auth }));

jest.unstable_mockModule("@shared/components/molecules/PageHeaderSlot", () => ({
  useBreadcrumb: () => undefined,
  useHeaderRight: () => undefined,
}));

jest.unstable_mockModule("@shared/components/organisms/form/FormShell", () => ({
  FormShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
jest.unstable_mockModule("@shared/components/organisms/form/FormFooter", () => ({ FormFooter: () => null }));
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { success: jest.fn(), error: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));
jest.unstable_mockModule("@/components/organisms/iam/IamListShell", () => ({
  IamListShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  useTableScrollY: () => ({ wrapRef: { current: null }, scrollY: 100 }),
}));

let UsersPage: typeof UsersPageExport;

const user = (over: Partial<User>): User =>
  ({ id: "usr-1", email: "ivan@example.test", invite_status: "ACTIVE", ...over }) as User;

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <UsersPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Строка таблицы, содержащая указанную почту. */
const rowOf = (container: HTMLElement, email: string): HTMLElement => {
  const row = [...container.querySelectorAll("tbody tr")].find((tr) => tr.textContent?.includes(email));
  if (!row) throw new Error(`строки с «${email}» нет`);
  return row as HTMLElement;
};

describe("UsersPage — запрет участия и его снятие", () => {
  beforeAll(async () => {
    ({ UsersPage } = await import("./UsersPage"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    mutations.length = 0;
    runs.length = 0;
    auth.whoami = null;
    window.localStorage.clear();
    contextApi.setAccount({ id: "acc-1", name: "Первый" });
    listUsers.mockResolvedValue({ users: [] });
  });

  it("запрет и снятие — ДЕЙСТВИЯ по ресурсу, а не правка поля с маской", () => {
    renderPage();

    const actions = mutations.filter((m) => m.method === "ACTION");
    // Оба направления обязаны быть провязаны: односторонний контроль оставил бы
    // запрещённого без пути возврата прямо в консоли.
    expect(actions).toHaveLength(2);
    expect(actions.map((m) => m.path("usr-9"))).toEqual(["/iam/v1/users/usr-9:block", "/iam/v1/users/usr-9:unblock"]);
  });

  it("действующему участнику предлагает запрет и называет цену запрета", async () => {
    listUsers.mockResolvedValue({ users: [user({ invite_status: "ACTIVE" })] });

    const { container } = renderPage();

    const row = await waitFor(() => rowOf(container, "ivan@example.test"));
    expect(row).toHaveTextContent("Запретить участие?");
    // Две вещи, про которые спрашивают в инциденте.
    expect(row).toHaveTextContent("Уже выданный токен доживёт свой срок");
    expect(row).toHaveTextContent("Участие в других аккаунтах не затрагивается");
  });

  it("запрет уходит по адресу именно этого участника и только после подтверждения", async () => {
    listUsers.mockResolvedValue({ users: [user({ id: "usr-9" })] });

    const { container } = renderPage();

    const row = await waitFor(() => rowOf(container, "ivan@example.test"));
    expect(runs).toHaveLength(0);

    fireEvent.click(within(row).getByRole("button", { name: "Запретить" }));

    expect(runs).toEqual([{ method: "ACTION", body: "/iam/v1/users/usr-9:block" }]);
  });

  it("запрещённому предлагает вернуть участие, а не запретить повторно", async () => {
    listUsers.mockResolvedValue({ users: [user({ id: "usr-9", invite_status: "BLOCKED" })] });

    const { container } = renderPage();

    const row = await waitFor(() => rowOf(container, "ivan@example.test"));
    expect(row).toHaveTextContent("Вернуть участие?");
    expect(row).not.toHaveTextContent("Запретить участие?");

    fireEvent.click(within(row).getByRole("button", { name: "Вернуть" }));

    expect(runs).toEqual([{ method: "ACTION", body: "/iam/v1/users/usr-9:unblock" }]);
  });

  it("неподтверждённому приглашению запрет недоступен — и причина названа", async () => {
    listUsers.mockResolvedValue({ users: [user({ invite_status: "PENDING" })] });

    const { container } = renderPage();

    const row = await waitFor(() => rowOf(container, "ivan@example.test"));
    // Живая кнопка обещала бы возможность, которой нет: backend такой вызов
    // отвергает. Приглашение отзывают, а не разблокируют.
    expect(row).not.toHaveTextContent("Запретить участие?");
    expect(row).not.toHaveTextContent("Вернуть участие?");
    expect(within(row).getByTitle(/Приглашение ещё не подтверждено/)).toBeInTheDocument();
  });

  it("запрет СЕБЕ предупреждает о необратимости до нажатия", async () => {
    auth.whoami = { user_id: "usr-9" } as AuthContextValue["whoami"];
    listUsers.mockResolvedValue({ users: [user({ id: "usr-9" })] });

    const { container } = renderPage();

    const row = await waitFor(() => rowOf(container, "ivan@example.test"));
    expect(row).toHaveTextContent("Запретить участие СЕБЕ?");
    // Три вещи, которые обязаны быть сказаны прямо.
    expect(row).toHaveTextContent("Снять запрет самостоятельно будет НЕЛЬЗЯ");
    expect(row).toHaveTextContent("восстановление пароля блокировку не снимает");
    expect(row).toHaveTextContent("администратор аккаунта или администратор облака");
    // Подтверждение названо иначе, чем в обычном случае, — чтобы нажатие «по
    // привычке» не прошло незамеченным.
    expect(within(row).getByRole("button", { name: "Да, запретить себе" })).toBeInTheDocument();
  });

  it("чужая строка остаётся обычным запретом, даже когда своя рядом", async () => {
    // Положительный контроль к предыдущему: без него предупреждение про «себе»
    // зеленело бы и на компоненте, который показывает его всем подряд.
    auth.whoami = { user_id: "usr-9" } as AuthContextValue["whoami"];
    listUsers.mockResolvedValue({
      users: [user({ id: "usr-9" }), user({ id: "usr-10", email: "petr@example.test" })],
    });

    const { container } = renderPage();

    const other = await waitFor(() => rowOf(container, "petr@example.test"));
    expect(other).toHaveTextContent("Запретить участие?");
    expect(other).not.toHaveTextContent("Запретить участие СЕБЕ?");
  });

  it("пустой список объясняет, откуда берутся пользователи", async () => {
    const { container } = renderPage();

    // Ждать надо ИМЕННО объяснение: строка «User'ов нет.» — заглушка таблицы, она
    // появляется и во время загрузки, поэтому ожидание по ней сработало бы раньше
    // предмета утверждения.
    await waitFor(() => expect(container).toHaveTextContent("magic-link"));
    expect(container).toHaveTextContent("User'ов нет");
  });
});
