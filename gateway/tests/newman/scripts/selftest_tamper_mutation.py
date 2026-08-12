#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: подстановка кейса о ПОРЧЕ ПОДПИСИ меняет подпись В БАЙТАХ, а не в строке.

ПРЕДМЕТ. Кейс `IBT-10-RS256-TAMPERED-SIGNATURE-REJECTED` берёт подлинного
предъявителя, портит его подпись и требует от края `401`. Утверждение держится
на одном допущении: отправленная подпись действительно ОТЛИЧАЕТСЯ от подлинной.
Допущение проверялось сравнением СТРОК — а в base64url разные строки бывают
одними и теми же байтами.

Значащи все шесть бит каждого символа, кроме ПОСЛЕДНЕГО символа кодировки: у
него часть младших бит — заполнение, и не-strict разборщик (то есть почти
любой) их игнорирует. Сколько бит — зависит от длины: подпись 512 байт (RS256
на ключе 4096 бит — ровно то, что чеканит стенд) оставляет 2 бита заполнения,
256 байт — 4. Значит у терминального символа есть соседи по значению.

ЧТО ЭТО СТОИЛО. Прежняя подстановка меняла ровно терминальный символ по правилу
«`A` → `B`, иначе → `A`». На 512-байтной подписи `A` и `B` различаются только
битом заполнения ⇒ край получал ПОБАЙТОВО ТУ ЖЕ подпись, отвечал `200` — верно,
предъявитель был подлинный, — а кейс читал этот `200` как «край принял
испорченную подпись» и краснел, обвиняя продукт в том, чего тот не делал.
Терминальный символ равномерен по алфавиту из 16 значений, поэтому ложное
обвинение выпадало примерно на каждом шестнадцатом прогоне. Замер по 19
прогонам с сохранившимися отчётами: символ `A` — 3 прогона, все три красные;
прочие символы — 16 прогонов, красных ноль.

ПОЧЕМУ ГЕЙТ, А НЕ КОММЕНТАРИЙ. Свойство «подстановка меняет байты» не видно ни
в диффе, ни в зелёном прогоне: пятнадцать прогонов из шестнадцати зелены при
СЛОМАННОЙ подстановке. Проверить его может только перебор входов, а перебирать
нечем, кроме как исполнив саму подстановку. Гейт исполняет ТУ ЖЕ строку, что
уедет в прогон, — он читает её из СГЕНЕРИРОВАННОЙ коллекции, а не из case-файла
и не из своей копии: второй копии подстановки в дереве не заводится.

РАЗДЕЛЕНИЕ ТРУДА. JS (node, без единой зависимости) — только ПРОИЗВОДИТЕЛЬ:
исполняет подстановку под минимальным подставным `pm`. Судит Python: он
декодирует обе подписи и сравнивает БАЙТЫ. Ни одна сторона не повторяет логику
другой.

Запуск (стенд, newman и npm не нужны):
    python3 scripts/selftest_tamper_mutation.py --self-test   # инъекции в обе стороны
    python3 scripts/selftest_tamper_mutation.py               # проверка дерева

