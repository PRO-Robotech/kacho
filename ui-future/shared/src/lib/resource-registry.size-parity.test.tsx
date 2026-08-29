import { render } from "@testing-library/react";

import { REGISTRY } from "./resource-registry";
import { buildSpecColumns } from "./spec-columns";

/**
 * ОДИН ПРЕДМЕТ — ОДИН ВИД: `size_bytes` домена registry (issue #1509).
 *
 * ПРЕДМЕТ. Размер артефакта — одно поле одного домена, и соседние экраны
 * показывали его по-разному: репозиторий человекочитаемо (`SizeCell`:
 * 2048 → «2.0 KB», пусто/0 → «—»), тег — сырым `int64` строкой. Различие
 * никем не решалось: оно унаследовано модульным реестром и пережило перенос,
 * где его сохранили ДОСЛОВНО намеренно — правка поведения внутри переноса
 * сделала бы перенос непроверяемым сравнением. Правило 3 канона консоли.
 *
 * ЧТО УТВЕРЖДАЕТСЯ — НАБЛЮДАЕМОЕ, А НЕ РАЗМЕТКА. Колонка берётся так, как её
 * берёт страница (`buildSpecColumns`), и сверяется ТЕКСТ ячейки. Проба вида
 * «в спеке стоит SizeCell» пережила бы свой предмет: имя компонента сменится,
 * вид останется — либо наоборот.
 *
 * ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ОБЯЗАТЕЛЕН. Утверждение «виды не расходятся» зеленело
 * бы на двух пустых ячейках и на двух сырых числах, поэтому рядом стоит
 * утверждение о том, ЧТО ИМЕННО показано.
 *
 * ДИАГНОСТИКА В СРАВНИВАЕМОМ ЗНАЧЕНИИ. `expect` этого харнесса второго
 * аргумента-сообщения не принимает, поэтому сверяются подписанные строки:
 * падение называет спеку и увиденный текст, а не «true !== false».
 */

const SPECS = ["repositories", "tags"] as const;
const HEADER = "Размер";

/** Текст ячейки «Размер» указанной спеки для данной строки. */
function sizeCellText(specId: string, row: Record<string, unknown>): string {
  const spec = REGISTRY[specId];
  if (!spec) throw new Error(`в общем реестре нет спеки ${specId}`);
  const col = buildSpecColumns(spec).find((c) => c.header === HEADER);
  if (!col) throw new Error(`у спеки ${specId} нет колонки «${HEADER}» — предмет пробы исчез`);
  const { container, unmount } = render(<>{col.cell(row)}</>);
  const text = (container.textContent ?? "").trim();
  unmount();
  return text;
}

/** Подписанный снимок: «спека=показанный текст», по каждой спеке домена. */
function seen(row: Record<string, unknown>): string[] {
  return SPECS.map((id) => `${id}="${sizeCellText(id, row)}"`);
}

describe("registry: размер артефакта показывается ОДНИМ видом", () => {
  it("перепись: обе спеки домена несут колонку «Размер» — иначе сверять нечего", () => {
    expect(SPECS.map((id) => `${id}:${(REGISTRY[id]?.columns ?? []).some((c) => c.header === HEADER)}`))
      .toEqual(["repositories:true", "tags:true"]);
  });

  it.each([
    ["2048", "2.0 KB"],
    ["1073741824", "1.0 GB"],
  ])("байты %s читаются одинаково у репозитория и у тега", (raw, human) => {
    // Положительный контроль и утверждение о совпадении — ОДНО сравнение:
    // ожидаемое называет и вид, и то, что он одинаков у обеих спек.
    expect(seen({ size_bytes: raw })).toEqual(SPECS.map((id) => `${id}="${human}"`));
  });

  it.each([["пусто", ""], ["ноль", "0"], ["поля нет", undefined]])(
    "%s читается прочерком у обеих спек — никогда «0 B» и никогда сырым значением",
    (_label, raw) => {
      expect(seen({ size_bytes: raw })).toEqual(SPECS.map((id) => `${id}="—"`));
    },
  );
});
