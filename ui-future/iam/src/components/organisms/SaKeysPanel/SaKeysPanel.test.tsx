import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Operation } from "@shared/api/types";
import type { SaKeysPanel as SaKeysPanelExport } from "./SaKeysPanel";
import { antdStub } from "@shared/test/antd-stub";

interface MutationOpts {
  method: string;
  path: unknown;
  invalidateKeys?: unknown;
  successText?: string;
  onSuccess?: (op: Operation) => void;
}

const listSaKeys = jest.fn<(id: string, q?: Record<string, string>) => Promise<{ keys?: unknown[] }>>();
const run = jest.fn<(body: unknown) => Promise<unknown>>();
const mutations: MutationOpts[] = [];

// Общий заменитель antd — ОДИН на дерево (#587). Свой дублёр iam снят: он
// реализовал те же виды по-своему, и правка, доехавшая до одной копии, не
// доезжала до другой.
jest.unstable_mockModule("antd", () => antdStub());

jest.unstable_mockModule("@shared/api/iam", () => ({
  iamApi: { listSaKeys },
  saKeysPath: (id: string) => `/iam/v1/serviceAccounts/${id}/keys`,
}));

jest.unstable_mockModule("@shared/components/organisms/iam/IamCommon", () => ({
  useIamMutation: (opts: MutationOpts) => {
    mutations.push(opts);
    return { run, submitting: false };
  },
  fmtTs: (v?: string) => v ?? "—",
  CopyableMonoId: ({ id }: { id?: string }) => <span>{id ?? ""}</span>,
}));

// Портал слота шапки живёт внутри DetailShell; вне его настоящий компонент не
// рисует НИЧЕГО. Подменяем ровно границу портала, чтобы кнопка стала достижима.
jest.unstable_mockModule("@shared/components/organisms/DetailShell", () => ({
  HeaderSlotPortal: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

// Тост подменяем, чтобы утверждать САМ ТЕКСТ, который читает арендатор:
// «сказано прямо» иначе неотличимо от «промолчали».
const toastError = jest.fn();
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), warning: jest.fn() },
}));

jest.unstable_mockModule("@/components/organisms/iam/IamListShell", () => ({
  useTableScrollY: () => ({ wrapRef: { current: null }, scrollY: 100 }),
}));

let SaKeysPanel: typeof SaKeysPanelExport;

const key = (over: Record<string, unknown> = {}) => ({
  id: "sak-1",
  description: "ключ для CI",
  expires_at: "",
  last_used_at: "",
  created_at: "2026-01-01T00:00:00Z",
  created_by_user_id: "usr-1",
  ...over,
});

function renderPanel(serviceAccountId = "sa-1") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SaKeysPanel serviceAccountId={serviceAccountId} />
    </QueryClientProvider>,
  );
}

/**
 * Таблица берётся из разметки, а не по `data-testid`: подменённый в наборе проб
 * antd читает у Table только колонки/строки/локаль и прочие свойства наружу не
 * отдаёт, поэтому опознавательный атрибут до разметки не доезжает. Зато СТРОКИ
 * подмена рисует по-настоящему — то есть проверяется именно то, что видно.
 */
const table = (root: HTMLElement) => root.querySelector("table")!;
const issueOpts = () => mutations.find((m) => m.method === "POST")!;
const revokeOpts = () => mutations.find((m) => m.method === "DELETE")!;

