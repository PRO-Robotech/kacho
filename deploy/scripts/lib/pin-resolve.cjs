// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// pin-resolve.cjs — пин имени на петлю для процесса node (newman), форма
// `curl --resolve <имя>:<порт>:127.0.0.1` (kacho#3315).
//
// ЗАЧЕМ. Собственный фронт службы доступа пробрасывается на петлю, а его лист
// несёт в SAN только DNS-имена Service. Адрес вида https://127.0.0.1:<порт>
// проверку имени не проходит, и newman отдаёт `errored` вместо вердикта.
// Ослабить проверку (`--insecure`, NODE_TLS_REJECT_UNAUTHORIZED=0) — значит
// мерить не тот предмет. Поэтому адрес проб строится ОТ ИМЕНИ из SAN, а имя
// разрешается в петлю здесь: TLS-клиент проверяет цепочку и имя как обычно,
// подменён лишь ответ разрешения имени.
//
// ВХОД — KACHO_PIN_RESOLVE: «имя=адрес» через запятую. Адрес — ТОЛЬКО петля
// (127.0.0.1 либо ::1): пин уводит имя на проброс этой машины и никуда больше;
// иное — отказ при загрузке, а не молчаливое перенаправление.
// Подключается `NODE_OPTIONS=--require <этот файл>`. Перехватывается
// `dns.lookup` — им разрешает имена исполнитель запросов newman
// (postman-runtime, lib/requester/core.js) и `net` по умолчанию.
'use strict';

const dns = require('dns');

const raw = process.env.KACHO_PIN_RESOLVE || '';
const pins = new Map();
for (const item of raw.split(',').map((s) => s.trim()).filter(Boolean)) {
  const at = item.lastIndexOf('=');
  if (at <= 0) {
    throw new Error(`KACHO_PIN_RESOLVE: «${item}» не по форме «имя=адрес»`);
  }
  const name = item.slice(0, at).toLowerCase();
  const addr = item.slice(at + 1);
  if (addr !== '127.0.0.1' && addr !== '::1') {
    throw new Error(`KACHO_PIN_RESOLVE: имя «${name}» пинится на «${addr}» — допустима лишь петля`);
  }
  pins.set(name, addr);
}

if (pins.size > 0) {
  const original = dns.lookup;
  dns.lookup = function pinnedLookup(hostname, options, callback) {
    let opts = options;
    let cb = callback;
    if (typeof opts === 'function') {
      cb = opts;
      opts = {};
    } else if (typeof opts === 'number') {
      opts = { family: opts };
    }
    const addr = typeof hostname === 'string' ? pins.get(hostname.toLowerCase()) : undefined;
    if (!addr) {
      return original.call(dns, hostname, options, callback);
    }
    const family = addr.includes(':') ? 6 : 4;
    process.nextTick(() => {
      if (opts && opts.all) {
        cb(null, [{ address: addr, family }]);
      } else {
        cb(null, addr, family);
      }
    });
    return {};
  };
}
