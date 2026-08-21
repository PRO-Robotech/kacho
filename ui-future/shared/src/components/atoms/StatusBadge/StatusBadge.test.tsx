// Статус — единственное, что таблица говорит о здоровье ресурса, и говорит она
// это ТОНОМ. Оттенок берётся из таблицы соответствий, а незнакомый статус падает
// в «muted» — тот же оттенок, что у остановленного. Поэтому пропущенная строка
// таблицы не ломает ничего видимо: здоровый ресурс просто выглядит неактивным
// (так уже было с AVAILABLE у тома, см. комментарий в StatusBadge.tsx).
//
// Утверждается наблюдаемое: подпись после нормализации префикса и оттенок фона,
// потому что именно оттенок несёт смысл.

import { render, screen } from "@testing-library/react";
import { StatusBadge } from "./StatusBadge";

/** Фон пилюли — то, чем оттенок наблюдаем: класс он не меняет, только style. */
function toneBackgroundOf(label: string): string {
  return screen.getByText(label).style.background;
}

describe("StatusBadge", () => {
  it("снимает префикс STATUS_ и пишет статус с заглавной", () => {
    render(<StatusBadge state="STATUS_RUNNING" />);
    expect(screen.getByText("Running")).toBeInTheDocument();
  });

  it("снимает и легаси-префикс STATE_", () => {
    render(<StatusBadge state="STATE_RUNNING" />);
    expect(screen.getByText("Running")).toBeInTheDocument();
  });

  it("принимает статус без префикса как есть", () => {
    render(<StatusBadge state="ACTIVE" />);
    expect(screen.getByText("Active")).toBeInTheDocument();
  });

  it("на отсутствующем статусе рисует прочерк, а не пустую пилюлю", () => {
    render(<StatusBadge />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText("Undefined")).not.toBeInTheDocument();
  });

  it("даёт здоровым статусам оттенок ok — включая AVAILABLE", () => {
    render(
      <>
        <StatusBadge state="ACTIVE" />
        <StatusBadge state="AVAILABLE" />
        <StatusBadge state="STOPPED" />
      </>,
    );
    // AVAILABLE — свободный том, готовый к attach. Он обязан читаться как
    // здоровый, а не как остановленный: именно этим отличием проба и держит
    // строку таблицы, которой когда-то не было.
    expect(toneBackgroundOf("Available")).toBe("var(--status-ok-bg)");
    expect(toneBackgroundOf("Active")).toBe("var(--status-ok-bg)");
    expect(toneBackgroundOf("Stopped")).toBe("var(--status-muted-bg)");
    expect(toneBackgroundOf("Available")).not.toBe(toneBackgroundOf("Stopped"));
  });

  it("отличает ошибку, переходное состояние и занятость друг от друга", () => {
    render(
      <>
        <StatusBadge state="STATUS_ERROR" />
        <StatusBadge state="STATUS_CREATING" />
        <StatusBadge state="STATUS_DELETING" />
        <StatusBadge state="STATUS_IN_USE" />
      </>,
    );
    expect(toneBackgroundOf("Error")).toBe("var(--status-error-bg)");
    expect(toneBackgroundOf("Creating")).toBe("var(--status-info-bg)");
    expect(toneBackgroundOf("Deleting")).toBe("var(--status-warn-bg)");
    expect(toneBackgroundOf("In_use")).toBe("var(--status-violet-bg)");
  });

  it("незнакомый статус показывает, а не прячет", () => {
    // Контроль в обратную сторону к пробе оттенков: fallback обязан быть
    // «muted», иначе «все оттенки различны» означало бы лишь, что таблица
    // вообще не читается.
    render(<StatusBadge state="STATUS_SOMETHING_NEW" />);
    expect(screen.getByText("Something_new")).toBeInTheDocument();
    expect(toneBackgroundOf("Something_new")).toBe("var(--status-muted-bg)");
  });
});

// ФОРМА пилюли состояния — ОДНА на продукт.
//
// Прежде форму задавал набор Tailwind-классов прямо в этом атоме, и второй такой
// же набор жил в соседнем — признаке открытости площадки. Правка одного до
// другого не доезжала, и в одной карточке стояли две пилюли разной геометрии.
// Теперь форма объявлена здесь один раз (`statusPillShape`/`statusPillStyle`) и
// БЕРЁТСЯ соседом, а не повторяется им.
//
// Утверждается наблюдаемая ГЕОМЕТРИЯ обеих пилюль, а не факт импорта: импорт
// можно поставить и рядом дописать своё. Пара, а не одно утверждение: рядом
// стоит пилюля другого ТОНА — она обязана совпасть геометрией и разойтись
// заливкой. Без неё «геометрия совпала» было бы верно и для двух узлов, у
// которых не задано вообще ничего.
import { PlacementBadge } from "@shared/components/atoms/PlacementBadge";

/** Геометрия пилюли — то, что задаёт форму, без тона. */
function геометрия(узел: HTMLElement) {
  const s = узел.style;
  return {
    display: s.display,
    alignItems: s.alignItems,
    height: s.height,
    padding: s.padding,
    borderRadius: s.borderRadius,
    borderWidth: s.borderWidth,
    borderStyle: s.borderStyle,
    fontSize: s.fontSize,
    fontWeight: s.fontWeight,
    lineHeight: s.lineHeight,
    whiteSpace: s.whiteSpace,
  };
}

describe("StatusBadge — форма пилюли одна на продукт", () => {
  it("признак открытости площадки берёт форму у значка состояния, а не повторяет её", () => {
    render(
      <>
        <StatusBadge state="ACTIVE" />
        <PlacementBadge open={true} />
        <PlacementBadge open={false} />
      </>,
    );

    const статус = screen.getByText("Active");
    const открыт = screen.getByText("Открыт");
    const закрыт = screen.getByText("Закрыт");

    expect(геометрия(открыт)).toEqual(геометрия(статус));
    // Контроль: сравниваются не две пустоты — форма действительно объявлена.
    expect(геометрия(статус).height).toBe("20px");
    expect(геометрия(статус).borderRadius).toBe("6px");

    // Близнец: другой ТОН той же формы — геометрия та же, заливка другая.
    // Иначе равенство выше выполнялось бы и на двух узлах без единого свойства.
    expect(геометрия(закрыт)).toEqual(геометрия(статус));
    expect(закрыт.style.background).not.toBe(открыт.style.background);
  });
});
