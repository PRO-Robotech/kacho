// Главная против канона консоли (`ui-future/docs/console-design-canon.md`).
//
// Утверждается НАБЛЮДАЕМОЕ — то, что видит человек, открывший страницу, — а не
// разметка: класс переименуют, компонент заменят, и утверждение о разметке
// переживёт свой предмет, оставшись зелёным. Поэтому здесь: величина шапки и
// полей взята из ОБЩЕГО объявления (свой заголовок её не воспроизведёт),
// описание домена ищется текстом, объяснение замка — подписью, состояние
// дерева — словами.
//
// Каждое отрицание идёт в паре с положительным контролем: «замка нет» без
// «замок есть» зеленело бы на странице, где нет вообще ничего.

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { jest } from "@jest/globals";

import { DashboardPage } from ".";
import { SERVICE_MODULES } from "../../lib/service-modules";
import type { HostContext } from "../../utils";
import { noMatchesText } from "@shared/lib/list-scope";
import { HEAD_CONTENT_HEIGHT, PAGE_PADDING } from "@shared/components/organisms/DetailShell/PageHead";

const withProject: HostContext = {
  account: { id: "account-1", name: "Account 1" },
  project: { id: "project-1", name: "Project 1", accountId: "account-1" },
};
const withoutProject: HostContext = { account: null, project: null };

const LOCK_REASON = "Модуль работает в границах проекта — выберите проект в дереве слева.";

function jsonResponse(body: unknown) {
  return Promise.resolve({ ok: true, text: () => Promise.resolve(JSON.stringify(body)), statusText: "OK" } as Response);
}

function pathOf(input: unknown): string {
  if (typeof input === "string") return input;
  const url = (input as { url?: unknown })?.url;
  return typeof url === "string" ? url : String(input);
}

/** Ответ на любой адрес витрины: один элемент под его же ключом. */
function answerFor(path: string): Record<string, unknown> {
  if (path.startsWith("/iam/v1/accounts")) return { accounts: [{ id: "account-1", name: "Account 1" }] };
  if (path.startsWith("/iam/v1/projects")) {
    return { projects: [{ id: "project-1", name: "Project 1", accountId: "account-1" }] };
  }
  for (const module of SERVICE_MODULES) {
    for (const stat of module.stats) {
      if (path.startsWith(stat.listPath)) return { [stat.payloadKey]: [{ id: "x-1", name: "x" }] };
    }
  }
  return {};
}

function mockFetch(impl: (path: string) => Promise<Response>) {
  global.fetch = jest.fn<typeof fetch>();
  jest.spyOn(global, "fetch").mockImplementation((input) => impl(pathOf(input)));
}

