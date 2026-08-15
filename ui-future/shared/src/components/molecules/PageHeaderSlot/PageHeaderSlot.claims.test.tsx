// Слот шапки: «мне нечего поставить» — это НЕ «сотри чужое».
//
// Класс. Слот один на страницу, а писателей в него на карточке ресурса двое:
// оболочка карточки и содержимое открытой вкладки. Порядок между ними задаёт
// React — эффект родителя выполняется ПОСЛЕ эффекта потомка, — поэтому родитель
// с пустыми руками затирал написанное потомком.
//
// Проявлялось не всегда, и это самое неприятное: при переключении вкладки ВНУТРИ
// карточки узел родителя не менялся, эффект не перезапускался и запись потомка
// доживала до экрана. При заходе ПРЯМЫМ адресом вкладки родитель монтировался
// заново, эффект отрабатывал последним — и действия вкладки исчезали. Ссылка на
// вкладку из документации, из чужого сообщения, из закладки открывала страницу,
// на которой нечем сделать то, ради чего вкладка и открыта (#425).
//
// Норма. Каждый писатель держит СВОЮ запись. Пустая запись означает «у меня для
// этой шапки ничего нет» и снимает только собственную заявку. Показывается
// последняя непустая — то есть при двух непустых по-прежнему побеждает внешний
// писатель, как было и раньше: изменена ровно семантика ПУСТОГО.
import { useMemo, useState, type ReactNode } from "react";
import { render, screen, act } from "@testing-library/react";
import { PageHeaderSlotProvider, useHeaderRight, HeaderRightSlot } from "./PageHeaderSlot";

function Writer({ node, children }: { node: ReactNode | null; children?: ReactNode }) {
  useHeaderRight(node);
  return <>{children}</>;
}

/** Кнопка-узел, стабильная между рендерами (иначе слот входит в цикл — см. соседнюю пробу). */
function useAction(label: string | null) {
  return useMemo(() => (label ? <button type="button">{label}</button> : null), [label]);
}

function Card({ parent, child }: { parent: string | null; child: string | null }) {
  const parentNode = useAction(parent);
  const childNode = useAction(child);
  return (
    <Writer node={parentNode}>
      <Writer node={childNode} />
    </Writer>
  );
}

function show(ui: ReactNode) {
  return render(
    <PageHeaderSlotProvider>
      <HeaderRightSlot />
      {ui}
    </PageHeaderSlotProvider>,
  );
}

describe("слот шапки — пустая заявка не стирает чужую", () => {
  it("родитель без действий оставляет на экране действия вкладки", () => {
    // Ровно случай #425: оболочка карточки для этой вкладки действий не даёт,
    // действия даёт сама вкладка. Оба монтируются одновременно — так выглядит
    // заход прямым адресом.
    show(<Card parent={null} child="Добавить правило" />);

    expect(screen.getByRole("button", { name: "Добавить правило" })).toBeInTheDocument();
  });

  it("при двух непустых заявках побеждает внешний писатель (порядок не изменён)", () => {
    // Положительный контроль в другую сторону: правка касается ТОЛЬКО пустого
    // значения. Без этой пробы «починка» могла бы незаметно перевернуть
    // приоритет там, где на него уже полагаются — например у списка
    // пользователей, который ставит свою кнопку поверх общей страницы.
    show(<Card parent="Пригласить" child="Добавить правило" />);

    expect(screen.getByRole("button", { name: "Пригласить" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Добавить правило" })).toBeNull();
  });

  it("ушедшая вкладка уносит свои действия, а не оставляет их висеть", () => {
    // Обратная сторона той же правки: если снятая заявка не убирается, кнопки
    // предыдущей вкладки останутся в шапке следующей — и нажатие уедет в
    // ресурс, которого на экране уже нет.
    function Switcher() {
      const [open, setOpen] = useState(true);
      const parentNode = useAction(null);
      const childNode = useAction("Добавить правило");
      return (
        <>
          <button type="button" onClick={() => setOpen(false)}>
            закрыть вкладку
          </button>
          <Writer node={parentNode}>{open ? <Writer node={childNode} /> : null}</Writer>
        </>
      );
    }

    show(<Switcher />);
    expect(screen.getByRole("button", { name: "Добавить правило" })).toBeInTheDocument();

    act(() => {
      screen.getByRole("button", { name: "закрыть вкладку" }).click();
    });
    expect(screen.queryByRole("button", { name: "Добавить правило" })).toBeNull();
  });

  it("пустая заявка одного не мешает пустоте всей шапки (отрицательный контроль)", () => {
    // Без него первые пробы зеленели бы и на слоте, который просто перестал
    // что-либо стирать и показывает последнее увиденное навсегда.
    show(<Card parent={null} child={null} />);

    expect(screen.queryByRole("button")).toBeNull();
  });
});