Коды возврата: 0 — свойство держится; 1 — находки; 2 — предпосылка гейта не
выполнена (нет node, нет коллекции, в коллекции нет шага — то есть гейт не
проверил НИЧЕГО и обязан сказать это отдельно от «находок нет»).
"""

from __future__ import annotations

import base64
import json
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
COLLECTION = HERE.parent / "collections" / "authn_edge.postman_collection.json"
STEP_NAME = "list-accounts-with-tampered-signature"

# Длины подписи в БАЙТАХ, на которых проверяется свойство. Первая — то, что
# чеканит стенд сегодня (наблюдено в отчётах: сегмент подписи 683 символа = 512
# байт). Остальные — формы, в которые установка может переехать сменой ключа или
# алгоритма; свойство обязано держаться и там, иначе гейт защищает один стенд, а
# не подстановку. Остатки длин по модулю 3 покрыты все три (512→2, 256→1, 384→0),
# то есть покрыты все три раскладки бит заполнения в терминальном символе.
SIG_LENS = (512, 384, 256, 64, 32)

# Обе краевые позиции перебираются ПОЛНОСТЬЮ. Терминальный байт — потому что
# именно он задаёт терминальный символ, на котором ломалась прежняя подстановка;
# первый байт — потому что его задевает нынешняя.
BYTE_VALUES = tuple(range(256))

_HDR = "eyJhbGciOiJSUzI1NiIsImtpZCI6InNlbGZ0ZXN0In0"
_PAYLOAD = "eyJzdWIiOiJrYWNoby1ib290c3RyYXAiLCJpc3MiOiJzZWxmdGVzdCJ9"


def b64u(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def unb64u(s: str) -> bytes:
    return base64.urlsafe_b64decode(s + "=" * (-len(s) % 4))


def make_tokens() -> list[dict]:
    """Производитель входа: предъявители, отличающиеся ровно краевыми байтами.

    Тело подписи — детерминированное (гейт обязан давать один и тот же вердикт
    на одном и том же дереве), но не постоянное по позиции, чтобы подстановка не
    попала случайно на однородный буфер.
    """
    tokens = []
    for n in SIG_LENS:
        body = bytes((i * 37 + 11) % 256 for i in range(n))
        for v in BYTE_VALUES:
            for edge in ("last", "first"):
                raw = bytearray(body)
                raw[-1 if edge == "last" else 0] = v
                tokens.append({
                    "id": f"len={n} {edge}-byte=0x{v:02x}",
                    "token": f"{_HDR}.{_PAYLOAD}.{b64u(bytes(raw))}",
                })
    return tokens


# ── подставной `pm`: не снисходительнее песочницы newman ─────────────────────
#
# `pm.test` ИСПОЛНЯЕТ обратный вызов (newman исполняет), `pm.expect.fail`
# бросает, `skipRequest` фиксируется. Дублёр, который молча глотал бы то, на чём
# настоящий отказывает, скрыл бы ровно тот дефект, ради которого он подставлен.
_SHIM = r"""
'use strict';
const MUTATION = process.argv[2];
const INPUT = JSON.parse(require('fs').readFileSync(process.argv[3], 'utf8'));
const run = new Function('pm', MUTATION);
const out = [];
for (const item of INPUT) {
  const rec = { id: item.id, header: null, skipped: false, failures: [] };
  const pm = {
    environment: { get: (k) => (k === 'jwtBootstrap' ? item.token : undefined) },
    variables: { get: () => undefined },
    request: { headers: { upsert: (h) => {
      if (String(h.key).toLowerCase() === 'authorization') rec.header = h.value;
    } } },
    execution: { skipRequest: () => { rec.skipped = true; } },
    test: (name, fn) => { try { fn(); } catch (e) { rec.failures.push(name + ': ' + e.message); } },
  };
  pm.expect = () => { throw new Error('подставной pm.expect вызван вне ожидаемой ветки'); };
  pm.expect.fail = (msg) => { throw new Error(msg); };
  try { run(pm); } catch (e) { rec.failures.push('исключение подстановки: ' + e.message); }
  out.push(rec);
}
process.stdout.write(JSON.stringify(out));
"""


def load_mutation(collection: Path) -> str:
    """Подстановка берётся из СГЕНЕРИРОВАННОЙ коллекции — из того, что исполнится."""
    doc = json.loads(collection.read_text(encoding="utf-8"))
    found: list[str] = []

    def walk(items):
        for it in items:
            if "item" in it:
                walk(it["item"])
            elif it.get("name") == STEP_NAME:
                for ev in it.get("event", []):
                    if ev.get("listen") == "prerequest":
                        found.append("\n".join(ev["script"]["exec"]))

    walk(doc["item"])
    if len(found) != 1:
        premise(f"в {collection.name} ожидался ровно один шаг «{STEP_NAME}» "
                f"с pre-request, найдено {len(found)}")
    return found[0]


def premise(msg: str) -> None:
    print(f"ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: {msg}", file=sys.stderr)
    print("гейт не проверил НИЧЕГО — это не «находок нет»", file=sys.stderr)
    sys.exit(2)


def run_mutation(mutation: str, tokens: list[dict], tmp: Path) -> list[dict]:
    node = shutil.which("node")
    if not node:
        premise("в PATH нет node — исполнить подстановку нечем")
    payload = tmp / "input.json"
    payload.write_text(json.dumps(tokens), encoding="utf-8")
    shim = tmp / "shim.js"
    shim.write_text(_SHIM, encoding="utf-8")
    proc = subprocess.run([node, str(shim), mutation, str(payload)],
                          capture_output=True, text=True, check=False)
    if proc.returncode != 0:
        premise(f"node вышел с кодом {proc.returncode}: {proc.stderr.strip()[:400]}")
    return json.loads(proc.stdout)


def audit(mutation: str, tokens: list[dict], tmp: Path) -> list[str]:
    """Находки: где подстановка НЕ поменяла байты подписи (или поломала форму)."""
    findings: list[str] = []
    by_id = {t["id"]: t["token"] for t in tokens}
    for rec in run_mutation(mutation, tokens, tmp):
        base = by_id[rec["id"]]
        if rec["failures"]:
            findings.append(f"{rec['id']}: подстановка упала — {rec['failures'][0]}")
            continue
        if rec["skipped"] or not rec["header"]:
            findings.append(f"{rec['id']}: подстановка не выставила Authorization")
            continue
        sent = rec["header"].replace("Bearer ", "", 1)
        a, b = sent.split("."), base.split(".")
        if len(a) != 3:
            findings.append(f"{rec['id']}: отправлено {len(a)} сегментов вместо 3")
            continue
        if a[0] != b[0] or a[1] != b[1]:
            findings.append(f"{rec['id']}: изменён заголовок или тело — кейс перестал "
                            "быть утверждением о подписи")
            continue
        if len(a[2]) != len(b[2]):
            findings.append(f"{rec['id']}: длина подписи {len(a[2])} против {len(b[2])}")
            continue
        # ЕДИНСТВЕННОЕ утверждение, ради которого гейт существует.
        if unb64u(a[2]) == unb64u(b[2]):
            findings.append(f"{rec['id']}: подпись совпала ПОБАЙТОВО — строка другая, "
                            "байты те же; край ответит 200 на ПОДЛИННЫЙ предъявитель, "
                            "а кейс прочтёт это как принятую подделку")
    return findings


# ── инъекции: гейт обязан краснеть на дефекте и молчать на законном близнеце ──
#
# Обе стороны обязательны. Без красной инъекции гейт неотличим от «всегда
# зелёного», без законного близнеца — ловит форму, а не существо, и первый же
# ложный срабат его отключит.
_INJECT_BROKEN = """
const _base = pm.environment.get('jwtBootstrap') || '';
const _s = _base.split('.');
const _sig = _s[2];
const _c = _sig.slice(-1);
const _flipped = _sig.slice(0, -1) + (_c === 'A' ? 'B' : 'A');
pm.request.headers.upsert({ key: 'Authorization',
  value: 'Bearer ' + _s[0] + '.' + _s[1] + '.' + _flipped });
