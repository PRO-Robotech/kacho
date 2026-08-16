// Общие части страниц прав. Три предмета, каждый ломается молча:
//   • разделение ролей на системные и свои читает ОБА имени поля (край отдаёт
//     camelCase, старое snake_case оставлено для совместимости) — промах уводит
//     все преднастроенные роли в «Кастомные»;
//   • отсутствующая метка времени обязана читаться прочерком, а не пустотой;
//   • обёртка мутации: пока операция не завершилась, форма ЗАНЯТА, а завершение
//     с ошибкой обязано показать её, а не молча закрыться успехом.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import type { Operation } from "@shared/api/types";

// Подставной край отдаёт `unknown`, и это не небрежность: ФОРМА ответа —
// ровно то, что проверяется. Прежний тип `{ operation: Operation }` объявлял
// вложенный конверт, которого настоящий край не отдаёт, поэтому все фикстуры
// ниже кормили обёртку формой, которой не бывает, — и дефект был невидим.
const create = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();
const toastSuccess = jest.fn();
let operation: Operation | undefined;

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { create, get: jest.fn(), list: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

// Наблюдатель операции подменён окном в ЕЁ состояние: настоящий поллит сеть, а
// предмет пробы — что обёртка делает по «готово» и по «готово с ошибкой».
jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useOperation: (id: string | null) => ({ data: id ? operation : undefined }),
}));

const { groupedRoleOptions, fmtTs, CopyableMonoId, SystemTag, useIamMutation } = await import("./IamCommon");
type Role = Parameters<typeof groupedRoleOptions>[0][number];

const role = (over: Partial<Role>): Role => ({ id: "rol-1", name: "viewer", ...over });

beforeEach(() => {
  jest.clearAllMocks();
  operation = undefined;
});

describe("groupedRoleOptions", () => {
  it("пустой список групп не выдумывает", () => {
    expect(groupedRoleOptions([])).toEqual([]);
  });

  it("системную роль опознаёт по camelCase-имени поля, как его отдаёт край", () => {
    const groups = groupedRoleOptions([role({ id: "rol-1", isSystem: true })]);

    expect(groups.map((g) => g.label)).toEqual(["Системные"]);
  });

  it("и по старому snake_case — совместимость не потеряна", () => {
    const groups = groupedRoleOptions([role({ id: "rol-1", is_system: true })]);

    expect(groups.map((g) => g.label)).toEqual(["Системные"]);
  });

  it("своя роль уходит в «Кастомные», а не к системным", () => {
    const groups = groupedRoleOptions([role({ id: "rol-9", name: "own" })]);

    expect(groups.map((g) => g.label)).toEqual(["Кастомные"]);
  });

  it("подпись варианта несёт и имя, и идентификатор — имена совпадают между аккаунтами", () => {
    const groups = groupedRoleOptions([role({ id: "rol-1", name: "viewer", isSystem: true })]);

    expect(groups[0].options).toEqual([{ value: "rol-1", label: "viewer · rol-1" }]);
  });

  it("обе группы показываются в порядке «системные, затем свои»", () => {
    const groups = groupedRoleOptions([role({ id: "rol-9", name: "own" }), role({ id: "rol-1", isSystem: true })]);

    expect(groups.map((g) => g.label)).toEqual(["Системные", "Кастомные"]);
  });
});

describe("fmtTs", () => {
  it("отсутствующее время читается прочерком", () => {
    expect(fmtTs(undefined)).toBe("—");
    expect(fmtTs("")).toBe("—");
  });

  it("нечитаемое значение тоже читается прочерком, а не «Invalid Date»", () => {
    expect(fmtTs("не-время")).toBe("—");
  });

  it("настоящую метку показывает по-русски, а не сырой строкой", () => {
    expect(fmtTs("2026-08-07T10:20:30Z")).toMatch(/^\d{2}\.\d{2}\.\d{4}, в \d{2}:\d{2}$/);
  });
});

