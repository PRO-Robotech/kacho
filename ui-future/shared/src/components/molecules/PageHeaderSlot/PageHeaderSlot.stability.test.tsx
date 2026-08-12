// Узел, который слот кладёт в СОСТОЯНИЕ, обязан быть стабильным между рендерами.
//
// Цена нестабильности не «лишний рендер»: слот пишет узел через `setState` в
// эффекте с зависимостью от самого узла. Новый узел на каждом рендере ⇒ новый
// setState ⇒ новый рендер — цикл без выхода. Наблюдается он не как падение
// утверждения, а как ИСЧЕРПАНИЕ ПАМЯТИ прогона: суита умирает до отчёта, и
// вердикта нет ни у одной пробы. Это случилось дважды за один день, оба раза на
// панели правил группы безопасности, и оба раза выглядело как «тесты зависли».
//
// Проба закрепляет само свойство, а не конкретную страницу: она считает, сколько
// раз слот получил новый узел при неизменных входных данных.
import { useMemo, useState } from "react";
import { render, screen, act } from "@testing-library/react";
import { PageHeaderSlotProvider, useHeaderRight, HeaderRightSlot } from "./PageHeaderSlot";

/** Считает, сколько РАЗНЫХ узлов слот принял за жизнь пробы. */
const accepted: unknown[] = [];

function Consumer({ stable }: { stable: boolean }) {
  const [, force] = useState(0);
  // Стабильный узел — как это обязан делать вызывающий; нестабильный — как это
  // было в дефекте: новый JSX на каждом рендере.
  const node = useMemo(
    () => <button type="button">Действие</button>,
    // eslint-disable-next-line react-hooks/exhaustive-deps -- ветка «как в дефекте» намеренно пересоздаёт узел
    stable ? [] : [Math.random()],
  );
  if (!accepted.includes(node)) accepted.push(node);
  return (
    <>
      <button type="button" onClick={() => force((n) => n + 1)}>
        перерисовать
      </button>
      <Slot node={node} />
    </>
  );
}

function Slot({ node }: { node: React.ReactNode }) {
  useHeaderRight(node);
  return null;
}

function show(stable: boolean) {
  accepted.length = 0;
  return render(
    <PageHeaderSlotProvider>
      <HeaderRightSlot />
      <Consumer stable={stable} />
    </PageHeaderSlotProvider>,
  );
}

describe("PageHeaderSlot — узел в слоте обязан быть стабильным", () => {
  it("стабильный узел не пересоздаётся при перерисовке вызывающего", () => {
    show(true);
    const before = accepted.length;
    act(() => {
      screen.getByRole("button", { name: "перерисовать" }).click();
      screen.getByRole("button", { name: "перерисовать" }).click();
    });
    expect(accepted.length).toBe(before);
    expect(before).toBe(1);
  });

  it("нестабильный узел виден как РОСТ числа принятых узлов", () => {
    // Положительный контроль: без него первая проба зеленела бы и на слоте,
    // который вообще перестал принимать узлы, — то есть на сломанном слоте.
    show(false);
    const before = accepted.length;
    act(() => {
      screen.getByRole("button", { name: "перерисовать" }).click();
    });
    expect(accepted.length).toBeGreaterThan(before);
  });

  it("узел доезжает до места отрисовки", () => {
    // Третий контроль: обе пробы выше считают узлы, ни одна не утверждает, что
    // слот вообще что-то показывает.
    show(true);
    expect(screen.getByRole("button", { name: "Действие" })).toBeInTheDocument();
  });
});
