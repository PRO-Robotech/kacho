#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Перевыпуск предъявителей фикстуры перед суитой и перед коллекцией (kacho#3238).

ПОЧЕМУ ЭТО НУЖНО. Посев (`prodseed_all.py`) идёт ОДИН раз на все суиты, а
предъявитель живёт столько, сколько назначил выдающий. На стенде t3238-sc срок
измерен по самим предъявителям: `exp - iat` = 900 с у всех 26 выданных. Посев
длится около 4 минут, суита vpc — около 14, и суиты при WAVE_JOBS=1 идут одна за
другой. Всё, что начиналось позже срока, получало сплошные 401 AUTHN_REQUIRED —
вердикт о продукте там, где истёк срок ФИКСТУРЫ. Срок продукта не трогается:
предъявитель выпускается заново тем же путём, что в посеве.

ЧТО ПЕРЕВЫПУСКАЕТСЯ — ТОЛЬКО ТО, ЧЕЙ РЕЦЕПТ ЗАПИСАЛ ПОСЕВ. Посев запоминает
рецепт каждого выданного им предъявителя (`remember`) и пишет перечень «ключ
окружения → рецепт» в файл состояния (PRODSEED_TOKEN_STATE, 0600, временный
каталог прогонщика). Рецептов три:

  * `bootstrap` — заново MintBootstrapToken (mTLS к внутреннему порту службы);
  * `key` — тот же ключ служебной учётки или персонального токена: новое
    подписанное утверждение и обмен у НАШЕГО издателя (`POST /iam/v1/token`);
  * `revoked` — новый ключ той же учётки, обмен, СНЯТИЕ ключа с проверкой исхода
    операции: отозванный предъявитель обязан быть отозванным, а не истёкшим,
    иначе отказ ему доказывал бы срок, а не отзыв.

Чего рецепта нет, то не трогается и называется поимённо: `apiTokenExpired`
истёк по замыслу, сессии людей посева — не предъявители выдающего (их срок не
измерен этим модулем), `apiTokenMalformed` — литерал.

КОГДА. Остаток срока читается у самих предъявителей окружения (`exp`, наименьший
из покрытых ключей). Перевыпуск — при остатке меньше запаса (`--margin`) либо
при `--force`. Прогонщик зовёт `--force` перед каждой суитой и обычный вызов
перед каждой коллекцией (обёртка `newman` в newman-parallel.sh): суита любой
длины не упирается в срок, пока ОДНА коллекция короче запаса. Запас по
умолчанию 600 с: самая длинная коллекция прогона 2026-10-16 шла 166 с.

ОТКАЗ ГРОМКИЙ И ЦЕЛЫЙ. Выпуск отказал — окружение не переписывается вовсе
(запись после выпуска всех рецептов, через временный файл и `os.replace`), код
1, причина названа. Окружение без единого покрытого ключа — тоже отказ: «нечего
перевыпускать» неотличимо от «посев рецептов не записал».

