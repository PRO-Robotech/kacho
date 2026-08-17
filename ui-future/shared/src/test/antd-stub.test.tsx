// Контракт общего стенда-заменителя: он обязан РИСОВАТЬ то, что видит оператор.
//
// Зачем проба на сам дублёр. Восемь имён antd получают видимое не детьми, а
// пропом (`menu`, `items`, `treeData`, `dataSource`, `title`, `count`), и
// подменённые пустым `<div>` они не показывали ничего — а значит всякая проба о
// составе меню, вкладок, дерева была истинна при любом составе (#570). Гейт
// `scripts/check-antd-double-draws-what-the-operator-sees.mjs` стережёт, чтобы
// такое имя не завелось снова; здесь стережётся обратное — что уже нарисованное
// рисуется ПО СУЩЕСТВУ, а не по форме.
//
// Каждое утверждение парное: непустой набор наблюдаем, пустой — не наблюдаем.
// Без второй половины «пункт виден» зеленело бы на дублёре, рисующем подпись
// всегда, и снятие пункта из продукта осталось бы незамеченным.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactElement } from "react";

import { antdStub } from "./antd-stub";

// Заменитель по своей природе нетипизирован: он отдаёт карту «имя → компонент»,
// собранную для подмены модуля. Приводить её к типам antd значило бы завести
// вторую декларацию его API — ровно ту копию, которой этот файл и противостоит.
// Поэтому одна общая форма компонента, принимающего любые пропы и несущего свои
// пространства имён (`List.Item.Meta`).
type StubComponent = ((props: Record<string, unknown>) => ReactElement | null) & {
  [key: string]: StubComponent;
};
const S = antdStub() as unknown as Record<string, StubComponent>;

describe("Dropdown рисует состав меню, а не только триггер", () => {
  it("пункты видны, выключенный помечен и не срабатывает", () => {
    const onEdit = jest.fn();
    const onDelete = jest.fn();
    render(
      <S.Dropdown
        menu={{
          items: [
            { key: "edit", label: "Редактировать", onClick: onEdit },
            { type: "divider" },
            { key: "del", label: "Удалить", disabled: true, onClick: onDelete },
          ],
        }}
      >
        <button type="button">Ещё</button>
      </S.Dropdown>,
    );

    expect(screen.getAllByRole("menuitem").map((i) => i.textContent)).toEqual(["Редактировать", "Удалить"]);
    fireEvent.click(screen.getByText("Редактировать"));
    expect(onEdit).toHaveBeenCalledTimes(1);
    // Выключенный пункт ВИДЕН и НЕ срабатывает — как у настоящего antd.
    fireEvent.click(screen.getByText("Удалить"));
    expect(onDelete).not.toHaveBeenCalled();
  });

  it("пустое меню не рисует ни одного пункта — контроль в обратную сторону", () => {
    render(
      <S.Dropdown menu={{ items: [] }}>
        <button type="button">Ещё</button>
      </S.Dropdown>,
    );
    expect(screen.queryAllByRole("menuitem")).toHaveLength(0);
    expect(screen.getByText("Ещё")).toBeInTheDocument();
  });
});

