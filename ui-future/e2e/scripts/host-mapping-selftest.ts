// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА ОТОБРАЖЕНИЯ ИМЕНИ СТЕНДА (#1750): отображение обязано закрывать
 * ОБЕ половины прогона — браузерную и путь запроса Node.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЗДЕСЬ НАСТОЯЩИЙ СЕРВЕР, А НЕ ОТВЕТ РЕЗОЛВЕРА
 *
 * «`dns.lookup` вернул адрес» — утверждение о резолвере, а предмет #1750 в
 * ДОСТАВКЕ: путь запроса умирал на `getaddrinfo ENOTFOUND`, и проверять надо,
 * дошёл ли запрос. Поэтому поднимается настоящий сервер на петле, и запрос идёт
 * по ОТОБРАЖАЕМОМУ имени сквозь тот же клиент Node, каким ходит
 * `APIRequestContext`.
 *
 * Сервер отдаёт обратно заголовок `Host` и путь — иначе «дошло» не отличалось бы
 * от «дошло не туда»: ingress стенда host-based, и подмена заголовка сломала бы
 * маршрутизацию молча, оставив проверку зелёной.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРОГОНОВ ТРИ, И ТРЕТИЙ ОБЯЗАТЕЛЕН
 *
 *   контроль  — отображение НЕ задано: имя не разрешается, путь запроса падает
 *               `ENOTFOUND`. Это и есть воспроизведение предмета #1750; без него
 *               второй прогон зеленел бы на машине, где имя разрешается и так;
 *   предмет   — отображение поставлено: путь запроса ДОХОДИТ, `Host` не подменён;
 *   сужение   — чужие имена не тронуты, повтор установки не наращивает цепочку.
 *
 * Третий прогон не украшение: подмена, широкая по имени, увела бы в стенд
 * обращения, к стенду не относящиеся, и отказ выглядел бы дефектом продукта. А
 * конфигурация проб загружается КАЖДЫМ рабочим процессом, поэтому установка
 * происходит многократно — без пометки каждая следующая оборачивала бы
 * предыдущую на всю длину прогона.
 *
 * Гоняется ГОЛЫМ `node`, без зависимостей и без стенда: секунды, и потому до
 * подъёма — как соседняя самопроверка решения о браузере.
 */

import dns from "node:dns";
import http from "node:http";

import { installHostMapping } from "../host-mapping.ts";

/** Имя берётся заведомо нерезолвимое: на живом имени контроль был бы зелёным даром. */
const NAME = "console.kacho.selftest.invalid";

let failed = 0;
function check(condition: boolean, what: string): void {
  if (condition) {
    console.log(`  ok — ${what}`);
    return;
  }
  console.log(`  ПРОВАЛ — ${what}`);
  failed++;
}

function lookup(host: string, options?: object): Promise<{ err: unknown; addr: unknown; family?: number }> {
  return new Promise((resolve) => {
    const done = (err: unknown, addr: unknown, family?: number) => resolve({ err, addr, family });
    if (options) dns.lookup(host, options as never, done as never);
    else dns.lookup(host, done as never);
  });
}

async function main(): Promise<void> {
  const server = http.createServer((req, res) => {
    res.end(JSON.stringify({ host: req.headers.host, path: req.url }));
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  const port = (server.address() as { port: number }).port;

  console.log("ПРОГОН 1 — КОНТРОЛЬ: отображение не задано");
  {
    check(installHostMapping(NAME, undefined) === null,
      "без адреса отображение НЕ ставится и это законный исход, а не отказ: " +
        "на внешнем стенде имя разрешается по-настоящему");
    const r = await lookup(NAME);
    check(!!r.err, `имя не разрешается: ${(r.err as { code?: string })?.code}`);
    let refused = false;
    try {
      await fetch(`http://${NAME}:${port}/iam/v1/projects`);
    } catch (e) {
      refused = JSON.stringify(e instanceof Error ? (e as Error & { cause?: unknown }).cause ?? e.message : e)
        .includes("ENOTFOUND");
    }
    check(refused,
      "путь запроса падает ENOTFOUND — ПРЕДМЕТ #1750 воспроизведён; без этого " +
        "прогон 2 зеленел бы на машине, где имя разрешается и так");
  }

  console.log("ПРОГОН 2 — ПРЕДМЕТ: отображение поставлено");
  {
    const m = installHostMapping(NAME, "127.0.0.1");
    check(m?.host === NAME && m?.ip === "127.0.0.1", "отображение объявлено");
    const r = await lookup(NAME);
    check(!r.err && r.addr === "127.0.0.1" && r.family === 4, `dns.lookup → ${String(r.addr)}`);
    const all = await lookup(NAME, { all: true });
    check(Array.isArray(all.addr) && (all.addr as Array<{ address: string }>)[0]?.address === "127.0.0.1",
      "форма all:true отдаёт МАССИВ, а не тройку: одна форма на оба вопроса " +
        "разобралась бы у вызывающего в undefined, и отказ выглядел бы отказом сети");
    const res = await fetch(`http://${NAME}:${port}/iam/v1/projects`);
    const body = (await res.json()) as { host: string; path: string };
    check(res.ok, "путь запроса ДОШЁЛ до сервера");
    check(body.host === `${NAME}:${port}`,
      `заголовок Host НЕ подменён (${body.host}) — ingress стенда host-based, ` +
        "и подмена сломала бы маршрутизацию молча");
    check(body.path === "/iam/v1/projects", `путь не тронут (${body.path})`);
  }

  console.log("ПРОГОН 3 — СУЖЕНИЕ: чужое имя не тронуто, повтор не наращивает цепочку");
  {
    const local = await lookup("localhost");
    check(!local.err && (local.addr === "127.0.0.1" || local.addr === "::1"),
      `localhost разрешает ПРЕЖНИЙ резолвер: ${String(local.addr)}`);
    const other = await lookup("nothing.kacho.selftest.invalid");
    check(!!other.err,
      "чужое несуществующее имя по-прежнему не разрешается — подмена не широкая");
    const before = dns.lookup;
    installHostMapping(NAME, "127.0.0.1");
    installHostMapping(NAME, "127.0.0.1");
    check(dns.lookup === before,
      "повторная установка НЕ обернула подмену второй раз: конфигурация грузится " +
        "каждым рабочим процессом, и цепочка росла бы на всю длину прогона");
  }

  server.close();
  if (failed > 0) {
    console.error(`\nсамопроверка отображения имени: провалов ${failed}`);
    process.exit(1);
  }
  console.log("\nсамопроверка отображения имени: все утверждения прошли");
}

await main();
