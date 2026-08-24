import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";
import type { Operation } from "@shared/api/types";
import type { IssueTokenBody } from "@shared/api/tokens";
import { MAX_TTL_SECONDS, SECRET_TTL_CEILING_SECONDS, SECRET_TTL_DEFAULT_DAYS } from "@shared/lib/tokens-util";
import type { TokenKindConfig } from "./TokenIssuancePage";

// Страница администрирования «Токены и ключи» — вторая живая поверхность выдачи
// (первая — вкладка субъекта в iam). Она несла тот же дефект (#1235): выпускала
// снятый вид и называла срок «бессрочным», а одноразовое значение читала ТОЛЬКО
// из опрошенной операции — то есть у секрета теряла его при исправной выдаче.
jest.unstable_mockModule("antd", () => antdStub());

const useOperation = jest.fn<(id: string | null) => { data: Operation | undefined }>();
jest.unstable_mockModule("@shared/lib/use-operation", () => ({ useOperation }));

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => ({ user: { id: "usr-1" } }),
}));

const toastError = jest.fn();
const toastSuccess = jest.fn();
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), warning: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/resource-registry", () => ({ getResource: () => undefined }));

jest.unstable_mockModule("@shared/components/organisms/iam/IamCommon", () => ({
  CopyableMonoId: ({ id }: { id?: string }) => <span>{id ?? ""}</span>,
  fmtTs: (v?: string) => v ?? "—",
}));

const { TokenIssuancePage } = await import("./TokenIssuancePage");

const issue = jest.fn<(subjectId: string, body: IssueTokenBody) => Promise<unknown>>();

const config = (): TokenKindConfig => ({
  kind: "sa",
  subjectSingular: "сервисный аккаунт",
  subjectLabel: "Сервисный аккаунт",
  credentialSingular: "ключ",
  credentialPlural: "Ключи",
  issuedTitle: "Ключ выпущен",
  listSubjects: () => Promise.resolve([]),
  listCredentials: () => Promise.resolve([]),
  issue: issue as unknown as TokenKindConfig["issue"],
  revoke: () => Promise.resolve({ operation: {} as Operation }),
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TokenIssuancePage config={config()} />
    </QueryClientProvider>,
  );
}

// Выбираем субъект вручную и открываем окно выпуска.
function openIssue() {
  fireEvent.change(screen.getByTestId("token-subject-input"), { target: { value: "sva-42" } });
  fireEvent.click(screen.getByTestId("token-issue-button"));
}

describe("TokenIssuancePage — вид, срок, радиус", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    useOperation.mockReturnValue({ data: undefined });
    issue.mockResolvedValue({ id: "opr-1", done: false });
  });

  it("по умолчанию выпускает СЕКРЕТ — вид, который принимает докерная полоса", async () => {
    renderPage();
    openIssue();
    fireEvent.click(screen.getByRole("button", { name: "Выпустить" }));

    await waitFor(() => expect(issue).toHaveBeenCalled());
    expect(issue.mock.calls[0][1].credential_kind).toBe("CREDENTIAL_KIND_SECRET");
  });

  // Положительный контроль: прежний вид не снят, он выбирается явно.
  it("ключевая пара выпускается по ЯВНОМУ выбору", async () => {
    renderPage();
    openIssue();
    fireEvent.click(screen.getByRole("radio", { name: /Ключевая пара/ }));
    fireEvent.click(screen.getByRole("button", { name: "Выпустить" }));

    await waitFor(() => expect(issue).toHaveBeenCalled());
    expect(issue.mock.calls[0][1].credential_kind).toBe("CREDENTIAL_KIND_KEYPAIR");
  });

  // Предикат здесь — НЕ вхождение слова, а ОБЕЩАНИЕ. «Бессрочного секрета не
  // бывает» слово содержит и при этом утверждает ровно обратное; проба на
  // подстроку измеряла бы лексику, а не то, что читает арендатор. Поэтому
  // утверждается пара: перечень величин назван ЧИСЛОМ, а бессрочность прямо
  // отвергнута, и потолок поля — свой у вида.
  const ttlInput = () => document.querySelector('input[type="number"]')!;

  it("срок секрета назван числом, а бессрочность прямо отвергнута", () => {
    const { container } = renderPage();
    openIssue();

    expect(container).toHaveTextContent("бессрочного секрета не бывает");
    expect(container).toHaveTextContent(`${SECRET_TTL_DEFAULT_DAYS} дней`);
    expect(container).not.toHaveTextContent("ключ бессрочный");
    expect(ttlInput()).toHaveAttribute("max", String(SECRET_TTL_CEILING_SECONDS));
    // Радиус назван там же, где выпускают.
    expect(container).toHaveTextContent(/не только в реестре/);
  });

  it("у ключевой пары бессрочность ПРЕДЛАГАЕТСЯ — отрицание выше не вакуумно", () => {
    const { container } = renderPage();
    openIssue();
    fireEvent.click(screen.getByRole("radio", { name: /Ключевая пара/ }));

    expect(container).toHaveTextContent("ключ бессрочный");
    expect(container).not.toHaveTextContent("бессрочного секрета не бывает");
    expect(ttlInput()).toHaveAttribute("max", String(MAX_TTL_SECONDS));
    // Радиус предъявительского секрета ключевой паре не приписывается.
    expect(container).not.toHaveTextContent(/не только в реестре/);
  });
});

describe("TokenIssuancePage — одноразовое значение из ответа выдачи", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    useOperation.mockReturnValue({ data: undefined });
  });

  // Строка операции секрета НЕ несёт НИ В КАКОЙ МОМЕНТ: он подменяется в теле
  // ответа после записи. Читатель, ждущий опроса, потеряет его при исправной
  // выдаче — а восстановить значение нельзя.
  it("секрет берётся из немедленного ответа, опрос его не приносит никогда", async () => {
    issue.mockResolvedValue({
      id: "opr-9",
      done: true,
      response: { key_id: "soc-9", client_id: "soc-9", secret: "kc.s.WWWW" },
    });
    renderPage();
    openIssue();
    fireEvent.click(screen.getByRole("button", { name: "Выпустить" }));

    await waitFor(() => expect(screen.getByDisplayValue("kc.s.WWWW")).toBeInTheDocument());
  });

  // Положительный контроль: асинхронный путь ключевой пары не сломан — её
  // значение по-прежнему приезжает опросом.
  it("ключ ключевой пары по-прежнему приезжает опросом операции", async () => {
    issue.mockResolvedValue({ id: "opr-8", done: false });
    useOperation.mockReturnValue({
      data: {
        id: "opr-8",
        done: true,
        response: { key_id: "soc-8", client_id: "soc-8", algorithm: "ES256", private_key_pem: "-----BEGIN K" },
      } as unknown as Operation,
    });
    renderPage();
    openIssue();
    fireEvent.click(screen.getByRole("button", { name: "Выпустить" }));

    await waitFor(() => expect(screen.getByDisplayValue("-----BEGIN K")).toBeInTheDocument());
  });
});
