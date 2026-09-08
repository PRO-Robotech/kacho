#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: бюджет прогрева проброса Hydra ОДИН на оба пути.

ПРЕДМЕТ. `prodseed_all.ensure_hydra_forward` отвечает на один вопрос — «отвечает ли
Hydra на этом порту» — и приходит к нему двумя путями: проброс открыт НАМИ либо
проброс открыт кем-то другим (харнессом `deploy/scripts/newman-parallel.sh`). Бюджет
прогрева обязан быть одинаков: он про то, сколько времени нужно потоку `kubectl
port-forward` встать, а не про то, кто этот проброс открыл.

ПОЧЕМУ ЭТО ГЕЙТ, А НЕ ЗАМЕТКА. Прежняя редакция давала своему пробросу сорок попыток,
а чужому — ОДНУ. Путь «чужой» — единственный, каким ходит конвейер, и на нём отказ
рушил ВЕСЬ шард: посев не состоялся, суиты не запускались, вердикта не было ни у
одной. Наблюдалось на вершине ствола 052ac97a39: два шарда спросили Hydra через 6.1s
и 6.2s после открытия проброса — один умер, второй ответил за 0.3s.

ЧАСЫ УПРАВЛЯЕМЫЕ. Бюджет меряется по `time.monotonic`, поэтому проба подменяет модулю
его `time` целиком: иначе вердикт зависел бы от загрузки машины, а гейт со случайным
исходом отключают первым.

СЕТИ ЗДЕСЬ НЕТ. Ни один случай не открывает сокета и не зовёт kubectl: судится ЛОГИКА
ожидания, а не доступность стенда. Гейт обязан быть исполним там, где кластера нет.

