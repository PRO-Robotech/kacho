#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: каждая суита шарда стартует на свежих предъявителях — одним путём (#3211, #3238).

ПРЕДМЕТ. Предъявители посева — токены нашего издателя со сроком профиля
(`clientToken.tokenTtl`, цепочка dev-prod — 15 минут). Шард из нескольких суит
идёт дольше: на шарде compute-storage набор storage получал `AUTHN_REQUIRED`
около 1047-го запроса из 1170 коллекции image. Прогонщик
`deploy/scripts/newman-parallel.sh` сеет окружение каждой следующей суиты прямо
перед её стартом (`reseed_before_suite`, задача #3211).

ВТОРОЙ ПУТЬ К ТОЙ ЖЕ СВЕЖЕСТИ — перевыпуск по рецептам посева (#3238): перед
суитой и перед коллекцией по остатку срока. Сведены оба пути при слиянии волны с
эпиком (#3258), и свести их неверно можно двумя способами, видимыми только исходом
планировщика:
  * ДВОЙНОЙ ПУТЬ — свежий посев суиты и следом принудительный перевыпуск её же
    предъявителей: выпусков вдвое, а отказ второго объявляет «не выполнилась»
    суиту на только что посеянном окружении;
  * ЧУЖИЕ РЕЦЕПТЫ — свежий посев суиты переписывает общий файл рецептов, и обёртка
    newman идущей суиты (WAVE_JOBS>1) перевыпускает её предъявителей по рецептам
    другой матрицы.

ЧТО УТВЕРЖДАЕТСЯ — ИСХОД ЖИВЫХ ФУНКЦИЙ, А НЕ ИХ ИМЕНА. Функции и обёртка
извлекаются из прогонщика (не переписываются здесь — иначе проверялась бы копия) и
исполняются с подменёнными посевом, перевыпуском и прогонщиком суиты:

  A. три суиты подряд — посев позван дважды, для второй и третьей, по одной суите,
     и каждому названа своя запись рецептов;
  B. близнец A с SEED_PER_SUITE=false — посев не позван ни разу, вместо него
     принудительный перевыпуск окружений второй и третьей;
  C. близнец A с SEED=false (стенд посеян заранее) — ни посева, ни перевыпуска;
  D. посев перед второй суитой отказал — прогон выходит кодом 2 и третья суита
     не запускается;
  E. в планировщике `launch_wave` вызов `reseed_before_suite` стоит ДО запуска
     прогонщика суиты (`./scripts/run.sh`) — иначе свежий посев доставался бы
     уже идущей суите;
  F. планировщик, три суиты: перед каждой суитой ровно один путь свежести —
     свежий посев второй и третьей, перевыпуска перед суитой нет ни разу;
  G. прогонщик суиты получает рецепты СВОЕЙ суиты: первая — общий файл посева
     прогона, вторая и третья — файлы своих посевов;
  H. SEED_PER_SUITE=false, перевыпуск отказал — вторая и третья суиты «не
     выполнились» (код суиты 2), первая прошла, планировщик дошёл до конца;
  I. свежий посев не записал рецептов — прогон недействителен (код 2);
  J. обёртка newman перевыпускает по файлу рецептов своей суиты
     (SUITE_TOKEN_STATE), без него — по общему файлу посева прогона.

Предпосылка проверяется: нет функции, ручки или обёртки — отказ, а не тишина.

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
SUITES = ("compute", "storage", "nlb")

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


def shim_body(text: str) -> str:
    m = re.search(r"cat <<'SHIM'\n(.*?)\nSHIM\n", text, re.S)
    return m.group(1) if m else ""


def stubs(td: str, log: pathlib.Path, *, seed: str, seed_rc: int, seed_writes: bool,
          refresh_rc: int) -> str:
    """Окружение функций: посев, перевыпуск и каталоги суит — подменённые, с журналом."""
    write = '[ -n "${2:-}" ] && printf "{}" > "$2"' if seed_writes else ":"
    return f"""
set -uo pipefail
SEED={seed}
TOKEN_STATE_DIR="{td}/state"; mkdir -p "$TOKEN_STATE_DIR"
PRODSEED_TOKEN_STATE="$TOKEN_STATE_DIR/token-recipes.json"; printf '{{}}' > "$PRODSEED_TOKEN_STATE"
NEWMAN_SHIM_DIR="{td}/shim"
suite_dir() {{ echo "{td}/suites/$1"; }}
seed_suites() {{ printf 'SEED %s %s\\n' "$1" "${{2:-}}" >> "{log}"; {write}; return {seed_rc}; }}
refresh_stub() {{ printf 'REFRESH %s\\n' "$*" >> "{log}"; return {refresh_rc}; }}
REFRESH_CMD=(refresh_stub)
"""


def suite_tree(root: pathlib.Path, log: pathlib.Path) -> None:
    for svc in SUITES:
        d = root / "suites" / svc
        (d / "scripts").mkdir(parents=True)
        (d / "environments").mkdir()
        (d / "environments" / "local.postman_environment.json").write_text("{}")
        run = d / "scripts" / "run.sh"
        run.write_text("#!/bin/bash\n"
                       f'printf "RUN %s %s\\n" "{svc}" "${{SUITE_TOKEN_STATE:-}}" >> "{log}"\n')
        run.chmod(0o755)


def run_fn(text: str, knobs: str, *, seed="true", per_suite="true", seed_rc=0, seed_writes=True,
           refresh_rc=0):
    """reseed_before_suite отдельно: три суиты подряд, старт записывается после неё."""
    fn = extract(text, "reseed_before_suite")
    with tempfile.TemporaryDirectory(prefix="seed-per-suite-") as td:
        root = pathlib.Path(td)
        log = root / "seed.log"
        suite_tree(root, log)
        script = stubs(td, log, seed=seed, seed_rc=seed_rc, seed_writes=seed_writes,
                       refresh_rc=refresh_rc) + f"""
{knobs}
{fn}
for svc in {' '.join(SUITES)}; do
  reseed_before_suite "$svc"
  printf 'START %s\\n' "$svc" >> "{log}"
done
"""
        proc = subprocess.run(["bash", "-c", script],
                              env={"PATH": "/usr/bin:/bin", "SEED_PER_SUITE": per_suite},
                              capture_output=True, text=True, timeout=30)
        lines = log.read_text().splitlines() if log.exists() else []
    return proc.returncode, lines, td


def run_wave(text: str, knobs: str, *, seed="true", per_suite="true", seed_rc=0,
             seed_writes=True, refresh_rc=0):
    """launch_wave целиком: три суиты, прогонщик суиты — заглушка, пишущая свой файл рецептов."""
    fns = "\n".join(extract(text, n) for n in ("_wave_wait_below", "reseed_before_suite", "launch_wave"))
    with tempfile.TemporaryDirectory(prefix="seed-per-suite-wave-") as td:
        root = pathlib.Path(td)
        log = root / "wave.log"
        suite_tree(root, log)
        script = stubs(td, log, seed=seed, seed_rc=seed_rc, seed_writes=seed_writes,
                       refresh_rc=refresh_rc) + f"""
{knobs}
JOBS=1; WAVE_JOBS=1; DELAY=0; RC=0
GW_PORT=1; GW_INTERNAL_PORT=2; GW_TLS_PORT=3; IAM_JWKS_PORT=4; IAM_REGTOKEN_PORT=5
OWN_FRONT_ENV_ARGS=(); OWN_FRONT_TLS_ARGS=(); INTERNAL_REST_NEWMAN_ARGS=(); OPT_ENV_ARR=()
declare -A SUITE_PID
WAVE_RUNNING=()
{fns}
launch_wave {' '.join(SUITES)}
printf 'WAVE-RC %s\\n' "$RC" >> "{log}"
"""
        proc = subprocess.run(["bash", "-c", script],
                              env={"PATH": "/usr/bin:/bin", "SEED_PER_SUITE": per_suite},
                              capture_output=True, text=True, timeout=60)
        lines = log.read_text().splitlines() if log.exists() else []
        rcs = {}
        for svc in SUITES:
            f = root / "suites" / svc / "out" / "suite.rc"
            rcs[svc] = f.read_text().strip() if f.exists() else None
    return proc.returncode, lines, rcs, td


def kinds(lines: list[str], kind: str) -> list[str]:
    return [ln.split(" ", 1)[1] if " " in ln else "" for ln in lines if ln.startswith(kind + " ")]


def main() -> int:
    print("гейт: каждая суита шарда стартует на свежих предъявителях — одним путём (#3211, #3238)")
    if not DRIVER.exists():
        print(f"ОТКАЗ: нет прогонщика {DRIVER} — проверять нечего")
        return 1
    text = DRIVER.read_text()
    fn = extract(text, "reseed_before_suite")
    lw = extract(text, "launch_wave")
    shim = shim_body(text)
    knobs = "\n".join(k for k in (knob(text, "SEED_PER_SUITE"), knob(text, "SUITES_LAUNCHED")) if k)
    check("предпосылка: в прогонщике есть reseed_before_suite, launch_wave, обёртка newman и ручки",
          bool(fn) and bool(lw) and bool(shim) and "SEED_PER_SUITE" in knobs and "SUITES_LAUNCHED" in knobs,
          f"функция={bool(fn)} планировщик={bool(lw)} обёртка={bool(shim)} ручки={knobs!r}")

    if fn:
        rc, lines, td = run_fn(text, knobs)
        want = [f"storage {td}/state/storage.token-recipes.json", f"nlb {td}/state/nlb.token-recipes.json"]
        check("A: три суиты — посев перед второй и третьей, по одной суите, каждой своя запись рецептов",
              rc == 0 and kinds(lines, "SEED") == want and kinds(lines, "START") == list(SUITES),
              f"rc={rc} журнал={lines}")
        rc, lines, td = run_fn(text, knobs, per_suite="false")
        envs = [f"--force --env {td}/suites/{s}/environments/local.postman_environment.json" for s in SUITES[1:]]
        check("B: SEED_PER_SUITE=false — посева нет, перевыпуск окружений второй и третьей",
              rc == 0 and kinds(lines, "SEED") == [] and kinds(lines, "REFRESH") == envs
              and kinds(lines, "START") == list(SUITES), f"rc={rc} журнал={lines}")
        rc, lines, _ = run_fn(text, knobs, seed="false")
        check("C: SEED=false — ни посева, ни перевыпуска",
              rc == 0 and kinds(lines, "SEED") == [] and kinds(lines, "REFRESH") == []
              and kinds(lines, "START") == list(SUITES), f"rc={rc} журнал={lines}")
        rc, lines, _ = run_fn(text, knobs, seed_rc=1)
        check("D: отказ посева перед второй суитой — код 2, дальше не запускается",
              rc == 2 and [s.split()[0] for s in kinds(lines, "SEED")] == ["storage"]
              and kinds(lines, "START") == ["compute"], f"rc={rc} журнал={lines}")

    i_seed, i_run = lw.find("reseed_before_suite"), lw.find("./scripts/run.sh")
    check("E: в launch_wave посев стоит до запуска прогонщика суиты",
          i_seed >= 0 and i_run >= 0 and i_seed < i_run,
          f"позиции: посев {i_seed}, прогонщик {i_run} (−1 — не найдено)")

    if fn and lw:
        rc, lines, rcs, td = run_wave(text, knobs)
        check("F: планировщик — перед суитой один путь: посев второй и третьей, перевыпуска нет",
              rc == 0 and [s.split()[0] for s in kinds(lines, "SEED")] == ["storage", "nlb"]
              and kinds(lines, "REFRESH") == [] and all(v == "0" for v in rcs.values()),
              f"rc={rc} журнал={lines} коды суит={rcs}")
        want_runs = [f"compute {td}/state/token-recipes.json",
                     f"storage {td}/state/storage.token-recipes.json",
                     f"nlb {td}/state/nlb.token-recipes.json"]
        check("G: прогонщик суиты получает рецепты своей суиты (первая — общий файл посева прогона)",
              kinds(lines, "RUN") == want_runs, f"запуски={kinds(lines, 'RUN')} ждали={want_runs}")
        rc, lines, rcs, td = run_wave(text, knobs, per_suite="false", refresh_rc=1)
        check("H: SEED_PER_SUITE=false, перевыпуск отказал — вторая и третья не выполнились, первая прошла",
              rc == 0 and [r.split()[0] for r in kinds(lines, "RUN")] == ["compute"]
              and rcs == {"compute": "0", "storage": "2", "nlb": "2"} and "WAVE-RC 1" in lines,
              f"rc={rc} журнал={lines} коды суит={rcs}")
        rc, lines, rcs, _ = run_wave(text, knobs, seed_writes=False)
        check("I: свежий посев не записал рецептов — прогон недействителен (код 2)",
              rc == 2 and [r.split()[0] for r in kinds(lines, "RUN")] == ["compute"],
              f"rc={rc} журнал={lines}")

    if shim:
        with tempfile.TemporaryDirectory(prefix="seed-per-suite-shim-") as td:
            log = pathlib.Path(td) / "shim.log"
            script = (f'refresh_stub() {{ printf "REFRESH %s\\n" "$*" >> "{log}"; return 0; }}\n'
                      "REFRESH=(refresh_stub)\nREAL=/bin/true\n" + shim + "\n")
            argv = ["bash", "-c", script, "newman", "run", "c.json", "-e", "/e/env.json"]
            p1 = subprocess.run(argv, env={"PATH": "/usr/bin:/bin", "SUITE_TOKEN_STATE": "/s/storage.json"},
                                capture_output=True, text=True, timeout=30)
            p2 = subprocess.run(argv, env={"PATH": "/usr/bin:/bin"}, capture_output=True, text=True, timeout=30)
            got = log.read_text().splitlines() if log.exists() else []
        check("J: обёртка newman — рецепты своей суиты, без них — общий файл",
              p1.returncode == 0 and p2.returncode == 0
              and got == ["REFRESH --state /s/storage.json --env /e/env.json", "REFRESH --env /e/env.json"],
              f"коды {p1.returncode}/{p2.returncode} журнал={got}")

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
