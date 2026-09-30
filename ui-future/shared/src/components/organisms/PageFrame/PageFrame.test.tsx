// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { render, screen } from "@testing-library/react";
import { PageFrame } from "./PageFrame";

// Модульная проба рамки: РАСКЛАДКИ jsdom не считает, поэтому здесь судится
// устройство, на котором стоит наблюдаемое, — одна область прокрутки и шапка вне
// её. Само наблюдаемое (колесо доводит кнопку до вида, шапка стоит) держит
// браузерная проба `e2e/specs/account-settings.spec.ts`.

const scrollAreas = (root: HTMLElement) =>
  [root, ...root.querySelectorAll<HTMLElement>("*")].filter((el) => /^(auto|scroll)$/.test(el.style.overflowY));

describe("PageFrame · шапка стоит, прокрутка одна", () => {
  it("содержимое — в единственной области прокрутки, шапка — вне её", () => {
    const { container } = render(
      <PageFrame head={<h1>Шапка</h1>}>
        <p>Содержимое</p>
      </PageFrame>,
    );
    const areas = scrollAreas(container);
    expect(areas).toHaveLength(1);
    expect(areas[0]).toContainElement(screen.getByText("Содержимое"));
    expect(areas[0]).not.toContainElement(screen.getByRole("heading", { name: "Шапка" }));
  });

  it("без шапки — та же единственная область прокрутки", () => {
    const { container } = render(
      <PageFrame>
        <p>Содержимое</p>
      </PageFrame>,
    );
    const areas = scrollAreas(container);
    expect(areas).toHaveLength(1);
    expect(areas[0]).toContainElement(screen.getByText("Содержимое"));
  });

  it("рамка сама не прокручивается и заполняет рабочую область", () => {
    const { container } = render(
      <PageFrame head={<h1>Шапка</h1>}>
        <p>Содержимое</p>
      </PageFrame>,
    );
    const frame = container.firstElementChild as HTMLElement;
    expect(frame.style.overflow).toBe("hidden");
    expect(frame.style.flex).toMatch(/^1/);
  });
});
