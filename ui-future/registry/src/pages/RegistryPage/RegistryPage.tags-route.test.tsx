// Маршрут «реестр → репозиторий → теги»: адрес несёт реестр, вкладка его читает.
//
// Предмет (#627). Карточка репозитория объявляет дочернюю связь «Теги», а чтение
// тегов требует ОБОИХ идентификаторов — реестра и репозитория:
// `/registry/v1/registries/{registryId}/repositories/{repository}/tags`. Маршрута,
// несущего реестр, у раздела не было, поэтому вкладка не оживала НИ ПРИ КАКОМ
// входе: не «иногда пусто», а неисполнимо by construction.
//
// Второй вход тоже был мёртв, и это стоит назвать: боковая панель тегов
// существовала, но открыть её было нечем — состояние выбранного репозитория
// никто не выставлял, поэтому панель не отрисовывалась ни разу. То есть теги не
// показывались НИГДЕ, а комментарий раздела утверждал обратное.
//
// Что утверждается ниже — прежде всего ЗАПРОС: дефект был ровно в том, что
// запрос не уходил либо уходил с литералом в адресе, и присутствие строк на
// экране об этом не говорит ничего.
//
// Проба монтирует НАСТОЯЩУЮ таблицу маршрутов раздела: подстановку, найденную
// разбором адреса, но не доезжающую до маршрутизации, она бы не приняла.
//
// > Здесь стояла оговорка «строки списка в этой суите не наблюдаемы», перенятая
// > из шапки `RegistryPage.test.tsx`. Она неверна: окружение проб у этого пакета
// > — общее (`shared/src/test/setup.ts`), строки рисуются, и проба про ССЫЛКУ
// > ниже читает настоящий `href` из настоящей ячейки. Оговорка, перенесённая
// > вместе с текстом, сузила бы следующему автору выбор утверждений: он написал
// > бы слабее, чем можно, поверив ей на слово.

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { RegistryPage } from "./RegistryPage";

const realFetch = globalThis.fetch;

const REGISTRY_ROW = {
  id: "reg-1",
  name: "основной",
  created_at: "2026-08-01T10:00:00Z",
  labels: {},
  project_id: "prj-1",
};

const REPOSITORY_ROW = {
  name: "nginx",
  registry_id: "reg-1",
  lifecycle: "DURABLE",
  visibility: "PRIVATE",
  tag_count: 2,
  size_bytes: "1048576",
  updated_at: "2026-08-02T10:00:00Z",
  artifact_types: ["ARTIFACT_TYPE_CONTAINER_IMAGE"],
};

const TAG_ROW = {
  tag: "v1",
  registry_id: "reg-1",
  repository: "nginx",
  digest: "sha256:abc",
  size_bytes: "1048576",
  media_type: "application/vnd.oci.image.manifest.v1+json",
  created_at: "2026-08-03T10:00:00Z",
};

/** Адрес запроса из любой формы, которую принимает настоящий `fetch`: на
 *  `URL`/`Request` приведение по умолчанию дало бы `[object Object]`, то есть
 *  один и тот же путь для любого запроса, и утверждения об адресе стали бы
 *  истинными сразу. */
function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/**
 * Стенд отвечает ТОЛЬКО по объявленным адресам; на прочие — отказ.
 *
 * Снисходительный стенд («на любой GET пустой объект») здесь недопустим: он
 * отвечал бы и на адрес с литералом `{registryId}`, и проба зеленела бы на
 * собственной терпимости — ровно на том дефекте, который ищет.
 */