"""

# Законный близнец: та же цель, ДРУГАЯ запись — портится символ в середине.
# Гейт обязан молчать: он проверяет свойство (байты разошлись), а не начертание.
_INJECT_LEGIT_TWIN = """
const _base = pm.environment.get('jwtBootstrap') || '';
const _s = _base.split('.');
const _sig = _s[2];
const _i = Math.floor(_sig.length / 2);
const _c = _sig[_i];
const _flipped = _sig.slice(0, _i) + (_c === 'Q' ? 'R' : 'Q') + _sig.slice(_i + 1);
pm.request.headers.upsert({ key: 'Authorization',
  value: 'Bearer ' + _s[0] + '.' + _s[1] + '.' + _flipped });
"""


def self_test(tokens: list[dict], tmp: Path) -> int:
    print("── самопроверка: инъекции в обе стороны ──")
    broken = audit(_INJECT_BROKEN, tokens, tmp)
    hit = [f for f in broken if "ПОБАЙТОВО" in f]
    ok_red = bool(hit)
    print(f"{'ok  ' if ok_red else 'FAIL'}  прежняя подстановка (терминальный символ) — "
          f"находок {len(hit)}"
          + (f"; первая: {hit[0]}" if hit else "; гейт её НЕ ловит"))

    twin = audit(_INJECT_LEGIT_TWIN, tokens, tmp)
    ok_green = not twin
    print(f"{'ok  ' if ok_green else 'FAIL'}  законный близнец (символ в середине) — "
          f"находок {len(twin)}"
          + (f"; первая: {twin[0]}" if twin else ""))

    if ok_red and ok_green:
        print("самопроверка пройдена: гейт краснеет на дефекте и молчит на законной форме")
        return 0
    print("самопроверка ПРОВАЛЕНА — гейт не является доказательством", file=sys.stderr)
    return 1


def main() -> int:
    import tempfile

    if not COLLECTION.exists():
        premise(f"нет {COLLECTION} — нечего проверять "
                "(коллекция генерируется: python3 gen.py authn_edge)")

    tokens = make_tokens()
    with tempfile.TemporaryDirectory() as td:
        tmp = Path(td)
        # Разбор именно через `sys.argv`, а не через параметр: обход
        # `deploy/scripts/run-gate-self-tests.sh` ищет самопроверки по
        # ИСПОЛНЯЕМОЙ форме разбора, и гейт, записанный иначе, выпадает из
        # переписи — то есть остаётся вне инварианта «у каждой самопроверки
        # есть вызывающий». Ровно тот класс, который этот гейт и закрывает.
        if "--self-test" in sys.argv:
            return self_test(tokens, tmp)

        mutation = load_mutation(COLLECTION)
        findings = audit(mutation, tokens, tmp)

        # Объём осмотренного печатается ВСЕГДА: «ноль находок» обязано быть
        # отличимо от «ноль прочитанного».
        print(f"осмотрено: коллекция {COLLECTION.name}, шаг «{STEP_NAME}», "
              f"длин подписи {len(SIG_LENS)} ({', '.join(str(n) for n in SIG_LENS)} байт), "
              f"значений краевого байта {len(BYTE_VALUES)}, "
              f"предъявителей исполнено {len(tokens)}")
        for f in findings:
            print(f"::error::{f}")
        if findings:
            print(f"✖ находок: {len(findings)}")
            return 1
        print("✔ подстановка меняет подпись побайтово на каждом входе")
        return 0


if __name__ == "__main__":
    sys.exit(main())
