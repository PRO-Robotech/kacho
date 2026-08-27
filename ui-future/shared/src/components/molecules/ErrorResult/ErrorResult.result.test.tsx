// Экран отказа НА ОБЩЕМ ЗАМЕНИТЕЛЕ — то есть на том, чем `Result` подменён во
// всех прочих пробах дерева.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ФАЙЛ, когда рядом лежит `ErrorResult.test.tsx`. Тот
// переопределяет `antd` СВОИМ моком (`jest.unstable_mockModule`) и потому об
// общем заменителе не говорит ничего: вернуть `Result` в проходной <div> —
// он останется зелёным. Его собственный мок к тому же изобретает
// `data-status` и `data-testid="subtitle"`, которых настоящий antd не
// производит НИ ОДНОГО, — утверждения там прибиты к форме дублёра (#588).
// Здесь наоборот: заменитель общий, а утверждения — о том, что видит
// оператор, и о той форме, которую производит настоящий antd.
//
// ЧТО ИМЕННО ДЕРЖИТСЯ. `Result` получает видимое оператору не детьми, а
// пропами `title`/`subTitle`/`extra`: проходной <div> роняет их в атрибуты, и
// экран отказа становится пустым — при том что проба, читающая только код
// состояния, осталась бы зелёной. Инъекция в обе стороны: `Result: Component`
// в заменителе — красное по каждому утверждению ниже; заменитель на месте —
// молчание.
//
// ВИД ИСХОДА настоящий `Result` выражает КЛАССОМ корня (`ant-result-404`), а не
// атрибутом `status`: атрибут — проп виджета. Поэтому здесь читается класс —
// это форма настоящего, а не выдумка заменителя.

import { render, screen } from "@testing-library/react";

import { ApiError } from "@shared/api/client";
import { NOT_FOUND_IS_AMBIGUOUS } from "@shared/lib/error-presentation";

import { ErrorResult } from "./ErrorResult";

function root(): HTMLElement {
  const el = document.querySelector(".ant-result");
  if (el === null) throw new Error("корень Result не найден: заменитель его не рисует");
  return el as HTMLElement;
}

describe("ErrorResult — экран отказа на общем заменителе antd", () => {
  it("на промахе показывает текст сервера дословно и оговорку о неоднозначности", () => {
    render(<ErrorResult error={new ApiError(404, 5, null, "Subnet sub-1 not found")} />);

    // Тон сообщений — часть контракта: текст сервера идёт на экран дословно.
    expect(screen.getByText("Subnet sub-1 not found")).toBeInTheDocument();
    expect(screen.getByText(NOT_FOUND_IS_AMBIGUOUS)).toBeInTheDocument();
    expect(root().className).toContain("ant-result-404");
  });

  it("на отказе в правах текст сервера виден, а оговорки нет — контроль в обратную сторону", () => {
    // Без этой половины утверждение «оговорка видна» зеленело бы на экране,
    // который не показывает ВООБЩЕ ничего.
    render(<ErrorResult error={new ApiError(403, 7, null, "no path")} />);

    expect(screen.getByText("no path")).toBeInTheDocument();
    expect(screen.queryByText(NOT_FOUND_IS_AMBIGUOUS)).toBeNull();
    expect(root().className).toContain("ant-result-403");
  });

  it("заголовок виден на экране, а не только в пропе", () => {
    render(<ErrorResult error={new ApiError(404, 5, null, "x")} title="Нет такой страницы" />);

    expect(screen.getByText("Нет такой страницы")).toBeInTheDocument();
  });

  it("действия вызывающего доходят до экрана и нажимаются", () => {
    render(
      <ErrorResult error={new ApiError(403, 7, null, "no path")} extra={<button type="button">К списку</button>} />,
    );

    expect(screen.getByRole("button", { name: "К списку" })).toBeInTheDocument();
  });
});
