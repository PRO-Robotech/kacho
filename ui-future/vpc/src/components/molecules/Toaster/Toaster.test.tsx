import { act, fireEvent, render, screen } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { Toaster } from ".";

// Хранилище уведомлений — модульное и переживает случай, поэтому каждый случай
// снимает ровно то, что сам поставил: иначе «контейнер пуст» зеленело бы или
// краснело в зависимости от порядка соседей.
describe("Toaster", () => {
  const ids: string[] = [];

  const push = (fn: () => string) => {
    let id = "";
    act(() => {
      id = fn();
    });
    ids.push(id);
    return id;
  };

  afterEach(() => {
    act(() => {
      for (const id of ids.splice(0)) toast.dismiss(id);
    });
  });

  it("без уведомлений не рисует контейнер вовсе", () => {
    const { container } = render(<Toaster />);

    expect(container).toBeEmptyDOMElement();
  });

  it("показывает текст уведомления, как только оно появилось", () => {
    render(<Toaster />);

    push(() => toast.success("Имя скопировано"));

    expect(screen.getByText("Имя скопировано")).toBeInTheDocument();
  });

  it("ошибку объявляет ролью alert, обычное уведомление — status", () => {
    render(<Toaster />);

    push(() => toast.error("Не удалось скопировать"));
    push(() => toast.info("Обновляем список"));

    expect(screen.getByRole("alert")).toHaveTextContent("Не удалось скопировать");
    expect(screen.getByRole("status")).toHaveTextContent("Обновляем список");
  });

  it("держит несколько уведомлений одновременно", () => {
    render(<Toaster />);

    push(() => toast.info("Первое"));
    push(() => toast.info("Второе"));

    expect(screen.getByText("Первое")).toBeInTheDocument();
    expect(screen.getByText("Второе")).toBeInTheDocument();
  });

  it("кнопка закрытия снимает своё уведомление и не трогает соседнее", () => {
    render(<Toaster />);

    push(() => toast.info("Первое"));
    push(() => toast.info("Второе"));

    const [firstClose] = screen.getAllByRole("button", { name: "Закрыть" });
    act(() => {
      fireEvent.click(firstClose);
    });

    expect(screen.queryByText("Первое")).not.toBeInTheDocument();
    expect(screen.getByText("Второе")).toBeInTheDocument();
  });

  it("когда снято последнее — контейнер исчезает, а не остаётся пустым", () => {
    const { container } = render(<Toaster />);

    push(() => toast.info("Единственное"));
    expect(container).not.toBeEmptyDOMElement();

    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "Закрыть" }));
    });

    expect(container).toBeEmptyDOMElement();
  });
});
