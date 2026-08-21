// Таблица маршрутов домена балансировщика — по тому, что видит пользователь.
//
// Прежняя редакция открывала `NlbPage.tsx` и утверждала, что в тексте файла
// встречаются имена организмов и строк маршрутов. Такая проба зелена, пока файл
// существует: страница не монтируется, ни один маршрут не разрешается, ни один
// исход не утверждается — а сломанная таблица маршрутов выглядит для неё ровно
// так же, как исправная. Здесь страница МОНТИРУЕТСЯ, адрес подаётся настоящий,
// и утверждается наблюдаемое: что отрисовано и куда уехал адрес.
//
// Ограничение среды названо честно: общий стаб antd (`src/test/setup.ts`)
// подменяет `Table` компонентом, который своих пропов не рисует, поэтому СТРОКИ
// списка в этой суите не наблюдаемы. Наблюдаемы заголовок страницы
// (`spec.plural`), кнопка создания и адрес после перенаправления.
//
// ЧЕМ РАЗЛИЧАЮТСЯ МАРШРУТЫ — ЗАГОЛОВКОМ, а не подписью кнопки. Кнопка теперь
// у всех одна и та же — «Создать» (канон §3: подпись называет ДЕЙСТВИЕ, предмет
// назван заголовком в двадцати точках левее и вчетверо крупнее). Прежняя
// редакция различала маршруты по «Создать <винительный падеж>» и потому
// закрепляла ровно ту подпись, которую канон снял: продукт поправился —
// покраснело здесь, как и было обещано в её собственном комментарии.
//
// Признак не слабее прежнего: заголовок у каждого из трёх ресурсов свой, а
// короткая подпись кнопки утверждается ОТДЕЛЬНО и дословно — вернётся длинная
// форма, и это упадёт.

import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";

import { NlbPage } from "./NlbPage";

const realFetch = globalThis.fetch;

/** Пустой список на любой GET: страница обязана дойти до отрисовки без стенда. */
function stubEmptyLists() {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve("{}"),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

/** Адрес после всех перенаправлений — наблюдаемый исход, а не строка в исходнике. */
function LocationProbe() {
  const { pathname } = useLocation();
  return <span data-testid="at">{pathname}</span>;
}

/** Монтирует раздел так же, как его монтирует оболочка: `/projects/:projectId/nlb/*`. */
function mountAt(pathname: string) {
  stubEmptyLists();
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <LocationProbe />
      <Routes>
        <Route path="/projects/:projectId/nlb/*" element={<NlbPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const ROUTES: Array<[string, string]> = [
  ["load-balancers", "Балансировщики нагрузки"],
  ["listeners", "Обработчики"],
  ["target-groups", "Целевые группы"],
];

describe("NlbPage — маршруты раздела разрешаются в свои списки", () => {
  it.each(ROUTES)("%s открывает свой список", async (route, plural) => {
    mountAt(`/projects/prj-1/nlb/${route}`);
    expect(await screen.findByText(plural)).toBeInTheDocument();
  });

  it.each(ROUTES)("%s предлагает создание короткой подписью", async (route) => {
    mountAt(`/projects/prj-1/nlb/${route}`);

    // Дословно «Создать», без предмета: предмет назван заголовком страницы.
    // Утверждение способно упасть в обе стороны — длинная подпись не совпадёт
    // с точным текстом, а исчезнувшая кнопка не найдётся вовсе.
    const create = await screen.findByRole("button", { name: "Создать" });
    expect(create).toBeInTheDocument();
    expect(create.textContent).toBe("Создать");
  });

  it("списки трёх ресурсов не путаются между собой", async () => {
    // Положительный близнец отрицания ниже: без него «не показал чужое» было бы
    // тривиально верно на странице, которая не показала вообще ничего.
    mountAt("/projects/prj-1/nlb/listeners");

    expect(await screen.findByText("Обработчики")).toBeInTheDocument();
    expect(screen.queryByText("Целевые группы")).toBeNull();
    expect(screen.queryByText("Балансировщики нагрузки")).toBeNull();
  });
});

describe("NlbPage — перенаправление по умолчанию", () => {
  it("корень раздела уводит на список балансировщиков", async () => {
    mountAt("/projects/prj-1/nlb");

    // Оба утверждения — внутри одного ожидания: после перенаправления шапка
    // списка перерисовывается (в неё приезжает счётчик строк), поэтому узел,
    // найденный отдельным `findByText`, к моменту проверки уже отсоединён —
    // и проба падала бы не по своему предмету.
    await waitFor(() => {
      expect(screen.getByTestId("at")).toHaveTextContent("/projects/prj-1/nlb/load-balancers");
      expect(screen.getByText("Балансировщики нагрузки")).toBeInTheDocument();
    });
  });

  it("неизвестный адрес раздела уводит туда же, а не в пустой экран", async () => {
    mountAt("/projects/prj-1/nlb/nothing-like-this");

    await waitFor(() => expect(screen.getByTestId("at")).toHaveTextContent("/projects/prj-1/nlb/load-balancers"));
  });

  it("перенаправление несёт ИМЕННО тот проект, что в адресе", async () => {
    // Контроль в другую сторону: адрес собирается из параметра маршрута, а не из
    // литерала — зашитый проект прошёл бы утверждение выше и провалился здесь.
    mountAt("/projects/prj-77/nlb");

    await waitFor(() => expect(screen.getByTestId("at")).toHaveTextContent("/projects/prj-77/nlb/load-balancers"));
  });
});
