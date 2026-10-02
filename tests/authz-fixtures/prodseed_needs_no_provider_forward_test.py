#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: посев матрицы НЕ требует проброса к прежнему OAuth-серверу (задача #2685).

ПРЕДМЕТ. Посев (`prodseed_all.main`) обменивает подписанное утверждение служебной
учётки на токен у НАШЕГО издателя: `mint_rs256.PLATFORM_TOKEN_URL` →
`POST /iam/v1/token`. По публичному слушателю прежнего OAuth-сервера посев не ходит
ни одним шагом. Условие «проброс :14444 отвечает» пережило свой предмет: посев
отказывал `FATAL` по недоступности стороны, с которой не разговаривает, и стенд без
прежнего издателя (цель эпика #2564) посев не проходил по причине, к его работе
отношения не имеющей.

ЧТО УТВЕРЖДАЕТСЯ — ИСХОД, А НЕ ОТСУТСТВИЕ ИМЕНИ. Проба зовёт НАСТОЯЩИЙ `main()` в
мире, где на петле не слушает никто и `kubectl` нет вовсе, и требует двух вещей:

  A. посев доходит до чеканки матрицы и выходит 0;
  B. по дороге не открыто НИ ОДНОГО сокета, не порождено ни одного процесса и не
     сделано ни одного HTTP-запроса — то есть посев не пытается даже спросить.

Поиск по имени функции проброса здесь не судит ничего: переименованный проброс
прошёл бы его, а исход — нет.

ПОЧЕМУ ОТРИЦАНИЕ НЕ ВАКУУМНО. У каждого «прошло» есть близнец той же формы, который
обязан НЕ пройти, и отличается он ровно одним фактом:

  C. чеканка матрицы отказала — `main()` не выходит 0 (проба видит отказ посева,
     а не зеленеет на любом исходе);
  D. клиентский сертификат не добыт — `main()` не выходит 0 (снятие проброса не
     унесло с собой настоящую предпосылку: внутренний порт службы доступа требует
     сертификат в любой посадке).

СЕТИ ЗДЕСЬ НЕТ. Сокеты, процессы, HTTP и часы модуля подменены целиком; чеканка
матрицы подменена двойником, который отвечает сразу. Судится ЛОГИКА предпосылок, а
не доступность стенда: гейт исполним там, где кластера нет. Файл матрицы, который
посев пишет в общий `/tmp`, уводится во временный каталог пробы — чужое имя в общем
каталоге проба не трогает.

КТО ЭТУ ПРОБУ ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — обходом дерева по
образцу `tests/authz-fixtures/*_test.py`. По имени её не зовёт никто.
"""

import pathlib
import sys
import tempfile
import types

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import prodseed_all as seed  # noqa: E402

FAILURES: list[str] = []
CHECKS = [0]

# Файл-передача, который `main()` пишет в общий каталог. Имя берётся тем же
# литералом, что у посева: разошлись — проба увидит запись в настоящий /tmp.
SHARED_HANDOFF = "/tmp/matrix.json"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
    CHECKS[0] += 1
    if not ok:
        FAILURES.append(f"{name}: {detail}")


class Dials:
    """Счёт всякой попытки выйти наружу: сокет, процесс, HTTP."""

    def __init__(self) -> None:
        self.sockets = 0
        self.spawned: list[list[str]] = []
        self.http: list[str] = []

    def total(self) -> int:
        return self.sockets + len(self.spawned) + len(self.http)


def _socket_module(d: Dials):
    class _Sock:
        def __init__(self, *_a, **_kw) -> None:
            d.sockets += 1

        def __enter__(self):
            return self

        def __exit__(self, *_a) -> None:
            return None

        def settimeout(self, _t) -> None:
            return None

        def connect_ex(self, _addr) -> int:
            return 111  # ECONNREFUSED: на петле не слушает никто

    return types.SimpleNamespace(socket=_Sock)


def _subprocess_module(d: Dials):
    class _Popen:
        def __init__(self, argv, *_a, **_kw) -> None:
            d.spawned.append(list(argv))

        def terminate(self) -> None:
            return None

    return types.SimpleNamespace(Popen=_Popen, DEVNULL=-3)


def _urllib_module(d: Dials, real_urllib):
    def urlopen(url, *_a, **_kw):
        d.http.append(str(getattr(url, "full_url", url)))
        raise real_urllib.error.URLError("проба: сети нет")

    request = types.SimpleNamespace(urlopen=urlopen, Request=real_urllib.request.Request)
    return types.SimpleNamespace(request=request, error=real_urllib.error)


class _Clock:
    """Управляемые часы: ожидание двигает время, а не ждёт его."""

    def __init__(self) -> None:
        self.now = 0.0

    def monotonic(self) -> float:
        return self.now

    def time(self) -> float:
        return 1_700_000_000.0 + self.now

    def sleep(self, s: float) -> None:
        self.now += s


def _path_module(real_pathlib, redirect: pathlib.Path):
    """`pathlib` модуля, у которого общий файл-передача уведён во временный каталог."""

    def Path(*a):  # noqa: N802 — имя навязано подменяемым модулем
        p = real_pathlib.Path(*a)
        return redirect if str(p) == SHARED_HANDOFF else p

    return types.SimpleNamespace(Path=Path)


def run(*, matrix_fails: bool = False, certs_fail: bool = False) -> tuple[str, Dials, int]:
    """Зовёт НАСТОЯЩИЙ `prodseed_all.main()` в мире без сети и без kubectl.

    Возвращает (исход, счёт выходов наружу, сколько раз звана чеканка матрицы).
    Мир одного случая отличается от соседнего РОВНО одним объявленным фактом.
    """
    d = Dials()
    calls = [0]

    def fake_seed() -> dict:
        calls[0] += 1
        if matrix_fails:
            raise SystemExit("[prodseed] FATAL: проба — чеканка матрицы отказала")
        return {"jwtBootstrap": "probe-bootstrap", "accountAId": "accPROBE",
                "projectA1Id": "prjPROBE", "projectA2Id": "prjPROBE2",
                "baseUrl": "http://probe.invalid", "internalBaseUrl": "http://probe.invalid"}

    def fake_certs() -> None:
        if certs_fail:
            raise SystemExit("[prodseed] FATAL: проба — клиентского сертификата нет")

    import pathlib as real_pathlib
    import urllib as real_urllib
    import urllib.error  # noqa: F401 — нужен настоящий подмодуль для URLError
    import urllib.request  # noqa: F401

    names = ("socket", "subprocess", "urllib", "time", "shutil", "pathlib", "ensure_certs")
    saved = {n: getattr(seed, n) for n in names if hasattr(seed, n)}
    saved_matrix = sys.modules.get("prodseed_matrix")
    saved_argv = sys.argv
    with tempfile.TemporaryDirectory(prefix="prodseed-no-forward-") as tmp:
        tmpdir = pathlib.Path(tmp)
        seed.socket = _socket_module(d)
        seed.subprocess = _subprocess_module(d)
        seed.urllib = _urllib_module(d, real_urllib)
        seed.time = _Clock()
        seed.shutil = types.SimpleNamespace(which=lambda _name: None)  # kubectl нет вовсе
        seed.pathlib = _path_module(real_pathlib, tmpdir / "matrix.json")
        seed.ensure_certs = fake_certs
        sys.modules["prodseed_matrix"] = types.SimpleNamespace(seed=fake_seed)
        sys.argv = ["prodseed_all.py", "--no-patch-env"]
        import os
        saved_out = os.environ.get("OUT_DIR")
        os.environ["OUT_DIR"] = str(tmpdir / "out")
        try:
            rc = seed.main()
            outcome = f"rc={rc}"
        except SystemExit as e:
            outcome = f"exit: {e}"
        finally:
            if saved_out is None:
                os.environ.pop("OUT_DIR", None)
            else:
                os.environ["OUT_DIR"] = saved_out
            sys.argv = saved_argv
            for n, v in saved.items():
                setattr(seed, n, v)
            if saved_matrix is None:
                sys.modules.pop("prodseed_matrix", None)
            else:
                sys.modules["prodseed_matrix"] = saved_matrix
    return outcome, d, calls[0]


def main() -> int:
    print("гейт: посев матрицы не требует проброса к прежнему OAuth-серверу")

    # A + B. НЕСУЩИЙ СЛУЧАЙ: на петле не слушает никто, kubectl нет.
    outcome, d, calls = run()
    check("A: посев доходит до чеканки и выходит 0 без проброса и без kubectl",
          outcome == "rc=0" and calls == 1,
          f"исход «{outcome}», чеканка звана {calls} раз(а)")
    check("B: по дороге ни сокета, ни процесса, ни HTTP-запроса",
          d.total() == 0,
          f"сокетов {d.sockets}, процессов {d.spawned}, HTTP {d.http}")

    # C. БЛИЗНЕЦ: отказ чеканки обязан быть виден — проба не зеленеет на любом исходе.
    outcome, _d, calls = run(matrix_fails=True)
    check("C: отказ чеканки матрицы — посев НЕ выходит 0",
          outcome != "rc=0" and calls == 1,
          f"исход «{outcome}», чеканка звана {calls} раз(а)")

    # D. БЛИЗНЕЦ: настоящая предпосылка осталась предпосылкой.
    outcome, _d, calls = run(certs_fail=True)
    check("D: сертификата нет — посев НЕ выходит 0 и до чеканки не доходит",
          outcome != "rc=0" and calls == 0,
          f"исход «{outcome}», чеканка звана {calls} раз(а)")

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
