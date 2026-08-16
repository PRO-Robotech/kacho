// Тон статуса берётся из ЕДИНОГО набора (issue #405).
//
// Копия значка в модуле отстала от общей: в ней не было `MIGRATING`, поэтому
// переезжающий том падал в запасной тон `muted` — тот же, которым помечены
// «остановлен» и «освобождён». То есть живой, наблюдаемый процесс выглядел
// неактивным. Модуль compute показывает тома (`volumes` есть в его реестре),
// значит расхождение видел оператор, а не только дерево.
//
// Проба закрепляет ИСХОД (какой тон получает значение), а не факт наличия
// файла: сведение к общей реализации проверяемо ровно этим.

import { render, screen } from "@testing-library/react";
import { StatusBadge } from "./index";

/** Значок печатает значение с заглавной: `MIGRATING` → `Migrating`. */
function labelOf(raw: string): string {
  return raw.charAt(0) + raw.slice(1).toLowerCase();
}

/** Фон тона читается из inline-стиля: тон задаётся CSS-переменной. */
function toneOf(raw: string): string {
  return screen.getByText(labelOf(raw)).getAttribute("style") ?? "";
}

describe("StatusBadge — тон статуса", () => {
  it("MIGRATING — тон «в процессе», а не приглушённый", () => {
    render(<StatusBadge state="MIGRATING" />);
    // Положительный контроль формы: значение вообще отрисовано.
    expect(screen.getByText(labelOf("MIGRATING"))).toBeTruthy();
    expect(toneOf("MIGRATING")).toContain("--status-info-bg");
    // Отрицание в паре: приглушённый тон — запасной путь для НЕИЗВЕСТНОГО
    // значения, и попадание в него означает, что набор снова отстал.
    expect(toneOf("MIGRATING")).not.toContain("--status-muted-bg");
  });

  it("неизвестное значение по-прежнему падает в приглушённый тон", () => {
    // Различающий контроль: без него утверждение выше зеленело бы на наборе,
    // где ВСЁ отдаёт «в процессе».
    render(<StatusBadge state="WATISTHIS" />);
    expect(toneOf("WATISTHIS")).toContain("--status-muted-bg");
  });

  it("здоровый и ошибочный статусы различимы", () => {
    render(<StatusBadge state="RUNNING" />);
    render(<StatusBadge state="ERROR" />);
    expect(toneOf("RUNNING")).toContain("--status-ok-bg");
    expect(toneOf("ERROR")).toContain("--status-error-bg");
  });
});
