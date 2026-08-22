import { render, screen } from "@testing-library/react";
import { HostRail } from ".";

describe("HostRail", () => {
  it("matches the unauthenticated original rail surface", async () => {
    render(<HostRail showReachability={false} />);

    expect(screen.getByRole("button", { name: "Kacho" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Все сервисы" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Поиск" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Compute" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Load Balancer" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Identity and Access Management" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Администрирование" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Войти" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Настройки" })).not.toBeInTheDocument();
  });

  it("enables dashboard launchers when project context exists", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/dashboard"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).not.toBeDisabled();
    expect(screen.getByRole("button", { name: "Compute" })).not.toBeDisabled();
    expect(screen.getByRole("button", { name: "Load Balancer" })).not.toBeDisabled();
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Ниже стояли пять проб о том, что рейл ВНУТРИ модуля показывает пункты его
  // ресурсов («Подсети», «Типы машин», «Образы») и НЕ показывает соседние
  // модули. Они закрепляли поведение, которое снято: два уровня иерархии стояли
  // одной полосой иконок без подписей, а перечень модулей внутри модуля исчезал
  // — уйти из VPC в Compute можно было только через «Все сервисы».
  //
  // Утверждения не выброшены, а ПЕРЕЕХАЛИ: пункты ресурсов теперь утверждает
  // проба второго уровня (`ModuleNav.test.tsx`), включая точность совпадения
  // путей. Здесь остаётся то, что относится к рейлу — модули.

  it("показывает перечень модулей ВНУТРИ модуля, а не только снаружи", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/vpc/networks"
        showReachability={false}
      />,
    );

    // Активный модуль помечен...
    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).toHaveAttribute(
      "data-active",
      "true",
    );
    // ...а соседние ОСТАЮТСЯ на месте: переход между сервисами не обязан идти
    // через дашборд. Прежде здесь утверждалось обратное.
    expect(screen.getByRole("button", { name: "Compute" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Load Balancer" })).toBeInTheDocument();
  });

  it("не показывает пункты ресурсов: их место — второй уровень", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/vpc/networks"
        showReachability={false}
      />,
    );

    await screen.findByRole("button", { name: "Virtual Private Cloud" });
    // Отрицание в паре с положительным выше: без него оно зеленело бы и на
    // пустом рейле.
    expect(screen.queryByRole("button", { name: "Подсети" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Облачные сети" })).not.toBeInTheDocument();
  });

  /*
   * ДВА ИМЕНИ ОДНОГО ПРЕДМЕТА В ОДНОЙ ПОЛОСЕ — класс, а не случай.
   *
   * Администрирование стало спрашиваться наравне с прочими модулями (ради
   * второго сайдбара), и его раздел поехал в полосу модулей ВДОБАВОК к кнопке
   * внизу: две кнопки, одно имя, один адрес назначения. Пользователю это
   * читается как два разных места продукта, а `getByRole` — как «найдено
   * несколько», то есть падением не по существу.
   *
   * Проба закрепляет СВОЙСТВО полосы, а не тот один раздел: следующий модуль,
   * которому каталог назначил своё место, попадётся здесь же. Объём
   * осмотренного назван числом — иначе «дублей нет» неотличимо от «кнопок не
   * прочитано».
   */
  it("ни одно имя в рейле не встречается дважды", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/vpc/networks"
        showReachability={false}
      />,
    );

    // Ждём приезда разделов: до него полоса несёт только собственные кнопки, и
    // утверждение о дублях говорило бы о неполной поверхности.
    await screen.findByRole("button", { name: "Virtual Private Cloud" });

    const names = screen.getAllByRole("button").map((b) => b.getAttribute("aria-label") ?? "");
    expect(names.filter((n) => n === "")).toEqual([]);
    // Кнопок в полосе: знак, «Все сервисы», «Поиск», разделы модулей,
    // «Администрирование», «Войти». Порог — нижняя граница, а не точное число:
    // перечень разделов растёт с продуктом, и точное число ломалось бы на
    // каждом новом модуле, ничего при этом не защищая.
    expect(names.length).toBeGreaterThanOrEqual(8);

    const twice = names.filter((n, i) => names.indexOf(n) !== i);
    expect(twice).toEqual([]);
  });

  /*
   * Отдельно — сам предмет, названный поимённо: администрирование стоит внизу
   * рейла и НЕ повторяется в полосе модулей. Утверждение выше о нём не говорит
   * (оно про свойство полосы), а это — про место конкретного раздела.
   */
  it("администрирование стоит ОДИН раз и не занимает место в полосе модулей", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/vpc/networks"
        showReachability={false}
      />,
    );

    // Положительный контроль: разделы приехали, полоса модулей непуста...
    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).toBeInTheDocument();
    // ...значит «одна кнопка» ниже — про место раздела, а не про пустой рейл.
    expect(screen.getAllByRole("button", { name: "Администрирование" })).toHaveLength(1);
    expect(document.querySelector(".rail-bottom")).toContainElement(
      screen.getByRole("button", { name: "Администрирование" }),
    );
  });

  it("помечает активным тот модуль, в котором находимся", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/iam/accounts"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Identity and Access Management" })).toHaveAttribute(
      "data-active",
      "true",
    );
    expect(screen.getByRole("button", { name: "Virtual Private Cloud" })).not.toHaveAttribute("data-active", "true");
  });
});
