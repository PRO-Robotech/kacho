// Диалог — единственная модальная поверхность дерева: в нём живут подтверждение
// удаления и формы создания/правки. Существенно то, что содержимое НЕ
// отрисовано, пока диалог закрыт (иначе скрытая форма перехватывает подписи и
// поиск по разметке находит поля несуществующего окна), что заголовок связан с
// окном, и что закрытие доезжает до вызывающего — на этом держится «отмена».

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "./Dialog";

function Sample({ open, onOpenChange }: { open?: boolean; onOpenChange?: (v: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger>Открыть</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Удалить подсеть</DialogTitle>
          <DialogDescription>Действие необратимо</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <button type="button">Отмена</button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

describe("Dialog", () => {
  it("не рисует содержимое, пока закрыт", () => {
    render(<Sample open={false} />);
    expect(screen.queryByText("Удалить подсеть")).not.toBeInTheDocument();
    expect(screen.queryByText("Действие необратимо")).not.toBeInTheDocument();
    expect(screen.getByText("Открыть")).toBeInTheDocument();
  });

  it("открытый показывает заголовок, пояснение и подвал", () => {
    render(<Sample open />);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Удалить подсеть")).toBeInTheDocument();
    expect(screen.getByText("Действие необратимо")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Отмена" })).toBeInTheDocument();
  });

  it("называет окно своим заголовком", () => {
    render(<Sample open />);
    // Без связи заголовка с окном средство чтения с экрана объявит модалку
    // безымянной — а это единственное окно, которое перехватывает весь ввод.
    expect(screen.getByRole("dialog")).toHaveAccessibleName("Удалить подсеть");
  });

  it("открывается по своему триггеру", () => {
    render(<Sample />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("Открыть"));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("сообщает вызывающему о закрытии крестиком", () => {
    const onOpenChange = jest.fn<(v: boolean) => void>();
    render(<Sample open onOpenChange={onOpenChange} />);

    fireEvent.click(screen.getByRole("button", { name: "Закрыть окно" }));

    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("сообщает вызывающему о закрытии по Escape", () => {
    const onOpenChange = jest.fn<(v: boolean) => void>();
    render(<Sample open onOpenChange={onOpenChange} />);

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });

    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
