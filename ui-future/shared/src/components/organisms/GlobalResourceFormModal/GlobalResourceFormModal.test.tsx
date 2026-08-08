// Глобальная точка монтирования форм-модалок. Её единственное решение —
// определить по адресу, В КАКОМ КОНТЕЙНЕРЕ создаётся ресурс: проект, iam или
// системная (кластерная) область. Ошибка здесь не видна на экране, но создаёт
// ресурс не в том проекте — а привязка к проекту неизменяема, и «переложить»
// созданное нельзя.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

jest.unstable_mockModule("@shared/components/organisms/ResourceFormModal", () => ({
  __esModule: true,
  ResourceFormModal: ({ projectId }: { projectId: string }) =>
    React.createElement("div", { "data-testid": "modal", "data-container": projectId }),
}));

const { GlobalResourceFormModal } = await import("./GlobalResourceFormModal");

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <GlobalResourceFormModal />
    </MemoryRouter>,
  );
}

function containerAt(path: string): string | null {
  renderAt(path);
  return screen.queryByTestId("modal")?.getAttribute("data-container") ?? null;
}

describe("GlobalResourceFormModal", () => {
  it("на странице проекта контейнер — идентификатор ЭТОГО проекта", () => {
    expect(containerAt("/projects/prj-1/vpc/subnets")).toBe("prj-1");
  });

  it("вложенный путь проекта контейнер не теряет", () => {
    expect(containerAt("/projects/prj-2/vpc/networks/net-1/subnets/create")).toBe("prj-2");
  });

  it("раздел управления доступом — свой контейнер, не проект", () => {
    expect(containerAt("/iam/users")).toBe("iam");
  });

  it("системный раздел — свой контейнер", () => {
    expect(containerAt("/system/address-pools")).toBe("system");
  });

  it("вне известных разделов модалки нет вовсе", () => {
    // Смонтировать её с выдуманным контейнером значило бы предложить создать
    // ресурс неизвестно где.
    const { container } = renderAt("/dashboard");
    expect(container).toBeEmptyDOMElement();
  });

  it("корень раздела управления доступом без слэша тоже опознаётся", () => {
    expect(containerAt("/iam")).toBe("iam");
  });
});