describe("CopyableMonoId", () => {
  it("пустой идентификатор показан прочерком и копировать нечего", () => {
    render(<CopyableMonoId id={undefined} />);

    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("идентификатор показан и кладётся в буфер по нажатию", () => {
    const writeText = jest.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(<CopyableMonoId id="usr-1" />);

    expect(screen.getByText("usr-1")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button"));

    expect(writeText).toHaveBeenCalledWith("usr-1");
    expect(toastSuccess).toHaveBeenCalledWith("Скопировано");
  });
});

describe("SystemTag", () => {
  it("системная и своя роль подписаны по-разному", () => {
    const { unmount } = render(<SystemTag isSystem />);
    expect(screen.getByText("Системная")).toBeInTheDocument();
    unmount();

    render(<SystemTag isSystem={false} />);
    expect(screen.getByText("Пользовательская")).toBeInTheDocument();
  });
});

describe("useIamMutation", () => {
  function Harness() {
    const [rejected, setRejected] = React.useState<string | null>(null);
    // Наблюдатель операции подменён чтением переменной стенда; чтобы новое её
    // значение вообще было прочитано, нужен повторный проход — его и даёт
    // «перечитать». Настоящему наблюдателю это даёт поллинг.
    const [, reread] = React.useReducer((n: number) => n + 1, 0);
    const { run, submitting } = useIamMutation({
      method: "POST",
      path: "/iam/v1/users",
      invalidateKeys: [["users"]],
      successText: "Готово",
    });
    // Отказ не проглатывается: обёртка пробрасывает его вызывающему, и стенд
    // ПОКАЗЫВАЕТ это. Пустой `catch` сделал бы «отказа не было» и «отказ был,
    // но потерян» неотличимыми — ровно тот класс, который мы и ловим.
    const start = () => {
      void run({ email: "a@b" }).then(
        () => setRejected(null),
        (e: unknown) => setRejected(e instanceof Error ? e.message : String(e)),
      );
    };
    return (
      <div>
        <button type="button" onClick={start}>
          запустить
        </button>
        <span>{submitting ? "занято" : "свободно"}</span>
        <button type="button" onClick={reread}>
          перечитать
        </button>
        <span data-testid="rejected">{rejected ?? ""}</span>
      </div>
    );
  }

  function show() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    return render(
      <QueryClientProvider client={client}>
        <Harness />
      </QueryClientProvider>,
    );
  }

  const start = () => fireEvent.click(screen.getByRole("button", { name: "запустить" }));

  it("до запуска форма свободна", () => {
    show();

    expect(screen.getByText("свободно")).toBeInTheDocument();
  });

  it("ответ без операции — нарушение контракта, а не успех", async () => {
    // Здесь стояло обратное утверждение: «синхронный ответ без операции сразу
    // отпускает форму и говорит об успехе». Оно ЗАКРЕПЛЯЛО дефект: все мутации
    // iam объявлены `returns (operation.Operation)`, поэтому ответ без операции
    // означает, что подтвердить выполнение нечем.
    create.mockResolvedValue({});
    show();

    start();

    await waitFor(() => expect(toastError).toHaveBeenCalledWith(expect.stringContaining("не вернул операцию")));
    expect(toastSuccess).not.toHaveBeenCalled();
    expect(screen.getByText("свободно")).toBeInTheDocument();
  });

  it("операция ВЕРХНИМ уровнем — та форма, которую отдаёт край, — читается", async () => {
    // ПРЕДМЕТ ЗАДАЧИ. Обёртка читала `resp.operation.id`; край отдаёт операцию
    // верхним уровнем, поэтому ключ был пуст ВСЕГДА, опрос не запускался ни разу,
    // и зелёный тост печатался по коду ответа.
    //
    // Прежние фикстуры этого не показывали, потому что кормили обёртку вложенным
    // конвертом — формой, которой у настоящего края нет.
    create.mockResolvedValue({ id: "opr-top", done: false });
    show();

    start();

    await waitFor(() => expect(screen.getByText("занято")).toBeInTheDocument());
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("операция ВЕРХНИМ уровнем, завершившаяся отказом, показывает отказ", async () => {
    // То же, доведённое до исхода: именно здесь пользователь видел успех при
    // отказе — на выдаче ролей, приглашении и членстве в группах.
    create.mockResolvedValue({ id: "opr-top-err", done: false });
    show();

    start();
    await waitFor(() => expect(screen.getByText("занято")).toBeInTheDocument());

    operation = { id: "opr-top-err", done: true, error: { code: 7, message: "permission denied" } };
    fireEvent.click(screen.getByRole("button", { name: "перечитать" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("permission denied"));
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("пока операция не завершилась, форма занята и успеха не объявляет", async () => {
    // Вложенный конверт остаётся понятным: старые точки вызова типизированы так.
      create.mockResolvedValue({ operation: { id: "opr-1", done: false } });
    show();

    start();

    await waitFor(() => expect(screen.getByText("занято")).toBeInTheDocument());
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("отказ края показан и форма отпущена", async () => {
    create.mockRejectedValue(new ApiError(409, 6, null, "user already exists"));
    show();

    start();

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("user already exists"));
    expect(screen.getByText("свободно")).toBeInTheDocument();
    expect(screen.getByTestId("rejected")).toHaveTextContent("user already exists");
  });

  it("операция, завершившаяся ошибкой, показывает её, а не успех", async () => {
    create.mockResolvedValue({ operation: { id: "opr-2", done: false } });
    show();

    start();
    await waitFor(() => expect(screen.getByText("занято")).toBeInTheDocument());

    operation = { id: "opr-2", done: true, error: { code: 9, message: "quota exceeded" } };
    fireEvent.click(screen.getByRole("button", { name: "перечитать" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("quota exceeded"));
    expect(toastSuccess).not.toHaveBeenCalled();
  });
});