describe("главная против канона консоли", () => {
  afterEach(() => jest.restoreAllMocks());

  describe("§1 заголовок и §8 поля страницы — общая конструкция", () => {
    beforeEach(() => mockFetch((path) => jsonResponse(answerFor(path))));

    it("шапку рисует общий блок: её высота — ОБЪЯВЛЕННАЯ величина, а не своя", () => {
      render(<DashboardPage context={withProject} />);
      const heading = screen.getByRole("heading", { name: "Сервисы облака" });
      // h3 → колонка заголовка → блок шапки.
      const headBlock = heading.parentElement?.parentElement as HTMLElement;
      expect(headBlock).toBeTruthy();
      expect(headBlock.style.height).toBe(`${HEAD_CONTENT_HEIGHT}px`);
    });

    it("поля рабочей области — те же, что у списка, карточки и формы", () => {
      const { container } = render(<DashboardPage context={withProject} />);
      const main = container.querySelector("main.dashboard-main") as HTMLElement;
      expect(main).toBeTruthy();
      expect(main.style.padding).toBe(PAGE_PADDING);
    });

    it("подсказка держит своё место в ОБОИХ состояниях — иначе выбор проекта двигал бы витрину", () => {
      const noProject = render(<DashboardPage context={withoutProject} />);
      expect(noProject.container.querySelector("p.dashboard-hint")).not.toBeNull();
      expect(noProject.container.querySelector("p.dashboard-hint")?.textContent).not.toBe("");
      noProject.unmount();

      const project = render(<DashboardPage context={withProject} />);
      // Место занято и пустое: сам элемент есть, текста в нём нет.
      expect(project.container.querySelector("p.dashboard-hint")).not.toBeNull();
      expect(project.container.querySelector("p.dashboard-hint")?.textContent).toBe("");
    });
  });

  describe("плитка называет то, чем домен владеет", () => {
    beforeEach(() => mockFetch((path) => jsonResponse(answerFor(path))));

    it("предпосылка: витрина непуста и у каждой плитки есть описание", () => {
      expect(SERVICE_MODULES.length).toBeGreaterThan(0);
      for (const m of SERVICE_MODULES) expect(m.description.length).toBeGreaterThan(20);
    });

    it("описание домена ВИДНО в каждой плитке, а не только объявлено в реестре", () => {
      render(<DashboardPage context={withProject} />);
      for (const module of SERVICE_MODULES) {
        const tile = screen.getByTestId(`dashboard-tile-${module.key}`);
        expect(within(tile).getByText(module.description)).toBeInTheDocument();
      }
    });
  });

  describe("отключённая плитка объясняет, почему она отключена", () => {
    beforeEach(() => mockFetch((path) => jsonResponse(answerFor(path))));

    it("без проекта замок несёт причину", () => {
      render(<DashboardPage context={withoutProject} />);
      const tile = screen.getByTestId("dashboard-tile-vpc");
      expect(tile).toHaveAttribute("data-disabled", "true");
      expect(within(tile).getByTitle(LOCK_REASON)).toBeInTheDocument();
    });

    it("положительный контроль: с проектом замка и причины НЕТ", () => {
      render(<DashboardPage context={withProject} />);
      const tile = screen.getByTestId("dashboard-tile-vpc");
      expect(tile).toHaveAttribute("data-disabled", "false");
      expect(within(tile).queryByTitle(LOCK_REASON)).toBeNull();
    });
  });

  describe("состояние дерева: три исхода, и они разные", () => {
    it("не прочитано — «Загрузка…», а не «ничего не найдено»", () => {
      mockFetch(() => new Promise<Response>(() => undefined));
      const { container } = render(<DashboardPage context={withoutProject} />);
      expect(container.querySelector(".dash-nav-empty")?.textContent).toBe("Загрузка…");
    });

    it("прочитано и пусто — «Аккаунтов нет»", async () => {
      mockFetch((path) => jsonResponse(path.startsWith("/iam/v1/accounts") ? { accounts: [] } : {}));
      const { container } = render(<DashboardPage context={withoutProject} />);
      await waitFor(() => expect(container.querySelector(".dash-nav-empty")?.textContent).toBe("Аккаунтов нет"));
    });

    it("не выполнилось — отказ чтения НЕ выдаётся за пустоту", async () => {
      mockFetch(() => Promise.reject(new Error("сеть недоступна")));
      const { container } = render(<DashboardPage context={withoutProject} />);
      await waitFor(() =>
        expect(container.querySelector(".dash-nav-empty")?.textContent).toBe("Список аккаунтов не загрузился"),
      );
    });

    it("сужение ничего не дало — словами ОБЩЕГО источника, а не своими", async () => {
      mockFetch((path) => jsonResponse(answerFor(path)));
      const { container } = render(<DashboardPage context={withProject} />);
      await waitFor(() => expect(container.querySelector(".dash-nav-empty")).toBeNull());

      const search = screen.getByPlaceholderText(/Поиск аккаунта или проекта/);
      fireEvent.change(search, { target: { value: "заведомо-нет-такого" } });

      await waitFor(() =>
        // Список прочитан целиком (курсора за ним нет) — область `whole`.
        expect(container.querySelector(".dash-nav-empty")?.textContent).toBe(noMatchesText("whole")),
      );
    });
  });
});
