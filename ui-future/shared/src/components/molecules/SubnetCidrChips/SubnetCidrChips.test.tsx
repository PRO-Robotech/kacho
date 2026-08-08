// Управляемые чипы диапазонов подсети — вариант виджета для формы СОЗДАНИЯ, где
// подсети ещё нет и глаголов правки звать некому. Ценность здесь в проверках
// перед добавлением: диапазон без префикса и «шестёрка» без двоеточия — это
// отказ сервера в конце длинной формы, а дубликат — тихая порча набора.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { CidrSection, SubnetCidrChips } from "./SubnetCidrChips";

let errorSpy: jest.Spied<typeof toast.error>;

beforeEach(() => {
  errorSpy = jest.spyOn(toast, "error").mockReturnValue("t");
});

afterEach(() => {
  jest.restoreAllMocks();
});

function renderSection(kind: "v4" | "v6", blocks: string[]) {
  const onChange = jest.fn<(next: string[]) => void>();
  render(<CidrSection kind={kind} blocks={blocks} onChange={onChange} />);
  return { onChange };
}

function type(value: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value } });
}

function add() {
  fireEvent.click(screen.getByRole("button", { name: /Add/ }));
}

describe("CidrSection", () => {
  it("показывает имеющиеся диапазоны", () => {
    renderSection("v4", ["10.0.1.0/24", "10.0.2.0/24"]);
    expect(screen.getByText("10.0.1.0/24")).toBeInTheDocument();
    expect(screen.getByText("10.0.2.0/24")).toBeInTheDocument();
  });

  it("пустой набор прямо называет пустым", () => {
    renderSection("v4", []);
    expect(screen.getByText("— пусто —")).toBeInTheDocument();
  });

  it("добавляет валидный диапазон в конец", () => {
    const { onChange } = renderSection("v4", ["10.0.1.0/24"]);

    type("10.0.2.0/24");
    add();

    expect(onChange).toHaveBeenCalledWith(["10.0.1.0/24", "10.0.2.0/24"]);
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it("диапазон без префикса отвергает с объяснением", () => {
    const { onChange } = renderSection("v4", []);

    type("10.0.1.0");
    add();

    expect(errorSpy).toHaveBeenCalledWith("CIDR должен содержать префикс (например /24).");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("для IPv6 требует именно шестёрку", () => {
    const { onChange } = renderSection("v6", []);

    type("10.0.1.0/24");
    add();

    expect(errorSpy).toHaveBeenCalledWith("Похоже не на IPv6-адрес.");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("дубликат не добавляет молча", () => {
    const { onChange } = renderSection("v4", ["10.0.1.0/24"]);

    type("10.0.1.0/24");
    add();

    expect(errorSpy).toHaveBeenCalledWith("Этот CIDR уже добавлен.");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("Enter добавляет так же, как кнопка", () => {
    const { onChange } = renderSection("v4", []);

    type("10.0.1.0/24");
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith(["10.0.1.0/24"]);
  });

  it("на пустом поле кнопка не активна", () => {
    renderSection("v4", []);
    expect(screen.getByRole("button", { name: /Add/ })).toBeDisabled();
  });
});

describe("SubnetCidrChips", () => {
  it("держит два независимых набора: правка одного не задевает другой", () => {
    const onV4 = jest.fn<(next: string[]) => void>();
    const onV6 = jest.fn<(next: string[]) => void>();
    render(
      <SubnetCidrChips v4Blocks={["10.0.1.0/24"]} onV4Change={onV4} v6Blocks={["fd00::/64"]} onV6Change={onV6} />,
    );

    const inputs = screen.getAllByRole("textbox");
    fireEvent.change(inputs[0], { target: { value: "10.0.2.0/24" } });
    fireEvent.click(screen.getAllByRole("button", { name: /Add/ })[0]);

    expect(onV4).toHaveBeenCalledWith(["10.0.1.0/24", "10.0.2.0/24"]);
    expect(onV6).not.toHaveBeenCalled();
  });
});
