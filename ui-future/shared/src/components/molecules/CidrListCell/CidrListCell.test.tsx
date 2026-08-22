// Ячейка набора CIDR в таблице. Предмет пробы — что пользователь ВИДИТ каждый
// префикс: прежние ячейки сворачивали хвост в «+N», и у подсети дополнительные
// диапазоны не показывались вовсе — число «+2» не называет ни одного адреса.

import { render, screen } from "@testing-library/react";
import { CidrListCell, cidrItems } from "./CidrListCell";

describe("CidrListCell", () => {
  it("показывает КАЖДЫЙ префикс, а не первые и «ещё сколько-то»", () => {
    render(<CidrListCell items={["10.0.1.0/24", ["10.0.2.0/24", "10.0.3.0/24", "10.0.4.0/24"]]} />);

    for (const c of ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24", "10.0.4.0/24"]) {
      expect(screen.getByText(c)).toBeInTheDocument();
    }
    expect(screen.queryByText(/^\+\d+$/)).not.toBeInTheDocument();
  });

  it("порядок источников сохраняется — основной блок остаётся первым", () => {
    render(<CidrListCell items={["10.0.1.0/24", ["10.0.9.0/24"]]} />);

    const shown = screen.getAllByText(/10\.0\./).map((el) => el.textContent);
    expect(shown).toEqual(["10.0.1.0/24", "10.0.9.0/24"]);
  });

  it("пустой набор — прочерк, а не пустая ячейка", () => {
    render(<CidrListCell items={["", [], undefined]} />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("собирает из строк и массивов, отбрасывая пустые и не-строки", () => {
    expect(cidrItems("a", ["b", "", 7, null], undefined, "")).toEqual(["a", "b"]);
  });
});

// Каждый префикс стоит СВОЕЙ строкой — и одной. Перенос ВНУТРИ префикса рвал бы
// `fd00:1234:5678:9abc::/64` посреди адреса и делал высоту строки списка
// функцией ширины колонки.
//
// Обрезка у CIDR стоит дороже, чем у имени: значение несёт длина префикса, и
// она стоит В КОНЦЕ — то есть теряется первой. Поэтому вместе с обрезкой обязана
// приезжать подсказка с полным значением; проба утверждает обе половины, иначе
// «одна строка» была бы куплена молчаливой потерей `/64`.
describe("CidrListCell — одна строка на префикс", () => {
  const V6 = "fd00:1234:5678:9abc::/64";

  it("префикс не переносится и обрезается многоточием", () => {
    render(<CidrListCell items={[V6]} />);
    const line = screen.getByText(V6);

    expect(line.style.whiteSpace).toBe("nowrap");
    expect(line.style.textOverflow).toBe("ellipsis");
    expect(line.style.overflow).toBe("hidden");
  });

  it("длина префикса не теряется молча: полное значение в подсказке", () => {
    render(<CidrListCell items={[V6]} />);

    expect(screen.getByText(V6)).toHaveAttribute("title", V6);
  });
});
