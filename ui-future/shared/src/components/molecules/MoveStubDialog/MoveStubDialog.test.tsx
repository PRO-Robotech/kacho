// Окно, которым консоль честно говорит: перемещения между проектами тут нет,
// вот запрос к API. Ценность окна — ТОЧНОСТЬ инструкции: путь ресурса, глагол
// `:move` и имя поля тела. Неверное имя поля край примет молча (тело разбирается
// с отбрасыванием неизвестных ключей) и вернёт успех, не переместив ничего, —
// поэтому подсказка обязана называть поле так, как его называет контракт.

import { render, screen } from "@testing-library/react";
import { MoveStubDialog } from "./MoveStubDialog";

function renderDialog() {
  return render(
    <MoveStubDialog
      open
      onOpenChange={() => {}}
      resourceLabel="Подсеть"
      name="frontend-subnet"
      apiPath="/vpc/v1/subnets/sub01h9zt6k3mqx4v"
    />,
  );
}

describe("MoveStubDialog", () => {
  it("называет предмет перемещения", () => {
    renderDialog();
    expect(screen.getByText("Подсеть:")).toBeInTheDocument();
    expect(screen.getByText("frontend-subnet")).toBeInTheDocument();
  });

  it("даёт запрос с путём ресурса и глаголом :move", () => {
    renderDialog();
    const snippet = screen.getByText(/POST/);
    expect(snippet.textContent).toContain("POST /vpc/v1/subnets/sub01h9zt6k3mqx4v:move");
  });

  it("называет поле тела так, как его называет контракт", () => {
    // `destination_project_id`. Ошибка в имени не даёт отказа: край
    // отбрасывает незнакомый ключ и отвечает успехом, ничего не переместив.
    renderDialog();
    expect(screen.getByText(/POST/).textContent).toContain('"destination_project_id"');
  });

  it("подставляет путь вызывающего, а не зашитый", () => {
    render(
      <MoveStubDialog
        open
        onOpenChange={() => {}}
        resourceLabel="Сеть"
        name="core"
        apiPath="/vpc/v1/networks/net01h9zt6k3mqx4v"
      />,
    );
    const snippet = screen.getByText(/POST/);
    expect(snippet.textContent).toContain("/vpc/v1/networks/net01h9zt6k3mqx4v:move");
    expect(snippet.textContent).not.toContain("/vpc/v1/subnets");
  });
});
