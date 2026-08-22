// Выбор чужого ресурса по ссылке. Здесь ошибаются молча: пустой список
// неотличим от «нет прав / не выбран проект / не выбрано поле выше», а
// значение, которого нет среди кандидатов, выглядит выбранным, хотя ссылается
// в никуда. Поэтому каждое из трёх состояний обязано СКАЗАТЬ, чего оно ждёт.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const list = jest.fn<(path: string, q: Record<string, string>) => Promise<Record<string, unknown>>>();
let project: { id: string; accountId?: string } | null = { id: "prj-1" };

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/context-store", () => ({
  useProjectStore: (sel: (s: { project: typeof project }) => unknown) => sel({ project }),
  useContext: (sel: (s: { account: null }) => unknown) => sel({ account: null }),
}));

const { RefSelect } = await import("./RefSelect");

function show(over: Partial<Parameters<typeof RefSelect>[0]> = {}) {
  const onChange = jest.fn<(uid: string) => void>();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RefSelect refResource="networks" refProjectScoped onChange={onChange} {...over} />
    </QueryClientProvider>,
  );
  return { onChange };
}

const picker = () => screen.getByRole("combobox");
const optionLabels = () => [...picker().querySelectorAll("option")].map((o) => o.textContent ?? "");

beforeEach(() => {
  jest.clearAllMocks();
  project = { id: "prj-1" };
  list.mockResolvedValue({
    networks: [
      { id: "net-1", name: "frontend" },
      { id: "net-2", name: "backend" },
    ],
  });
});

describe("подсказка выбора склоняет имя ресурса", () => {
  // «Выбрать» управляет ВИНИТЕЛЬНЫМ падежом ровно так же, как «Создать».
  // Подсказка собиралась из ИМЕНИТЕЛЬНОГО (`spec.singular`) и читалась «Выбрать
  // таблица маршрутов». У мужского и среднего рода винительный совпадает с
  // именительным, поэтому половина подсказок выглядела исправной — и класс
  // держался ровно на этом.
  it("падеж берётся из объявления ресурса", () => {
    show({ refResource: "route-tables" });

    expect(optionLabels()).toContain("Выбрать таблицу маршрутов…");
  });

  // Положительный контроль: там, где падежи совпадают, подсказка обязана
  // остаться прежней — иначе «склонение работает» означало бы лишь, что текст
  // стал другим.
  it("не портит имена, у которых падежи совпадают", () => {
    show({ refResource: "subnets" });

    expect(optionLabels()).toContain("Выбрать подсеть…");
  });

  it("подсказку, заданную вызывающим, не переписывает", () => {
    show({ placeholder: "Выберите сеть" });

    expect(optionLabels()).toContain("Выберите сеть");
  });
});

