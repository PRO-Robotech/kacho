import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Operation } from "@shared/api/types";
import type { TokenRow, TokensPanel as TokensPanelExport } from "./TokensPanel";
import { antdStub } from "@shared/test/antd-stub";

// Отзыв токена — НЕОБРАТИМЫЙ шаг, и он стоит за подтверждением. Проба живёт
// рядом с самой реализацией, а не у её обёрток.
//
// ПОЧЕМУ ЗДЕСЬ, А НЕ У ОБЁРТОК. `UserTokensPanel` и `SaKeysPanel` сведены к
// этой панели и стали тонкими: их собственные пробы утверждают ПАРАМЕТРЫ, с
// которыми они её зовут. Подтверждение же объявлено здесь — и до этого файла
// его не читала ни одна проба того файла, где оно написано. Правка вопроса
// здесь не покраснила бы ничего: обёртки о его тексте не знают by construction.
//
// Вопрос утверждается ДОСЛОВНО: это последнее, что арендатор видит перед
// потерей доступа, и текст — часть контракта, а не оформление.

interface MutationOpts {
  method: string;
  path: unknown;
  invalidateKeys?: unknown;
  successText?: string;
  onSuccess?: (op: Operation) => void;
}

const run = jest.fn<(body: unknown) => Promise<unknown>>();
const mutations: MutationOpts[] = [];

// Общий заменитель antd — ОДИН на дерево (#587).
jest.unstable_mockModule("antd", () => antdStub());

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

const { TokensPanel } = (await import("./TokensPanel")) as { TokensPanel: typeof TokensPanelExport };

const token = (over: Partial<TokenRow> = {}): TokenRow => ({
  id: "tok-1",
  description: "прогон проб",
  created_at: "2026-08-21T10:00:00Z",
  ...over,
});

// Таблицу ищем узлом, а не признаком `data-testid`: общий заменитель antd
// (`@shared/test/antd-stub`) перечисленные им пропы `Table` пробрасывает, а
// прочие — нет, и `tableTestId` в разметку не попадает. Признак у продукта при
// этом стоит и нужен сквозным пробам, где рисует настоящий antd. Соседние пробы
// панелей ищут так же — форма общая, а не своя на каждый файл.
const table = (root: HTMLElement) => root.querySelector("table")!;

function renderPanel(rows: TokenRow[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TokensPanel
        subjectId="usr-42"
        collectionPath={(id) => `/iam/v1/users/${id}/tokens`}
        queryKind="user-tokens"
        list={() => Promise.resolve(rows)}
        fallbackFileName="token.txt"
        descriptionExample="сборочный конвейер"
        tableTestId="tokens-table"
      />
    </QueryClientProvider>,
  );
}

describe("TokensPanel — отзыв за подтверждением", () => {
  beforeEach(() => {
    run.mockReset();
    run.mockResolvedValue({});
    mutations.length = 0;
  });

  it("вопрос подтверждения назван дословно, и открытие его ничего не отзывает", async () => {
    const { container } = renderPanel([token({ id: "tok-9" })]);
    await waitFor(() => expect(table(container)).toHaveTextContent("tok-9"));

    // Подтверждение появляется ТОЛЬКО после нажатия — как у настоящего antd.
    // Без этой строки «действие за подтверждением» было бы неотличимо от
    // «действие по первому же нажатию».
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(run).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Отозвать" }));

    const confirm = screen.getByRole("tooltip");
    expect(confirm).toHaveTextContent("Отозвать токен?");
    expect(confirm).toHaveTextContent("Токен перестанет действовать безвозвратно.");
    // Открытие вопроса САМО ПО СЕБЕ необратимого шага не делает.
    expect(run).not.toHaveBeenCalled();

    fireEvent.click(within(confirm).getByRole("button", { name: "Отозвать" }));

    expect(run).toHaveBeenCalledWith({ id: "tok-9" });
  });

  it("отказ от подтверждения НЕ отзывает — отрицание в паре с положительным выше", async () => {
    const { container } = renderPanel([token({ id: "tok-9" })]);
    await waitFor(() => expect(table(container)).toHaveTextContent("tok-9"));

    fireEvent.click(screen.getByRole("button", { name: "Отозвать" }));
    fireEvent.click(within(screen.getByRole("tooltip")).getByRole("button", { name: "Отмена" }));

    expect(run).not.toHaveBeenCalled();
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("отзывается ТОТ токен, на строке которого нажали, а не первый в списке", async () => {
    const { container } = renderPanel([token({ id: "tok-1" }), token({ id: "tok-2" })]);
    await waitFor(() => expect(table(container)).toHaveTextContent("tok-2"));

    // Строку выбираем по её содержимому, а не по индексу: порядок строк —
    // свойство ответа края, и проба, ключующаяся на него, утверждала бы
    // сортировку вместо адресации.
    const second = screen.getAllByRole("button", { name: "Отозвать" })[1];
    fireEvent.click(second);
    fireEvent.click(within(screen.getByRole("tooltip")).getByRole("button", { name: "Отозвать" }));

    expect(run).toHaveBeenCalledWith({ id: "tok-2" });
  });

  it("глагол отзыва — DELETE по адресу СВОЕГО субъекта", async () => {
    const { container } = renderPanel([token()]);
    await waitFor(() => expect(table(container)).toHaveTextContent("tok-1"));

    const revoke = mutations.find((m) => m.successText === "Токен отозван");
    expect(revoke?.method).toBe("DELETE");
    expect((revoke?.path as (b: unknown) => string)({ id: "tok-1" })).toBe("/iam/v1/users/usr-42/tokens/tok-1");
  });
});
