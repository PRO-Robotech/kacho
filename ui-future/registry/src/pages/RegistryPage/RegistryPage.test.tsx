// Таблица маршрутов раздела реестра — по тому, что видит пользователь.
//
// Прежняя редакция открывала `RegistryPage.tsx` и утверждала, что в тексте файла
// встречаются имена организмов и строк маршрутов. Такая проба зелена, пока файл
// существует: страница не монтируется, ни один маршрут не разрешается, ни один
// исход не утверждается — сломанная таблица маршрутов выглядит для неё так же,
// как исправная. Здесь страница МОНТИРУЕТСЯ, адрес подаётся настоящий, и
// утверждается наблюдаемое: что отрисовано и куда уехал адрес.
//
// Ограничение среды названо честно: стаб antd (`src/test/setup.ts`) подменяет
// `Table` компонентом, который своих пропов не рисует, поэтому СТРОКИ списка в
// этой суите не наблюдаемы. Наблюдаемы шапка списка (`spec.plural`), призыв к
// созданию (`Создать <singular>`) и адрес после перенаправления — они и
// различают маршруты между собой.

import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";

import { RegistryPage } from "./RegistryPage";

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

/** Монтирует раздел так же, как его монтирует оболочка: `/projects/:projectId/registry/*`. */
function mountAt(pathname: string) {
  stubEmptyLists();
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <LocationProbe />
      <Routes>
        <Route path="/projects/:projectId/registry/*" element={<RegistryPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("RegistryPage — маршруты раздела разрешаются в свои списки", () => {
  it.each([
    ["registries", "Реестры"],
    ["repositories", "Репозитории"],
    ["tags", "Теги"],
  ])("%s открывает свой список", async (route, plural) => {
    mountAt(`/projects/prj-1/registry/${route}`);

    expect(await screen.findByText(plural)).toBeInTheDocument();
  });

  it("призыв к созданию есть ровно там, где ресурс создаётся клиентом", async () => {
    // Реестр создаётся из консоли (`ops.create: true`), а репозиторий и тег —
    // нет: они появляются от `docker push`. Призыв к созданию на них означал бы
    // форму, которой сервер не примет. Список строит текст как «Создать » +
    // `spec.singular.toLowerCase()`, без склонения, — закрепляем как есть.
    mountAt("/projects/prj-1/registry/registries");
    // CTA живёт в правом слоте шапки (useHeaderRight), который рисует RegistryFrame:
    // его наличие доказывает, что смонтирована не только страница, но и рама.
    expect(await screen.findByText("Создать реестр")).toBeInTheDocument();
  });

  it.each(["repositories", "tags"])("%s не предлагает создание, которого у него нет", async (route) => {
    // Отрицание в паре с положительным выше: иначе «призыва нет» было бы верно и
    // на странице, которая вообще ничего не нарисовала.
    mountAt(`/projects/prj-1/registry/${route}`);

    await screen.findByText(route === "tags" ? "Теги" : "Репозитории");
    expect(screen.queryByText(/^Создать /)).toBeNull();
  });

  it("списки трёх ресурсов не путаются между собой", async () => {
    // Положительный близнец отрицания ниже: без него «не показал чужое» было бы
    // тривиально верно на странице, которая не показала вообще ничего.
    mountAt("/projects/prj-1/registry/repositories");

    expect(await screen.findByText("Репозитории")).toBeInTheDocument();
    expect(screen.queryByText("Реестры")).toBeNull();
    expect(screen.queryByText("Теги")).toBeNull();
  });
});

describe("RegistryPage — перенаправление по умолчанию", () => {
  it("корень раздела уводит на список реестров", async () => {
    mountAt("/projects/prj-1/registry");

    // Оба утверждения — внутри одного ожидания: после перенаправления шапка
    // списка перерисовывается (в неё приезжает счётчик строк), поэтому узел,
    // найденный отдельным `findByText`, к моменту проверки уже отсоединён.
    await waitFor(() => {
      expect(screen.getByTestId("at")).toHaveTextContent("/projects/prj-1/registry/registries");
      expect(screen.getByText("Реестры")).toBeInTheDocument();
    });
  });

  it("неизвестный адрес раздела уводит туда же, а не в пустой экран", async () => {
    mountAt("/projects/prj-1/registry/nothing-like-this");

    await waitFor(() => expect(screen.getByTestId("at")).toHaveTextContent("/projects/prj-1/registry/registries"));
  });

  it("перенаправление несёт ИМЕННО тот проект, что в адресе", async () => {
    // Контроль в другую сторону: адрес собирается из параметра маршрута, а не из
    // литерала — зашитый проект прошёл бы утверждение выше и провалился здесь.
    mountAt("/projects/prj-77/registry");

    await waitFor(() => expect(screen.getByTestId("at")).toHaveTextContent("/projects/prj-77/registry/registries"));
  });
});
