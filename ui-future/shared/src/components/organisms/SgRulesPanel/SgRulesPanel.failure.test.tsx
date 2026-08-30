// Отказ края не имеет права уносить с собой набранное правило.
//
// Форма правила закрывалась ДО того, как край отвечал: `saveEdit` звал
// `cancelEdit()`, а отправку — следующим стейтментом, и та была асинхронной.
// Любой отказ (ссылка на группу чужой сети, несуществующий набор префиксов,
// потолок правил) приходил в уже размонтированную форму: оператор видел
// всплывающее сообщение и список — набранное направление, протокол, порты и
// источник исчезали без следа, восстановить их можно было только по памяти.
//
// Предмет проб — ПОРЯДОК: закрытие формы обязано быть следствием успеха, а не
// предшествовать ответу. И причина обязана быть привязана к имени поля, которое
// оператор ВИДИТ: край называет `addition_rule_specs[0].ports.from_port`, а на
// экране это «От».

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update, get: jest.fn(), list: jest.fn(), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { SgRulesPanel } = await import("./SgRulesPanel");
const { PageHeaderSlotProvider, HeaderRightSlot } = await import("@shared/components/molecules/PageHeaderSlot");
// Панель живёт ВНУТРИ карточки ресурса, и это условие рендера, а не декорация:
// по нему `FormShell` решает, рисовать ли собственную шапку. Без провайдера
// проба показывала форму в посадке, которой на странице не бывает, — с шапкой,
// которой на экране нет. Тот же довод, по которому здесь настоящий слот шапки.
const { DetailHeaderProvider } = await import("@shared/components/molecules/PanelHeader");
// Роутер здесь не декорация: ссылочные цели рисуются `<Link>`, а он без роутера
// роняет рендер целиком.
const { MemoryRouter } = await import("react-router");

const EMPTY_LIST = "Правил нет — трафик блокируется (default-deny).";

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/security-groups/sg-1"]}>
        <PageHeaderSlotProvider>
          <DetailHeaderProvider value={{ icon: <span aria-hidden /> }}>
            <div data-testid="header-slot">
              <HeaderRightSlot />
            </div>
            <SgRulesPanel sgId="sg-1" projectId="prj-1" rules={[]} networkId="net-1" />
          </DetailHeaderProvider>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Заменитель `Form.Item` рисует подпись в `<label>` рядом с полем. */
const field = (label: string) => within(screen.getByText(label).parentElement as HTMLElement);

/** Действие списка живёт в шапке; у пустой вкладки есть своё такое же в теле. */
const openForm = () =>
  fireEvent.click(within(screen.getByTestId("header-slot")).getByRole("button", { name: /Добавить правило/ }));
/**
 * Основное действие подвала — по КОНСТРУКЦИИ, а не по подписи.
 *
 * Подпись подвала называет одно действие («Добавить»): предмет уже назван
 * заголовком над формой (канон §8). По имени кнопку теперь не выбрать — тем же
 * словом подписаны кнопки добавления блока в наборах CIDR внутри правила, и
 * прежнее `getByRole("button", { name: "Добавить правило" })` искало подпись,
 * которой продукт больше не печатает.
 *
 * Единственность `DopplerButton` в подвале утверждается, а не предполагается.
 */
function submitButton(): HTMLElement {
  const found = screen.getAllByRole("button").filter((b) => b.classList.contains("doppler-btn"));
  expect(found).toHaveLength(1);
  return found[0];
}
const submitForm = () => fireEvent.click(submitButton());
/** Форма открыта — доказывается её заголовком, а не тем, что где-то есть кнопка. */
const formIsOpen = () => expect(screen.getByRole("heading", { name: "Добавить правило" })).toBeInTheDocument();

const TYPED = "из офиса";
const typeDescription = () => fireEvent.change(field("Описание").getByRole("textbox"), { target: { value: TYPED } });
const typedValueSurvives = () => expect(screen.getByDisplayValue(TYPED)).toBeInTheDocument();

