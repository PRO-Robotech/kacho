// Тон факта на карточке адреса следует за СМЫСЛОМ, а не за истинностью.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОБА УТВЕРЖДАЛА ПРЕЖДЕ И ПОЧЕМУ ПЕРЕПИСАНА
//
// Предмет заводился по #446: владелец увидел, что «Используется ресурсом»
// выглядит неактивным (серым) рядом с включёнными соседями. Тогда выделение было
// одно на всю консоль (`accent` → `--kc-primary`), поэтому проба требовала, чтобы
// занятость была выделена ТАК ЖЕ, как соседние факты той же карточки, — и держала
// это равенство положительным контролем на «Защите от удаления».
//
// Владелец это правило отменил (канон §5): одинаковый тон у защиты и у занятости
// назван ПРИЗНАКОМ НАРУШЕНИЯ — охрана и задействованность выглядели одним и тем
// же событием. Теперь тон объявляется стороне по смыслу: занятость — `active`,
// защита — `good`, снятая защита — `attention`.
//
// Поэтому равенство сменилось на РАЗЛИЧИЕ, а исходная сила пробы сохранена
// целиком: «занят» по-прежнему обязан не быть приглушённым — ровно то, с чего
// #446 и начался.
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { detailExtension } from "./ResourceDetailExtensions";

/** Тоны канона §5. Дословно, а не «то, что вернул код»: они часть контракта. */
const TONE = {
  neutral: "var(--kc-text-tertiary)",
  active: "var(--kc-cyan)",
  good: "var(--status-ok-fg)",
  attention: "var(--status-warn-fg)",
} as const;

/** Нейтральный текст тише глифа — отдельный токен. */
const NEUTRAL_TEXT = "var(--kc-text-secondary)";

function rows(data: Record<string, unknown>) {
  const ext = detailExtension("addresses");
  if (!ext?.overviewExtra)
    throw new Error(
      "у карточки адреса нет строк обзора — предмет пробы отсутствует",
    );
  return ext.overviewExtra({ data, projectId: "prj-1" } as never);
}

function showRow(data: Record<string, unknown>, label: string) {
  const row = rows(data).find((r) => r.label === label);
  if (!row)
    throw new Error(
      `строки «${label}» на карточке адреса нет — предмет пробы отсутствует`,
    );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{row.value}</MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Цвет показанного текста — то, чем тон наблюдаем. Утверждается он, а не проп:
 *  проп можно передать и не отрисовать. */
function color(phrase: string): string {
  return screen.getByText(phrase).style.color;
}

const ADDRESS = {
  id: "adr-1",
  internal_ipv4_address: { address: "10.0.0.5", subnet_id: "sub-1" },
  used: true,
  reserved: true,
  deletion_protection: true,
};

describe("карточка адреса — тон факта следует за смыслом", () => {
  it("«Используется ресурсом» несёт тон задействованности, а не приглушение (#446)", () => {
    showRow(ADDRESS, "Занятость");

    expect(color("Используется ресурсом")).toBe(TONE.active);
    // Исходная жалоба дословно: «выглядит неактивным». Утверждение выше её
    // закрывает, но проверку на приглушение оставляем явной — она и есть предмет.
    expect(color("Используется ресурсом")).not.toBe(NEUTRAL_TEXT);
  });

  it("защита и занятость — РАЗНЫЕ тона: это разные события", () => {
    const { unmount } = showRow(ADDRESS, "Занятость");
    const occupancy = color("Используется ресурсом");
    unmount();

    showRow(ADDRESS, "Защита от удаления");
    const protection = color("Удаление запрещено");

    expect(protection).toBe(TONE.good);
    // Прежняя редакция требовала здесь РАВЕНСТВА. Канон §5 назвал равенство
    // признаком нарушения: одинаково окрашенные, охрана и занятость читались
    // одним событием.
    expect(protection).not.toBe(occupancy);
  });

  it("снятая защита — сторона, о которой стоит знать, и она окрашена", () => {
    showRow({ ...ADDRESS, deletion_protection: false }, "Защита от удаления");

    // «Удаление разрешено» приглушённым и было тем вторым признаком нарушения:
    // из двух сторон это единственная, о которой стоит знать.
    expect(color("Удаление разрешено")).toBe(TONE.attention);
  });

  it("тон принадлежит СТОРОНЕ, а не строке: свободный адрес тона занятости не получает", () => {
    // Парное отрицание к первому утверждению. Без него «занят выделен» зеленело
    // бы и на карточке, где выделены обе стороны подряд, — то есть на
    // противоположном дефекте, где цвет перестаёт что-либо означать.
    showRow({ ...ADDRESS, used: false }, "Занятость");

    expect(screen.getByText("Свободен")).toBeInTheDocument();
    expect(color("Свободен")).not.toBe(TONE.active);
  });
});
