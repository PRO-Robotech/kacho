// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Момент на экране «Безопасность» — локальным форматом «14 окт. 2026, 21:04»;
// полный момент (с секундами и поясом) — подсказкой (приёмка F8-S7, Р2).
//
// Момента нет — ничего не рисуется: пустого факта прочерком экран не
// показывает (`ui-no-field-without-source`); что значит отсутствие («ещё не
// использовалось»), решает тот, кто знает контракт ключа, а не формат.

function parts(iso: string): Date | null {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** «14 окт. 2026, 21:04» — в часовом поясе браузера. */
export function shortMoment(iso: string): string {
  const d = parts(iso);
  if (!d) return iso;
  const date = d
    .toLocaleDateString("ru-RU", { day: "numeric", month: "short", year: "numeric" })
    .replace(/\s*г\.$/, "");
  const time = d.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
  return `${date}, ${time}`;
}

/** Полный момент: дата, время до секунды и пояс. */
export function fullMoment(iso: string): string {
  const d = parts(iso);
  if (!d) return iso;
  return d.toLocaleString("ru-RU", {
    day: "numeric",
    month: "long",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
  });
}

export function Moment({ at }: { at: string }) {
  return (
    <time dateTime={at} title={fullMoment(at)}>
      {shortMoment(at)}
    </time>
  );
}
