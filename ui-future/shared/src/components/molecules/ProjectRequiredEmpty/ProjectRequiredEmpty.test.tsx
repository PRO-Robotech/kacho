// Заглушка, которую видит пользователь на списке проектного ресурса, пока
// проект не выбран. Её ценность — назвать ресурс и путь выхода: пустой экран без
// названия ресурса неотличим от «список пуст», и человек начинает искать
// отсутствующие данные вместо того, чтобы выбрать проект.

import { render, screen } from "@testing-library/react";
import { ProjectRequiredEmpty } from "./ProjectRequiredEmpty";

describe("ProjectRequiredEmpty", () => {
  it("говорит, что делать", () => {
    render(<ProjectRequiredEmpty resource="Подсети" />);
    expect(screen.getByText("Выберите проект")).toBeInTheDocument();
  });

  it("называет ресурс, из-за которого экран пуст", () => {
    render(<ProjectRequiredEmpty resource="Подсети" />);
    expect(screen.getByText(/Подсети/)).toBeInTheDocument();
  });

  it("даёт страницу проектов как путь выхода", () => {
    render(<ProjectRequiredEmpty resource="Подсети" />);
    // Адрес — часть подсказки: без него «зайдите на страницу» ни на что не
    // указывает.
    expect(screen.getByText("/iam/projects")).toBeInTheDocument();
  });

  it("подставляет именно переданный ресурс, а не зашитый текст", () => {
    render(<ProjectRequiredEmpty resource="Балансировщики" />);
    expect(screen.getByText(/Балансировщики/)).toBeInTheDocument();
    expect(screen.queryByText(/Подсети/)).not.toBeInTheDocument();
  });
});
