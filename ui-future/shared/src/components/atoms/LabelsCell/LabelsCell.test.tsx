import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { LabelsCell } from "./LabelsCell";

describe("LabelsCell", () => {
  const writeText = jest.fn<(text: string) => Promise<void>>();

  // Утверждения ссылаются на СПЯЩИЕ ФУНКЦИИ, а не на `toast.success`/`toast.error`
  // по имени владельца: метод, оторванный от своего объекта, теряет получателя —
  // сегодня тела этих методов `this` не читают, но проба не должна зависеть от
  // свойства чужого тела.
  let successSpy: jest.Spied<typeof toast.success>;
  let errorSpy: jest.Spied<typeof toast.error>;

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    successSpy = jest.spyOn(toast, "success").mockReturnValue("toast-id");
    errorSpy = jest.spyOn(toast, "error").mockReturnValue("toast-id");
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("renders an empty placeholder when labels are empty", () => {
    render(<LabelsCell labels={{}} />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders visible labels and collapses the remaining count", () => {
    render(
      <LabelsCell
        max={2}
        labels={{
          env: "prod",
          owner: "network",
          region: "eu",
        }}
      />,
    );

    // Ключ и значение — РАЗНЫЕ узлы (решение владельца: ключ обязан быть виден
    // отдельно, метки ищут именно по нему). Утверждаем обе половины, а не
    // склеенную строку: склеенной в разметке больше нет by construction.
    expect(screen.getByText("env")).toBeInTheDocument();
    expect(screen.getByText("prod")).toBeInTheDocument();
    expect(screen.getByText("owner")).toBeInTheDocument();
    expect(screen.getByText("network")).toBeInTheDocument();
    expect(screen.queryByText("region")).not.toBeInTheDocument();
    expect(screen.queryByText("eu")).not.toBeInTheDocument();
    expect(screen.getByText("+1")).toBeInTheDocument();
    // Машинная форма остаётся — она уходит в буфер и она же в подсказке.
    expect(screen.getByTitle("Скопировать env=prod")).toBeInTheDocument();
  });

  // РАЗНЫЕ УЗЛЫ — ещё не «ключ виден отдельно».
  //
  // Проба выше утверждает, что ключ и значение перестали быть одной строкой в
  // разметке. Само по себе это дефекта не закрывает: два соседних узла с
  // одинаковым видом на глаз остаются одной строкой, а именно от этого метку и
  // разделили — `team=networking` и `teamnet=working` на беглый взгляд одинаковы,
  // ищут же метки по КЛЮЧУ, и он обязан находиться сразу.
  //
  // Поэтому здесь закреплено то, чем половины отличаются ВИДОМ (канон §6): у
  // ключа своя заливка, больший вес и тон основного текста, у значения — ничего
  // из этого. И знак равенства ушёл из показа: разделяет половины линия, её не
  // приходится читать. В буфере машинная форма остаётся — это соседняя проба.
  it("ключ отличим от значения ВИДОМ, а не только узлом", () => {
    render(<LabelsCell labels={{ env: "prod" }} />);

    const key = screen.getByText("env");
    const value = screen.getByText("prod");

    expect(key.style.background).not.toBe("");
    expect(value.style.background).toBe("");
    expect(Number(key.style.fontWeight)).toBeGreaterThan(
      Number(value.style.fontWeight),
    );
    expect(key.style.color).not.toBe(value.style.color);
    // Линия вместо знака равенства — и то, и другое: разделитель есть...
    expect(key.style.borderRight).not.toBe("");
    // ...а `=` в показанном тексте метки нет.
    expect(key.parentElement?.textContent).toBe("envprod");
  });

  it("copies a label without bubbling the click", async () => {
    const onClick = jest.fn();

    document.addEventListener("click", onClick);
    try {
      render(<LabelsCell labels={{ env: "prod" }} />);

      fireEvent.click(screen.getByText("env"));

      await waitFor(() => expect(writeText).toHaveBeenCalledWith("env=prod"));
      await waitFor(() =>
        expect(successSpy).toHaveBeenCalledWith("Скопировано: env=prod"),
      );
      expect(onClick).not.toHaveBeenCalled();
    } finally {
      document.removeEventListener("click", onClick);
    }
  });

  it("метка достижима с КЛАВИАТУРЫ: она кнопка, а не текст с обработчиком", async () => {
    // Метка совершает действие — кладёт машинную форму в буфер. Пока она была
    // `span` с обработчиком клика, этого действия для клавиатуры не
    // существовало вовсе: элемент не получал фокус и не отвечал на Enter.
    //
    // Утверждается НАБЛЮДАЕМОЕ, а не имя тега: метка находится по своей роли
    // (её видит и программа чтения с экрана), получает фокус и срабатывает по
    // клавише. Проба, искавшая `button` по тегу, зеленела бы на кнопке,
    // недостижимой из-за `tabIndex={-1}`.
    render(<LabelsCell labels={{ env: "prod" }} />);

    const label = screen.getByRole("button", { name: /env/ });
    label.focus();
    expect(label).toHaveFocus();

    fireEvent.keyDown(label, { key: "Enter", code: "Enter" });
    fireEvent.click(label); // браузер порождает click по Enter на кнопке сам

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("env=prod"));
  });

  it("счётчик скрытых меток кнопкой НЕ является — он ничего не делает (близнец)", () => {
    // Обратная сторона утверждения выше: роль кнопки принадлежит тому, что
    // действует. Без этого близнеца «метка — кнопка» зеленело бы и на разметке,
    // где кнопками объявлено всё подряд, включая нажимаемое впустую.
    render(<LabelsCell labels={{ a: "1", b: "2", c: "3", d: "4", e: "5" }} />);

    const counter = screen.getByText(/^\+\d+$/);
    expect(counter.tagName.toLowerCase()).not.toBe("button");
  });

  it("shows an error toast when copying fails", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));

    render(<LabelsCell labels={{ env: "prod" }} />);

    fireEvent.click(screen.getByText("env"));

    await waitFor(() =>
      expect(errorSpy).toHaveBeenCalledWith("Не удалось скопировать"),
    );
  });
});

