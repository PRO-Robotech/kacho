// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { fireEvent, render, screen, within } from "@testing-library/react";

// Опасный диалог экрана «Безопасность» держит фокус внутри (приёмка F8-S7, Р9).
//
// Ловушка окна antd возвращает фокус по `focusin`; Tab с последнего действия
// окна, стоящего в конце документа, уводит фокус из страницы, и `focusin` не
// наступает — фокус оказывается вне диалога (сквозная проба F8-S7-70 на стенде).
// Обход Tab и Shift+Tab замыкается самим диалогом.

const { DangerDialog } = await import("./DangerDialog");

function renderDialog() {
  render(
    <DangerDialog
      open
      title="Удалить ключ доступа"
      consequences={["Войти этим ключом станет нельзя."]}
      confirmText="Удалить"
      onConfirm={jest.fn()}
      onCancel={jest.fn()}
    />,
  );
  const dialog = screen.getByRole("dialog");
  return {
    dialog,
    cancel: within(dialog).getByRole("button", { name: "Отмена" }),
    confirm: within(dialog).getByRole("button", { name: "Удалить" }),
  };
}

describe("DangerDialog · фокус замкнут внутри (Р9)", () => {
  it("Tab с последнего действия возвращает фокус на первое", () => {
    const { cancel, confirm } = renderDialog();
    confirm.focus();
    const ev = fireEvent.keyDown(confirm, { key: "Tab" });
    expect(ev).toBe(false); // переход браузера отменён — фокус не уходит из страницы
    expect(cancel).toHaveFocus();
  });

  it("Shift+Tab с первого действия переводит фокус на последнее", () => {
    const { cancel, confirm } = renderDialog();
    cancel.focus();
    const ev = fireEvent.keyDown(cancel, { key: "Tab", shiftKey: true });
    expect(ev).toBe(false);
    expect(confirm).toHaveFocus();
  });

  it("близнец: Tab внутри обхода — переход браузера не трогается", () => {
    const { cancel } = renderDialog();
    cancel.focus();
    const ev = fireEvent.keyDown(cancel, { key: "Tab" });
    expect(ev).toBe(true);
    expect(cancel).toHaveFocus();
  });
});
