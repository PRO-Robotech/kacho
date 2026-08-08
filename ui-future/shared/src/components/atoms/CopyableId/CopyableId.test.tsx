// Идентификатор — единственная внешне-адресуемая координата ресурса (core #15),
// и консоль показывает его ЦЕЛИКОМ именно затем, чтобы его можно было скопировать
// и вставить. Поэтому проверяется не «компонент отрисовался», а то, что в буфер
// уходит полный id, что клик не всплывает (кнопка живёт внутри кликабельной
// строки таблицы — всплытие открыло бы карточку вместо копирования) и что отказ
// буфера виден пользователю, а не проглатывается.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { CopyableId } from "./CopyableId";

describe("CopyableId", () => {
  const writeText = jest.fn<(text: string) => Promise<void>>();

  // Шпионы ставятся на СПЯЩИЕ функции объекта `toast`, а не на оторванные от него
  // ссылки: метод без получателя — свойство чужого тела, на которое проба
  // опираться не должна.
  let successSpy: jest.Spied<typeof toast.success>;
  let errorSpy: jest.Spied<typeof toast.error>;

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    successSpy = jest.spyOn(toast, "success").mockReturnValue("toast-id");
    errorSpy = jest.spyOn(toast, "error").mockReturnValue("toast-id");
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("показывает идентификатор целиком, без усечения", () => {
    render(<CopyableId id="net01h9zt6k3mqx4vabc" />);
    expect(screen.getByText("net01h9zt6k3mqx4vabc")).toBeInTheDocument();
  });

  it("на пустом идентификаторе рисует прочерк и не даёт кнопки", () => {
    render(<CopyableId id="" />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("кладёт в буфер полный идентификатор и не даёт клику всплыть", async () => {
    const onOuterClick = jest.fn();
    document.addEventListener("click", onOuterClick);
    try {
      render(<CopyableId id="net01h9zt6k3mqx4vabc" />);
      const button = screen.getByRole("button");
      expect(button).toHaveAttribute("title", "Скопировать ID");

      fireEvent.click(button);

      await waitFor(() => expect(writeText).toHaveBeenCalledWith("net01h9zt6k3mqx4vabc"));
      await waitFor(() => expect(successSpy).toHaveBeenCalledWith("ID скопирован"));
      // Кнопка живёт внутри кликабельной строки: всплывший клик открыл бы
      // карточку ресурса вместо копирования.
      expect(onOuterClick).not.toHaveBeenCalled();
      await waitFor(() => expect(button).toHaveAttribute("title", "Скопировано"));
    } finally {
      document.removeEventListener("click", onOuterClick);
    }
  });

  it("показывает отказ буфера, а не молчит", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));
    render(<CopyableId id="net01h9zt6k3mqx4vabc" />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() => expect(errorSpy).toHaveBeenCalledWith("Не удалось скопировать"));
    expect(successSpy).not.toHaveBeenCalled();
    // Отметки «скопировано» после отказа быть не должно — иначе пользователь
    // вставит пустоту, будучи уверен в обратном.
    expect(screen.getByRole("button")).toHaveAttribute("title", "Скопировать ID");
  });

  it("без иконки остаётся кликабельным", () => {
    render(<CopyableId id="net01h9zt6k3mqx4vabc" showIcon={false} />);
    const button = screen.getByRole("button");
    expect(button.querySelector("svg")).toBeNull();
    fireEvent.click(button);
    expect(writeText).toHaveBeenCalledWith("net01h9zt6k3mqx4vabc");
  });
});