describe("SaKeysPanel", () => {
  beforeAll(async () => {
    ({ SaKeysPanel } = await import("./SaKeysPanel"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    mutations.length = 0;
    listSaKeys.mockResolvedValue({ keys: [] });
    // Настоящий `run` возвращает промис, и вызывающий вешает на него `.catch`;
    // дублёр обязан вести себя так же, иначе он строже продукта.
    run.mockResolvedValue(undefined);
  });

  it("спрашивает ключи именно того сервисного аккаунта, который открыт", async () => {
    renderPanel("sa-42");

    await waitFor(() => expect(listSaKeys).toHaveBeenCalledWith("sa-42", { page_size: "1000" }));
  });

  it("без идентификатора аккаунта не спрашивает ничего", () => {
    renderPanel("");

    expect(listSaKeys).not.toHaveBeenCalled();
  });

  it("пустой список не выглядит поломкой — объясняет, что делать", async () => {
    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("Токенов нет. Создайте первый токен."));
  });

  it("показывает строку ключа: описание, срок и идентификатор", async () => {
    listSaKeys.mockResolvedValue({ keys: [key({ id: "sak-9", description: "ключ для CI" })] });

    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("ключ для CI"));
    expect(table(container)).toHaveTextContent("sak-9");
    // Пустой `expires_at` — это «бессрочный», а не «истёк»: подмена смысла здесь
    // означала бы ложную тревогу на каждом бессрочном ключе.
    expect(table(container)).toHaveTextContent("Бессрочный");
  });

  it("ключ без описания показывает прочерк, а не пустую ячейку", async () => {
    listSaKeys.mockResolvedValue({ keys: [key({ description: "" })] });

    const { container } = renderPanel();

    await waitFor(() => expect(table(container)).toHaveTextContent("—"));
  });

  it("выпуск идёт на коллекцию ключей ЭТОГО аккаунта и обновляет его список", () => {
    renderPanel("sa-42");

    expect(issueOpts().path).toBe("/iam/v1/serviceAccounts/sa-42/keys");
    expect(issueOpts().invalidateKeys).toEqual([["iam", "sa-keys", "sa-42"]]);
  });

  it("отзыв адресуется конкретному ключу и экранирует его идентификатор", () => {
    renderPanel("sa-42");

    const path = revokeOpts().path as (body: unknown) => string;
    expect(path({ id: "sak-9" })).toBe("/iam/v1/serviceAccounts/sa-42/keys/sak-9");
    expect(path({ id: "a/b" })).toBe("/iam/v1/serviceAccounts/sa-42/keys/a%2Fb");
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

  it("секрет показывается ОДИН раз — с ключом, предупреждением и идентификаторами", () => {
    const { container } = renderPanel();

    expect(container).not.toHaveTextContent("Приватный ключ (PEM)");

    act(() => {
      issueOpts().onSuccess?.({
        id: "opr-1",
        done: true,
        response: { private_key_pem: "-----BEGIN PRIVATE KEY-----", key_id: "sak-9", client_id: "cli-9" },
      } as unknown as Operation);
    });

    expect(container).toHaveTextContent("Сохраните значение — оно больше не будет показано");
    expect(container).toHaveTextContent("Приватный ключ (PEM)");
    expect(container).toHaveTextContent("sak-9");
    expect(container).toHaveTextContent("cli-9");
    expect(container.querySelector("textarea")).toHaveValue("-----BEGIN PRIVATE KEY-----");
    expect(screen.getByRole("button", { name: "Скопировать" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Скачать" })).toBeInTheDocument();
  });

  // ЗДЕСЬ СТОЯЛА ПРОБА «окно всё равно открывается» — она закрепляла ФАНТОМ.
  //
  // Прежний код открывал показ безусловно, поэтому операция без значения давала
  // ПУСТУЮ рамку с подписью «Приватный ключ (PEM)» и алгоритмом ES256 — то есть
  // утверждение, что ключ показан, при том что показывать было нечего.
  //
  // С видом SECRET (#1235) это стало опаснее: строка операции секрета НЕ НЕСЁТ
  // НИКОГДА, и опрос приходит с телом без значения ШТАТНО — пустое окно
  // перекрывало бы уже показанный секрет при каждой исправной выдаче.
  //
  // Предмет пробы — «молча терять нельзя» — сохранён и усилен: значение
  // потеряно ⇒ об этом сказано, и сказано, что делать. Близнец в
  // `UserTokensPanel.test.tsx` утверждает то же самое: панель у них одна.
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
