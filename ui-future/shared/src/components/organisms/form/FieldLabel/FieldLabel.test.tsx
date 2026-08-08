// Подпись поля формы. Её единственное решение: пояснение живёт в подсказке
// рядом с подписью, а НЕ в скобках самой подписи. Когда пояснения нет, лишней
// обёртки быть не должно — подпись обязана остаться голым текстом, иначе она
// перестаёт совпадать по разметке с подписями, у которых пояснения нет, и
// колонка полей «пляшет».

import { render, screen } from "@testing-library/react";
import { FieldLabel } from "./FieldLabel";

describe("FieldLabel", () => {
  it("без пояснения отдаёт голый текст", () => {
    const { container } = render(<FieldLabel text="Имя" />);
    expect(container).toHaveTextContent("Имя");
    expect(screen.queryByLabelText("field-info")).not.toBeInTheDocument();
    expect(container.querySelector("div")).toBeNull();
  });

  it("с пояснением ставит рядом значок подсказки", () => {
    render(<FieldLabel text="CIDR" info="RFC 4632, префикс обязателен" />);
    expect(screen.getByText("CIDR")).toBeInTheDocument();
    expect(screen.getByLabelText("field-info")).toBeInTheDocument();
  });

  it("принимает узел, а не только строку", () => {
    render(<FieldLabel text={<em>Зона</em>} />);
    expect(screen.getByText("Зона").tagName).toBe("EM");
  });

  it("пустое пояснение значком не считает", () => {
    // Пустая строка — «пояснения нет»; значок без содержимого открывал бы
    // пустую подсказку.
    render(<FieldLabel text="Имя" info="" />);
    expect(screen.queryByLabelText("field-info")).not.toBeInTheDocument();
  });
});
