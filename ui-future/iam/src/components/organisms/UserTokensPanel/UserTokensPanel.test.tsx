import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Operation } from "@shared/api/types";
import type { UserTokensPanel as UserTokensPanelExport } from "./UserTokensPanel";
import { antdStub } from "@shared/test/antd-stub";

interface MutationOpts {
  method: string;
  path: unknown;
  invalidateKeys?: unknown;
  successText?: string;
  onSuccess?: (op: Operation) => void;
}

const listUserTokens = jest.fn<(id: string, q?: Record<string, string>) => Promise<{ tokens?: unknown[] }>>();
const run = jest.fn<(body: unknown) => Promise<unknown>>();
const mutations: MutationOpts[] = [];

// Общий заменитель antd — ОДИН на дерево (#587). Свой дублёр iam снят: он
// реализовал те же виды по-своему, и правка, доехавшая до одной копии, не
// доезжала до другой.
jest.unstable_mockModule("antd", () => antdStub());

jest.unstable_mockModule("@shared/api/iam", () => ({
  iamApi: { listUserTokens },
  userTokensPath: (id: string) => `/iam/v1/users/${id}/tokens`,
}));

jest.unstable_mockModule("@shared/components/organisms/iam/IamCommon", () => ({
  useIamMutation: (opts: MutationOpts) => {
    mutations.push(opts);
    return { run, submitting: false };
  },
  fmtTs: (v?: string) => v ?? "—",
  CopyableMonoId: ({ id }: { id?: string }) => <span>{id ?? ""}</span>,
}));

jest.unstable_mockModule("@shared/components/organisms/DetailShell", () => ({
  HeaderSlotPortal: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

jest.unstable_mockModule("@/components/organisms/iam/IamListShell", () => ({
  useTableScrollY: () => ({ wrapRef: { current: null }, scrollY: 100 }),
}));

// Тост подменяем, чтобы утверждать САМ ТЕКСТ, который читает арендатор: «сказано
// прямо» иначе неотличимо от «промолчали».
const toastError = jest.fn();
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), warning: jest.fn() },
}));

let UserTokensPanel: typeof UserTokensPanelExport;

const token = (over: Record<string, unknown> = {}) => ({
  id: "tok-1",
  description: "токен для CI",
  expires_at: "",
  last_used_at: "",
  created_at: "2026-01-01T00:00:00Z",
  created_by_user_id: "usr-1",
  ...over,
});

function renderPanel(userId = "usr-1") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <UserTokensPanel userId={userId} />
    </QueryClientProvider>,
  );
}

const table = (root: HTMLElement) => root.querySelector("table")!;
const issueOpts = () => mutations.find((m) => m.method === "POST")!;
const revokeOpts = () => mutations.find((m) => m.method === "DELETE")!;