describe("RefSelect", () => {
  it("неизвестный тип ссылки назван прямо, а не показан пустым списком", () => {
    show({ refResource: "no-such-resource" });

    expect(screen.getByText(/Неизвестная ссылка: no-such-resource/)).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("кандидаты загружаются в области выбранного проекта", async () => {
    show();

    await waitFor(() => expect(list).toHaveBeenCalledWith("/vpc/v1/networks", { project_id: "prj-1" }));
    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    expect(optionLabels()).toContain("backend");
  });

  it("без выбранного проекта список не запрашивается и сказано, чего ждать", () => {
    project = null;
    show();

    expect(list).not.toHaveBeenCalled();
    expect(screen.getByText("Выберите проект в шапке для загрузки.")).toBeInTheDocument();
    expect(picker()).toBeDisabled();
  });

  it("пока поле-источник выше не заполнено, список не запрашивается и это названо", () => {
    show({ refQueryFromField: { param: "subnet_id", field: "subnet_id" }, formValue: {} });

    expect(list).not.toHaveBeenCalled();
    expect(screen.getByText("Сначала выберите «subnet_id» выше.")).toBeInTheDocument();
  });

  it("заполненное поле-источник уезжает в запрос параметром", async () => {
    show({ refQueryFromField: { param: "subnet_id", field: "subnet_id" }, formValue: { subnet_id: "sub-7" } });

    await waitFor(() =>
      expect(list).toHaveBeenCalledWith("/vpc/v1/networks", { project_id: "prj-1", subnet_id: "sub-7" }),
    );
  });

  it("клиентский фильтр убирает из выбора то, что полю не подходит", async () => {
    show({ refFilter: (r) => (r.name as string) === "backend" });

    await waitFor(() => expect(optionLabels()).toContain("backend"));
    expect(optionLabels()).not.toContain("frontend");
  });

  it("выбор кандидата отдаёт вызывающему его идентификатор", async () => {
    const { onChange } = show();

    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    fireEvent.change(picker(), { target: { value: "net-2" } });

    expect(onChange).toHaveBeenCalledWith("net-2");
  });

  it("значение вне списка кандидатов не выдаётся за выбранное — сказано, что его нет", async () => {
    show({ value: "net-999" });

    expect(await screen.findByText(/ID не найден среди загруженных/)).toBeInTheDocument();
  });

  it("значение из списка предупреждения не вызывает", async () => {
    show({ value: "net-1" });

    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    expect(screen.queryByText(/ID не найден среди загруженных/)).not.toBeInTheDocument();
  });

  it("отказ края показан, а не превращён в пустой список", async () => {
    list.mockRejectedValue(new ApiError(403, 7, null, "no access"));
    show();

    expect(await screen.findByText(/no access/)).toBeInTheDocument();
  });

  it("без права создавать пункта «Создать» в списке нет", async () => {
    show();

    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    expect(optionLabels().some((l) => l.startsWith("+ Создать"))).toBe(false);
  });

  it("выбор пункта «Создать» открывает окно, но значения поля не меняет", async () => {
    const { onChange } = show({ createResource: "networks" });

    await waitFor(() => expect(optionLabels().some((l) => l.startsWith("+ Создать"))).toBe(true));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    fireEvent.change(picker(), { target: { value: "__create__" } });

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("подсказка выбора называет тип ресурса, если своей не задано", () => {
    show();

    expect(optionLabels()[0]).toMatch(/^Выбрать /);
  });

  // ── область поиска (#528) ───────────────────────────────────────────────────
  //
  // Поле подбора читало список ОДНОЙ страницей и искало по нему в браузере.
  // Отрицание такого поля («нет совпадений») — утверждение о прочитанном, а
  // выглядит как утверждение о мире: продолжения у выпадающего списка нет, и
  // пользователь заключает, что ресурса не существует.

  const input = () => screen.getByLabelText("select-search");

  it("ввод уходит запросом, если владелец умеет сужать список", async () => {
    show();
    await waitFor(() => expect(list).toHaveBeenCalledWith("/vpc/v1/networks", { project_id: "prj-1" }));

    fireEvent.change(input(), { target: { value: "front" } });

    // Выражение собрано по грамматике владельца: поле из его белого списка и
    // оператор подстроки. Точное равенство здесь бесполезно — набирающий имя по
    // частям не совпадёт никогда.
    await waitFor(() =>
      expect(list).toHaveBeenCalledWith("/vpc/v1/networks", {
        project_id: "prj-1",
        filter: 'name CONTAINS "front"',
      }),
    );
  });

  it("сужение сервером не пересеивается ещё раз в браузере", async () => {
    show();
    fireEvent.change(input(), { target: { value: "front" } });
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));

    // Край ответил по вводу; всё, что он прислал, обязано остаться в выборе.
    // Повторное сужение по метке варианта вычло бы из ответа края строки,
    // которые он прислал именно по этому вводу.
    await waitFor(() => expect(optionLabels()).toContain("backend"));
  });

  it("выбранное значение переживает сужение и остаётся именем, а не идентификатором", async () => {
    show({ value: "net-1" });
    await waitFor(() => expect(optionLabels()).toContain("frontend"));

    // Сервер ответил по вводу, и выбранного в ответе нет — обычное дело.
    list.mockResolvedValue({ networks: [{ id: "net-2", name: "backend" }] });
    fireEvent.change(input(), { target: { value: "back" } });

    await waitFor(() => expect(optionLabels()).toContain("backend"));
    // Без запоминания метки поле показало бы `net-1` — идентификатор вместо
    // имени, ровно то, что канон консоли запрещает.
    expect(optionLabels()).toContain("frontend");
  });

  it("пустой ответ края называет свою область, а не утверждает отсутствие", async () => {
    list.mockResolvedValue({ networks: [] });
    show();

    expect(await screen.findByText(/искали по всему списку/)).toBeInTheDocument();
  });

  it("у ресурса без серверного поиска ввод не уходит, и поле об этом говорит", async () => {
    // Каталог типов машин серверного сужения не объявляет — и не должен:
    // белого списка выражения у его владельца нет. Тогда единственный законный
    // исход — сказать правду об области, а не выдавать прочитанную страницу за
    // список.
    list.mockResolvedValue({ machine_types: [] });
    show({ refResource: "machine-types", refProjectScoped: false });

    await waitFor(() => expect(list).toHaveBeenCalledTimes(1));
    fireEvent.change(input(), { target: { value: "чего-нет" } });

    expect(await screen.findByText(/нет среди загруженных/i)).toBeInTheDocument();
    expect(list).toHaveBeenCalledTimes(1);
  });
});
