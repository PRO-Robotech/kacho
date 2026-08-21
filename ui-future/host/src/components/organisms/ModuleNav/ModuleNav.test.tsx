import { render, screen, within } from "@testing-library/react";
import { ModuleNav } from ".";

// Второй уровень сайдбара — типы ресурсов активного модуля.
//
// Утверждения о пунктах ресурсов ПЕРЕЕХАЛИ сюда из пробы рейла: там они
// закрепляли поведение, которое снято (рейл показывал два уровня иерархии одной
// полосой безымянных иконок). Перенос, а не снятие: то, что перечень ресурсов
// модуля виден и подсвечивается по адресу, — по-прежнему свойство продукта,
// просто теперь оно принадлежит другому месту.

const ctx = {
  account: { id: "acc-1", name: "Account" },
  project: { id: "project-1", name: "Project", accountId: "acc-1" },
};

describe("ModuleNav", () => {
  it("показывает ресурсы активного модуля списком и метит открытый", async () => {
    render(<ModuleNav context={ctx} currentPath="/projects/project-1/vpc/networks" />);

    expect(await screen.findByRole("button", { name: "Облачные сети" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Подсети" })).toBeInTheDocument();
    // Положительный контроль к отрицанию ниже: перечень непуст, значит «не
    // нашли» дальше означает отсутствие предмета, а не пустую панель.
    expect(screen.getByRole("navigation", { name: /Ресурсы:/ })).toBeInTheDocument();
  });

  /*
   * Канон §2: «Имени модуля во втором сайдбаре нет: модуль назван иконкой рейла
   * и крошками». Здесь стоял заголовок прописными («VIRTUAL PRIVATE CLOUD») с
   * линией под ним — набранный разрядкой, он привлекал внимание сильнее самого
   * перечня, ради которого колонка и заведена.
   *
   * Правило держалось НИЧЕМ: снять заголовок обратно можно было молча. Проба
   * утверждает отсутствие, поэтому положительный контроль рядом обязателен —
   * без него «имени нет» зеленело бы и на колонке, не отрисовавшей ничего.
   *
   * Имя модуля при этом не пропало совсем: колонка называет его своим ДОСТУПНЫМ
   * именем — это адресует её тому, кто читает страницу не глазами, и по нему же
   * колонку находит проба каркаса (`App.test.tsx`).
   */
  it("не называет модуль видимым текстом: его называют рейл и крошки", async () => {
    render(<ModuleNav context={ctx} currentPath="/projects/project-1/vpc/networks" />);

    const nav = await screen.findByRole("navigation", { name: "Ресурсы: Virtual Private Cloud" });
    // Положительный контроль: перечень непуст...
    expect(within(nav).getByRole("button", { name: "Облачные сети" })).toBeInTheDocument();
    // ...значит «имени нет» означает отсутствие предмета, а не пустую колонку.
    expect(within(nav).queryByText("Virtual Private Cloud")).not.toBeInTheDocument();
  });

  it("держит подсветку раздела на КАРТОЧКЕ ресурса, а не только в списке", async () => {
    render(<ModuleNav context={ctx} currentPath="/projects/project-1/vpc/subnets/sub-1" />);

    // Открыв подсеть, пользователь по-прежнему в разделе подсетей. Точное
    // совпадение адреса гасило бы подсветку, и колонка выглядела бы так, будто
    // ничего не выбрано.
    expect(await screen.findByRole("button", { name: "Подсети" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Облачные сети" })).not.toHaveAttribute("data-active", "true");
  });

  it("показывает ресурсы compute, включая каталог типов машин", async () => {
    render(<ModuleNav context={ctx} currentPath="/projects/project-1/compute/instances" />);

    expect(await screen.findByRole("button", { name: "Виртуальные машины" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Типы машин" })).toBeInTheDocument();
  });

  it("показывает ресурсы storage, включая образы", async () => {
    render(<ModuleNav context={ctx} currentPath="/projects/project-1/storage/volumes" />);

    expect(await screen.findByRole("button", { name: "Тома" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Образы" })).toBeInTheDocument();
  });

  it("различает привязки доступа и права доступа — совпадение по адресу, а не по началу имени", async () => {
    render(<ModuleNav context={ctx} currentPath="/iam/access-bindings" />);

    expect(await screen.findByRole("button", { name: "Привязки доступа" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Права доступа" })).not.toHaveAttribute("data-active", "true");
  });

  it("вне модуля панели нет вовсе", () => {
    const { container } = render(<ModuleNav context={ctx} currentPath="/dashboard" />);

    // Пустая колонка в 238px, сообщающая «ничего не выбрано», занимала бы место
    // и не отвечала ни на один вопрос.
    expect(container).toBeEmptyDOMElement();
  });

  it("называет темы документации, но НЕ обещает переход, которого нет", async () => {
    render(<ModuleNav context={ctx} currentPath="/projects/project-1/vpc/networks" />);

    await screen.findByRole("button", { name: "Облачные сети" });

    // Темы названы — панель отвечает на вопрос «что читать про этот раздел».
    expect(screen.getByText("Документация")).toBeInTheDocument();
    expect(screen.getByText("Начать работу с сетями")).toBeInTheDocument();

    // И НИ ОДНОЙ ссылки: адресов у документации в дереве нет ни одного (все
    // объявления несут href="#"), а ссылка на ту же страницу обещает переход,
    // который обнаруживается несостоявшимся только по клику.
    //
    // Проба утверждает ОТСУТСТВИЕ, поэтому рядом стоит положительный контроль
    // выше: без него «ссылок нет» зеленело бы и на панели, которая не
    // отрисовала вообще ничего.
    expect(screen.queryAllByRole("link")).toHaveLength(0);
  });

  it("раздел вне области проекта тоже получает колонку — не только IAM", async () => {
    // Сопоставление идёт по объявленному `segment`, а не по перечню особых
    // случаев. Пока случаи перечислялись, `iam` был назван поимённо, а
    // администрирование — нет, и весь раздел оставался без второго сайдбара,
    // подавая свои разделы горизонтальными вкладками.
    render(<ModuleNav context={ctx} currentPath="/system/regions" />);

    expect(await screen.findByRole("navigation", { name: /Ресурсы:/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Регионы" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Зоны" })).toBeInTheDocument();
  });

  it("несуществующий раздел колонки не получает — контроль к утверждению выше", () => {
    const { container } = render(<ModuleNav context={ctx} currentPath="/такого-раздела-нет/что-то" />);
    expect(container).toBeEmptyDOMElement();
  });
});