describe("UserTokensPanel", () => {
  beforeAll(async () => {
    ({ UserTokensPanel } = await import("./UserTokensPanel"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    mutations.length = 0;
    listUserTokens.mockResolvedValue({ tokens: [] });
    run.mockResolvedValue(undefined);
  });

  // ЦЕПОЧКА, А НЕ ОДНО ЗВЕНО (#1235). Вид объявлен в контракте, читается
  // проекцией, едет краем, типизирован в `api/iam` и рисуется столбцом. Проба
  // здесь спрашивает про ЗВЕНО ОБЁРТКИ — переживает ли вид разворот ответа и
  // приведение к строке панели: значение, которое пишут и не читают, невидимо
  // отовсюду, и обрыв на любом звене выглядит одинаково.
  it("вид удостоверения доезжает из ответа края до столбца списка", async () => {
    listUserTokens.mockResolvedValue({
      tokens: [token({ id: "tok-7", credential_kind: "CREDENTIAL_KIND_SECRET" })],
    });

    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("Секрет"));
  });

  it("спрашивает токены именно того пользователя, который открыт", async () => {
    renderPanel("usr-42");

    await waitFor(() => expect(listUserTokens).toHaveBeenCalledWith("usr-42", { page_size: "1000" }));
  });

  it("без идентификатора пользователя не спрашивает ничего", () => {
    renderPanel("");

    expect(listUserTokens).not.toHaveBeenCalled();
  });

  it("пустой список объясняет, что делать, а не выглядит поломкой", async () => {
    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("Токенов нет. Создайте первый токен."));
  });

  it("показывает строку токена: описание, срок и идентификатор", async () => {
    listUserTokens.mockResolvedValue({ tokens: [token({ id: "tok-9", description: "токен для CI" })] });

    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("токен для CI"));
    expect(table(container)).toHaveTextContent("tok-9");
    // Пустой `expires_at` — «бессрочный», а не «истёк».
    expect(table(container)).toHaveTextContent("Бессрочный");
  });

  it("истёкший срок назван прямо", async () => {
    listUserTokens.mockResolvedValue({ tokens: [token({ expires_at: "2020-01-01T00:00:00Z" })] });

    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("Истек"));
  });

  it("выпуск идёт на коллекцию токенов ЭТОГО пользователя и обновляет его список", () => {
    renderPanel("usr-42");

    expect(issueOpts().path).toBe("/iam/v1/users/usr-42/tokens");
    expect(issueOpts().invalidateKeys).toEqual([["iam", "user-tokens", "usr-42"]]);
  });

  it("отзыв адресуется конкретному токену и экранирует его идентификатор", () => {
    renderPanel("usr-42");

    const path = revokeOpts().path as (body: unknown) => string;
    expect(path({ id: "tok-9" })).toBe("/iam/v1/users/usr-42/tokens/tok-9");
    expect(path({ id: "a/b" })).toBe("/iam/v1/users/usr-42/tokens/a%2Fb");
  });

  it("до нажатия окно выпуска не показано, по кнопке — открывается", () => {
    renderPanel();

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Создать токен" }));

    expect(screen.getByRole("dialog")).toHaveTextContent("Создать токен");
    expect(screen.getByRole("dialog")).toHaveTextContent("Срок действия");
  });

  // ЗДЕСЬ СТОЯЛИ ДВЕ ПРОБЫ О ПОДТВЕРЖДЕНИИ ОТЗЫВА — они переехали туда, где
  // подтверждение объявлено: `TokensPanel.test.tsx`.
  //
  // Причина не в дублировании, а в предмете. Эта панель сведена к тонкой
  // обёртке: она объявляет путь коллекции, ключ кэша и разворот ответа, а
  // разметку — включая `Popconfirm` — рисует общая реализация. Проба, стоящая
  // рядом с обёрткой и утверждающая о подтверждении, утверждает о ЗАМЕНИТЕЛЕ
  // antd, а не о продукте: сломай разметку в общей реализации — она бы этого
  // не заметила, потому что смотрит на свой заменитель.
  //
  // Это же нашёл гейт «дублёр antd рисует то, что видит оператор»: проба для
  // `Popconfirm` обязана лежать рядом с его продуктовым потребителем.
  //
  // Что осталось здесь — предмет ОБЁРТКИ: чей список спрашивается, на какую
  // коллекцию уходит выпуск и отзыв, как разворачивается ответ.

  it("ключевая пара показывается ОДИН раз — с ключом, предупреждением и идентификаторами", () => {
    const { container } = renderPanel();

    expect(container).not.toHaveTextContent("Приватный ключ (PEM)");

    act(() => {
      issueOpts().onSuccess?.({
        id: "opr-1",
        done: true,
        response: { private_key_pem: "-----BEGIN PRIVATE KEY-----", key_id: "tok-9", client_id: "cli-9" },
      } as unknown as Operation);
    });

    expect(container).toHaveTextContent("Сохраните значение — оно больше не будет показано");
    expect(container).toHaveTextContent("Приватный ключ (PEM)");
    expect(container).toHaveTextContent("tok-9");
    expect(container).toHaveTextContent("cli-9");
    expect(container.querySelector("textarea")).toHaveValue("-----BEGIN PRIVATE KEY-----");
  });

  // ЗДЕСЬ СТОЯЛА ПРОБА «окно всё равно открывается» — она закрепляла ФАНТОМ.
  //
  // Прежний код звал показ безусловно, поэтому операция без значения открывала
  // ПУСТУЮ рамку с подписью «Приватный ключ (PEM)» и алгоритмом ES256. Это
  // утверждение о том, что значение показано, — при том что показывать было
  // нечего; арендатор закрывал окно с мыслью, что ключ у него есть.
  //
  // Опасность выросла вместе с видом SECRET (#1235): у него строка операции
  // секрета НЕ НЕСЁТ НИКОГДА, и опрос приходит с телом без значения ШТАТНО.
  // Прежнее поведение открывало бы пустое окно поверх уже показанного секрета
  // при каждой исправной выдаче.
  //
  // Предмет пробы — «молча терять нельзя» — сохранён и усилен: значение
  // потеряно ⇒ об этом СКАЗАНО, и сказано, что делать.
  it("значение не пришло ни одним путём — сказано прямо, а не показано пустое окно", () => {
    const { container } = renderPanel();

    act(() => {
      issueOpts().onSuccess?.({ id: "opr-1", done: true } as unknown as Operation);
    });

    expect(container).not.toHaveTextContent("Приватный ключ (PEM)");
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("значение не пришло"));
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("выпустите новый"));
  });
});
