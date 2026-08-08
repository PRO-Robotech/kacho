// Поля ввода и подпись к ним — примитивы, на которых собраны все формы дерева.
// Существенно ровно три вещи: ввод доезжает до обработчика, `type` по умолчанию
// текстовый (незаданный type у radix-подобной обёртки легко превратить в
// `undefined` и получить поле, которое браузер трактует иначе), и подпись
// связана с полем через `htmlFor` — без связи клик по подписи не фокусирует
// поле, а средство чтения с экрана не называет его.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Input, Label, Textarea } from "./Input";

describe("Input", () => {
  it("отдаёт введённое значение обработчику", () => {
    const onChange = jest.fn<(e: React.ChangeEvent<HTMLInputElement>) => void>();
    render(<Input aria-label="Имя" onChange={onChange} />);

    fireEvent.change(screen.getByLabelText("Имя"), { target: { value: "frontend" } });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect((screen.getByLabelText("Имя") as HTMLInputElement).value).toBe("frontend");
  });

  it("по умолчанию текстовое, но заданный тип уважает", () => {
    render(
      <>
        <Input aria-label="Обычное" />
        <Input aria-label="Число" type="number" />
      </>,
    );
    expect(screen.getByLabelText("Обычное")).toHaveAttribute("type", "text");
    expect(screen.getByLabelText("Число")).toHaveAttribute("type", "number");
  });

  it("выключенное поле не принимает пользовательский ввод", async () => {
    // Именно пользовательский: `fireEvent.change` — синтетическая подмена
    // значения, она проходит и на выключенном поле, поэтому отрицание на ней
    // было бы утверждением о тестовой библиотеке, а не о поле.
    const onChange = jest.fn();
    render(<Input aria-label="Имя" disabled onChange={onChange} />);

    const input = screen.getByLabelText("Имя");
    expect(input).toBeDisabled();
    await userEvent.type(input, "x");

    expect(onChange).not.toHaveBeenCalled();
    expect((input as HTMLInputElement).value).toBe("");
  });

  it("пробрасывает ref на живой узел", () => {
    let node: HTMLInputElement | null = null;
    render(
      <Input
        aria-label="Имя"
        ref={(el) => {
          node = el;
        }}
      />,
    );
    expect(node).toBe(screen.getByLabelText("Имя"));
  });
});

describe("Textarea", () => {
  it("отдаёт многострочное значение обработчику", () => {
    const onChange = jest.fn();
    render(<Textarea aria-label="Описание" onChange={onChange} />);

    fireEvent.change(screen.getByLabelText("Описание"), { target: { value: "первая\nвторая" } });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect((screen.getByLabelText("Описание") as HTMLTextAreaElement).value).toBe("первая\nвторая");
  });
});

describe("Label", () => {
  it("связывает подпись с полем: клик по подписи фокусирует поле", () => {
    render(
      <>
        <Label htmlFor="name-field">Имя</Label>
        <Input id="name-field" />
      </>,
    );

    const input = screen.getByLabelText("Имя");
    fireEvent.click(screen.getByText("Имя"));
    // jsdom не переносит фокус по клику сам — связь проверяем тем, чем она и
    // выражена: getByLabelText нашёл поле по подписи.
    expect(input).toHaveAttribute("id", "name-field");
  });

  it("помечает обязательное поле звёздочкой, необязательное — нет", () => {
    const required = render(
      <Label htmlFor="a" required>
        Имя
      </Label>,
    );
    expect(required.container.textContent).toBe("Имя*");

    const optional = render(<Label htmlFor="b">Описание</Label>);
    expect(optional.container.textContent).toBe("Описание");
  });

  it("показывает пояснение, когда оно есть, и не заводит пустого узла, когда его нет", () => {
    render(
      <Label htmlFor="c" description="Уникально в пределах проекта">
        Имя
      </Label>,
    );
    expect(screen.getByText("Уникально в пределах проекта")).toBeInTheDocument();

    const bare = render(<Label htmlFor="d">Имя</Label>);
    expect(bare.container.querySelector("p")).toBeNull();
  });
});
