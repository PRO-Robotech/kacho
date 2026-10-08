// Администраторы кластера — на публичной ClusterService (kacho#3094, край
// #3093): выдача и отзыв показываются на любой посадке края, страница ходит
// только публичными путями и посадку края не спрашивает.
//
// Подменены транспорт и соседи страницы, чьё поведение здесь не предмет; сама
// страница — настоящая.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
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

/** Ответ края: синглтон кластера и перечень — публичными путями. */
function edgeAnswers() {
  get.mockImplementation((path: string) => {
    if (path === "/iam/v1/cluster/admins") return Promise.resolve({ admins: ADMINS });
    if (path === "/iam/v1/cluster") return Promise.resolve({ id: "cluster_root" });
    return Promise.reject(new ApiError(404, 5, [], "Not Found"));
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

describe("ClusterAdminsPage — публичная ClusterService (#3094)", () => {
  it("выдача и отзыв на месте, как только пришёл перечень", async () => {
    edgeAnswers();

    open();

    expect(await screen.findByText("other@kacho.local")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Добавить администратора/ })).toBeInTheDocument();
    expect(screen.getByTestId("cluster-admins-revoke-usr-other")).toBeInTheDocument();
  });

  it("кнопка выдачи названа действием, а не «устаревшим путём» — путь у неё публичный", async () => {
    edgeAnswers();

    open();

    expect(await screen.findByText("other@kacho.local")).toBeInTheDocument();
    expect(screen.queryByText(/устаревший путь/)).not.toBeInTheDocument();
  });

  it("страница читает только публичные пути — внутренних у внешнего края нет", async () => {
    edgeAnswers();

    open();

    expect(await screen.findByText("other@kacho.local")).toBeInTheDocument();
    const paths = get.mock.calls.map(([p]) => p);
    expect(paths).toContain("/iam/v1/cluster/admins");
    expect(paths.filter((p) => p.includes("/internal"))).toEqual([]);
  });

  it("свою строку и последнего администратора отозвать нельзя — подсказка называет почему", async () => {
    edgeAnswers();

    open();

    expect((await screen.findAllByText("self@kacho.local")).length).toBeGreaterThan(0);
    expect(screen.getByTestId("cluster-admins-revoke-usr-self")).toBeDisabled();
    expect(screen.getByTestId("cluster-admins-revoke-usr-other")).not.toBeDisabled();
  });
});
