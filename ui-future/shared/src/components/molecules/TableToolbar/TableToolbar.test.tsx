// Тулбар таблицы: поиск и настройка видимости колонок. Видимость ПЕРЕЖИВАЕТ
// перезагрузку страницы, и это её смысл — иначе настройку пришлось бы делать
// заново на каждом заходе. Хранилище браузера может быть недоступно (приватный
// режим, запрет сторонних данных): тогда тулбар обязан работать без сохранения,
// а не падать вместе со всей таблицей.
//

import { jest } from "@jest/globals";
import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";

// Содержимое выпадающего блока приходит пропом `dropdownRender`, а не детьми, —
// его рисует общий стенд-заменитель. Своей копии здесь больше нет (#570).
jest.unstable_mockModule("antd", () => antdStub());

const { ColumnSettings, TableSearch, useHiddenColumns } = await import("./TableToolbar");

const COLS = [
  { key: "name", label: "Имя" },
  { key: "cidr", label: "CIDR" },
];

beforeEach(() => {
  window.localStorage.clear();
});

describe("TableSearch", () => {
  it("отдаёт введённое вызывающему", () => {
    const onChange = jest.fn<(v: string) => void>();
    render(<TableSearch value="" onChange={onChange} scope="whole" />);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "front" } });

    expect(onChange).toHaveBeenCalledWith("front");
  });

  it("показывает текущее значение и подсказку", () => {
    render(<TableSearch value="front" onChange={() => {}} scope="whole" placeholder="Найти" />);
    expect(screen.getByRole("textbox")).toHaveValue("front");
    expect(screen.getByRole("textbox")).toHaveAttribute("placeholder", "Найти");
  });
});

describe("ColumnSettings", () => {
  it("отмеченными показывает ВИДИМЫЕ колонки, а хранит скрытые", () => {
    // Инверсия здесь — не мелочь: перепутав её, настройка показала бы ровно
    // обратное тому, что видно в таблице.
    render(<ColumnSettings columns={COLS} hidden={new Set(["cidr"])} onToggle={() => {}} />);

    const boxes = screen.getAllByRole("checkbox");
    expect(boxes).toHaveLength(2);
    expect(boxes[0]).toBeChecked();
    expect(boxes[1]).not.toBeChecked();
  });

  it("снятие галочки просит СКРЫТЬ эту колонку", () => {
    const onToggle = jest.fn<(key: string, hidden: boolean) => void>();
    render(<ColumnSettings columns={COLS} hidden={new Set()} onToggle={onToggle} />);

    fireEvent.click(screen.getAllByRole("checkbox")[0]);

    expect(onToggle).toHaveBeenCalledWith("name", true);
  });

  it("возврат галочки просит показать", () => {
    const onToggle = jest.fn<(key: string, hidden: boolean) => void>();
    render(<ColumnSettings columns={COLS} hidden={new Set(["name"])} onToggle={onToggle} />);

    fireEvent.click(screen.getAllByRole("checkbox")[0]);

    expect(onToggle).toHaveBeenCalledWith("name", false);
  });
});

describe("useHiddenColumns", () => {
  it("на чистом хранилище скрытых колонок нет", () => {
    const { result } = renderHook(() => useHiddenColumns("t-1"));
    expect([...result.current[0]]).toEqual([]);
  });

  it("скрытое сохраняется и переживает перезагрузку", () => {
    const first = renderHook(() => useHiddenColumns("t-2"));
    act(() => first.result.current[1]("cidr", true));
    expect([...first.result.current[0]]).toEqual(["cidr"]);

    // Новый экземпляр — то же, что новый заход на страницу.
    const second = renderHook(() => useHiddenColumns("t-2"));
    expect([...second.result.current[0]]).toEqual(["cidr"]);
  });

  it("показ обратно снимает запись", () => {
    const { result } = renderHook(() => useHiddenColumns("t-3"));
    act(() => result.current[1]("cidr", true));
    act(() => result.current[1]("cidr", false));

    expect([...result.current[0]]).toEqual([]);
    expect(renderHook(() => useHiddenColumns("t-3")).result.current[0].size).toBe(0);
  });

  it("настройки разных таблиц не смешиваются", () => {
    const a = renderHook(() => useHiddenColumns("table-a"));
    act(() => a.result.current[1]("cidr", true));

    expect([...renderHook(() => useHiddenColumns("table-b")).result.current[0]]).toEqual([]);
  });

  it("испорченная запись читается как «ничего не скрыто», а не роняет таблицу", () => {
    window.localStorage.setItem("t-4", "не json");
    const { result } = renderHook(() => useHiddenColumns("t-4"));
    expect([...result.current[0]]).toEqual([]);
  });

  it("недоступное хранилище не мешает скрывать колонки в текущем сеансе", () => {
    // Приватный режим/запрет сторонних данных: сохранение недоступно, но
    // таблица обязана оставаться настраиваемой.
    const setItem = jest.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceededError");
    });
    try {
      const { result } = renderHook(() => useHiddenColumns("t-5"));
      act(() => result.current[1]("cidr", true));
      expect([...result.current[0]]).toEqual(["cidr"]);
    } finally {
      setItem.mockRestore();
    }
  });
});
