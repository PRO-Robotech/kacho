#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Проба: посев берёт пространство стенда ОДНИМ правилом во всех модулях (kacho#3246).

ПРИЗНАК. Прогонщик newman называет стенд переменной SETUP_NS; `mint_rs256` читал
только KACHO_NAMESPACE и освежал лист оператора из пространства `kacho` — рабочего
стенда. Чеканка у своей службы отказывала «tls: unknown certificate authority», а
`prodseed_matrix` тем же правилом писал бы `kubectl exec` кортежи в базу чужого стенда.

  A. задан только SETUP_NS — mint_rs256 и prodseed_all называют его;
  B. БЛИЗНЕЦ: заданы оба — побеждает KACHO_NAMESPACE (явное имя посева);
  C. БЛИЗНЕЦ: не задано ничего — `kacho`, прежнее умолчание;
  D. prodseed_matrix не держит своего правила: KACHO_NS — это m.BOOTSTRAP_OPERATOR_NS
     (разбором AST: модуль чеканит удостоверение при импорте и без стенда не грузится).

Исполнитель — `.github/scripts/run-python-probes.py` по образцу
`tests/authz-fixtures/*_test.py`.
"""

import ast
import os
import pathlib
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
FAILURES: list[str] = []
CHECKS = [0]


def check(name, ok, detail=""):
    CHECKS[0] += 1
    print(f"{'ок   ' if ok else 'ОТКАЗ'} {name}" + ("" if ok else f" — {detail}"))
    if not ok:
        FAILURES.append(f"{name}: {detail}")


def resolved(env_over):
    env = {k: v for k, v in os.environ.items() if k not in ("KACHO_NAMESPACE", "SETUP_NS")}
    env.update(env_over)
    code = ("import sys; sys.path.insert(0, sys.argv[1]); import mint_rs256, prodseed_all; "
            "print(mint_rs256.BOOTSTRAP_OPERATOR_NS, prodseed_all.NS)")
    p = subprocess.run([sys.executable, "-c", code, str(HERE)], env=env,
                       capture_output=True, text=True, timeout=60)
    return p.stdout.split() if p.returncode == 0 else ["ОШИБКА", p.stderr.strip()[-200:]]


def main():
    got = resolved({"SETUP_NS": "t1-probe"})
    check("A: только SETUP_NS — оба модуля называют его", got == ["t1-probe", "t1-probe"], got)
    got = resolved({"SETUP_NS": "t1-probe", "KACHO_NAMESPACE": "t2-probe"})
    check("B: заданы оба — KACHO_NAMESPACE", got == ["t2-probe", "t2-probe"], got)
    got = resolved({})
    check("C: не задано ничего — kacho", got == ["kacho", "kacho"], got)

    tree = ast.parse((HERE / "prodseed_matrix.py").read_text())
    vals = [n.value for n in tree.body if isinstance(n, ast.Assign)
            and any(isinstance(t, ast.Name) and t.id == "KACHO_NS" for t in n.targets)]
    ok = (len(vals) == 1 and isinstance(vals[0], ast.Attribute)
          and vals[0].attr == "BOOTSTRAP_OPERATOR_NS"
          and isinstance(vals[0].value, ast.Name) and vals[0].value.id == "m")
    check("D: prodseed_matrix.KACHO_NS = m.BOOTSTRAP_OPERATOR_NS (одно правило)", ok,
          [ast.unparse(v) for v in vals])

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
