// Поле с подсказками — что оператор ВИДИТ и что оказывается в поле.
//
// ЧТО ИМЕННО ДЕРЖИТСЯ — две стороны одного контракта, и обе ломаются молча.
//
// ПЕРВАЯ: видимое оператору у поля с подсказками — СПИСОК ПОДСКАЗОК, и приходит
// он пропом `options`, а не детьми. Поле без списка выглядит обычной строкой
// ввода: выбрать нечего, и «никого не предложено» неотличимо от «предложены
// не те». Поэтому ниже стоит положительный контроль присутствия подсказок —
// без него утверждение о поле зеленело бы на форме, где выбирать не из чего.
//
// ВТОРАЯ: настоящее поле с подсказками зовёт `onChange` ЗНАЧЕНИЕМ (оно
// наследует `InternalSelectProps`), а не событием DOM, — и продукт на это
// рассчитывает: `onChange={(v) => setQuery(v)}`. Поле, зовущее `onChange`
// событием, кладёт в состояние объект, а поле показывает `[object Object]`
// С ПЕРВОГО НАЖАТИЯ: человек печатает почту и видит вместо неё служебную
// строку, а край получает запрос по ней же.
//
// Утверждается НАБЛЮДАЕМОЕ оператором — текст подсказок на экране и значение
// в поле, — а не форма заменителя: ни одного признака, которого настоящая
// библиотека не производит, здесь нет.
//
// Инъекция в обе стороны: `AutoComplete: Input` в заменителе — красное на обоих
// утверждениях; заменитель на месте — молчание.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const listUsers = jest.fn<() => Promise<{ users: unknown[] }>>();

jest.unstable_mockModule("@shared/api/iam", () => ({ iamApi: { listUsers } }));
jest.unstable_mockModule("@shared/api/cluster", () => ({ clusterApi: { grantAdmin: jest.fn() } }));
jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: jest.fn(), success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));
jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useOperation: () => ({ data: undefined }),
  useInvalidateResourceList: () => jest.fn(),
}));

const { GrantAdminModal } = await import("./GrantAdminModal");

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <GrantAdminModal open onClose={jest.fn()} />
    </QueryClientProvider>,
  );
}

/** Поле ищется по подсказке-заполнителю: её настоящая библиотека кладёт на само поле ввода. */
const field = () => screen.getByPlaceholderText(/ищется по всему списку/i) as HTMLInputElement;

beforeEach(() => {
  jest.clearAllMocks();
  listUsers.mockResolvedValue({ users: [] });
});

describe("GrantAdminModal — поле с подсказками", () => {
  it("показывает подсказки, из которых оператор выбирает", async () => {
    // Положительный контроль: без него утверждение о значении в поле проходило
    // бы на поле, у которого подсказок нет вовсе.
    listUsers.mockResolvedValue({
      users: [
        { id: "usr-1", email: "one@example.com", invite_status: "ACTIVE" },
        { id: "usr-2", email: "two@example.com", invite_status: "ACTIVE" },
      ],
    });
    show();

    await waitFor(() => expect(screen.getByText("one@example.com")).toBeInTheDocument());
    expect(screen.getByText("two@example.com")).toBeInTheDocument();
  });

  it("напечатанное стоит в поле СТРОКОЙ, а не служебной записью объекта", async () => {
    show();
    await waitFor(() => expect(listUsers).toHaveBeenCalled());

    fireEvent.change(field(), { target: { value: "ops@example.com" } });

    // Оператор видит то, что напечатал. Обработчик, зовущийся событием, кладёт
    // в состояние объект, и поле показывает служебную строку.
    expect(field().value).toBe("ops@example.com");
    expect(field().value).not.toBe("[object Object]");
  });
});
