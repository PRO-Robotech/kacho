// Администраторы кластера на посадке без admin-плоскости не предлагают выдачу и
// отзыв, которые край этой посадки не обслуживает (#3091).
//
// Выдача «устаревшим путём» и отзыв идут в `InternalClusterService` — на
// внешнем слушателе края этих маршрутов нет, и нажатие кончалось бы «маршрута
// нет». Выдача через привязку доступа — публичный путь, она остаётся.
//
// Подменены транспорт (ответ пробы посадки и списка) и соседи страницы, чьё
// поведение здесь не предмет; вывод посадки и сама страница — настоящие.

import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const get = jest.fn<(path: string) => Promise<unknown>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list: jest.fn(), get, create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

const ADMINS = [
  {
    cluster_admin_grant_id: "cag-1",
    subject_type: "USER",
    subject_id: "usr-self",
    subject_email: "self@kacho.local",
    subject_display_name: "",
    granted_by_user_id: "bootstrap",
    granted_by_email: "",
  },
  {
    cluster_admin_grant_id: "cag-2",
    subject_type: "USER",
    subject_id: "usr-other",
    subject_email: "other@kacho.local",
    subject_display_name: "",
    granted_by_user_id: "usr-self",
    granted_by_email: "self@kacho.local",
  },
];

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => ({ user: { id: "usr-self" } }),
}));
jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useOperation: () => ({ data: undefined }),
}));
jest.unstable_mockModule("@shared/components/organisms/system/GrantAdminModal", () => ({
  GrantAdminModal: () => null,
}));

const { default: ClusterAdminsPage } = await import("./ClusterAdminsPage");

/** Ответ края: проба посадки (`/iam/v1/internal/cluster`) и список — одним производителем. */
function edgeAnswers(plane: "absent" | "present") {
  get.mockImplementation((path: string) => {
    if (plane === "absent") return Promise.reject(new ApiError(404, 5, [], "Not Found"));
    if (path.endsWith("/admins")) return Promise.resolve({ admins: ADMINS });
    return Promise.resolve({ id: "cluster_root" });
  });
}

function open() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <ClusterAdminsPage />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  jest.clearAllMocks();
});

describe("ClusterAdminsPage — посадка admin-плоскости", () => {
  it("плоскости нет: выдачи устаревшим путём и отзыва нет, выдача через привязку доступа остаётся", async () => {
    edgeAnswers("absent");

    open();

    expect(await screen.findByTestId("cluster-admins-grant-via-binding")).toBeInTheDocument();
    await waitFor(() => expect(get).toHaveBeenCalledWith("/iam/v1/internal/cluster"));
    await waitFor(() => expect(screen.queryByTestId("cluster-admins-grant-button")).not.toBeInTheDocument());
  });

  it("пока посадка не названа, строки списка без отзыва — кнопка успела бы отказать раньше слов", async () => {
    get.mockImplementation((path: string) =>
      path.endsWith("/admins") ? Promise.resolve({ admins: ADMINS }) : new Promise(() => {}),
    );

    open();

    expect(await screen.findByText("other@kacho.local")).toBeInTheDocument();
    expect(screen.queryByTestId("cluster-admins-revoke-usr-other")).not.toBeInTheDocument();
    expect(screen.queryByTestId("cluster-admins-grant-button")).not.toBeInTheDocument();
  });

  it("без отзыва в строках нет и пустого столбца под него — смотрящий не видит колонку без содержимого", async () => {
    get.mockImplementation((path: string) =>
      path.endsWith("/admins") ? Promise.resolve({ admins: ADMINS }) : new Promise(() => {}),
    );

    open();

    const row = (await screen.findByText("other@kacho.local")).closest("tr");
    expect(row).not.toBeNull();
    const empty = [...row!.querySelectorAll("td")].filter((td) => td.textContent === "" && td.children.length === 0);
    expect(empty).toHaveLength(0);
  });

  it("плоскость есть: выдача и отзыв на месте — контроль в обратную сторону", async () => {
    edgeAnswers("present");

    open();

    expect(await screen.findByTestId("cluster-admins-grant-button")).toBeInTheDocument();
    expect(await screen.findByTestId("cluster-admins-revoke-usr-other")).toBeInTheDocument();
    expect(screen.getByTestId("cluster-admins-revoke-usr-other").closest("td")).not.toBeNull();
  });
});
