// Слоты шапки: страница объявляет свои действия/хлебные крошки/заголовок, а
// рисует их шапка приложения. Существенны две вещи: значение доезжает до слота
// и СНИМАЕТСЯ при уходе страницы — иначе кнопка предыдущего экрана остаётся в
// шапке следующего и действует на ресурс, которого на экране уже нет.
//
// КОНТРАКТ ХУКА, который проба соблюдает намеренно: узел обязан быть
// СТАБИЛЬНЫМ между рендерами (`useMemo` у вызывающего — так его зовут все
// страницы дерева). Узел, создаваемый заново на каждый рендер, меняет
// зависимость эффекта, эффект пишет в состояние провайдера, провайдер
// перерисовывает потомка — и цикл не сходится. Фикстура повторяет вызывающего,
// а не изобретает более суровый: иначе она краснела бы на свойстве, которого
// продукт не обещает.

import { useMemo } from "react";
import { render, screen } from "@testing-library/react";
import {
  HeaderBreadcrumbSlot,
  HeaderRightSlot,
  PageHeaderSlotProvider,
  PageTitleSlot,
  useBreadcrumb,
  useHeaderRight,
  usePageTitle,
} from "./PageHeaderSlot";

function Page({ title }: { title: string }) {
  useHeaderRight(useMemo(() => <button type="button">{`Создать ${title}`}</button>, [title]));
  useBreadcrumb(useMemo(() => <span>{`крошка ${title}`}</span>, [title]));
  usePageTitle(title);
  return <div>тело {title}</div>;
}

function Shell({ children }: { children?: React.ReactNode }) {
  return (
    <PageHeaderSlotProvider>
      <HeaderRightSlot />
      <HeaderBreadcrumbSlot />
      <PageTitleSlot />
      {children}
    </PageHeaderSlotProvider>
  );
}

describe("PageHeaderSlot", () => {
  it("доносит действия, крошки и заголовок страницы до шапки", () => {
    render(
      <Shell>
        <Page title="Подсети" />
      </Shell>,
    );
    expect(screen.getByRole("button", { name: "Создать Подсети" })).toBeInTheDocument();
    expect(screen.getByText("крошка Подсети")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Подсети" })).toBeInTheDocument();
  });

  it("снимает всё, когда страница уходит", () => {
    // Кнопка ушедшей страницы, оставшаяся в шапке, действует на ресурс, которого
    // на экране больше нет.
    const { rerender } = render(
      <Shell>
        <Page title="Подсети" />
      </Shell>,
    );
    expect(screen.getByRole("button", { name: "Создать Подсети" })).toBeInTheDocument();

    rerender(<Shell />);

    expect(screen.queryByRole("button", { name: "Создать Подсети" })).not.toBeInTheDocument();
    expect(screen.queryByText("крошка Подсети")).not.toBeInTheDocument();
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("сменившаяся страница вытесняет предыдущую, а не дополняет её", () => {
    const { rerender } = render(
      <Shell>
        <Page title="Подсети" />
      </Shell>,
    );

    rerender(
      <Shell>
        <Page title="Сети" />
      </Shell>,
    );

    expect(screen.getByRole("heading", { name: "Сети" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Создать Сети" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Создать Подсети" })).not.toBeInTheDocument();
  });

  it("без заголовка не рисует пустой заголовок", () => {
    render(<Shell />);
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("страница вправе объявить, что действий в шапке нет", () => {
    function Bare() {
      useHeaderRight(useMemo(() => null, []));
      return null;
    }
    render(
      <Shell>
        <Bare />
      </Shell>,
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