function stubApi(): string[] {
  const calls: string[] = [];
  const bodies: Record<string, unknown> = {
    "/registry/v1/registries": { registries: [REGISTRY_ROW], nextPageToken: "" },
    "/registry/v1/registries/reg-1": REGISTRY_ROW,
    "/registry/v1/registries/reg-1/repositories": { repositories: [REPOSITORY_ROW], nextPageToken: "" },
    "/registry/v1/registries/reg-1/repositories/nginx": REPOSITORY_ROW,
    "/registry/v1/registries/reg-1/repositories/nginx/tags": { tags: [TAG_ROW], nextPageToken: "" },
  };
  const stub: typeof globalThis.fetch = (input) => {
    const u = new URL(requestUrl(input), "http://console.test");
    // Путь берём РАСКОДИРОВАННЫМ: `URL` процентно кодирует фигурные скобки, и
    // утверждение «в адресе нет `{`» стало бы истинным при уехавшем литерале.
    const p = decodeURIComponent(u.pathname);
    calls.push(p);
    const body = bodies[p];
    return Promise.resolve({
      ok: body !== undefined,
      status: body !== undefined ? 200 : 404,
      statusText: body !== undefined ? "OK" : "Not Found",
      text: () => Promise.resolve(JSON.stringify(body ?? { code: 5, message: `no stub for ${p}` })),
    } as Response);
  };
  globalThis.fetch = stub;
  return calls;
}

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

