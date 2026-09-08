#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Доказательство: гейт бюджета прогрева СПОСОБЕН упасть и способен смолчать.

Гоняется НАСТОЯЩИЙ гейт `prodseed_hydra_forward_test.main` целиком, а не его пересказ.
Подменяется РОВНО одна вещь — судимая функция `ensure_hydra_forward`, — и по каждой оси
у инъекции есть законный близнец, отличающийся одним фактом.

ПОЧЕМУ ИНЪЕКЦИЯ ТРОГАЕТ ТОЛЬКО СУДИМОЕ. Инъекция, ломающая заодно что-то ещё, приносит
красное от соседа: гейт мог бы оказаться мёртвым и не показать этого ничем. Поэтому
каждая инъекция здесь — самостоятельная реализация одной и той же функции, а мир,
часы и подставные процессы остаются те же, что у гейта.

ОСЬ 1 — асимметричный бюджет (дефект, ради которого гейт заведён): чужому пробросу одна
попытка, своему сорок. Гейт обязан покраснеть, и ИМЕННО на несущем случае и на
утверждении о симметрии, а не «где-нибудь».
ОСЬ 2 — проверка, разучившаяся отказывать: соглашается всегда. Отрицание гейта обязано
это поймать, иначе оно вакуумно.
ОСЬ 3 — отказ, приписывающий неизмеренную причину. Гейт обязан поймать текст.
КОНТРОЛЬ — настоящая функция дерева: гейт обязан СМОЛЧАТЬ.

КТО ЭТУ ПРОБУ ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — обходом дерева по
образцу `tests/authz-fixtures/*_test.py`. По имени её не зовёт никто, поэтому предикат
`git grep <имя файла>` отвечает «не зовёт никто», хотя конвейер исполняет её каждым
прогоном. Проводку держит `tools/pythonprobes`.
"""

import contextlib
import io
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import prodseed_all as seed  # noqa: E402
import prodseed_hydra_forward_test as gate  # noqa: E402

FAILURES: list[str] = []
CHECKS = [0]


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
    CHECKS[0] += 1
    if not ok:
        FAILURES.append(f"{name}: {detail}")


# ── инъекции: каждая — своя реализация ОДНОЙ функции ────────────────────────

def asymmetric_budget():
    """ОСЬ 1. Дословная механика до правки: чужому — один вопрос, своему — сорок."""
    if seed._hydra_serves(seed.HYDRA_PF_PORT):
        return None
    if not seed.shutil.which("kubectl"):
        raise SystemExit("[prodseed] FATAL: kubectl not found")
    if seed._port_is_bound(seed.HYDRA_PF_PORT):
        raise SystemExit(
            f"[prodseed] FATAL: :{seed.HYDRA_PF_PORT} is bound but Hydra does not "
            f"answer on it — a stale port-forward. HYDRA_FORWARD_WARMUP_SECONDS")
    proc = seed.subprocess.Popen(["kubectl"], stdout=seed.subprocess.DEVNULL,
                                 stderr=seed.subprocess.DEVNULL)
    for _ in range(40):
        if seed._hydra_serves(seed.HYDRA_PF_PORT):
            return proc
        seed.time.sleep(0.5)
    proc.terminate()
    raise SystemExit("[prodseed] FATAL: Hydra did not answer")


def never_refuses():
    """ОСЬ 2. Соглашается при любом мире — проверка, потерявшая способность отказать."""
    return None


def blames_an_unmeasured_cause():
    """ОСЬ 3. Бюджет верный, но отказ объясняет себя тем, чего не мерил."""
    if seed._port_is_bound(seed.HYDRA_PF_PORT):
        if seed._wait_until_hydra_serves(seed.HYDRA_PF_PORT, seed.HYDRA_WARMUP_BUDGET_S):
            return None
        raise SystemExit(
            f"[prodseed] FATAL: :{seed.HYDRA_PF_PORT} занят — его под перекатили. "
            f"HYDRA_FORWARD_WARMUP_SECONDS")
    proc = seed.subprocess.Popen(["kubectl"], stdout=seed.subprocess.DEVNULL,
                                 stderr=seed.subprocess.DEVNULL)
    if seed._wait_until_hydra_serves(seed.HYDRA_PF_PORT, seed.HYDRA_WARMUP_BUDGET_S):
        return proc
    proc.terminate()
    raise SystemExit("[prodseed] FATAL: не ответила")


def verdict(fn=None) -> tuple[int, list[str]]:
    """Прогон НАСТОЯЩЕГО гейта. Возвращает (код, имена упавших проверок)."""
    saved_fn = seed.ensure_hydra_forward
    saved_state = (list(gate.FAILURES), gate.CHECKS[0])
    if fn is not None:
        seed.ensure_hydra_forward = fn
    gate.FAILURES.clear()
    gate.CHECKS[0] = 0
    buf = io.StringIO()
    try:
        with contextlib.redirect_stdout(buf):
            rc = gate.main()
    finally:
        seed.ensure_hydra_forward = saved_fn
        gate.FAILURES[:] = saved_state[0]
        gate.CHECKS[0] = saved_state[1]
    # Строка берётся ЦЕЛИКОМ: имена проверок сами содержат двоеточие, и разбор по
    # первому из них резал их посередине — «упало» называло не то, что упало.
    failed = [ln.strip()[5:].strip()
              for ln in buf.getvalue().splitlines() if ln.strip().startswith("FAIL ")]
    return rc, failed


def main() -> int:
    print("доказательство: гейт бюджета прогрева умеет краснеть и умеет молчать")

    # КОНТРОЛЬ — настоящая функция дерева.
    rc, failed = verdict(None)
    check("контроль: на функции дерева гейт МОЛЧИТ", rc == 0 and not failed,
          f"код={rc} упало={failed}")

    # ОСЬ 1 — тот самый дефект.
    rc, failed = verdict(asymmetric_budget)
    check("ось 1: асимметричный бюджет — гейт КРАСНЕЕТ", rc == 1, f"код={rc}")
    check("ось 1: краснеет НА НЕСУЩЕМ случае, а не где-нибудь",
          any("прогрев дожидается ответа" in f for f in failed), f"упало={failed}")
    check("ось 1: краснеет и на утверждении о симметрии бюджета",
          any("бюджет прогрева одинаков" in f for f in failed), f"упало={failed}")

    # ОСЬ 2 — проверка, разучившаяся отказывать.
    rc, failed = verdict(never_refuses)
    check("ось 2: функция, согласная всегда, — гейт КРАСНЕЕТ", rc == 1, f"код={rc}")
    check("ось 2: краснеет на ОТРИЦАНИИ — оно не вакуумно",
          any("отказ остаётся отказом" in f for f in failed), f"упало={failed}")

    # ОСЬ 3 — неизмеренная причина в отказе.
    rc, failed = verdict(blames_an_unmeasured_cause)
    check("ось 3: отказ с неизмеренной причиной — гейт КРАСНЕЕТ", rc == 1, f"код={rc}")
    check("ось 3: краснеет ИМЕННО на приписанной причине",
          any("не приписывает причину" in f for f in failed), f"упало={failed}")

    # Законный близнец оси 3: бюджет тот же, текст честный — молчит. Без него
    # ось 3 ловила бы «любой отказ», а не приписанную причину.
    rc, failed = verdict(None)
    check("законный близнец оси 3: честный текст того же бюджета — МОЛЧИТ",
          rc == 0 and not failed, f"код={rc} упало={failed}")

    print(f"\nперепись: осей проверено 3, проверок исполнено {CHECKS[0]}, "
          f"отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
