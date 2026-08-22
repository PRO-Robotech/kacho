// Раздел «Администрирование» подчиняется общей логике построения страниц.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ ПРОБЫ ИЗМЕНИЛСЯ ВМЕСТЕ С ПРЕДМЕТОМ ПРОДУКТА
//
// Прежде проба утверждала, что каркас РИСУЕТ пункты раздела — сперва своим
// горизонтальным рядом, потом общим рейлом вкладок. Решением владельца раздел
// сведён к общей логике: перечень его частей приезжает к каркасу от самого
// модуля (`system/navigation`) и показывается КОЛОНКОЙ СЛЕВА, как у всех прочих
// разделов, — а этот каркас перестал рисовать и пункты, и собственный заголовок.
//
// Иначе в разделе жили три конструкции навигации разом: рейл, колонка и полоса
// вкладок, перечислявшая ровно то же, что колонка. И два заголовка сразу — имя
// раздела над именем страницы.
//
// Поэтому проба теперь утверждает ДВЕ вещи, и обе наблюдаемые:
//   1. каркас показывает содержимое открытого адреса — то, ради чего он есть;
//   2. каркас НЕ заводит своей навигации и своего заголовка — иначе они встанут
//      поверх колонки, и раздел снова разойдётся с прочими.
//
// Утверждение (2) — отрицание, поэтому рядом стоит (1): без него «навигации
// нет» зеленело бы и на каркасе, не рисующем вообще ничего.
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

const ITEMS = ["Регионы", "Зоны", "Пулы адресов", "Администраторы кластера"];

describe("раздел «Администрирование»", () => {
  it("показывает содержимое открытого адреса", () => {
    show("/system/address-pools");

    expect(screen.getByText("содержимое пулов")).toBeInTheDocument();
  });

  it("другой адрес — другое содержимое (контроль к утверждению выше)", () => {
    show("/system/zones");

    expect(screen.getByText("содержимое зон")).toBeInTheDocument();
    expect(screen.queryByText("содержимое пулов")).not.toBeInTheDocument();
  });

  it("не заводит СВОЕЙ навигации: перечень частей показывает колонка раздела", () => {
    show("/system/regions");

    // Ни вкладок, ни пунктов меню, ни ссылок на части раздела — иначе они
    // встанут поверх колонки, и в разделе снова окажется две навигации.
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    for (const item of ITEMS) {
      expect(screen.queryByText(item)).not.toBeInTheDocument();
    }
  });

  it("не рисует собственного заголовка раздела", () => {
    show("/system/regions");

    // Заголовок называет предмет ОТКРЫТОЙ страницы. Имя раздела над ним было бы
    // вторым заголовком на одном экране — у прочих разделов такого нет.
    expect(screen.queryByText("Администрирование")).not.toBeInTheDocument();
  });

  it("не пересказывает модель прав поясняющим абзацем", () => {
    show("/system/regions");

    expect(screen.queryByText(/аутентифицированный/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/администратор кластера может/i)).not.toBeInTheDocument();
  });
});
