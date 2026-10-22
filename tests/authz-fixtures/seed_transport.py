# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Транспорт посева: один HTTP-вызов к краю, отказ транспорта — громкий (kacho#3246).

ПРЕДМЕТ. Посев (`prodseed_matrix._curl`) говорит с краем через `curl`. Прежняя форма
читала только стандартный вывод и превращала ПУСТОЙ вывод в `{}`: отказ соединения,
истёкший срок, отказ рукопожатия mTLS внутреннего слушателя — всё это становилось
«ответом» без единого поля. Дальше `_await` писал `no op id: {}`, и находка читалась
как «мутация вернула не Operation» (ban #9), то есть как дефект продукта, а не как
то, чем она была: запрос до края не дошёл либо ответа не принёс. Код возврата и
диагностика `curl` уходили в выброшенный поток ошибок.

ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ:
  * код `curl` не ноль — `SeedTransportError` с кодом, методом, путём, поверхностью
    и строкой диагностики `curl`; тело и предъявитель в текст не попадают;
  * пустое тело — тоже отказ, при любом статусе: край на этих маршрутах отвечает
    JSON всегда (ответ без полей — `{}` текстом), и пустота — признак обрыва, а не
    «пустой ответ»;
  * непустое тело, не разбираемое как JSON, — прежняя форма `{"raw": …}` вместе со
    статусом: вызывающие, сверяющие поле, видят отказ своим путём.

Модуль не читает окружения и не ходит в сеть при импорте — поэтому его проба
(`seed_transport_test.py`) исполнима без стенда, в отличие от `prodseed_matrix`,
который чеканит удостоверение при импорте.
"""
from __future__ import annotations

import json
import subprocess

# Отделитель статуса от тела в выводе `curl -w`: строка, которой в JSON края нет.
_STATUS_MARK = "\n__kacho_seed_http_status__="


class SeedTransportError(RuntimeError):
    """Запрос посева не принёс ответа края: обрыв транспорта или пустое тело."""


def curl_json(method, url, token=None, body=None, tls_args=(), surface="край", timeout=20):
    """Один запрос к краю; разобранный JSON ответа либо `SeedTransportError`.

    `surface` — имя поверхности для текста отказа (публичный край, внутренний
    слушатель края): адрес проброса меняется от прогона к прогону и в текст отказа
    не пишется, путь — пишется.
    """
    path = _path_of(url)
    args = ["curl", "-sS", "-m", str(timeout), "-X", method,
            "-H", "Content-Type: application/json",
            "-w", _STATUS_MARK + "%{http_code}"]
    args += list(tls_args)
    if token:
        args += ["-H", f"Authorization: Bearer {token}"]
    if body is not None:
        args += ["--data", json.dumps(body)]
    args.append(url)
    p = subprocess.run(args, capture_output=True, text=True)
    out, _, status = (p.stdout or "").rpartition(_STATUS_MARK)
    if not _:
        out, status = p.stdout or "", ""
    if p.returncode != 0:
        diag = (p.stderr or "").strip().splitlines()
        raise SeedTransportError(
            f"{method} {path} ({surface}): curl код {p.returncode} — ответа края нет; "
            f"диагностика curl: {diag[-1] if diag else '(пусто)'}")
    if not out.strip():
        raise SeedTransportError(
            f"{method} {path} ({surface}): HTTP {status or '?'} с ПУСТЫМ телом — край на "
            "этом маршруте отвечает JSON всегда; пустота — обрыв, а не ответ")
    try:
        return json.loads(out)
    except json.JSONDecodeError:
        return {"raw": out, "httpStatus": status}


def _path_of(url):
    rest = url.split("://", 1)[-1]
    i = rest.find("/")
    return rest[i:] if i >= 0 else "/"
