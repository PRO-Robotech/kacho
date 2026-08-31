// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import dns from "node:dns";

/**
 * ОТОБРАЖЕНИЕ ИМЕНИ СТЕНДА — ОБЕ ПОЛОВИНЫ ПРОГОНА, А НЕ ОДНА (#1750).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ: У ПРОБЫ КОНСОЛИ ДВА КЛИЕНТА, И РЕЗОЛВЕР У НИХ РАЗНЫЙ
 *
 * Проба ходит на стенд ДВУМЯ путями, и это не деталь, а устройство:
 *
 *   браузер      — `page.goto`, загрузка модулей, запросы самой консоли. Имя
 *                  разрешает резолвер Chromium;
 *   путь запроса — `page.request.*` (`APIRequestContext`). Он исполняется
 *                  В ПРОЦЕССЕ NODE, а не в браузере, и имя разрешает Node.
 *
 * `--host-resolver-rules` — флаг Chromium. Он закрывает ПЕРВУЮ половину и о
 * второй не знает by construction. Пока имя стоит в `/etc/hosts` ранера, разрыв
 * не виден никогда: обе половины разрешают имя, каждая своим способом.
 *
 * НАБЛЮДАЛОСЬ (#1750): на стенде, где имя разрешается ТОЛЬКО отображением,
 * КАЖДАЯ проба, зовущая `registerAndSignIn`, умирала на
 * `getaddrinfo ENOTFOUND console.kacho.local` — то есть ни одна не доходила до
 * продукта, и «не выполнилось» подавалось как красное. Ровно тот класс, из
 * которого выведены #935 и #985, и проявлялся он ровно там, ради чего ручка
 * заведена.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ПОДМЕНА `dns.lookup`, А НЕ ЗАПИСЬ В `/etc/hosts`
 *
 * Запись в `/etc/hosts` — действие над МАШИНОЙ, и прогон проб на неё не вправе:
 * нужен корень, а след переживает прогон. Подмена резолвера живёт ровно столько,
 * сколько процесс, и видна только ему.
 *
 * ПОЧЕМУ ИМЕННО `dns.lookup`, А НЕ `dns.resolve`. Клиент Node (`http`/`https`,
 * через `net.Socket.connect`) разрешает имя `dns.lookup` — это единственная
 * точка, через которую проходит путь запроса. `dns.resolve*` ходит к серверу имён
 * напрямую и `/etc/hosts` не читает вовсе, поэтому подменять его значило бы
 * подменять то, чем никто здесь не пользуется.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * СУЖЕНИЕ — ЧАСТЬ СВОЙСТВА, А НЕ ОСТОРОЖНОСТЬ
 *
 * Отображается РОВНО ОДНО имя — то, что названо адресом стенда. Всякое другое
 * уходит прежнему резолверу нетронутым. Это не перестраховка: подмена, широкая
 * по имени, увела бы в стенд обращения, к стенду не относящиеся (реестр модулей,
 * сборщик отчёта), и отказ выглядел бы дефектом продукта.
 *
 * ПОВТОРНЫЙ ВЫЗОВ БЕЗВРЕДЕН by construction: подмена ставится один раз и помечает
 * себя. Конфигурация проб загружается КАЖДЫМ рабочим процессом, поэтому вызов
 * происходит столько раз, сколько процессов, — и без пометки каждая следующая
 * подмена оборачивала бы предыдущую, наращивая цепочку на всю длину прогона.
 */

/** Пометка на подменённой функции: подмена ставится один раз на процесс. */
const INSTALLED = Symbol.for("kacho.console.e2e.hostMapping");

/** Форма `dns.lookup` ровно в том виде, в каком её зовёт клиент Node. */
type Lookup = typeof dns.lookup;

export interface HostMapping {
  /** Имя, которое отображается. */
  host: string;
  /** Адрес, на который оно отображается. */
  ip: string;
}

/**
 * installHostMapping — отобразить ОДНО имя на адрес для пути запроса Node.
 *
 * Возвращает поставленное отображение либо `null`, если ставить нечего
 * (отображение не задано). `null` — это законный исход, а не отказ: на внешнем
 * стенде имя разрешается по-настоящему.
 */
export function installHostMapping(host: string, ip: string | undefined): HostMapping | null {
  if (!ip) return null;
  if (!host) {
    throw new Error(
      "installHostMapping: имя стенда пусто при заданном адресе отображения. " +
        "Отображение в никуда увело бы путь запроса на чужой адрес молча.",
    );
  }

  const current = dns.lookup as Lookup & { [INSTALLED]?: HostMapping[] };
  const table = current[INSTALLED];
  if (table) {
    // Подмена уже стоит: дописываем в её таблицу, а не оборачиваем повторно.
    if (!table.some((e) => e.host === host && e.ip === ip)) table.push({ host, ip });
    return { host, ip };
  }

  const original = current as Lookup;
  const mapped: HostMapping[] = [{ host, ip }];

  // Сигнатура `dns.lookup` перегружена (с настройками и без), поэтому
  // разбираются ОБЕ формы: подмена, знающая одну, роняла бы вторую — а какая из
  // них придёт, решает вызывающий, а не мы.
  const patched = ((
    hostname: string,
    options: unknown,
    callback?: (...args: unknown[]) => void,
  ) => {
    const done = (typeof options === "function" ? options : callback) as
      | ((...args: unknown[]) => void)
      | undefined;
    const entry = mapped.find((e) => e.host === hostname);
    if (entry && done) {
      const family = entry.ip.includes(":") ? 6 : 4;
      const all =
        typeof options === "object" && options !== null && (options as { all?: boolean }).all;
      // Ответ отдаётся В ТОЙ ЖЕ ФОРМЕ, в какой его просили: `all: true` ждёт
      // МАССИВ записей, прочие — тройку. Одна форма на оба вопроса разобралась
      // бы у вызывающего в `undefined`, и отказ выглядел бы отказом сети.
      queueMicrotask(() =>
        all
          ? done(null, [{ address: entry.ip, family }])
          : done(null, entry.ip, family),
      );
      return;
    }
    return (original as unknown as (...a: unknown[]) => unknown)(
      hostname,
      options,
      callback as never,
    );
  }) as Lookup & { [INSTALLED]?: HostMapping[] };

  patched[INSTALLED] = mapped;
  // `dns.promises.lookup` — отдельная реализация, и путь запроса Node её не
  // зовёт; подменяется ровно то, через что путь проходит.
  (dns as unknown as { lookup: Lookup }).lookup = patched;
  return { host, ip };
}
