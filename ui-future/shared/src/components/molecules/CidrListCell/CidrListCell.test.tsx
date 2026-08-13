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
