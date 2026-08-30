// Вкладка тегов читает СПИСОК, а не его первую страницу.
//
// Предмет. List отдаёт одну страницу плюс непрозрачный курсор; общего числа нет.
// Раздел реестра держал СВОЮ копию хука чтения списка, и она курсор ответа не
// читала вовсе: всё, что за ним, арендатору не показывалось никогда, а число
// строк на экране читалось как размер списка. Дефект тихий — отличить «тегов
// два» от «тегов двести, показаны два» по экрану нельзя ничем.
//
// Что утверждается ниже — ДОСТИЖИМОСТЬ второй страницы, то есть три разных
// факта, каждый своим утверждением: продолжение предложено пользователю; по
// нажатию уходит запрос С КУРСОРОМ, который отдал сервер; строка второй
// страницы появляется на экране. Первое без второго было бы кнопкой без
// предмета, второе без третьего — запросом, ответ которого никуда не доехал.
//
// Отрицание («продолжения нет») стоит здесь только в паре с положительным
// контролем: на пустом или неотрисованном экране оно выполняется само собой.

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

function tagRow(tag: string) {
  return {
    tag,
    registry_id: "reg-1",
    repository: "nginx",
    digest: `sha256:${tag}`,
    size_bytes: "1048576",
    media_type: "application/vnd.oci.image.manifest.v1+json",
    created_at: "2026-08-03T10:00:00Z",
  };
}

const TAGS_PATH = "/registry/v1/registries/reg-1/repositories/nginx/tags";

/** Адрес запроса из любой формы, которую принимает настоящий `fetch`. */
function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

interface Call {
  path: string;
  pageToken: string | null;
}

/**
 * Стенд отвечает ТОЛЬКО по объявленным адресам, а страницу тегов выбирает ПО
 * КУРСОРУ запроса. Снисходительный стенд («на любой pageToken одна и та же
 * страница») здесь недопустим: продолжение выглядело бы работающим при
 * невыполненном запросе, то есть проба зеленела бы на собственной терпимости.
 *
 * `tagPages` — курсор запроса → тело ответа. Пустой ключ означает первую
 * страницу: её просят без курсора.
 */
function stubApi(tagPages: Record<string, unknown>): Call[] {
  const calls: Call[] = [];
  const bodies: Record<string, unknown> = {
    "/registry/v1/registries": { registries: [REGISTRY_ROW], nextPageToken: "" },
    "/registry/v1/registries/reg-1": REGISTRY_ROW,
    "/registry/v1/registries/reg-1/repositories": { repositories: [REPOSITORY_ROW], nextPageToken: "" },
    "/registry/v1/registries/reg-1/repositories/nginx": REPOSITORY_ROW,
  };
  const stub: typeof globalThis.fetch = (input) => {
    const u = new URL(requestUrl(input), "http://console.test");
    const p = decodeURIComponent(u.pathname);
    const pageToken = u.searchParams.get("pageToken");
    calls.push({ path: p, pageToken });
    const body = p === TAGS_PATH ? tagPages[pageToken ?? ""] : bodies[p];
    return Promise.resolve({
      ok: body !== undefined,
      status: body !== undefined ? 200 : 404,
      statusText: body !== undefined ? "OK" : "Not Found",
      text: () =>
        Promise.resolve(JSON.stringify(body ?? { code: 5, message: `no stub for ${p}?pageToken=${pageToken}` })),
    } as Response);
  };
  globalThis.fetch = stub;
  return calls;
}

afterEach(() => {
  globalThis.fetch = realFetch;
  localStorage.clear();
});

function mountTags() {
  return render(
    <MemoryRouter initialEntries={["/projects/prj-1/registry/registries/reg-1/repositories/nginx/tags"]}>
      <Routes>
        <Route path="/projects/:projectId/registry/*" element={<RegistryPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

/** Два ответа: первая страница несёт курсор, вторая его гасит. */
const TWO_PAGES = {
  "": { tags: [tagRow("v1")], nextPageToken: "tok2" },
  tok2: { tags: [tagRow("v2-za-kursorom")], nextPageToken: "" },
};

/** Один ответ без курсора — список помещается на страницу целиком. */
const ONE_PAGE = {
  "": { tags: [tagRow("v1")], nextPageToken: "" },
};

describe("вкладка тегов: вторая страница достижима", () => {
  it("первая страница видна — положительный контроль ко всему остальному", async () => {
    stubApi(TWO_PAGES);
    mountTags();

    expect(await screen.findByText("v1")).toBeInTheDocument();
  });

  it("сервер отдал курсор — пользователю предложено продолжение", async () => {
    stubApi(TWO_PAGES);
    mountTags();

    // Ждём строку первой страницы, а не время: до её появления таблицы нет
    // вовсе, и утверждение о кнопке говорило бы о незагруженном экране.
    await screen.findByText("v1");
    expect(await screen.findByRole("button", { name: /Показать ещё/ })).toBeInTheDocument();
  });

  it("нажатие уходит запросом С КУРСОРОМ, который отдал сервер", async () => {
    const calls = stubApi(TWO_PAGES);
    mountTags();

    await screen.findByText("v1");
    fireEvent.click(await screen.findByRole("button", { name: /Показать ещё/ }));

    await waitFor(() => expect(calls.some((c) => c.path === TAGS_PATH && c.pageToken === "tok2")).toBe(true));
  });

  it("строка второй страницы появляется на экране", async () => {
    // Запрос без доехавшего ответа — то же самое, что его отсутствие: тег за
    // курсором остаётся невидимым, ради чего проба и написана.
    stubApi(TWO_PAGES);
    mountTags();

    await screen.findByText("v1");
    fireEvent.click(await screen.findByRole("button", { name: /Показать ещё/ }));

    expect(await screen.findByText("v2-za-kursorom")).toBeInTheDocument();
  });

  it("курсор пуст — продолжения нет и лишних запросов тоже", async () => {
    // Отрицание в паре с положительным выше: без него правка, рисующая кнопку
    // безусловно, прошла бы все четыре утверждения.
    const calls = stubApi(ONE_PAGE);
    mountTags();

    await screen.findByText("v1");
    expect(screen.queryByRole("button", { name: /Показать ещё/ })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.path === TAGS_PATH && c.pageToken !== null)).toEqual([]);
  });
});
