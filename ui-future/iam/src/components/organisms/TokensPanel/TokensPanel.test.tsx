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

// ─────────────────────────────────────────────────────────────────────────────
// ВЫПУСК: вид, срок числом, радиус (#1235)
//
// Консоль выдавала СНЯТЫЙ вид (ключевую пару) и называла его бессрочным. Полоса
// докера ключевой материал в поле пароля больше не принимает — окно перехода
// закрыто по умолчанию, — поэтому арендатор, у которого перестал работать вход
// в реестр, получал в консоли ровно то, что платформа отвергает.
describe("TokensPanel — выпуск называет вид, срок и радиус", () => {
  beforeEach(() => {
    run.mockReset();
    run.mockResolvedValue({});
    mutations.length = 0;
  });

  const openCreate = () => fireEvent.click(screen.getByRole("button", { name: /Создать токен/ }));

  const issueBody = () => (run.mock.calls.at(-1)?.[0] ?? {}) as Record<string, unknown>;

  it("по умолчанию выпускается СЕКРЕТ — вид, который принимает докерная полоса", async () => {
    renderPanel([]);
    openCreate();

    fireEvent.click(screen.getByRole("button", { name: "Создать" }));

    await waitFor(() => expect(run).toHaveBeenCalled());
    expect(issueBody().credential_kind).toBe("CREDENTIAL_KIND_SECRET");
  });

  // Положительный контроль: прежний вид не снят, он выбирается явно. Без этой
  // пары «по умолчанию секрет» зеленело бы на форме, где другого вида нет
  // вовсе, — а он нужен внешней федерации.
  it("ключевая пара по-прежнему выпускается — но по ЯВНОМУ выбору", async () => {
    renderPanel([]);
    openCreate();

    fireEvent.click(screen.getByRole("radio", { name: /Ключевая пара/ }));
    fireEvent.click(screen.getByRole("button", { name: "Создать" }));

    await waitFor(() => expect(run).toHaveBeenCalled());
    expect(issueBody().credential_kind).toBe("CREDENTIAL_KIND_KEYPAIR");
  });

  it("у секрета нет варианта «Без срока» — бессрочного секрета не бывает", () => {
    renderPanel([]);
    openCreate();

    expect(screen.queryByRole("radio", { name: "Без срока" })).not.toBeInTheDocument();
    // Срок назван ЧИСЛОМ, а не словом.
    expect(screen.getByRole("radio", { name: "90 дней" })).toBeInTheDocument();
  });

  it("у ключевой пары «Без срока» остаётся — там это правда", () => {
    renderPanel([]);
    openCreate();
    fireEvent.click(screen.getByRole("radio", { name: /Ключевая пара/ }));

    expect(screen.getByRole("radio", { name: "Без срока" })).toBeInTheDocument();
  });

  it("радиус секрета назван в самом окне выпуска, а не оставлен умолчанием", () => {
    const { container } = renderPanel([]);
    openCreate();

    expect(container).toHaveTextContent(/не только в реестре/);
    expect(container).toHaveTextContent(/учётная запись/);
  });

  // Потолок срока — СВОЙ у каждого вида, и «30 дней» в подсказке этого не
  // доказывает: такой вариант есть у обоих. Различает ПОТОЛОК своего срока,
  // и утверждается он там, где вводится число.
  //
  // Утверждается АТРИБУТ поля ввода, а не подсказка рядом с ним: общий
  // заменитель antd `help` у `Form.Item` не рисует, поэтому проба на подсказку
  // говорила бы о дублёре, а не о продукте. `max` при этом и есть действующее
  // ограничение — оно уезжает в разметку у настоящего antd тоже.
  const customDaysInput = (root: HTMLElement) => root.querySelector('input[type="number"]')!;

  it("свой срок секрета ограничен 90 днями, а не двумя годами ключевой пары", () => {
    const { container } = renderPanel([]);
    openCreate();
    fireEvent.click(screen.getByRole("radio", { name: "Свой срок" }));

    expect(customDaysInput(container)).toHaveAttribute("max", "90");
    // Умолчание платформы названо ЧИСЛОМ, а не словом «бессрочно».
    expect(container).toHaveTextContent("платформа поставит 30 дней");
    expect(container).toHaveTextContent("бессрочного секрета не бывает");
  });

  it("свой срок ключевой пары по-прежнему до 730 дней — отрицание выше не вакуумно", () => {
    const { container } = renderPanel([]);
    openCreate();
    fireEvent.click(screen.getByRole("radio", { name: /Ключевая пара/ }));
    fireEvent.click(screen.getByRole("radio", { name: "Свой срок" }));

    expect(customDaysInput(container)).toHaveAttribute("max", "730");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// СЕКРЕТ ЧИТАЕТСЯ ИЗ НЕМЕДЛЕННОГО ОТВЕТА, А НЕ ИЗ ОПРОШЕННОЙ ОПЕРАЦИИ (#1235)
//
// Выдача секрета завершается на пути запроса, и секрет подменяется в теле
// ответа ПОСЛЕ записи: строка операции его не несёт ни в какой момент. Читатель,
// ждущий опроса, получит тело без секрета — то есть невосстановимое значение
// будет потеряно при исправной выдаче.
describe("TokensPanel — одноразовый секрет берётся из ответа выдачи", () => {
  beforeEach(() => {
    run.mockReset();
    mutations.length = 0;
  });

  it("секрет из немедленного ответа показывается целиком", async () => {
    run.mockResolvedValue({
      id: "opr-1",
      done: true,
      response: { key_id: "tok-9", client_id: "tok-9", secret: "kc.s.ZZZZ" },
    });
    renderPanel([]);
    fireEvent.click(screen.getByRole("button", { name: /Создать токен/ }));
    fireEvent.click(screen.getByRole("button", { name: "Создать" }));

    await waitFor(() => expect(screen.getByDisplayValue("kc.s.ZZZZ")).toBeInTheDocument());
  });

  // Положительный контроль к утверждению выше: прежняя форма читается тем же
  // путём. Без пары «секрет показан» зеленело бы на окне, которое показывает
  // что угодно.
  it("ключ ключевой пары из немедленного ответа показывается целиком", async () => {
    run.mockResolvedValue({
      id: "opr-2",
      done: true,
      response: { key_id: "tok-8", client_id: "tok-8", algorithm: "ES256", private_key_pem: "-----BEGIN PRIVATE" },
    });
    renderPanel([]);
    fireEvent.click(screen.getByRole("button", { name: /Создать токен/ }));
    fireEvent.click(screen.getByRole("button", { name: "Создать" }));

    await waitFor(() => expect(screen.getByDisplayValue("-----BEGIN PRIVATE")).toBeInTheDocument());
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// СПИСОК НАЗЫВАЕТ ВИД И НЕ ЛЖЁТ ПРО СРОК (#1235)
describe("TokensPanel — список называет вид", () => {
  beforeEach(() => {
    run.mockReset();
    run.mockResolvedValue({});
    mutations.length = 0;
  });

  it("вид строки назван словами клиента", async () => {
    const { container } = renderPanel([token({ id: "tok-9", credential_kind: "CREDENTIAL_KIND_SECRET" })]);

    await waitFor(() => expect(table(container)).toHaveTextContent("Секрет"));
  });

  it("строка секрета без срока НЕ называется бессрочной", async () => {
    const { container } = renderPanel([
      token({ id: "tok-9", credential_kind: "CREDENTIAL_KIND_SECRET", expires_at: undefined }),
    ]);

    await waitFor(() => expect(table(container)).toHaveTextContent("tok-9"));
    expect(table(container)).not.toHaveTextContent("Бессрочный");
  });

  // Положительный контроль: слово не вычищено отовсюду, оно осталось там, где
  // контракт его допускает.
  it("строка ключевой пары без срока по-прежнему бессрочная", async () => {
    const { container } = renderPanel([
      token({ id: "tok-8", credential_kind: "CREDENTIAL_KIND_KEYPAIR", expires_at: undefined }),
    ]);

    await waitFor(() => expect(table(container)).toHaveTextContent("Бессрочный"));
  });
});