/** Отказ с указанием поля — в той форме, в какой его строит `serviceerr.InvalidArg`. */
function fieldViolation(fieldPath: string, description: string) {
  return new ApiError(
    400,
    3,
    [
      {
        "@type": "type.googleapis.com/google.rpc.BadRequest",
        fieldViolations: [{ field: fieldPath, description }],
      },
    ],
    description,
  );
}

beforeEach(() => {
  jest.clearAllMocks();
  // Успешный ответ несёт ОПЕРАЦИЮ: правка правил — мутация группы
  // безопасности, а мутации Kachō отвечают `Operation`. Пустой объект,
  // стоявший здесь прежде, был ответом БЕЗ операции — то есть тем самым
  // случаем, который подтвердить нечем, — и подавался как успех.
  update.mockResolvedValue({ id: "opr-1", done: false });
});

describe("SgRulesPanel — отказ края при сохранении правила", () => {
  it("форма остаётся на экране, а набранное — на месте", async () => {
    update.mockRejectedValue(
      fieldViolation("addition_rule_specs[0].security_group_id", "security group belongs to another network"),
    );
    show();

    openForm();
    typeDescription();
    submitForm();

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));

    // Форма НА ЭКРАНЕ — её заголовок. Прежде здесь искалась кнопка «Добавить
    // правило»: та подпись была у подвала формы, и утверждение читалось как
    // «форма открыта». Подпись подвала укоротилась до действия, а такая же
    // кнопка осталась у списка — то есть по имени «форма открыта» и «список
    // вернулся» стали неотличимы. Заголовок формы есть только у формы.
    formIsOpen();
    expect(screen.queryByText(EMPTY_LIST)).not.toBeInTheDocument();
    // Подпись подвала — та, что и должна быть: без неё «форма осталась»
    // зеленело бы на форме, потерявшей своё действие.
    expect(submitButton().textContent).toBe("Добавить");
    typedValueSurvives();
  });

  it("причина показана над подвалом и названа именем ВИДИМОГО поля", async () => {
    update.mockRejectedValue(
      fieldViolation("addition_rule_specs[0].ports.from_port", "from_port must be between 1 and 65535"),
    );
    show();

    openForm();
    typeDescription();
    submitForm();

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());

    const alert = screen.getByRole("alert");
    // «От» — подпись поля в самой форме; `ports.from_port` оператор не видит нигде.
    expect(alert).toHaveTextContent("От");
    expect(alert).toHaveTextContent("from_port must be between 1 and 65535");
  });

  it("отказ без указания поля показан как есть — имя поля не выдумывается", async () => {
    // Потолок правил край относит ко всему набору (`addition_rule_specs`), а не
    // к полю формы. Приписать эту причину видимому полю значило бы солгать.
    update.mockRejectedValue(fieldViolation("addition_rule_specs", "at most 100 rules per security group"));
    show();

    openForm();
    typeDescription();
    submitForm();

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());

    expect(screen.getByRole("alert")).toHaveTextContent("at most 100 rules per security group");
    typedValueSurvives();
  });

  it("успех — и только он — закрывает форму", async () => {
    show();

    openForm();
    typeDescription();
    submitForm();

    // Положительный контроль: без него «форма осталась» зеленело бы на форме,
    // которая не закрывается никогда.
    await waitFor(() => expect(screen.getByText(EMPTY_LIST)).toBeInTheDocument());
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("ответ без операции оставляет форму открытой и называет причину", async () => {
    // Третий исход: край ответил, но подтвердить выполнение нечем. Прежде он
    // был неотличим от успеха — форма закрывалась, список показывал прежний
    // набор, и оператор уходил уверенным, что правило добавлено.
    update.mockResolvedValue({});
    show();

    openForm();
    typeDescription();
    submitForm();

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent(
      "сервер не вернул операцию — подтвердить выполнение невозможно",
    );
    formIsOpen();
    typedValueSurvives();
  });

  it("повторная отправка после отказа убирает прежнюю причину", async () => {
    update.mockRejectedValueOnce(fieldViolation("addition_rule_specs[0].target", "target is required"));
    show();

    openForm();
    typeDescription();
    submitForm();
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());

    submitForm();

    await waitFor(() => expect(screen.getByText(EMPTY_LIST)).toBeInTheDocument());
  });
});
