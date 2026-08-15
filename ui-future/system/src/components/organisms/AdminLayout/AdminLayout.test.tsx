// Раздел «Администрирование» подчиняется общей логике построения страниц.
//
// Предмет (#447). Владелец: «Регионы, Зоны, Пулы адресов, Администраторы
// кластера перевести на вкладки — как на карточке сети», и «убрать поясняющий
// текст».
//
// Вкладки в разделе были, но СВОИ: горизонтальный ряд поверх содержимого, тогда
// как карточка ресурса рисует вертикальный рейл общей оболочкой. Два вида одного
// предмета — пользователь читает разницу как «другое место продукта» (правило 8
// `ui.md`).
//
// Поясняющий абзац снят по решению владельца: он пересказывал модель прав
// («каталог читает любой аутентифицированный, изменять может администратор»),
// то есть отвечал на вопрос, которого на этой странице не задают.
import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

// Права решают, какие пункты видны; сам снимок здесь не предмет.
jest.unstable_mockModule("@shared/lib/permissions", () => ({
  usePermissions: () => ({ isSystemAdmin: true }),
}));

const { AdminLayout } = await import("./AdminLayout");

/** Раздел, открытый по адресу одного из своих пунктов. */
function show(at: string) {
  return render(
    <MemoryRouter initialEntries={[at]}>
      <Routes>
        <Route element={<AdminLayout />}>
          <Route path="/system/regions" element={<div>содержимое регионов</div>} />
          <Route path="/system/zones" element={<div>содержимое зон</div>} />
          <Route path="/system/address-pools" element={<div>содержимое пулов</div>} />
          <Route path="/system/cluster/admins" element={<div>содержимое администраторов</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

const ПУНКТЫ = ["Регионы", "Зоны", "Пулы адресов", "Администраторы кластера"];

describe("раздел «Администрирование»", () => {
  it("рисует пункты тем же рейлом, что карточка ресурса", () => {
    // Наблюдаемое различие, а не разбор исходника: общий рейл РИСУЕТ свои
    // пункты (заменитель antd повторяет это поведение настоящего меню), а
    // прежний горизонтальный ряд отдавал их атрибутом и на экран не выводил.
    show("/system/regions");

    for (const п of ПУНКТЫ) {
      expect(screen.getByRole("button", { name: п })).toBeInTheDocument();
    }
  });

  it("отмечает открытый пункт", () => {
    show("/system/zones");

    expect(screen.getByRole("button", { name: "Зоны" })).toHaveAttribute("aria-current", "page");
    // Отрицание в паре: отмечен ровно открытый, а не все подряд.
    expect(screen.getByRole("button", { name: "Регионы" })).not.toHaveAttribute("aria-current", "page");
  });

  it("показывает содержимое открытого пункта", () => {
    show("/system/address-pools");

    expect(screen.getByText("содержимое пулов")).toBeInTheDocument();
  });

  it("не пересказывает модель прав поясняющим абзацем", () => {
    show("/system/regions");

    expect(screen.queryByText(/Глобальные ресурсы инфраструктуры/)).toBeNull();
    // Положительный контроль: страница отрисована, и утверждение об отсутствии
    // абзаца сделано не на пустом экране.
    expect(screen.getByText("Администрирование")).toBeInTheDocument();
  });
});