Вызов: prodseed_refresh.py --state S --env E [--env E2 ...] [--force] [--margin N]
"""
from __future__ import annotations

import argparse
import base64
import dataclasses
import fcntl
import json
import os
import pathlib
import sys
import time
from typing import Callable

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

DEFAULT_MARGIN_S = 600
# Ключ, под которым расширение посева отдаёт рецепты своих предъявителей; в
# окружение суиты он не попадает (prodseed_all.run_ext снимает его до записи).
TOKEN_RECIPES_KEY = "__tokenRecipes__"


class RefreshError(RuntimeError):
    """Перевыпуск не состоялся; окружение не тронуто."""


def log(msg: str) -> None:
    print(f"[refresh] {msg}", file=sys.stderr, flush=True)


# ── рецепты посева ──────────────────────────────────────────────────────────
def remember(registry: dict, token: str, recipe: dict) -> str:
    """Записать рецепт выданного предъявителя; вернуть сам предъявитель."""
    if token:
        registry[token] = recipe
    return token


def recipes_for(registry: dict, fixtures: dict) -> dict:
    """«ключ окружения → рецепт» для ключей, чьё значение выдано с рецептом."""
    return {k: registry[v] for k, v in fixtures.items() if isinstance(v, str) and v in registry}


def write_state(path: str, recipes: dict) -> None:
    """Файл состояния 0600: в нём приватные ключи учёток, в дерево он не попадает."""
    p = pathlib.Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(str(p), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as fh:
        json.dump({"recipes": recipes}, fh)


# ── решение ─────────────────────────────────────────────────────────────────
def jwt_exp(value: str) -> int | None:
    """`exp` предъявителя либо None, если значение не JWT с числовым `exp`."""
    parts = value.split(".") if isinstance(value, str) else []
    if len(parts) != 3:
        return None
    try:
        body = parts[1] + "=" * (-len(parts[1]) % 4)
        exp = json.loads(base64.urlsafe_b64decode(body)).get("exp")
    except (ValueError, json.JSONDecodeError, AttributeError):
        return None
    return exp if isinstance(exp, int) else None


@dataclasses.dataclass
class Report:
    covered: int = 0
    reissued: int = 0
    least_remaining_before: int | None = None
    least_remaining_after: int | None = None
    uncovered: list = dataclasses.field(default_factory=list)


def _env_values(path: pathlib.Path) -> tuple[dict, dict]:
    data = json.loads(path.read_text())
    return data, {v.get("key"): v.get("value", "") for v in data.get("values", [])}


def _least_remaining(values: dict, keys, now: int) -> int:
    rem = []
    for k in keys:
        exp = jwt_exp(values.get(k, ""))
        rem.append((exp - now) if exp is not None else 0)
    return min(rem) if rem else 0


def refresh_envs(recipes: dict, env_paths: list, *, margin: int, force: bool, now: int,
                 mint: Callable[[dict], str]) -> Report:
    """Перевыпустить покрытые ключи окружений, если остаток срока меньше запаса.

    Выпуск — по одному на РАЗЛИЧНЫЙ рецепт (у нескольких ключей бывает один
    предъявитель). Запись — только после того, как выпущено всё.
    """
    rep = Report()
    loaded = []
    for p in map(pathlib.Path, env_paths):
        data, values = _env_values(p)
        keys = [k for k in values if k in recipes]
        if not keys:
            raise RefreshError(f"{p}: ни одного ключа с рецептом посева — перевыпускать нечего "
                               "либо посев рецептов не записал; окружение не тронуто")
        rep.uncovered += sorted(k for k, v in values.items()
                                if k not in recipes and jwt_exp(v) is not None)
        least = _least_remaining(values, keys, now)
        rep.least_remaining_before = least if rep.least_remaining_before is None else min(least, rep.least_remaining_before)
        rep.covered += len(keys)
        loaded.append((p, data, keys))

    if not force and rep.least_remaining_before >= margin:
        rep.least_remaining_after = rep.least_remaining_before
        return rep

    minted: dict[str, str] = {}
    try:
        for _p, _data, keys in loaded:
            for k in keys:
                rk = json.dumps(recipes[k], sort_keys=True)
                if rk not in minted:
                    minted[rk] = mint(recipes[k])
    except Exception as e:  # noqa: BLE001 — любой отказ выпуска одинаково останавливает
        raise RefreshError(f"выпуск отказал: {e}; окружения не тронуты") from e

    after = []
    for p, data, keys in loaded:
        new = {k: minted[json.dumps(recipes[k], sort_keys=True)] for k in keys}
        for v in data.get("values", []):
            if v.get("key") in new:
                v["value"] = new[v["key"]]
                v["enabled"] = True
        tmp = p.with_name(p.name + ".refresh.tmp")
        tmp.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
        os.replace(tmp, p)
        rep.reissued += len(keys)
        after.append(_least_remaining(new, keys, now))
    rep.least_remaining_after = min(after)
    return rep


# ── выпуск у стенда ─────────────────────────────────────────────────────────
class LiveMinter:
    """Выпуск у стенда теми же путями, что в посеве (prodseed_matrix / mint_rs256).

    Модуль матрицы импортируется лениво: при импорте он выпускает бутстрап-
    удостоверение и проверяет, что край его принял, — это и есть рецепт
    `bootstrap`, а при достаточном остатке срока к стенду не ходит ничего.
    """

    def __init__(self):
        self._pm = None

    def _matrix(self):
        if self._pm is None:
            import prodseed_matrix as pm
            self._pm = pm
        return self._pm

    def __call__(self, recipe: dict) -> str:
        pm = self._matrix()
        kind = recipe.get("kind")
        if kind == "bootstrap":
            return pm.boot
        if kind == "key":
            assertion = pm.m.sign_client_assertion(
                recipe["clientId"], recipe["key"], recipe["clientId"], pm.PLATFORM_ASSERT_AUD,
                token_type=pm.m.CLIENT_ASSERTION_TOKEN_TYPE)
            return pm.m.exchange_at_platform(pm.PLATFORM_TOKEN_URL, assertion, pm.API_AUD)
        if kind == "revoked":
            sva = recipe["sva"]
            token, key_id = pm.sa_token_with_key(sva)
            resp = pm._curl("DELETE", f"/iam/v1/serviceAccounts/{sva}/keys/{key_id}", pm.boot)
            done = pm._poll(resp.get("id"), pm.boot) if resp.get("id") else {}
            if not done or done.get("error"):
                raise RuntimeError(f"снятие ключа {key_id} учётки {sva} не подтверждено: "
                                   f"ответ {resp}, исход {done}")
            return token
        raise RuntimeError(f"неизвестный рецепт {kind!r}")


def main() -> int:
    ap = argparse.ArgumentParser(description="перевыпуск предъявителей фикстуры")
    ap.add_argument("--state", default=os.environ.get("PRODSEED_TOKEN_STATE", ""))
    ap.add_argument("--env", action="append", default=[], required=True)
    ap.add_argument("--force", action="store_true")
    ap.add_argument("--margin", type=int,
                    default=int(os.environ.get("TOKEN_REFRESH_MARGIN_S", DEFAULT_MARGIN_S)))
    args = ap.parse_args()
    if not args.state or not pathlib.Path(args.state).is_file():
        log(f"УСЛОВИЕ НЕ СОЗДАНО: файла рецептов посева нет ({args.state or 'PRODSEED_TOKEN_STATE пуст'})")
        return 1
    recipes = json.loads(pathlib.Path(args.state).read_text()).get("recipes", {})
    lock = open(args.state + ".lock", "w")
    fcntl.flock(lock, fcntl.LOCK_EX)
    try:
        rep = refresh_envs(recipes, args.env, margin=args.margin, force=args.force,
                           now=int(time.time()), mint=LiveMinter())
    except RefreshError as e:
        log(f"УСЛОВИЕ НЕ СОЗДАНО: {e}")
        return 1
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        lock.close()
    what = "перевыпущено" if rep.reissued else "не перевыпускалось"
    log(f"{' '.join(args.env)}: ключей с рецептом {rep.covered}, {what} {rep.reissued}; "
        f"наименьший остаток срока {rep.least_remaining_before}с → {rep.least_remaining_after}с "
        f"(запас {args.margin}с{', force' if args.force else ''}); "
        f"без рецепта: {', '.join(sorted(set(rep.uncovered))) or '—'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
