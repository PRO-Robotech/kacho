import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { ModulePlaceholderPage } from ".";

// Раздел, до которого дошли, а показать нечего. Проверяется то, что читает
// ПОЛЬЗОВАТЕЛЬ: как назван раздел, чего ему не обещают и куда он может уйти.
// Разметка не утверждается: класс переживёт свой предмет, а надпись — нет.

const renderAt = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/dashboard" element={<div data-testid="services-list">список сервисов</div>} />
        <Route path="/projects/:projectId/:moduleKey/*" element={<ModulePlaceholderPage />} />
      </Routes>
    </MemoryRouter>,
  );

describe("страница раздела, которого нет", () => {
  it("называет раздел так, как он подписан в меню, а не ключом адреса", () => {
    renderAt("/projects/prj-1/vpc/networks");

    const panel = screen.getByTestId("module-unavailable");
    expect(panel).toHaveTextContent("Раздел «Virtual Private Cloud» временно недоступен");
    // Отрицание в паре с утверждением выше: без него «названо по-человечески»
    // осталось бы верным и для страницы, которая печатает обе подписи сразу.
    expect(panel.textContent).not.toContain("vpc");
  });

  it("ведёт себя как экран недоступности, а не как заготовка для разработчика", () => {
    renderAt("/projects/prj-1/vpc/networks");

    const panel = screen.getByTestId("module-unavailable");
    expect(panel).toHaveTextContent("Ведутся технические работы");
    expect(panel.textContent).not.toContain("Маршрут");
    expect(panel.textContent).not.toContain("модуль");
  });

  it("незнакомый ключ адреса именем раздела НЕ становится", () => {
    renderAt("/projects/prj-1/чего-то-нет/дальше");

    const panel = screen.getByTestId("module-unavailable");
    expect(panel).toHaveTextContent("Раздел временно недоступен");
    expect(panel.textContent).not.toContain("чего-то-нет");
  });

  it("не предлагает повтор: повторять здесь нечего", () => {
    renderAt("/projects/prj-1/vpc/networks");

    expect(screen.queryByRole("button", { name: "Повторить" })).not.toBeInTheDocument();
  });

  it("уводит к списку сервисов переходом ВНУТРИ приложения", async () => {
    const user = userEvent.setup();
    renderAt("/projects/prj-1/vpc/networks");

    await user.click(screen.getByRole("button", { name: "Все сервисы" }));

    // Список отрисован тем же маршрутизатором — значит перехода браузера не
    // было. Полная перезагрузка увела бы страницу и оставила бы этот узел на
    // месте: утверждение о ней в jsdom неотличимо от бездействия.
    expect(screen.getByTestId("services-list")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
  });
});
