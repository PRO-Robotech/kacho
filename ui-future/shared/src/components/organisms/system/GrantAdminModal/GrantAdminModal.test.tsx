// Выдача прав администратора кластера — что видит и что отправляет человек.
//
// Предмет — три свойства, каждое из которых ломается молча:
//
//  1. в подсказку не попадает тот, кому выдавать НЕЧЕГО: приглашённый, но не
//     зарегистрированный (`PENDING`) и заблокированный (`BLOCKED`) войти не
//     могут вовсе — выдача им создаёт привязку, которой никто не воспользуется,
//     и при этом выглядит исполненной;
//  2. один человек, приглашённый в несколько аккаунтов, приезжает несколькими
//     строками с одной почтой — подсказка обязана показать его один раз, иначе
//     выбирающий не понимает, чем строки отличаются (а не отличаются они ничем:
//     права кластера аккаунта не знают);
//  3. отказ края НЕ закрывает окно. Закрывшееся окно читается как успех, и
//     человек уходит, не выдав ничего.
//
// Подсказки рисует ОБЩИЙ стенд-заменитель (#1348): местная подмена
// `AutoComplete` здесь стояла ровно потому, что общий отдавал его псевдонимом
// текстового поля и списка не рисовал. Предмет подмены снят — снята и она,
// иначе это два места об одном предмете, из которых расходиться будет молча.
// Подсказки ищутся по их роли, а поле — по подсказке-заполнителю: и то и другое
// производит настоящая библиотека, в отличие от признаков прежней подмены.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const listUsers = jest.fn<() => Promise<{ users: unknown[] }>>();
const grantAdmin = jest.fn<(id: string) => Promise<{ operation?: { id: string } }>>();
const toastError = jest.fn();
const toastSuccess = jest.fn();

jest.unstable_mockModule("@shared/api/iam", () => ({ iamApi: { listUsers } }));
jest.unstable_mockModule("@shared/api/cluster", () => ({ clusterApi: { grantAdmin } }));
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));
jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useOperation: () => ({ data: undefined }),
  useInvalidateResourceList: () => jest.fn(),
}));

const { GrantAdminModal } = await import("./GrantAdminModal");

const user = (over: Record<string, unknown>) => ({
  id: "usr-1",
  email: "a@example.com",
  display_name: "A",
  invite_status: "ACTIVE",
  ...over,
});

function show() {
  const onClose = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <GrantAdminModal open onClose={onClose} />
    </QueryClientProvider>,
  );
  return { onClose };
}

const optionLabels = () => screen.queryAllByRole("option").map((o) => o.textContent ?? "");
const submit = () => screen.getByTestId<HTMLButtonElement>("grant-admin-submit");

beforeEach(() => {
  jest.clearAllMocks();
  listUsers.mockResolvedValue({ users: [] });
  grantAdmin.mockResolvedValue({ operation: { id: "op-1" } });
});

describe("GrantAdminModal", () => {
  it("показывает пользователя, который может войти", async () => {
    // Положительный контроль отрицаний ниже: без него «никого не показывает»
    // удовлетворило бы их все разом.
    listUsers.mockResolvedValue({ users: [user({ id: "usr-ok", email: "ok@example.com" })] });
    show();

    await waitFor(() => expect(optionLabels().join(" ")).toContain("ok@example.com"));
  });

  it("не предлагает приглашённого и заблокированного — выдавать им нечего", async () => {
    listUsers.mockResolvedValue({
      users: [
        user({ id: "usr-ok", email: "ok@example.com" }),
        user({ id: "usr-p", email: "pending@example.com", invite_status: "PENDING" }),
        user({ id: "usr-b", email: "blocked@example.com", invite_status: "BLOCKED" }),
      ],
    });
    show();

    await waitFor(() => expect(optionLabels()).toHaveLength(1));
    const shown = optionLabels().join(" ");
    expect(shown).toContain("ok@example.com");
    expect(shown).not.toContain("pending@example.com");
    expect(shown).not.toContain("blocked@example.com");
  });

  it("один человек из двух аккаунтов показан один раз", async () => {
    listUsers.mockResolvedValue({
      users: [
        user({ id: "usr-1", email: "same@example.com", display_name: "первый аккаунт" }),
        user({ id: "usr-2", email: "SAME@example.com", display_name: "второй аккаунт" }),
      ],
    });
    show();

    await waitFor(() => expect(optionLabels()).toHaveLength(1));
    expect(optionLabels().join(" ")).toContain("same@example.com");
  });

  it("до выбора пользователя выдать нельзя", async () => {
    listUsers.mockResolvedValue({ users: [user({})] });
    show();

    await waitFor(() => expect(optionLabels()).toHaveLength(1));
    expect(submit().disabled).toBe(true);
  });

  it("выбранный отправляется по своему идентификатору, а не по почте", async () => {
    listUsers.mockResolvedValue({ users: [user({ id: "usr-777", email: "pick@example.com" })] });
    show();

    await waitFor(() => expect(optionLabels()).toHaveLength(1));
    fireEvent.click(screen.getAllByRole("option")[0]);
    await waitFor(() => expect(submit().disabled).toBe(false));
    fireEvent.click(submit());

    await waitFor(() => expect(grantAdmin).toHaveBeenCalledWith("usr-777"));
  });

  it("отказ края показан и окно НЕ закрыто", async () => {
    listUsers.mockResolvedValue({ users: [user({ id: "usr-777" })] });
    grantAdmin.mockRejectedValue(new Error("нет прав"));
    const { onClose } = show();

    await waitFor(() => expect(optionLabels()).toHaveLength(1));
    fireEvent.click(screen.getAllByRole("option")[0]);
    await waitFor(() => expect(submit().disabled).toBe(false));
    fireEvent.click(submit());

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("нет прав"));
    // Закрывшееся окно читается как успех — а выдачи не было.
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId("grant-admin-modal-body")).toBeInTheDocument();
  });

  // ── область поиска (#528) ───────────────────────────────────────────────────

  it("ввод уходит запросом выделенным словом владельца, а не сужается в браузере", async () => {
    listUsers.mockResolvedValue({ users: [user({ id: "usr-1", email: "ops@example.com" })] });
    show();
    await waitFor(() => expect(listUsers).toHaveBeenCalledTimes(1));

    fireEvent.change(screen.getByPlaceholderText(/ищется по всему списку/i), { target: { value: "ops" } });

    // `search="…"`, а НЕ `email CONTAINS "…"`: iam отвергает CONTAINS явно, и
    // подстановка общего механизма списков уронила бы страницу целиком.
    await waitFor(() => expect(listUsers).toHaveBeenCalledWith({ pageSize: "20", filter: 'search="ops"' }));
  });

  it("край сузил — в браузере не пересеиваем: показано то, что он прислал", async () => {
    // Имя из профиля с введённым не совпадает; сужай мы ещё раз по нему, край
    // ответил бы, а поле показало бы «нет совпадений» — то есть отрицание при
    // положительном ответе сервера.
    listUsers.mockResolvedValue({ users: [user({ id: "usr-9", email: "ops@example.com", display_name: "Иван" })] });
    show();
    await waitFor(() => expect(optionLabels().join(" ")).toContain("ops@example.com"));

    fireEvent.change(screen.getByPlaceholderText(/ищется по всему списку/i), { target: { value: "usr-9" } });

    await waitFor(() => expect(listUsers).toHaveBeenCalledTimes(2));
    expect(optionLabels().join(" ")).toContain("ops@example.com");
  });
});