/** Монтирует раздел так же, как его монтирует оболочка. */
function mountAt(pathname: string) {
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <Routes>
        <Route path="/projects/:projectId/registry/*" element={<RegistryPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const TAGS_PATH = "/registry/v1/registries/reg-1/repositories/nginx/tags";
const REPO_PATH = "/registry/v1/registries/reg-1/repositories/nginx";

describe("карточка репозитория живёт под своим реестром", () => {
  it("карточка читается по адресу, в котором реестр подставлен", async () => {
    const calls = stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx");

    await waitFor(() => expect(calls).toContain(REPO_PATH));
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
  });

  it("вкладка тегов спрашивает адрес, в котором подставлены ОБА параметра", async () => {
    const calls = stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx/tags");

    await waitFor(() => expect(calls).toContain(TAGS_PATH));
    // Ни один запрос не ушёл с литералом: закрытая подстановка — это не только
    // «нужный адрес появился», но и «ненужный не уходил».
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
  });

  it("вкладка показывает СВОЁ содержимое, а не отказ по неполному адресу", async () => {
    // Положительный контроль к утверждениям про запрос: они выполнялись бы и
    // тогда, когда оболочка запрос отправила, а пользователю показала ошибку.
    stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx/tags");

    // «Теги» встречается дважды — в полосе вкладок и в заголовке зоны
    // содержимого, — поэтому спрашиваем ВСЕ совпадения: точный счёт закреплял бы
    // раскладку оболочки, а предмет пробы не она.
    expect((await screen.findAllByText("Теги")).length).toBeGreaterThan(0);
    // Имя родителя на странице есть — то есть открыт репозиторий, а не список.
    expect(screen.getAllByText(/nginx/).length).toBeGreaterThan(0);
    // И это не состояние «адрес неполон»: оно означало бы, что запрос не ушёл,
    // а утверждения о запросе выше выполнялись бы и в этом случае.
    expect(screen.queryByText(/Адрес неполон/i)).not.toBeInTheDocument();
  });

  it("к карточке ведёт ССЫЛКА со вкладки репозиториев, а не только адрес", async () => {
    // Маршрут, к которому нет входа, — та же неисполнимая возможность, только
    // на шаг дальше: страница есть, найти её нельзя. Ссылка обязана нести адрес
    // ПОД РЕЕСТРОМ: плоский `/registry/repositories/nginx` реестра не называет,
    // и карточка по нему не собирается by construction.
    stubApi();
    const { container } = mountAt("/projects/prj-1/registry/registries/reg-1/repositories");

    await waitFor(() => {
      const hrefs = [...container.querySelectorAll("a")].map((a) => a.getAttribute("href"));
      expect(hrefs).toContain("/projects/prj-1/registry/registries/reg-1/repositories/nginx");
    });
  });

  it("вкладка репозиториев реестра по-прежнему работает — контроль соседа", async () => {
    // Одна подстановка вместо двух: без этого контроля правка могла бы починить
    // двойной случай и сломать одиночный, который работал.
    const calls = stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories");

    await waitFor(() => expect(calls).toContain("/registry/v1/registries/reg-1/repositories"));
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
  });
});

/**
 * Объявленная связь доезжает до рейла ВКЛАДКОЙ — вторая половина #627.
 *
 * Первая половина (выше) — адрес: маршрут несёт реестр, и чтение тегов уходит с
 * обоими сегментами. Она была закрыта, а сквозная проба всё равно краснела: на
 * карточке репозитория не находилось НИ ОДНОЙ вкладки. Вкладка строилась и
 * рисовалась — но объявлена была пунктом меню, потому что рейл рисует меню antd,
 * а роли ему никто не назначал.
 *
 * Почему это отдельный предмет, а не придирка к разметке. «Связь не доехала до
 * оболочки» и «доехала, объявлена не тем» снаружи выглядят ОДИНАКОВО (вкладки
 * нет), а чинятся в разных местах: первое — в реестре ресурсов и маршрутах,
 * второе — в оболочке. Утверждения ниже разделяют эти два исхода.
 *
 * Здесь спрашивается именно РОЛЬ, а не текст: «Теги» на странице есть и в
 * заголовке зоны содержимого, поэтому утверждение о тексте выполнялось бы и
 * тогда, когда вкладки нет вовсе.
 */
describe("объявленная связь доезжает до рейла ВКЛАДКОЙ", () => {
  it("у репозитория есть вкладка «Теги» — адрес ребёнка с ДВУМЯ подстановками", async () => {
    stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx");

    expect(await screen.findByRole("tab", { name: /Теги/ })).toBeInTheDocument();
  });

  it("у реестра есть вкладка «Репозитории» — контроль на ОДНОЙ подстановке", async () => {
    // Парный положительный контроль: без него правка могла бы починить случай с
    // двумя подстановками и сломать одиночный, который работал, — и оба раза
    // утверждение выше осталось бы зелёным.
    stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1");

    expect(await screen.findByRole("tab", { name: /Репозитории/ })).toBeInTheDocument();
  });

  it("рейл перечисляет ОБЪЯВЛЕННОЕ: у репозитория вкладки «Репозитории» нет", async () => {
    // Отрицание к обоим утверждениям выше. Без него они зеленели бы на оболочке,
    // рисующей вкладку на каждый ресурс каталога: «вкладка нашлась» значило бы
    // «нашлась хоть какая-то», а не «нашлась объявленная этой карточкой».
    stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx");

    // Ждём рейл, а не время: пока карточка грузится, вкладок нет ни одной, и
    // утверждение об отсутствии выполнялось бы само собой.
    await screen.findByRole("tab", { name: /Теги/ });
    expect(screen.queryByRole("tab", { name: /Репозитории/ })).not.toBeInTheDocument();
  });

  it("нажатие на вкладку читает теги — вкладка ведёт туда, куда обещает", async () => {
    // Присутствие вкладки и её РАБОТА — разные утверждения. Вкладка, которая
    // есть, но по нажатию никуда не ведёт, — та же неисполнимая возможность на
    // шаг дальше: обещание на экране без предмета за ним. Утверждается запрос, а
    // не строки: у свежего репозитория тегов нет вовсе, и «улов» тут ни при чём.
    const calls = stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx");

    fireEvent.click(await screen.findByRole("tab", { name: /Теги/ }));

    await waitFor(() => expect(calls).toContain(TAGS_PATH));
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
  });

  it("открытая вкладка помечена ВЫБРАННОЙ, остальные — явно нет", async () => {
    // Выбранность — состояние, а не подсветка: пока её несёт только класс, тот,
    // кто читает страницу не глазами, не знает, какой вид открыт.
    stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx/tags");

    const теги = await screen.findByRole("tab", { name: /Теги/ });
    expect(теги).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /Обзор/ })).toHaveAttribute("aria-selected", "false");
  });
});
