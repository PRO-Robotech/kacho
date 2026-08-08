// Иконка ресурса — декорация рядом с названием в боковом меню, шапке формы и
// шапке карточки. Компонент отвечает за три вещи: значок есть ВСЕГДА (пустое
// место сдвигает заголовок), он скрыт от средства чтения с экрана (озвученный —
// удваивал бы имя ресурса в каждой шапке), и класс вызывающего доезжает.
//
// ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ. Утверждения «разным ресурсам — разные значки» тут
// быть не может: заменитель иконок (`src/test/antd-icons-stub.tsx`) рисует ВСЕ
// глифы одним и тем же пустым узлом, поэтому любая такая проба была бы зелёной
// при любой карте — форма без содержания. Соответствие ключей карты
// идентификаторам спек дерева держит `ResourceIcon.registry.test.ts`: он читает
// карту и реестры и краснеет на ключе-сироте и на выдуманном ключе.

import { render } from "@testing-library/react";
import { ResourceIcon } from "./ResourceIcon";

function iconRoot(specId: string, className?: string): HTMLElement {
  const { container } = render(<ResourceIcon specId={specId} className={className} />);
  return container.firstElementChild as HTMLElement;
}

describe("ResourceIcon", () => {
  it("рисует ровно один узел значка", () => {
    const root = iconRoot("networks");
    expect(root.tagName).toBe("SPAN");
    expect(root.children).toHaveLength(1);
  });

  it("скрывает значок от средства чтения с экрана", () => {
    expect(iconRoot("networks")).toHaveAttribute("aria-hidden");
  });

  it("незнакомый идентификатор всё равно даёт узел, а не пустоту", () => {
    // Отсутствие узла сдвинуло бы заголовок ресурса относительно соседних.
    const root = iconRoot("нет-такого-ресурса");
    expect(root.children).toHaveLength(1);
  });

  it("класс вызывающего доезжает до узла", () => {
    expect(iconRoot("networks", "mr-2").className).toBe("mr-2");
  });

  it("без класса не выдумывает своего", () => {
    expect(iconRoot("networks").className).toBe("");
  });
});
