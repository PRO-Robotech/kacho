import { jest } from "@jest/globals";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import type { Operation } from "@shared/api/types";
// Чистый nav-helper импортируется напрямую: у него нет графа UI, и его ответ —
// самостоятельный предмет утверждения.
import { groupDetailPathFromOp } from "./groupNav";
import type { GroupCreatePage as GroupCreatePageExport } from "./GroupsPage";
import { antdStub } from "@shared/test/antd-stub";

interface MutationOpts {
  method: string;
  path: unknown;
  successText?: string;
  onSuccess?: (op: Operation) => void;
}

const mutations: MutationOpts[] = [];
const run = jest.fn<(body: unknown) => Promise<unknown>>();

jest.unstable_mockModule("antd", () => antdStub());

jest.unstable_mockModule("@shared/api/client", () => ({ api: { get: jest.fn(), post: jest.fn() } }));

jest.unstable_mockModule("@shared/api/iam", () => ({
  IAM: { groups: "/iam/v1/groups" },
  iamApi: { listGroups: jest.fn() },
}));

jest.unstable_mockModule("@shared/components/organisms/iam/IamCommon", () => ({
  useIamMutation: (opts: MutationOpts) => {
    mutations.push(opts);
    return { run, submitting: false };
  },
  fmtTs: (v?: string) => v ?? "—",
  CopyableMonoId: ({ id }: { id?: string }) => <span>{id ?? ""}</span>,
}));

jest.unstable_mockModule("@shared/components/molecules/PageHeaderSlot", () => ({
  useBreadcrumb: () => undefined,
  useHeaderRight: () => undefined,
}));

jest.unstable_mockModule("@shared/components/organisms/LabelsEditor", () => ({
  LabelsEditor: () => <div data-testid="labels-editor" />,
  labelsFromEntries: () => ({}),
}));

jest.unstable_mockModule("@shared/components/organisms/form/FormShell", () => ({
  FormShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

jest.unstable_mockModule("@shared/components/organisms/form/FormFooter", () => ({
  FormFooter: ({ submitLabel, onSubmit }: { submitLabel: string; onSubmit: () => void }) => (
    <button type="button" onClick={onSubmit}>
      {submitLabel}
    </button>
  ),
}));

jest.unstable_mockModule("@shared/components/molecules/SectionHeader", () => ({ SectionHeader: () => null }));
jest.unstable_mockModule("@shared/components/organisms/form/ResourceIcon", () => ({ ResourceIcon: () => null }));
jest.unstable_mockModule("@/components/molecules/IamRefLink", () => ({ IamRefLink: () => null }));
jest.unstable_mockModule("@/components/organisms/iam/IamListShell", () => ({
  IamListShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  useTableScrollY: () => ({ wrapRef: { current: null }, scrollY: 100 }),
}));

let GroupCreatePage: typeof GroupCreatePageExport;

function Where() {
  const { pathname } = useLocation();
  return <div data-testid="where">{pathname}</div>;
}

const renderCreate = () =>
  render(
    <MemoryRouter initialEntries={["/iam/groups/create"]}>
      <GroupCreatePage />
      <Routes>
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  );

const where = () => screen.getByTestId("where").textContent;

describe("groupDetailPathFromOp", () => {
  it("ведёт на карточку созданной группы, когда операция принесла её идентификатор", () => {
    const op: Operation = { id: "op-1", done: true, metadata: { "@type": "…", group_id: "grp-abc" } };
    expect(groupDetailPathFromOp(op)).toBe("/iam/groups/grp-abc");
  });

  it("без идентификатора возвращает к списку, а не в никуда", () => {
    expect(groupDetailPathFromOp({ id: "op-1", done: true })).toBe("/iam/groups");
    expect(groupDetailPathFromOp({ id: "op-1", done: true, metadata: { "@type": "…" } })).toBe("/iam/groups");
    expect(groupDetailPathFromOp(undefined)).toBe("/iam/groups");
  });
});

describe("GroupCreatePage", () => {
  beforeAll(async () => {
    ({ GroupCreatePage } = await import("./GroupsPage"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    mutations.length = 0;
    run.mockResolvedValue(undefined);
  });

  it("создание идёт на коллекцию групп", () => {
    renderCreate();

    expect(mutations[0].method).toBe("POST");
    expect(mutations[0].path).toBe("/iam/v1/groups");
  });

  it("после создания открывает карточку СОЗДАННОЙ группы, а не список", () => {
    renderCreate();
    expect(where()).toBe("/iam/groups/create");

    act(() => {
      mutations[0].onSuccess?.({ id: "op-1", done: true, metadata: { "@type": "…", group_id: "grp-abc" } });
    });

    expect(where()).toBe("/iam/groups/grp-abc");
  });

  it("если операция не назвала группу — возвращает к списку", () => {
    renderCreate();

    act(() => {
      mutations[0].onSuccess?.({ id: "op-1", done: true });
    });

    expect(where()).toBe("/iam/groups");
  });

  it("форма предлагает имя, метки и описание", () => {
    renderCreate();

    expect(screen.getByText("Имя")).toBeInTheDocument();
    expect(screen.getByText("Метки")).toBeInTheDocument();
    expect(screen.getByText("Описание")).toBeInTheDocument();
    expect(screen.getByTestId("labels-editor")).toBeInTheDocument();
  });

  it("аккаунт полем формы не является — он берётся из контекста", () => {
    // Поле только для чтения обещало бы выбор, которого нет: аккаунт выводится
    // из контекста секции и в тело запроса уезжает сам.
    renderCreate();

    expect(screen.queryByText("Account")).not.toBeInTheDocument();
    expect(screen.queryByText("Аккаунт")).not.toBeInTheDocument();
  });
});
