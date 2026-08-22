import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import App from "./App";

const jsonResponse = (body: unknown) => {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

describe("App", () => {
  beforeEach(() => {
    window.localStorage.clear();
    // jsdom не пересоздаёт <html> между кейсами: без снятия атрибута «тема
    // тёмная» осталась бы от предыдущего рендера, и утверждение об умолчании
    // держалось бы на соседе, а не на коде.
    delete document.documentElement.dataset.theme;
    window.history.pushState(null, "", "/");
    jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ accounts: [] }));
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
