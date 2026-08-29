import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Operation } from "@shared/api/types";

/**
 * Исход удаления тега читается ОБЩИМ разбором, а не своим условием (issue #409).
 *
 * ПРЕДМЕТ. Мутация отвечает `Operation`, и у ответа ТРИ исхода, а не два:
 * операция завершилась с ошибкой · её статус прочитать не удалось · выполнена.
 * Своё условие рядом с местом показа читало первый и третий, а второй выходил
 * молча — и опрос, упавший на 403/404/сети, оставлял бесконечное ожидание:
 * подтверждения не будет никогда, а кнопка так и стоит в состоянии «идёт».
 *
 * Четвёртый способ ошибиться — ответ БЕЗ операции. `DeleteTag` объявлен
 * возвращающим `Operation`, поэтому ответ без неё есть нарушение контракта, а не
 * синхронный успех: подтвердить выполнение нечем, и объявлять тег удалённым не
 * на чем. Прежнее условие объявляло — и показывало успешный тост на ответе, из
 * которого не следует ничего.
 *
 * ПОЧЕМУ ПРОБА МОНТИРУЕТ, А НЕ ЗОВЁТ РАЗБОР. Сам разбор (`operationOutcome`)
 * проверен у себя дома. Здесь утверждается ДРУГОЕ — что панель его исполняет:
 * проба, зовущая разбор напрямую, закрепила бы его ответ, а не место, где он
 * читается, и осталась бы зелёной при собственном условии рядом.
 */

const listTags = jest.fn<() => Promise<unknown>>();
const deleteTag = jest.fn<() => Promise<unknown>>();
const get = jest.fn<() => Promise<unknown>>();
const success = jest.fn();
const error = jest.fn();

jest.unstable_mockModule("@/api/resources", () => ({
  registriesApi: { listTags, deleteTag, get },
}));

jest.unstable_mockModule("@/lib/toast", () => ({
  toast: { error, success, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

/**
 * Опрос операции подменён ЦЕЛИКОМ — вместе с его собственным отказом.
 *
 * Подмена, отдающая один `data`, воспроизвела бы ровно тот дефект, ради которого
 * проба написана: полоса «статус не прочитан» была бы недостижима, и утверждение
 * о ней прошло бы, ничего не измерив.
 */
let poll: { data: Operation | undefined; isError: boolean; error: unknown } = {
  data: undefined,
  isError: false,
  error: null,
};
jest.unstable_mockModule("@/lib/use-operation", () => ({
  useOperation: (opId: string | null) => (opId ? poll : { data: undefined, isError: false, error: null }),
  useInvalidateResourceList: () => () => undefined,
}));

const { RepositoryTagsPanel } = await import("./RepositoryTagsPanel");

const TAG = {
  registry_id: "reg-1",
  repository: "nginx",
  tag: "v1",
  digest: "sha256:abcdef0123456789",
  size_bytes: "2048",
};

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RepositoryTagsPanel registryId="reg-1" repository="nginx" onClose={() => undefined} />
    </QueryClientProvider>,
  );
}

/** Открывает подтверждение удаления тега и нажимает «Удалить». */
async function confirmDelete() {
  const trigger = await screen.findByRole("button", { name: /Удалить тег/ });
  fireEvent.click(trigger);
  const ok = await screen.findByRole("button", { name: /^Удалить$/ });
  fireEvent.click(ok);
}

beforeEach(() => {
  jest.clearAllMocks();
  poll = { data: undefined, isError: false, error: null };
  listTags.mockResolvedValue({ tags: [TAG] });
  get.mockResolvedValue({ endpoint: "registry.kacho.local/reg-1" });
});

describe("удаление тега: исход мутации читается общим разбором", () => {
  it("положительный контроль: операция выполнена — успех назван, и назван один раз", async () => {
    // Отрицания ниже зеленели бы на панели, которая вообще ничего не делает:
    // рядом стоит утверждение, что рабочий путь работает.
    deleteTag.mockResolvedValue({ operation: { id: "op-1" } });
    poll = { data: { id: "op-1", done: true } as Operation, isError: false, error: null };
    wrap();
    await confirmDelete();
    await waitFor(() => expect(success).toHaveBeenCalledWith("Тег v1 удалён"));
    expect(error).not.toHaveBeenCalled();
  });

  it("операция завершилась С ОШИБКОЙ — сказано это, и успех не объявлен", async () => {
    deleteTag.mockResolvedValue({ operation: { id: "op-1" } });
    poll = {
      data: { id: "op-1", done: true, error: { code: 9, message: "тег используется" } } as Operation,
      isError: false,
      error: null,
    };
    wrap();
    await confirmDelete();
    await waitFor(() => expect(error).toHaveBeenCalledWith(expect.stringContaining("тег используется")));
    expect(success).not.toHaveBeenCalled();
  });

  it("СТАТУС ПРОЧИТАТЬ НЕ УДАЛОСЬ — это отказ, а не бесконечное ожидание", async () => {
    // Полоса, которой у своего условия не было вовсе. Операция не «выполняется»
    // — про неё просто ничего не известно, и молча оставленное ожидание не
    // кончится никогда.
    deleteTag.mockResolvedValue({ operation: { id: "op-1" } });
    poll = { data: undefined, isError: true, error: new Error("403") };
    wrap();
    await confirmDelete();
    await waitFor(() => expect(error).toHaveBeenCalledWith(expect.stringContaining("статус операции")));
    expect(success).not.toHaveBeenCalled();
  });

  it("ответ БЕЗ операции — нарушение контракта, а не синхронный успех", async () => {
    // `DeleteTag` объявлен возвращающим `Operation` (`ops.delete` у спеки `tags`,
    // `mutationsReturnOperation` не отменён). Ответ без неё не даёт подтверждения
    // — объявлять тег удалённым не на чем.
    deleteTag.mockResolvedValue({});
    wrap();
    await confirmDelete();
    await waitFor(() => expect(error).toHaveBeenCalled());
    expect(success).not.toHaveBeenCalled();
  });
});
