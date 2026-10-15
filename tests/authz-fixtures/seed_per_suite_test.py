#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: каждая суита шарда стартует на свежем посеве (задача #3211).

ПРЕДМЕТ. Предъявители посева — токены нашего издателя со сроком профиля
(`clientToken.tokenTtl`, цепочка dev-prod — 15 минут). Шард из нескольких суит
идёт дольше: на шарде compute-storage набор storage получал `AUTHN_REQUIRED`
около 1047-го запроса из 1170 коллекции image. Прогонщик
`deploy/scripts/newman-parallel.sh` теперь сеет окружение каждой следующей суиты
прямо перед её стартом (`reseed_before_suite`).

ЧТО УТВЕРЖДАЕТСЯ — ИСХОД ЖИВОЙ ФУНКЦИИ, А НЕ ЕЁ ИМЯ. Функция извлекается из
прогонщика (не переписывается здесь — иначе проверялась бы копия) и исполняется с
подменённым посевом, который записывает, для какой суиты его позвали:

  A. три суиты подряд — посев позван дважды, для второй и третьей, по одной суите;
  B. близнец A с SEED_PER_SUITE=false — посев не позван ни разу;
  C. близнец A с SEED=false (стенд посеян заранее) — посев не позван ни разу;
  D. посев перед второй суитой отказал — прогон выходит кодом 2 и третья суита
     не запускается;
  E. в планировщике `launch_wave` вызов `reseed_before_suite` стоит ДО запуска
     прогонщика суиты (`./scripts/run.sh`) — иначе свежий посев доставался бы
     уже идущей суите.

Предпосылка проверяется: нет функции либо нет вызова — отказ, а не тишина.

КТО ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — обходом по образцу
`tests/authz-fixtures/*_test.py`.
"""

import pathlib
import re
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[2]
DRIVER = ROOT / "deploy" / "scripts" / "newman-parallel.sh"

FAILURES: list[str] = []
CHECKS = [0]


def check(name: str, ok: bool, detail: str = "") -> None:
    CHECKS[0] += 1
    print(f"  {'ok ' if ok else 'ОТКАЗ'} {name}" + ("" if ok else f" — {detail}"))
    if not ok:
        FAILURES.append(f"{name}: {detail}")


def extract(text: str, name: str) -> str:
    m = re.search(rf"^{re.escape(name)}\(\) \{{.*?^\}}", text, re.S | re.M)
    return m.group(0) if m else ""


def knob(text: str, name: str) -> str:
    m = re.search(rf"^{name}=.*$", text, re.M)
    return m.group(0) if m else ""


def run(fn: str, knobs: str, env: dict[str, str], seed_rc: int = 0):
    with tempfile.TemporaryDirectory(prefix="seed-per-suite-") as td:
        log = pathlib.Path(td) / "seed.log"
        script = f"""
set -uo pipefail
{knobs}
seed_suites() {{ printf '%s\\n' "$1" >> "{log}"; return {seed_rc}; }}
{fn}
for svc in compute storage nlb; do
  reseed_before_suite "$svc"
  printf 'START %s\\n' "$svc" >> "{log}"
done
"""
        proc = subprocess.run(["bash", "-c", script], env={"PATH": "/usr/bin:/bin", **env},
                              capture_output=True, text=True, timeout=30)
        lines = log.read_text().splitlines() if log.exists() else []
    seeds = [ln for ln in lines if not ln.startswith("START ")]
    starts = [ln.split(" ", 1)[1] for ln in lines if ln.startswith("START ")]
    return proc.returncode, seeds, starts, lines


def main() -> int:
    print("гейт: каждая суита шарда стартует на свежем посеве (#3211)")
    if not DRIVER.exists():
        print(f"ОТКАЗ: нет прогонщика {DRIVER} — проверять нечего")
        return 1
    text = DRIVER.read_text()
    fn = extract(text, "reseed_before_suite")
    knobs = "\n".join(k for k in (knob(text, "SEED_PER_SUITE"), knob(text, "SUITES_LAUNCHED")) if k)
    check("предпосылка: в прогонщике есть reseed_before_suite и её ручки",
          bool(fn) and "SEED_PER_SUITE" in knobs and "SUITES_LAUNCHED" in knobs,
          "функция или ручки не найдены — проверка сторожила бы не то")
    if fn:
        rc, seeds, starts, _ = run(fn, knobs, {"SEED": "true"})
        check("A: три суиты — посев перед второй и третьей, по одной суите",
              rc == 0 and seeds == ["storage", "nlb"] and starts == ["compute", "storage", "nlb"],
              f"rc={rc} посевы={seeds} старты={starts}")
        rc, seeds, starts, _ = run(fn, knobs, {"SEED": "true", "SEED_PER_SUITE": "false"})
        check("B: SEED_PER_SUITE=false — посев не позван", rc == 0 and seeds == [] and len(starts) == 3,
              f"rc={rc} посевы={seeds}")
        rc, seeds, starts, _ = run(fn, knobs, {"SEED": "false"})
        check("C: SEED=false — посев не позван", rc == 0 and seeds == [] and len(starts) == 3,
              f"rc={rc} посевы={seeds}")
        rc, seeds, starts, _ = run(fn, knobs, {"SEED": "true"}, seed_rc=1)
        check("D: отказ посева перед второй суитой — код 2, дальше не запускается",
              rc == 2 and seeds == ["storage"] and starts == ["compute"],
              f"rc={rc} посевы={seeds} старты={starts}")
    lw = extract(text, "launch_wave")
    i_seed, i_run = lw.find("reseed_before_suite"), lw.find("./scripts/run.sh")
    check("E: в launch_wave посев стоит до запуска прогонщика суиты",
          i_seed >= 0 and i_run >= 0 and i_seed < i_run,
          f"позиции: посев {i_seed}, прогонщик {i_run} (−1 — не найдено)")

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
