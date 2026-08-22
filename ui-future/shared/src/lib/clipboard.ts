// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Копирование в буфер — ОДНО на всю консоль.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ ОБЩИЙ ПОМОЩНИК, ЕСЛИ ЭТО ОДНА СТРОКА
 *
 * Одна строка — только в защищённом контексте. `navigator.clipboard` объявлен
 * доступным ТОЛЬКО при `https://` либо на `localhost`/`127.0.0.1`; по обычному
 * `http://` на любом другом имени его нет ВОВСЕ — не «он отказывает», а
 * `navigator.clipboard` равен `undefined`, и обращение к его методу роняет
 * обработчик до всякого `catch`.
 *
 * Это не теоретический случай: конвейер поднимает консоль как
 * `http://console.kacho.local:<порт>`, и внутренний стенд, открытый по имени
 * хоста, устроен так же. То есть копирование не работало ни в одном из десяти
 * мест, где оно объявлено, — и падало молча, потому что `TypeError` в
 * обработчике события никуда не всплывает.
 *
 * Локально этого не видно: `localhost` защищённым контекстом СЧИТАЕТСЯ.
 *
 * ЗАПАСНОЙ ПУТЬ — НЕ УСТАРЕВШИЙ ХЛАМ, А ЕДИНСТВЕННОЕ, ЧТО ТАМ РАБОТАЕТ
 *
 * `document.execCommand("copy")` объявлен устаревшим, но остаётся исполнимым во
 * всех браузерах и не требует защищённого контекста. Он и стоит второй ветвью:
 * сперва современный путь, при его отсутствии — этот.
 *
 * ИСХОД ВОЗВРАЩАЕТСЯ, А НЕ ГЛОТАЕТСЯ. Вызывающий обязан знать, легло ли
 * значение в буфер: подпись «Скопировано» над пустым буфером — обещание, которое
 * продукт не выполнил, и человек узнаёт об этом, вставив не то.
 */
export async function copyText(value: string): Promise<boolean> {
  // Современный путь — там, где он есть.
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      // Отказ разрешения или занятый буфер — падать на этом нельзя, ниже есть
      // второй путь, который в этой же среде обычно работает.
    }
  }
  return copyViaSelection(value);
}

/**
 * Запасной путь: временное поле, выделение, `execCommand`.
 *
 * Поле уводится за пределы видимой области, а не прячется `display: none`:
 * невидимый узел нельзя выделить, и команда копирования не сработает. Оно же
 * помечено `readonly` и `aria-hidden`, чтобы не попадать ни в обход клавишей,
 * ни в чтение с экрана.
 */
function copyViaSelection(value: string): boolean {
  if (typeof document === "undefined") return false;
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.setAttribute("aria-hidden", "true");
  textarea.tabIndex = -1;
  textarea.style.position = "fixed";
  textarea.style.top = "-1000px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  try {
    textarea.select();
    textarea.setSelectionRange(0, value.length);
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    textarea.remove();
  }
}
