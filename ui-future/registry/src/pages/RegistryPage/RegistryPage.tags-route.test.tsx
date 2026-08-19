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
  // Дайджест настоящей длины: на «sha256:abc» сокращённая и полная формы
  // совпадали бы, и утверждение о сокращении стало бы истинным при любом
  // поведении — то есть не различало бы ничего.
  digest: "sha256:793a57cec5ee88d1c38575cefc16cc6512f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0",
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
    // Удаление тега — единственная мутация тега. Пустой объект: операции в
    // ответе нет, поэтому путь завершается синхронно и поллер не заводится.
    "/registry/v1/registries/reg-1/repositories/nginx/tags/v1": {},
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

/**
 * Возможности тега живут на ВКЛАДКЕ, а не в недостижимой панели (#633).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Боковая панель тегов существовала, но открыть её было нечем: состояние
 * выбранного репозитория не выставлял никто, поэтому панель не отрисовывалась
 * НИ РАЗУ. После #627 вкладка тегов работает, и панель стала вторым видом одного
 * предмета (правило 3 `ui.md`) — притом недостижимым.
 *
 * Панель снята, а то, что несла только она, перенесено на строку тега: команда
 * `docker pull`, сокращённый дайджест и удаление тега. Снять компонент, не
 * перенеся возможности, было бы потерей функциональности (LEAN запрещает её
 * прямо), а оставить недостижимый — тем самым мёртвым кодом, который выглядит
 * работающим.
 *
 * ПОЧЕМУ УДАЛЕНИЕ ПРОВЕРЯЕТСЯ АДРЕСОМ, А НЕ НАЛИЧИЕМ КНОПКИ
 *
 * Кнопка «удалить» на строке тега БЫЛА и до этой правки — её рисовало общее
 * меню действий строки. Она не работала: общий строитель адреса берёт
 * `spec.apiPath` НЕПОДСТАВЛЕННЫМ и опознаёт строку по полю `id`, которого у тега
 * нет вовсе (его натуральный ключ — сам тег). Получался запрос по адресу с
 * литералом `{registryId}` и пустым последним сегментом. Поэтому утверждается
 * УШЕДШИЙ ЗАПРОС: наличие кнопки было истинным и на сломанном.
 */
describe("возможности тега живут на вкладке, а не в недостижимой панели", () => {
  /** Открывает вкладку тегов и дожидается строки тега. */
  async function откудаВидноТег() {
    const calls = stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories/nginx/tags");
    await waitFor(() => expect(calls).toContain(TAGS_PATH));
    expect(await screen.findByText("v1")).toBeInTheDocument();
    return calls;
  }

  it("строка тега предлагает скопировать команду docker pull", async () => {
    await откудаВидноТег();

    // Команда pull — то, ради чего в реестр и ходят. Она была только в панели,
    // которой никто не мог открыть, то есть возможность существовала лишь в коде.
    expect(await screen.findByRole("button", { name: "Копировать docker pull" })).toBeInTheDocument();
  });

  it("дайджест показан СОКРАЩЁННО, а не целиком", async () => {
    await откудаВидноТег();

    // Полный дайджест — 71 символ; в ячейке таблицы он распирает строку и
    // вытесняет всё остальное. Сокращение было только в панели.
    expect(await screen.findByText(/793a57cec…/)).toBeInTheDocument();
    expect(screen.queryByText(TAG_ROW.digest)).not.toBeInTheDocument();
  });

  it("удаление тега уходит по адресу, в котором подставлены ОБА родителя и сам тег", async () => {
    const calls = await откудаВидноТег();

    // Нажатие + подтверждение: удаление необратимо, поэтому спрашивается.
    fireEvent.click(await screen.findByRole("button", { name: "Удалить тег" }));
    fireEvent.click(await screen.findByRole("button", { name: "Удалить" }));

    await waitFor(() => expect(calls).toContain(`${TAGS_PATH}/v1`));
    // Ни один запрос не ушёл с литералом: именно так выглядел общий путь
    // удаления, и «кнопка есть» об этом не говорило ничего.
    expect(calls.filter((p) => p.includes("{"))).toEqual([]);
  });

  it("панель тегов не рисуется и открыть её нечем — второго вида у списка нет", async () => {
    // Отрицание к утверждениям выше: они выполнялись бы и тогда, когда рядом с
    // вкладкой остался бы второй список тех же тегов.
    await откудаВидноТег();

    expect(screen.queryByRole("button", { name: "Закрыть теги" })).not.toBeInTheDocument();
  });

  it("у соседа — репозиториев — строка по-прежнему без действий: контроль", async () => {
    // Парный положительный контроль: правка могла бы снять действия строки у
    // ВСЕХ дочерних вкладок, и утверждения выше остались бы зелёными.
    const calls = stubApi();
    mountAt("/projects/prj-1/registry/registries/reg-1/repositories");

    await waitFor(() => expect(calls).toContain("/registry/v1/registries/reg-1/repositories"));
    expect(await screen.findByText("nginx")).toBeInTheDocument();
    // Репозиторий read-only (появляется от docker push) — своих действий строки
    // у него нет и быть не должно.
    expect(screen.queryByRole("button", { name: "Удалить тег" })).not.toBeInTheDocument();
  });
});
