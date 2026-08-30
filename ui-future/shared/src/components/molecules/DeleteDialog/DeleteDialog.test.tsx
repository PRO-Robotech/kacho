// Подтверждение удаления — необратимое действие, и у него ровно три свойства,
// каждое из которых ломается молча:
//
//   * у ресурса повышенного риска кнопка не активна, пока не введено ИМЯ.
//     Совпадение проверяется точно: «почти то же» имя означает другой ресурс;
//   * запрос уходит по переданному пути — DELETE не по тому адресу вернёт 404
//     или, хуже, удалит соседа;
//   * ресурс, объявивший асинхронные мутации, подтверждается ОПЕРАЦИЕЙ. Ответ
//     без операции у такого ресурса — не «удалено синхронно», а «подтвердить
//     нечем»; принять его за успех значит объявить удаление, которого не было.
//
// antd переопределён локально: общий стенд подменяет `Modal` div'ом, который
// рисует детей и НЕ рисует подвал, — а обе кнопки живут именно в подвале.

import { jest } from "@jest/globals";
import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Modal: ({ open, children, footer }: React.PropsWithChildren<{ open?: boolean; footer?: React.ReactNode }>) =>
    open ? React.createElement("div", { role: "dialog" }, children, footer) : null,
}));

const { toast } = await import("@shared/lib/toast");
const { DeleteDialog, requiresNameConfirm, deleteConsequence } = await import("./DeleteDialog");

const realFetch = globalThis.fetch;
let calls: Array<{ method: string; url: string }> = [];

/** Отвечает на DELETE заданным телом, на GET операции — заданным состоянием. */
function stubServer(deleteBody: unknown, operations: Record<string, unknown> = {}) {
  globalThis.fetch = ((url: string, init?: RequestInit) => {
    calls.push({ method: init?.method ?? "GET", url: String(url) });
    const body = String(url).includes("/operations/")
      ? operations[String(url).split("/operations/")[1]]
      : deleteBody;
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(body ?? {})),
    } as Response);
  }) as typeof fetch;
}

beforeEach(() => {
  calls = [];
  jest.restoreAllMocks();
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderDialog(props: Partial<Parameters<typeof DeleteDialog>[0]> = {}) {
  const onOpenChange = jest.fn<(open: boolean) => void>();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <DeleteDialog
        open
        onOpenChange={onOpenChange}
        apiPath="/vpc/v1/subnets/sub-1"
        resourceId="subnets"
        resourceLabel="Подсеть"
        name="frontend"
        projectId="prj-1"
        {...props}
      />
    </QueryClientProvider>,
  );
  return { onOpenChange };
}

describe("DeleteDialog — подтверждение именем", () => {
  it("без требования подтверждения удалить можно сразу", () => {
    stubServer({ operation: { id: "op-1" } });
    renderDialog();
    expect(screen.getByRole("button", { name: "Удалить" })).not.toBeDisabled();
  });

  it("с требованием — кнопка мертва, пока имя не введено точно", () => {
    stubServer({ operation: { id: "op-1" } });
    renderDialog({ requireNameConfirm: true });

    const confirm = screen.getByRole("button", { name: "Удалить" });
    expect(confirm).toBeDisabled();

    // «Почти то же» имя — другой ресурс.
    fireEvent.change(screen.getByPlaceholderText("frontend"), { target: { value: "frontend2" } });
    expect(confirm).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("frontend"), { target: { value: "frontend" } });
    expect(confirm).not.toBeDisabled();
  });

  it("ввод имени спрашивают там, где исчезают данные, и не спрашивают, где нет", () => {
    // ЗДЕСЬ ЗАКРЕПЛЯЛСЯ ПЕРЕВЁРНУТЫЙ КРИТЕРИЙ: «networks → да, subnets → нет».
    // Проба была верна про механизм и неверна про предмет — она пиннила ровно
    // то состояние, из-за которого самая дорогая ошибка стоила одного клика, а
    // самая безобидная требовала набирать имя (#1606).
    //
    // Сам критерий держит гейт `console-delete-ritual-tracks-risk` (множества
    // «требует имя» и «защищён RESTRICT» не пересекаются). Здесь — только то,
    // что признак ЧИТАЕТСЯ из объявления причины и отвечает обе стороны.
    expect(requiresNameConfirm("volumes")).toBe(true);
    expect(requiresNameConfirm("networks")).toBe(false);
    expect(deleteConsequence("volumes")).toMatch(/Данные тома/);
    expect(deleteConsequence("networks")).toBeUndefined();
  });

  it("диалог называет, ЧТО исчезнет, — и не выдумывает потерю там, где её нет", () => {
    // Отрицание в паре с положительным: без второй половины проба зеленела бы
    // на диалоге, который вообще ничего не пишет.
    renderDialog({ resourceId: "volumes", requireNameConfirm: true });
    expect(screen.getByTestId("delete-consequence").textContent).toMatch(/Данные тома будут стёрты/);
    cleanup();

    renderDialog({ resourceId: "networks" });
    expect(screen.getByTestId("delete-consequence").textContent).toMatch(/будет удалён безвозвратно/);
  });
});

