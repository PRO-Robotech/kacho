import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { stubNetwork } from "@shared/test/network-stub";
import ts from "typescript";
import { CEREMONY_ADDRESSES, CEREMONY_ROUTING } from "@shared/pages/auth/ceremony-addresses";
import App from "./App";

const jsonResponse = (body: unknown) => {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

/**
 * Ответ края о сессии — по построению стража над каркасом (приёмка F6b, Р7):
 * каркас открывается ТОЛЬКО на «адрес подтверждён». Пробы каркаса ниже судят
 * каркас, поэтому их «Дано» — подтверждённая сессия; неподтверждённая и
 * неизвестная — свои пробы стража.
 */
const sessionOf = (emailVerified: boolean | null) =>
  emailVerified === null
    ? { user: null }
    : {
        user: { id: "usr-1", email: "a@kacho.local", displayName: "a", subjectType: "user", permissions: [] },
        session: { expiresAt: "2026-09-24T00:00:00Z", assuranceLevel: "1", emailVerified },
      };

function urlOf(input: RequestInfo | URL): string {
  return new URL(
    typeof input === "string" ? input : input instanceof URL ? input.href : input.url,
    "http://console.test",
  ).pathname;
}

/** Сеть каркаса: «кто я» отвечает сессией с данной подтверждённостью, прочее — пустым списком. */
function stubConsole(emailVerified: boolean | null) {
  return stubNetwork((input) =>
    urlOf(input) === "/iam/v1/auth/me" ? jsonResponse(sessionOf(emailVerified)) : jsonResponse({ accounts: [] }),
  );
}

describe("App", () => {
  beforeEach(() => {
    window.localStorage.clear();
    // jsdom не пересоздаёт <html> между кейсами: без снятия атрибута «тема
    // тёмная» осталась бы от предыдущего рендера, и утверждение об умолчании
    // держалось бы на соседе, а не на коде.
    delete document.documentElement.dataset.theme;
    window.history.pushState(null, "", "/");
    stubConsole(true);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("renders the host shell without non-original header actions", async () => {
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Сервисы облака" })).toBeInTheDocument();
    // Тема уже тёмная (умолчание), поэтому переключатель предлагает светлую.
    expect(screen.getByRole("button", { name: "Включить светлую тему" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Поиск" })).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Activity" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Notifications" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "API reachability" })).not.toBeInTheDocument();
  });

  /*
   * Умолчание темы. Три утверждения вместо одного, потому что «по умолчанию
   * тёмная» и «всегда тёмная» на одном кейсе неразличимы: первый закрепляет само
   * умолчание, второй и третий — что сохранённый выбор сильнее него В ОБЕ
   * СТОРОНЫ. Без светлого кейса проба зеленела бы и на коде, который выбор
   * пользователя игнорирует.
   */
  it("defaults to the dark theme when nothing is stored", async () => {
    render(<App />);

    expect(await screen.findByRole("button", { name: "Включить светлую тему" })).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("hydrates the theme from localStorage", async () => {
    window.localStorage.setItem("kacho-theme", "dark");

    render(<App />);

    expect(await screen.findByRole("button", { name: "Включить светлую тему" })).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("keeps the light theme when the user has explicitly chosen it", async () => {
    window.localStorage.setItem("kacho-theme", "light");

    render(<App />);

    expect(await screen.findByRole("button", { name: "Включить тёмную тему" })).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("hydrates project dashboard context from the route", async () => {
    window.history.pushState(null, "", "/projects/project-1/dashboard");

    render(<App />);

    expect((await screen.findAllByText("project-1")).length).toBeGreaterThan(0);
  });

  it("routes VPC module paths to the VPC remote", async () => {
    window.history.pushState(null, "", "/projects/project-1/vpc/networks");

    render(<App />);

    expect(await screen.findByTestId("vpc-remote")).toBeInTheDocument();
    expect(screen.queryByTestId("module-placeholder-page")).not.toBeInTheDocument();
    // Модуль назван САМИМ ХОСТОМ, и утверждение теперь про хост.
    //
    // Прежняя редакция искала видимый текст «Virtual Private Cloud» где угодно
    // на странице — и находила его в <h3> ДУБЛЁРА remote'а (src/test/vpc-remote.tsx),
    // то есть утверждала о фикстуре, а не о продукте. Она зеленела бы и на
    // каркасе, который про модуль не говорит ничего.
    //
    // Имени модуля видимым текстом во втором сайдбаре больше нет (канон §2:
    // «Имени модуля во втором сайдбаре нет: модуль назван иконкой рейла и
    // крошками»). Осталось то, чем колонка называет свой модуль машинно, — её
    // доступное имя; оно точное, принадлежит хосту и держит ту же связь
    // «адрес → модуль», ради которой утверждение и стояло.
    expect(await screen.findByRole("navigation", { name: "Ресурсы: Virtual Private Cloud" })).toBeInTheDocument();
    // Раздел помечен и в рейле: адрес назвал модуль обеим поверхностям, а не
    // одной. Без этого «колонка приехала» не отличалось бы от «приехала чужая».
    expect(screen.getByRole("button", { name: "Virtual Private Cloud" })).toHaveAttribute("data-active", "true");
  });

  /*
   * Адреса церемоний принадлежат консоли МАРШРУТОМ (приёмка F8, Р3): все шесть
   * получают маршрут и все шесть консоль ведёт — восстановление доступа с
   * приёмки NTF-2 (NTF2-43, NTF2-72), — и ни один не уводится замыкающим
   * правилом на панель. Радиус правки назван: адрес вне шести (`/error`)
   * по-прежнему уходит на панель.
   */
  // Заголовок экрана по адресу — для ВСЕХ адресов перечня, который читает
  // маршрутизатор (условие C1): адрес, добавленный в перечень без экрана,
  // краснит эту пробу, а не уходит на панель молча.
  const SCREEN_TITLE: Record<string, string> = {
    login: "Вход в консоль",
    registration: "Новая учётная запись",
    logout: "Выход из консоли",
    verification: "Подтвердите адрес почты",
    recovery: "Восстановление доступа",
  };
  const outsideShell = CEREMONY_ADDRESSES.filter((a) => CEREMONY_ROUTING[a].kind !== "in-shell").map((a) => {
    const serving = CEREMONY_ROUTING[a];
    return [a, serving.kind === "in-shell" ? "" : SCREEN_TITLE[serving.screen]];
  });

  it("C1 · перечень адресов церемоний — шесть, без /error и /consent", () => {
    expect([...CEREMONY_ADDRESSES].sort()).toEqual(
      ["/login", "/logout", "/recovery", "/registration", "/settings", "/verification"].sort(),
    );
  });

  it.each(outsideShell)(
    "F8-01/F8-03 · адрес церемонии %s отвечает экраном консоли, а не переводом на панель",
    async (path, title) => {
      window.history.pushState(null, "", path);
      // Экран подтверждения — экран НЕПОДТВЕРЖДЁННОЙ сессии; прочим экранам
      // вне каркаса сессия не нужна (Р7: без сессии — как до этой под-фазы).
      stubConsole(path === "/verification" ? false : null);

      render(<App />);

      expect(await screen.findByRole("heading", { name: title })).toBeInTheDocument();
      expect(window.location.pathname).toBe(path);
      // Экраны церемоний стоят вне каркаса: рейла с разделами у них нет.
      expect(screen.queryByRole("navigation", { name: "Host navigation" })).toBeNull();
    },
  );

  it("F8-01 · /settings — экран параметров учётной записи внутри каркаса", async () => {
    window.history.pushState(null, "", "/settings");

    render(<App />);

    expect(await screen.findByRole("heading", { name: "Параметры учётной записи" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/settings");
    expect(screen.getByRole("navigation", { name: "Host navigation" })).toBeInTheDocument();
  });

  it("C1 · маршрутизатор берёт адреса церемоний из ОДНОГО перечня, а не пишет их литералами", () => {
    // Второй перечень тех же адресов разошёлся бы с первым молча: адрес,
    // добавленный в перечень и забытый в маршрутизаторе, ушёл бы на панель.
    const file = fileURLToPath(new URL("./App.tsx", import.meta.url));
    const sf = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const literals: string[] = [];
    const walk = (n: ts.Node) => {
      if (
        (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) &&
        (CEREMONY_ADDRESSES as readonly string[]).includes(n.text)
      ) {
        literals.push(`${sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1}: ${n.text}`);
      }
      ts.forEachChild(n, walk);
    };
    walk(sf);
    expect(literals).toEqual([]);
  });

  it("радиус правки: адрес вне шести церемоний по-прежнему уходит на панель", async () => {
    window.history.pushState(null, "", "/error");

    render(<App />);

    expect(await screen.findByRole("heading", { name: "Сервисы облака" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/dashboard");
  });

  it("routes IAM module paths to the IAM remote", async () => {
    window.history.pushState(null, "", "/iam/accounts");

    render(<App />);

    expect(await screen.findByTestId("iam-remote")).toBeInTheDocument();
    expect(screen.queryByTestId("module-placeholder-page")).not.toBeInTheDocument();
    expect(
      await screen.findByRole("navigation", { name: "Ресурсы: Identity and Access Management" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Identity and Access Management" })).toHaveAttribute(
      "data-active",
      "true",
    );
  });
});

// ─── приёмка F6b, Р7 · страж над каркасом ─────────────────────────────────────
//
// Каркас открывается только на «адрес подтверждён». Неподтверждённая сессия на
// любом адресе консоли, кроме четырёх экранов вне каркаса, уходит на экран
// подтверждения с адресом возврата; чтения каркаса (`/iam/v1/accounts`) при этом
// не выпускаются. Сквозная проба того же предмета — `address-confirmation.spec.ts`.
describe("F6b · страж над каркасом консоли", () => {
  beforeEach(() => {
    window.localStorage.clear();
    delete document.documentElement.dataset.theme;
  });
  afterEach(() => {
    jest.restoreAllMocks();
  });

  const GUARDED: Array<[string, string]> = [
    ["/", "/verification"],
    ["/dashboard", "/verification?returnTo=%2Fdashboard"],
    ["/projects/prj-1/vpc/networks", "/verification?returnTo=%2Fprojects%2Fprj-1%2Fvpc%2Fnetworks"],
    ["/projects/prj-1/compute", "/verification?returnTo=%2Fprojects%2Fprj-1%2Fcompute"],
    ["/iam/", "/verification?returnTo=%2Fiam%2F"],
    ["/system/", "/verification?returnTo=%2Fsystem%2F"],
    ["/settings", "/verification?returnTo=%2Fsettings"],
    ["/recovery", "/verification?returnTo=%2Frecovery"],
    ["/no-such-address", "/verification?returnTo=%2Fno-such-address"],
  ];

  it.each(GUARDED)(
    "F6b-15 · %s у неподтверждённой сессии ведёт на экран подтверждения, каркас не монтируется",
    async (path, expected) => {
      window.history.pushState(null, "", path);
      const network = stubConsole(false);

      render(<App />);

      expect(await screen.findByRole("heading", { name: "Подтвердите адрес почты" })).toBeInTheDocument();
      expect(`${window.location.pathname}${window.location.search}`).toBe(expected);
      expect(screen.queryByRole("navigation", { name: "Host navigation" })).toBeNull();
      const asked = network.mock.calls.map(([input]) => urlOf(input));
      expect(asked.length).toBeGreaterThan(0);
      expect(asked.filter((u) => u !== "/iam/v1/auth/me" && u !== "/iam/v1/auth/csrf")).toEqual([]);
    },
  );

  it("F6b-16 · близнец: подтверждённая сессия на тех же адресах получает каркас", async () => {
    window.history.pushState(null, "", "/settings");
    const network = stubConsole(true);

    render(<App />);

    expect(await screen.findByRole("heading", { name: "Параметры учётной записи" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/settings");
    expect(screen.getByRole("navigation", { name: "Host navigation" })).toBeInTheDocument();
    expect(network.mock.calls.map(([input]) => urlOf(input))).toContain("/iam/v1/accounts");
  });

  it("F6b-16 · NTF2-72 · близнец: /recovery у подтверждённой сессии — экран восстановления вне каркаса: страж пропустил", async () => {
    // verifies #2917
    window.history.pushState(null, "", "/recovery");
    stubConsole(true);

    render(<App />);

    expect(await screen.findByRole("heading", { name: "Восстановление доступа" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Адрес электронной почты" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/recovery");
    expect(screen.queryByRole("navigation", { name: "Host navigation" })).toBeNull();
  });
});