КТО ЭТУ ПРОБУ ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — обходом дерева по
образцу `tests/authz-fixtures/*_test.py`. По имени её не зовёт никто, и искать
вызывающего предикатом `git grep <имя файла>` бесполезно. Проводку держит
`tools/pythonprobes`.
"""

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import prodseed_all as seed  # noqa: E402

FAILURES: list[str] = []
CHECKS = [0]


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
    CHECKS[0] += 1
    if not ok:
        FAILURES.append(f"{name}: {detail}")


class Clock:
    """Управляемые часы: `sleep` двигает время, а не ждёт его."""

    def __init__(self) -> None:
        self.now = 0.0

    def monotonic(self) -> float:
        return self.now

    def sleep(self, d: float) -> None:
        self.now += d


class Popen:
    """Подставной процесс проброса. Настоящий kubectl не зовётся никогда."""

    def __init__(self, *_a, **_kw) -> None:
        self.terminated = False

    def terminate(self) -> None:
        self.terminated = True


class Subprocess:
    DEVNULL = -3

    def __init__(self) -> None:
        self.spawned: list[Popen] = []

    def Popen(self, *a, **kw):  # noqa: N802 — имя навязано подменяемым модулем
        p = Popen(*a, **kw)
        self.spawned.append(p)
        return p


class World:
    """Мир одного случая. Отличается от соседнего РОВНО одним объявленным фактом."""

    def __init__(self, *, bound: bool, answers_after: int | None, have_kubectl: bool = True):
        self.bound = bound
        self.answers_after = answers_after   # None — не отвечает никогда
        self.asks = 0
        self.clock = Clock()
        self.subprocess = Subprocess()
        self.have_kubectl = have_kubectl

    def hydra_serves(self, _port: int) -> bool:
        self.asks += 1
        if self.answers_after is None:
            return False
        return self.asks > self.answers_after

    def port_is_bound(self, _port: int) -> bool:
        return self.bound

    def which(self, _name: str):
        return "/usr/bin/kubectl" if self.have_kubectl else None


def run(world: World):
    """Зовёт НАСТОЯЩУЮ `ensure_hydra_forward` в подставленном мире."""
    saved = (seed.time, seed.subprocess, seed._hydra_serves, seed._port_is_bound, seed.shutil)
    shutil_stub = type("S", (), {"which": staticmethod(world.which)})
    seed.time = world.clock
    seed.subprocess = world.subprocess
    seed._hydra_serves = world.hydra_serves
    seed._port_is_bound = world.port_is_bound
    seed.shutil = shutil_stub
    try:
        return ("ok", seed.ensure_hydra_forward())
    except SystemExit as e:
        return ("exit", str(e))
    finally:
        (seed.time, seed.subprocess, seed._hydra_serves,
         seed._port_is_bound, seed.shutil) = saved


def main() -> int:
    print("гейт: бюджет прогрева проброса Hydra — один на оба пути")

    # A. НЕСУЩИЙ СЛУЧАЙ. Чужой проброс, поток к API-серверу встаёт не мгновенно.
    #    До правки здесь был отказ, и он убивал весь шард.
    w = World(bound=True, answers_after=4)
    kind, val = run(w)
    check("чужой проброс: прогрев дожидается ответа, а не объявляется протухшим",
          kind == "ok" and val is None, f"исход={kind} значение={val!r}")

    # B. ЗАКОННЫЙ БЛИЗНЕЦ отрицания: отличается от A ровно тем, что ответа НЕТ.
    #    Без него A зеленел бы на проверке, которая не умеет отказывать вовсе.
    w = World(bound=True, answers_after=None)
    kind, val = run(w)
    check("чужой проброс, ответа нет за весь бюджет — отказ остаётся отказом",
          kind == "exit", f"исход={kind} значение={val!r}")

    # C. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: отвечает сразу — переиспользуем, kubectl не зовём.
    w = World(bound=True, answers_after=0)
    kind, val = run(w)
    check("чужой проброс, ответ сразу — переиспользован, свой не открывается",
          kind == "ok" and val is None and not w.subprocess.spawned,
          f"исход={kind} значение={val!r} открыто={len(w.subprocess.spawned)}")

    # D. СВОЙ путь, прогрев: открываем проброс и дожидаемся.
    w = World(bound=False, answers_after=4)
    kind, val = run(w)
    check("свой проброс: открыт и дождался",
          kind == "ok" and isinstance(val, Popen) and len(w.subprocess.spawned) == 1,
          f"исход={kind} значение={val!r} открыто={len(w.subprocess.spawned)}")

    # E. СВОЙ путь, не ожил: отказ И снятие своего процесса (иначе он переживёт посев).
    w = World(bound=False, answers_after=None)
    kind, val = run(w)
    check("свой проброс не ожил — отказ, и наш процесс снят",
          kind == "exit" and len(w.subprocess.spawned) == 1
          and w.subprocess.spawned[0].terminated,
          f"исход={kind} снят={w.subprocess.spawned[0].terminated if w.subprocess.spawned else 'процесса нет'}")

    # F. НЕСУЩЕЕ СВОЙСТВО, утверждаемое ПРЯМО: бюджет у обоих путей ОДИН.
    #    Считается по числу заданных вопросов — величине наблюдаемой, а не объявленной.
    foreign = World(bound=True, answers_after=None)
    run(foreign)
    own = World(bound=False, answers_after=None)
    run(own)
    check("бюджет прогрева одинаков у чужого и своего проброса",
          foreign.asks == own.asks and foreign.asks > 1,
          f"чужой спросил {foreign.asks} раз(а), свой — {own.asks}")

    # G. Диагностика называет ИЗМЕРЕННОЕ и не утверждает причины, которой не мерила.
    #    Исход проверяется ПЕРВЫМ: судимая функция может и не отказать вовсе, и тогда
    #    текста нет. Гейт обязан назвать это отказом, а не рухнуть — упавший гейт
    #    вердикта не выносит НИ ОДНОГО, включая проверки, которые успели пройти.
    w = World(bound=True, answers_after=None)
    kind, msg = run(w)
    text = msg if kind == "exit" and isinstance(msg, str) else ""
    check("отказ не приписывает причину, которую не измерял",
          kind == "exit" and "перекат" not in text and "re-roll" not in text,
          f"исход={kind} текст={text[:160]!r}")
    check("отказ называет ручку бюджета — читателю есть что покрутить",
          kind == "exit" and "HYDRA_FORWARD_WARMUP_SECONDS" in text,
          f"исход={kind} текст={text[:160]!r}")

    # H. Бюджет КОНЕЧЕН: ожидание завершается, а не висит. Часы управляемые, поэтому
    #    «завершилось» здесь — факт, а не наблюдение за секундомером.
    w = World(bound=True, answers_after=None)
    run(w)
    check("ожидание конечно: часы дошли до бюджета и остановились",
          w.clock.now >= seed.HYDRA_WARMUP_BUDGET_S,
          f"часы={w.clock.now} бюджет={seed.HYDRA_WARMUP_BUDGET_S}")

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