describe("DeleteDialog — запрос", () => {
  it("шлёт DELETE ровно по переданному пути", async () => {
    stubServer({ operation: { id: "op-1", done: false } }, { "op-1": { id: "op-1", done: false } });
    renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Удалить" }));

    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    const del = calls.find((c) => c.method === "DELETE")!;
    expect(del.url).toContain("/vpc/v1/subnets/sub-1");
  });

  // Тексты ниже сменились вместе с единым механизмом сигнала об исходе
  // (`lib/mutation-signal.ts`). Прежние — «Подсеть frontend удалён», «Удалить
  // Подсеть frontend: …» — закрепляли ДЕФЕКТ: причастие было впаяно в мужском
  // роде, а «Подсеть» женского. Класс держался на том, что у мужского рода
  // форма совпадает с верной, поэтому половина сообщений выглядела исправной.
  it("успешная операция закрывает окно и сообщает об удалении", async () => {
    const successSpy = jest.spyOn(toast, "success").mockReturnValue("t");
    stubServer({ operation: { id: "op-1" } }, { "op-1": { id: "op-1", done: true } });
    const { onOpenChange } = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Удалить" }));

    await waitFor(() => expect(successSpy).toHaveBeenCalledWith("Подсеть frontend удалена"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("операция с ошибкой удалением НЕ считается", async () => {
    const errorSpy = jest.spyOn(toast, "error").mockReturnValue("t");
    const successSpy = jest.spyOn(toast, "success").mockReturnValue("t");
    stubServer(
      { operation: { id: "op-1" } },
      { "op-1": { id: "op-1", done: true, error: { code: 9, message: "subnet is not empty" } } },
    );
    renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Удалить" }));

    await waitFor(() => expect(errorSpy).toHaveBeenCalledWith("Подсеть frontend не удалена: subnet is not empty"));
    expect(successSpy).not.toHaveBeenCalled();
  });

  it("ресурс обещал операцию, а её не пришло — это отказ, а не тихий успех", async () => {
    // Иначе окно закрылось бы, список обновился, и пользователь считал бы
    // ресурс удалённым, не имея на это ни одного подтверждения.
    const errorSpy = jest.spyOn(toast, "error").mockReturnValue("t");
    const successSpy = jest.spyOn(toast, "success").mockReturnValue("t");
    stubServer({ id: "sub-1", name: "frontend" });
    const { onOpenChange } = renderDialog({ expectOperation: true });

    fireEvent.click(screen.getByRole("button", { name: "Удалить" }));

    await waitFor(() => expect(errorSpy).toHaveBeenCalled());
    expect(String(errorSpy.mock.calls[0][0])).toContain("Подсеть frontend не удалена:");
    expect(successSpy).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
  });

  it("отмена закрывает окно и запроса не шлёт", () => {
    stubServer({ operation: { id: "op-1" } });
    const { onOpenChange } = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Отмена" }));

    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
  });
});