// Метки живут в ячейке СПИСКА, поэтому их ряд обязан оставаться в одну строку.
//
// Перенос здесь — не «красивее/некрасивее»: вторая строка появляется у одной
// строки таблицы, соседние остаются в одну, и список идёт лесенкой. Ужимаются
// при этом сами метки, а не счётчик скрытых: счётчик короток и он и есть ответ
// на вопрос «сколько ещё» — ужавшись, он превратился бы в «+…».
//
// Пара утверждений, а не одно: если бы ужимались ВСЕ теги одинаково, первое
// (ряд не переносится) было бы верным, а ячейка перестала бы отвечать. Второе —
// и есть тот законный близнец, который обязан остаться нетронутым.
describe("LabelsCell — одна строка", () => {
  const LABELS = {
    env: "production-eu-central",
    owner: "network-platform",
    region: "eu",
    tier: "edge",
    extra: "x",
  };

  it("ряд меток не переносится, а видимые метки ужимаются с многоточием", () => {
    render(<LabelsCell max={2} labels={LABELS} />);

    // Метка стала двухчастной: ключ и значение — разные узлы (решение
    // владельца). Ужимается ЗНАЧЕНИЕ, а не ключ: обрезанное значение остаётся
    // понятным, обрезанный ключ — нет, а по ключу метку и ищут.
    const value = screen.getByText("production-eu-central");
    const label = value.parentElement!;
    const row = label.parentElement!;
    expect(row.style.flexWrap).toBe("nowrap");

    expect(label.style.whiteSpace).toBe("");
    expect(label.style.flexShrink).toBe("1");
    expect(parseFloat(label.style.minWidth)).toBe(0);

    expect(value.style.whiteSpace).toBe("nowrap");
    expect(value.style.textOverflow).toBe("ellipsis");
    expect(value.style.overflow).toBe("hidden");
    // Без этого ужимание невозможно by construction, и «nowrap» дал бы ряд,
    // вылезающий за колонку, вместо ряда, помещающегося в неё.
    expect(parseFloat(value.style.minWidth)).toBe(0);

    // Ключ, наоборот, НЕ ужимается — положительный контроль к утверждению выше.
    const key = screen.getByText("env");
    expect(key.style.flexShrink).toBe("0");
  });

  it("счётчик скрытых НЕ ужимается — иначе он перестаёт отвечать (близнец)", () => {
    render(<LabelsCell max={2} labels={LABELS} />);

    const counter = screen.getByText("+3");
    expect(counter.style.flexShrink).toBe("0");
    expect(counter.style.textOverflow).not.toBe("ellipsis");
  });
});
