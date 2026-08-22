// «Токены и ключи» — часть раздела администрирования, а не отдельное место
// продукта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ ПРОБЫ ИЗМЕНИЛСЯ ВМЕСТЕ С ПРЕДМЕТОМ ПРОДУКТА
//
// Прежде проба утверждала, что каркас РИСУЕТ пункты части — сперва своим
// горизонтальным рядом antd, затем общим рейлом вкладок. Решением владельца
// раздел сведён к общей логике построения страниц: перечень частей приезжает
// от самого модуля (`system/navigation.ts`) и показывается КОЛОНКОЙ СЛЕВА, как
// у любого другого раздела консоли, — а каркас перестал рисовать и пункты, и
// собственный заголовок.
//
// Соседняя часть того же раздела (`AdminLayout`) устроена так же, и это не
// совпадение, а условие: пока две части ОДНОГО раздела строили пункты
// по-разному — столбцом слева и рядом сверху, — пользователь читал разницу как
// «другое место продукта». Это предмет #447; проба браузером
// (`ui-future/e2e/specs/findings.spec.ts`) меряет ПОЛОЖЕНИЕ пунктов у обеих
// частей и падает, пока они расходятся. Здесь — её модульная половина: она
// видит устройство каркаса, но не видит экрана, поэтому одна другую не
// заменяет.
//
// Абзац снят вместе с прежним рядом: он пересказывал название части, а
// единственный факт, который в нём был — секрет показывается один раз, — живёт
// там, где он нужен: в окне выпуска (`OneTimeSecretModal`).
import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { TokensLayout } = await import("./TokensLayout");

/** Часть раздела, открытая по адресу одного из своих пунктов. */
function show(at: string) {
  return render(
    <MemoryRouter initialEntries={[at]}>
      <Routes>
        <Route element={<TokensLayout />}>
          <Route path="/system/tokens/service-account-keys" element={<div>содержимое ключей</div>} />
          <Route path="/system/tokens/user-tokens" element={<div>содержимое токенов</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

const ITEMS = ["Ключи сервисных аккаунтов", "Токены пользователей"];

describe("раздел «Токены и ключи»", () => {
  it("показывает содержимое открытого адреса", () => {
    show("/system/tokens/service-account-keys");

    expect(screen.getByText("содержимое ключей")).toBeInTheDocument();
  });

  it("другой адрес — другое содержимое (контроль к утверждению выше)", () => {
    show("/system/tokens/user-tokens");

    expect(screen.getByText("содержимое токенов")).toBeInTheDocument();
    expect(screen.queryByText("содержимое ключей")).not.toBeInTheDocument();
  });

  it("не заводит СВОЕЙ навигации: перечень частей показывает колонка раздела", () => {
    show("/system/tokens/service-account-keys");

    // Ни вкладок, ни пунктов меню, ни ссылок на части — иначе они встанут
    // поверх колонки, и в разделе снова окажется две навигации, а его части
    // разойдутся по виду между собой.
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    for (const item of ITEMS) {
      expect(screen.queryByText(item)).not.toBeInTheDocument();
    }
  });

  it("не рисует собственного заголовка части", () => {
    show("/system/tokens/service-account-keys");

    // Заголовок называет предмет ОТКРЫТОЙ страницы. Имя части над ним было бы
    // вторым заголовком на одном экране — у прочих разделов такого нет, а имя
    // части уже сказано подсвеченным пунктом колонки и хлебными крошками.
    expect(screen.queryByText("Токены и ключи")).not.toBeInTheDocument();
  });

  it("не пересказывает своё название поясняющим абзацем", () => {
    show("/system/tokens/service-account-keys");

    expect(screen.queryByText(/Выпуск и отзыв/i)).not.toBeInTheDocument();
  });
});
