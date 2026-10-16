#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Проба перевыпуска предъявителей фикстуры (`prodseed_refresh.py`, kacho#3238).

ПРЕДМЕТ. Посев идёт один раз на все суиты, а предъявитель живёт столько, сколько
назначил выдающий (на стенде t3238-sc измерено `exp - iat` = 900 с). Суита длиннее
срока получала сплошные 401 — вердикт о продукте там, где истёк срок фикстуры.
Перевыпуск обязан: (1) сработать, когда остаток срока меньше запаса, и при
`force`; (2) НЕ трогать окружение, когда остаток достаточен; (3) менять только
ключи, у которых есть рецепт, — истёкший по замыслу `apiTokenExpired` и прочие
величины остаются байт в байт; (4) не дописывать ключей, которых в окружении нет;
(5) на отказе выпуска не оставлять окружение наполовину переписанным;
(6) «покрыто ноль ключей» — отказ, а не «перевыпускать нечего».

СЕТИ НЕТ: выпуск подменён двойником, часы — параметром. Судится логика решения.
Исполняет `.github/scripts/run-python-probes.py` (образец tests/authz-fixtures/*_test.py).
"""
from __future__ import annotations

import base64
import json
import pathlib
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import prodseed_refresh as pr  # noqa: E402

NOW = 1_800_000_000
FAILS: list[str] = []
CHECKS = [0]


def check(name: str, cond: bool, detail: str = "") -> None:
    CHECKS[0] += 1
    print(f"{'ok  ' if cond else 'FAIL'} {name}{(' — ' + detail) if detail and not cond else ''}")
    if not cond:
        FAILS.append(name)


def jwt(exp: int, tag: str = "x") -> str:
    def b64(o):
        return base64.urlsafe_b64encode(json.dumps(o).encode()).decode().rstrip("=")
    return f"{b64({'alg': 'ES256'})}.{b64({'exp': exp, 'iat': exp - 900, 'tag': tag})}.sig"


def env_file(d: pathlib.Path, values: dict) -> pathlib.Path:
    p = d / "local.postman_environment.json"
    p.write_text(json.dumps({"name": "t", "values": [
        {"key": k, "value": v, "type": "default", "enabled": True} for k, v in values.items()]}, indent=2) + "\n")
    return p


def values_of(p: pathlib.Path) -> dict:
    return {v["key"]: v["value"] for v in json.loads(p.read_text())["values"]}


class Minter:
    """Двойник выпуска: считает вызовы по рецепту и отдаёт свежий предъявитель."""

    def __init__(self, fail: bool = False):
        self.calls: list[str] = []
        self.fail = fail

    def __call__(self, recipe: dict) -> str:
        self.calls.append(json.dumps(recipe, sort_keys=True))
        if self.fail and recipe.get("kind") == "revoked":
            raise RuntimeError("выпуск отвергнут двойником")
        return jwt(NOW + 900, tag="new-" + recipe.get("clientId", recipe["kind"]))


RECIPES = {
    "jwtBootstrap": {"kind": "bootstrap"},
    "jwtSAA": {"kind": "key", "clientId": "cli_a", "key": "PEM-A"},
    "jwtServiceAccountEditor": {"kind": "key", "clientId": "cli_a", "key": "PEM-A"},
    "apiTokenRevoked": {"kind": "revoked", "sva": "sva_rev"},
    "jwtOnlyInOtherSuite": {"kind": "key", "clientId": "cli_o", "key": "PEM-O"},
}


def base_env(remaining: int) -> dict:
    exp = NOW + remaining
    return {
        "baseUrl": "http://localhost:18080",
        "jwtBootstrap": jwt(exp, "boot"),
        "jwtSAA": jwt(exp, "saa"),
        "jwtServiceAccountEditor": jwt(exp, "saa"),
        "apiTokenRevoked": jwt(exp, "rev"),
        "apiTokenExpired": jwt(NOW - 60, "expired-by-design"),
        "apiTokenMalformed": "eyJhbGciOiJIUzI1NiJ9.bm90LWEtcmVhbC10b2tlbg",
    }


def main() -> int:
    with tempfile.TemporaryDirectory() as td:
        d = pathlib.Path(td)

        # A. остаток меньше запаса → перевыпуск ровно покрытых ключей.
        p = env_file(d, base_env(remaining=200))
        before = values_of(p)
        mt = Minter()
        rep = pr.refresh_envs(RECIPES, [p], margin=600, force=False, now=NOW, mint=mt)
        after = values_of(p)
        check("A1 остаток 200с < запаса 600с — перевыпущено", rep.reissued == 4, f"{rep}")
        check("A2 общий рецепт выпускается один раз", len(mt.calls) == 3, f"{mt.calls}")
        check("A3 ключи с рецептом получили новый предъявитель",
              all(after[k] != before[k] for k in ("jwtBootstrap", "jwtSAA", "jwtServiceAccountEditor", "apiTokenRevoked")))
        check("A4 ключи с общим рецептом получили ОДИН предъявитель",
              after["jwtSAA"] == after["jwtServiceAccountEditor"])
        check("A5 истёкший по замыслу и нерецептурные ключи не тронуты",
              all(after[k] == before[k] for k in ("apiTokenExpired", "apiTokenMalformed", "baseUrl")))
        check("A6 ключ без места в окружении не дописан", "jwtOnlyInOtherSuite" not in after)
        check("A7 остаток после перевыпуска назван", rep.least_remaining_after == 900, f"{rep}")
        check("A8 нерецептурный предъявитель назван поимённо",
              "apiTokenExpired" in rep.uncovered, f"{rep.uncovered}")

        # B. остаток достаточен → окружение байт в байт, выпуска нет.
        p = env_file(d, base_env(remaining=800))
        raw = p.read_bytes()
        mt = Minter()
        rep = pr.refresh_envs(RECIPES, [p], margin=600, force=False, now=NOW, mint=mt)
        check("B1 остаток 800с ≥ запаса 600с — выпуска нет", mt.calls == [] and rep.reissued == 0, f"{rep}")
        check("B2 окружение не переписано", p.read_bytes() == raw)
        check("B3 остаток назван и без перевыпуска", rep.least_remaining_before == 800, f"{rep}")

        # C. force → перевыпуск и при достаточном остатке.
        mt = Minter()
        rep = pr.refresh_envs(RECIPES, [p], margin=600, force=True, now=NOW, mint=mt)
        check("C1 force перевыпускает при остатке 800с", rep.reissued == 4 and len(mt.calls) == 3, f"{rep}")

        # D. ровно на границе: остаток == запасу не перевыпускается, на 1 с меньше — да.
        p = env_file(d, base_env(remaining=600))
        mt = Minter()
        pr.refresh_envs(RECIPES, [p], margin=600, force=False, now=NOW, mint=mt)
        check("D1 остаток == запасу — выпуска нет", mt.calls == [])
        p = env_file(d, base_env(remaining=599))
        mt = Minter()
        pr.refresh_envs(RECIPES, [p], margin=600, force=False, now=NOW, mint=mt)
        check("D2 остаток на 1с меньше запаса — выпуск", len(mt.calls) == 3)

        # E. непрочитываемое значение покрытого ключа считается истёкшим.
        vals = base_env(remaining=800)
        vals["jwtSAA"] = "placeholder"
        p = env_file(d, vals)
        mt = Minter()
        rep = pr.refresh_envs(RECIPES, [p], margin=600, force=False, now=NOW, mint=mt)
        check("E1 заглушка вместо предъявителя — перевыпуск", rep.reissued == 4, f"{rep}")

        # F. отказ выпуска → окружение не тронуто, отказ поднят.
        p = env_file(d, base_env(remaining=100))
        raw = p.read_bytes()
        try:
            pr.refresh_envs(RECIPES, [p], margin=600, force=False, now=NOW, mint=Minter(fail=True))
            raised = False
        except pr.RefreshError:
            raised = True
        check("F1 отказ выпуска поднят", raised)
        check("F2 окружение не переписано наполовину", p.read_bytes() == raw)

        # G. ноль покрытых ключей — отказ, а не «нечего перевыпускать».
        p = env_file(d, {"baseUrl": "x", "jwtSomething": jwt(NOW + 10)})
        try:
            pr.refresh_envs(RECIPES, [p], margin=600, force=True, now=NOW, mint=Minter())
            raised = False
        except pr.RefreshError:
            raised = True
        check("G1 окружение без единого покрытого ключа — отказ", raised)

    # H. рецепты посева: запомненный выпуск попадает в состояние, незапомненный — нет.
    reg: dict = {}
    t_saa, t_exp = jwt(NOW + 900, "a"), jwt(NOW - 1, "e")
    pr.remember(reg, t_saa, {"kind": "key", "clientId": "c", "key": "k"})
    got = pr.recipes_for(reg, {"jwtSAA": t_saa, "jwtSAAlias": t_saa, "apiTokenExpired": t_exp, "baseUrl": "u"})
    check("H1 рецепт найден по значению у всех ключей предъявителя",
          set(got) == {"jwtSAA", "jwtSAAlias"}, f"{got}")
    check("H2 незапомненный (истёкший по замыслу) рецепта не получает", "apiTokenExpired" not in got)

    print(f"prodseed_refresh: проверок {CHECKS[0]}, провалено {len(FAILS)}")
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