describe("Popconfirm рисует вопрос и обе кнопки", () => {
  it("вопрос, пояснение и подтверждение достижимы", () => {
    const onConfirm = jest.fn();
    render(
      <S.Popconfirm title="Отозвать ключ?" description="Действие необратимо" okText="Отозвать" onConfirm={onConfirm}>
        <button type="button">Отзыв</button>
      </S.Popconfirm>,
    );
    const popup = screen.getByRole("tooltip");
    expect(within(popup).getByText("Отозвать ключ?")).toBeInTheDocument();
    expect(within(popup).getByText("Действие необратимо")).toBeInTheDocument();
    fireEvent.click(within(popup).getByRole("button", { name: "Отозвать" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("выключенное подтверждение не показывает вопроса — контроль в обратную сторону", () => {
    render(
      <S.Popconfirm title="Отозвать ключ?" disabled>
        <button type="button">Отзыв</button>
      </S.Popconfirm>,
    );
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
});

describe("Tree рисует узлы и вложенность", () => {
  it("узлы видны, дочерний лежит внутри родителя", () => {
    render(
      <S.Tree
        treeData={[{ key: "net", title: "Сеть", children: [{ key: "sub", title: "Подсеть" }] }]}
        selectedKeys={["net"]}
      />,
    );
    const nodes = screen.getAllByRole("treeitem");
    expect(nodes.map((n) => n.textContent)).toEqual(["СетьПодсеть", "Подсеть"]);
    expect(nodes[0].getAttribute("aria-selected")).toBe("true");
  });

  it("пустое дерево не рисует узлов — контроль в обратную сторону", () => {
    render(<S.Tree treeData={[]} />);
    expect(screen.queryAllByRole("treeitem")).toHaveLength(0);
  });
});

describe("Tabs рисует подписи всех вкладок и содержимое активной", () => {
  it("подписи видны, содержимое — только у активной", () => {
    render(
      <S.Tabs
        activeKey="b"
        items={[
          { key: "a", label: "Обзор", children: <span>тело обзора</span> },
          { key: "b", label: "Связи", children: <span>тело связей</span> },
        ]}
      />,
    );
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Обзор", "Связи"]);
    expect(screen.getByRole("tabpanel").textContent).toBe("тело связей");
    // Контроль в обратную сторону: тело неактивной вкладки не показано.
    expect(screen.queryByText("тело обзора")).toBeNull();
  });

  it("переключение уходит ключом вкладки", () => {
    const onChange = jest.fn();
    render(<S.Tabs activeKey="a" onChange={onChange} items={[{ key: "a", label: "Обзор" }, { key: "b", label: "Связи" }]} />);
    fireEvent.click(screen.getByText("Связи"));
    expect(onChange).toHaveBeenCalledWith("b");
  });
});

describe("List рисует строки из dataSource", () => {
  it("строки и их действия видны", () => {
    const onRemove = jest.fn();
    render(
      <S.List
        dataSource={[{ id: "k1", name: "ключ один" }]}
        renderItem={(e: { id: string; name: string }) => (
          <S.List.Item
            actions={[
              <button key="del" type="button" onClick={onRemove}>
                Удалить
              </button>,
            ]}
          >
            <S.List.Item.Meta title={e.name} description={e.id} />
          </S.List.Item>
        )}
      />,
    );
    expect(screen.getByRole("heading", { name: "ключ один" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Удалить" }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("пустой список показывает свою подпись пустоты, а строк не рисует", () => {
    render(<S.List dataSource={[]} locale={{ emptyText: "Пусто" }} renderItem={() => <S.List.Item>х</S.List.Item>} />);
    expect(screen.getByText("Пусто")).toBeInTheDocument();
    expect(screen.queryByText("х")).toBeNull();
  });
});

describe("Collapse рисует подписи панелей и содержимое раскрытых", () => {
  it("подписи всех видны, содержимое — у раскрытой", () => {
    render(
      <S.Collapse
        activeKey={["1"]}
        items={[
          { key: "0", label: "правило первое", extra: <button type="button">снять</button>, children: <span>тело 0</span> },
          { key: "1", label: "правило второе", children: <span>тело 1</span> },
        ]}
      />,
    );
    expect(screen.getByText("правило первое")).toBeInTheDocument();
    expect(screen.getByText("правило второе")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "снять" })).toBeInTheDocument();
    expect(screen.getByText("тело 1")).toBeInTheDocument();
    // Контроль в обратную сторону: свёрнутая панель тела не показывает.
    expect(screen.queryByText("тело 0")).toBeNull();
  });

  it("пустой набор панелей не рисует ни одного заголовка", () => {
    render(<S.Collapse items={[]} />);
    expect(screen.queryAllByRole("button", { expanded: false })).toHaveLength(0);
  });
});

describe("Statistic рисует подпись и значение", () => {
  it("оба видны", () => {
    render(<S.Statistic title="Сетей" value={7} />);
    expect(screen.getByText("Сетей")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
  });

  it("другое значение даёт другой текст — контроль в обратную сторону", () => {
    render(<S.Statistic title="Сетей" value="—" />);
    expect(screen.queryByText("7")).toBeNull();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});

describe("Badge рисует число по правилам настоящего", () => {
  it("число видно, перебор порога рисуется как «N+»", () => {
    const { rerender } = render(<S.Badge count={3} />);
    expect(screen.getByText("3")).toBeInTheDocument();
    rerender(<S.Badge count={150} overflowCount={99} />);
    expect(screen.getByText("99+")).toBeInTheDocument();
  });

  it("ноль скрыт, если не просили обратного — контроль в обратную сторону", () => {
    const { rerender } = render(<S.Badge count={0} />);
    expect(screen.queryByText("0")).toBeNull();
    rerender(<S.Badge count={0} showZero />);
    expect(screen.getByText("0")).toBeInTheDocument();
  });
});

describe("Spin рисует свою подпись загрузки", () => {
  it("подпись видна", () => {
    render(<S.Spin tip="Загрузка…" />);
    expect(screen.getByText("Загрузка…")).toBeInTheDocument();
  });

  it("без подписи текста нет — контроль в обратную сторону", () => {
    const { container } = render(<S.Spin />);
    expect(container.textContent).toBe("");
  });
});
