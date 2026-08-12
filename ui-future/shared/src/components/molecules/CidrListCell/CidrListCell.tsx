// CidrListCell — набор CIDR в ячейке таблицы: каждый префикс СВОЕЙ строкой,
// друг под другом, моноширинно.
//
// Свёртки «+N» здесь нет намеренно (решение владельца 2026-08-12). Она экономила
// высоту строки ценой того, ради чего в таблицу и смотрят: у подсети видимым
// оставался только основной блок, а дополнительные — числом «+2», из которого не
// узнать ни одного адреса; у сети — первые два из объявленного супернета.
// Пользователь всё равно шёл на карточку ресурса, то есть экономия обходилась
// лишним переходом на каждой строке.
import type { ReactNode } from "react";

interface Props {
  /** Префиксы в порядке показа; пустые строки и не-строки отбрасываются. */
  items: unknown[];
}

/** Собирает список из произвольных источников (primary + extra, v4 + v6). */
export function cidrItems(...sources: unknown[]): string[] {
  const out: string[] = [];
  for (const s of sources) {
    if (typeof s === "string") {
      if (s) out.push(s);
      continue;
    }
    if (Array.isArray(s)) {
      for (const v of s) if (typeof v === "string" && v) out.push(v);
    }
  }
  return out;
}

export function CidrListCell({ items }: Props): ReactNode {
  const list = cidrItems(...items);
  if (list.length === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 2, alignItems: "flex-start" }}>
      {list.map((c, i) => (
        <span key={`${c}-${i}`} className="font-mono text-xs">
          {c}
        </span>
      ))}
    </span>
  );
}
