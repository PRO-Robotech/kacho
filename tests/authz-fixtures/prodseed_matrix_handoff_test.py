#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: матрица посева передаётся в каталоге СВОЕГО прогона (задача #3264).

ПРЕДМЕТ. Посев матрицы authz-фикстур передаёт её между шагами файлом: базовый
посев (`prodseed_all.main`, прогонщик `prodrun.sh`) пишет, расширения
(`prodseed_<svc>_ext.py`) читают. Пока файл был ОДНИМ на машину, два посева против
разных стендов, идущие параллельно, подменяли матрицу друг другу: расширение одного
стенда читало токены и учётки другого, и прогон получал чужие фикстуры.

ЧТО УТВЕРЖДАЕТСЯ — ИСХОД. Каждый случай гонит НАСТОЯЩИЙ код дважды, двумя
«прогонами» с разными каталогами, и требует, чтобы каждый увидел ровно свою
матрицу:

  A. `prodseed_all.main()` кладёт матрицу в каталог своего прогона (`OUT_DIR`) и
     не пишет ни одного файла вне каталогов прогонов;
  B. `prodseed_all.run_ext` называет расширению путь матрицы своего прогона;
  C. каждое расширение читает матрицу по названному пути, а не по общему;
  D. `prodrun.sh` держит матрицу и кэш расширения в каталоге своего прогона, и
     расширение, запущенное им, видит матрицу своего прогона.

ПОЧЕМУ ОТРИЦАНИЕ НЕ ВАКУУМНО. Близнецы, отличающиеся одним фактом:

  E. путь матрицы не назван — расширение ОТКАЗЫВАЕТ, а не берёт общий файл машины
     (молчаливый откат к общему пути и есть исходный дефект);
  F. перепись: ни один файл каталога посева, кроме этой пробы, не называет
     общий файл матрицы в `/tmp` (предикат задачи).

СЕТИ ЗДЕСЬ НЕТ. Чеканка матрицы, выдача токенов и запись окружений подменены
двойниками; `prodrun.sh` исполняется в копии дерева из двойников, без `kubectl` в
`PATH`. Прогонщик, который всё ещё пишет общий файл машины, НЕ исполняется: проба
не трогает чужое имя в общем каталоге и записывает это отказом, а не пропуском.

КТО ЭТУ ПРОБУ ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — обходом дерева по
образцу `tests/authz-fixtures/*_test.py`. По имени её не зовёт никто.
"""

import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import types

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

FAILURES: list[str] = []
CHECKS = [0]

# Общий каталог машины, куда передача матрицы писаться не должна. Имя собрано из
# частей: цельный литерал общего пути этот файл не несёт (предикат F).
SHARED_DIR = "/" + "tmp"
SHARED_NAME = "matrix" + ".json"
EXT_SEEDERS = ("prodseed_vpc_ext.py", "prodseed_nlb_ext.py", "prodseed_compute_ext.py")
ENV = "PRODSEED_MATRIX_PATH"


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
    CHECKS[0] += 1
    if not ok:
        FAILURES.append(f"{name}: {detail}")


def fixtures_of(stand: str) -> dict:
    return {"jwtBootstrap": f"boot-{stand}", "accountAId": f"acc{stand}",
            "projectA1Id": f"prj{stand}", "projectA2Id": f"prj{stand}2",
            "baseUrl": "http://probe.invalid", "internalBaseUrl": "http://probe.invalid"}


class _Writes:
    """`pathlib` посева, у которого запись в общий каталог машины уведена и учтена."""

    def __init__(self, real_pathlib, sink: pathlib.Path, own_root: pathlib.Path) -> None:
        self.shared: list[str] = []
        outer = self
        own = str(own_root) + "/"

        def Path(*a):  # noqa: N802 — имя навязано подменяемым модулем
            p = real_pathlib.Path(*a)
            # Каталоги прогонов пробы сами лежат во временном каталоге машины —
            # общим считается всё в нём, КРОМЕ корня пробы.
            if str(p).startswith(SHARED_DIR + "/") and not (str(p) + "/").startswith(own):
                outer.shared.append(str(p))
                return sink / p.name
            return p

        self.module = types.SimpleNamespace(Path=Path)


def seed_once(seed, stand: str, out_dir: pathlib.Path, sink: pathlib.Path) -> tuple[str, list[str]]:
    """Один базовый посев `prodseed_all.main()` в мире без сети.

    Мир без сети — тот же, что у соседней пробы посева: сокеты, процессы, HTTP и
    часы модуля подменены её двойниками, `kubectl` нет вовсе.
    """
    import pathlib as real_pathlib
    import urllib as real_urllib
    import urllib.error  # noqa: F401 — нужен настоящий подмодуль для URLError
    import urllib.request  # noqa: F401

    import prodseed_needs_no_provider_forward_test as world

    names = ("socket", "subprocess", "urllib", "time", "shutil", "pathlib", "ensure_certs")
    saved = {n: getattr(seed, n) for n in names if hasattr(seed, n)}
    saved_matrix = sys.modules.get("prodseed_matrix")
    saved_argv = sys.argv
    saved_out = os.environ.get("OUT_DIR")
    writes = _Writes(real_pathlib, sink, sink.parent)
    dials = world.Dials()
    seed.socket = world._socket_module(dials)
    seed.subprocess = world._subprocess_module(dials)
    seed.urllib = world._urllib_module(dials, real_urllib)
    seed.time = world._Clock()
    seed.shutil = types.SimpleNamespace(which=lambda _name: None)
    seed.pathlib = writes.module
    seed.ensure_certs = lambda: None
    sys.modules["prodseed_matrix"] = types.SimpleNamespace(seed=lambda: fixtures_of(stand))
    sys.argv = ["prodseed_all.py", "--no-patch-env"]
    os.environ["OUT_DIR"] = str(out_dir)
    try:
        try:
            outcome = f"rc={seed.main()}"
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
    return outcome, writes.shared


def case_a_b(tmp: pathlib.Path) -> None:
    import prodseed_all as seed

    sink = tmp / "shared-sink"
    sink.mkdir()
    seen = {}
    for stand in ("A", "B"):
        out_dir = tmp / f"run-{stand}"
        outcome, shared = seed_once(seed, stand, out_dir, sink)
        mine = out_dir / "matrix.json"
        got = json.loads(mine.read_text()).get("accountAId") if mine.exists() else None
        seen[stand] = got
        check(f"A{stand}: посев кладёт матрицу в каталог своего прогона",
              outcome == "rc=0" and got == f"acc{stand}",
              f"исход «{outcome}», в {mine} учётка {got!r}")
        check(f"A{stand}: посев не пишет вне каталогов прогонов", not shared,
              f"запись в общий каталог машины: {shared}")
    check("A: два прогона видят каждый свою матрицу", seen == {"A": "accA", "B": "accB"},
          f"увидено {seen}")

    # B. Путь, названный расширению, — путь матрицы своего прогона.
    captured: list[dict] = []

    def fake_run(argv, **kw):
        captured.append(dict(kw.get("env") or {}))
        return types.SimpleNamespace(returncode=0, stdout="{}", stderr="")

    saved_sub = seed.subprocess
    saved_here = seed.HERE
    ext_dir = tmp / "ext-here"
    ext_dir.mkdir()
    (ext_dir / "prodseed_probe_ext.py").write_text("")
    seed.subprocess = types.SimpleNamespace(run=fake_run)
    seed.HERE = ext_dir
    try:
        for stand in ("A", "B"):
            matrix = tmp / f"run-{stand}" / "matrix.json"
            try:
                seed.run_ext("probe", f"prj{stand}", f"prj{stand}2", matrix)
                outcome = "ok"
            except TypeError as e:
                outcome = f"отказ вызова: {e}"
            env = captured[-1] if captured and outcome == "ok" else {}
            check(f"B{stand}: расширению назван путь матрицы своего прогона",
                  env.get(ENV) == str(matrix), f"{outcome}; {ENV}={env.get(ENV)!r}")
    finally:
        seed.subprocess = saved_sub
        seed.HERE = saved_here


class _Stop(Exception):
    """Останов расширения сразу после чтения матрицы — дальше идёт посев по сети."""


def read_by_ext(ext: str, matrix: "pathlib.Path | None") -> tuple[str, object]:
    """Исполняет пролог настоящего расширения до первого сетевого шага."""
    def stop():
        raise _Stop()

    stubs = {
        "mint_rs256": types.SimpleNamespace(mint_bootstrap=stop),
        "prodseed_matrix": types.SimpleNamespace(boot=None, RID="probe"),
        "expired_bearer": types.SimpleNamespace(),
    }
    saved_mods = {k: sys.modules.get(k) for k in stubs}
    saved_env = os.environ.get(ENV)
    sys.modules.update(stubs)
    if matrix is None:
        os.environ.pop(ENV, None)
    else:
        os.environ[ENV] = str(matrix)
    path = HERE / ext
    ns: dict = {"__file__": str(path), "__name__": "probe_ext"}
    try:
        try:
            exec(compile(path.read_text(), str(path), "exec"), ns)  # noqa: S102 — пролог своего файла
            outcome = "дошло до конца без сетевого шага"
        except _Stop:
            outcome = "read"
        except SystemExit as e:
            outcome = f"exit: {e}"
        except Exception as e:  # noqa: BLE001 — исход любого рода и есть предмет
            outcome = f"{type(e).__name__}: {e}"
    finally:
        for k, v in saved_mods.items():
            if v is None:
                sys.modules.pop(k, None)
            else:
                sys.modules[k] = v
        if saved_env is None:
            os.environ.pop(ENV, None)
        else:
            os.environ[ENV] = saved_env
    return outcome, ns.get("MATRIX")


def case_c_e(tmp: pathlib.Path) -> None:
    for stand in ("A", "B"):
        d = tmp / f"ext-run-{stand}"
        d.mkdir()
        (d / "matrix.json").write_text(json.dumps(fixtures_of(stand)))
    for ext in EXT_SEEDERS:
        for stand in ("A", "B"):
            outcome, matrix = read_by_ext(ext, tmp / f"ext-run-{stand}" / "matrix.json")
            got = matrix.get("accountAId") if isinstance(matrix, dict) else None
            check(f"C {ext} {stand}: расширение читает матрицу своего прогона",
                  outcome == "read" and got == f"acc{stand}", f"исход «{outcome}», учётка {got!r}")
        outcome, matrix = read_by_ext(ext, None)
        check(f"E {ext}: путь не назван — отказ, а не общий файл машины",
              outcome.startswith("exit: ") and ENV in outcome and matrix is None,
              f"исход «{outcome}», прочитано {type(matrix).__name__}")


_PRODRUN_STUB_MATRIX = """import json, os
print(json.dumps({"stand": os.environ["PROBE_STAND"]}))
"""
_PRODRUN_STUB_EXT = """import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
import matrix_handoff as mh
print(json.dumps({"extSaw": mh.read()["stand"]}))
"""
_PRODRUN_STUB_PATCH = """import json, os, sys
with open(os.environ["PROBE_LOG"], "a") as fh:
    fh.write(json.dumps(json.load(open(sys.argv[1]))) + "\\n")
"""


def case_d(tmp: pathlib.Path) -> None:
    src = (HERE / "prodrun.sh").read_text()
    if SHARED_DIR + "/" + SHARED_NAME in src or SHARED_DIR + "/matrix-" in src:
        check("D: prodrun.sh держит передачу матрицы в каталоге своего прогона", False,
              "прогонщик пишет общий файл машины — не исполняется, чтобы не тронуть чужое имя")
        return
    tree = tmp / "tree"
    fix = tree / "tests" / "authz-fixtures"
    fix.mkdir(parents=True)
    shutil.copy(HERE / "prodrun.sh", fix / "prodrun.sh")
    handoff = HERE / "matrix_handoff.py"
    if handoff.exists():
        shutil.copy(handoff, fix / "matrix_handoff.py")
    # Пространство имён стенда прогонщик разрешает подключаемым stand-ns.sh (#2882) —
    # настоящим, не двойником: он входит в ключ каталога прогона.
    shutil.copy(HERE / "stand-ns.sh", fix / "stand-ns.sh")
    (fix / "prodseed_matrix.py").write_text(_PRODRUN_STUB_MATRIX)
    (fix / "prodseed_vpc_ext.py").write_text(_PRODRUN_STUB_EXT)
    (fix / "patch-env.py").write_text(_PRODRUN_STUB_PATCH)
    newman = tree / "services" / "vpc" / "tests" / "newman"
    (newman / "environments").mkdir(parents=True)
    (newman / "environments" / "local.postman_environment.json").write_text('{"values": []}')
    (newman / "scripts").mkdir()
    (newman / "scripts" / "run.sh").write_text("#!/usr/bin/env bash\nexit 0\n")
    (newman / "scripts" / "run.sh").chmod(0o755)
    # PATH без kubectl: прогонщик не обращается ни к одному кластеру.
    bindir = tmp / "bin"
    bindir.mkdir()
    for tool in ("bash", "python3", "date", "stat", "mktemp", "mv", "rm", "mkdir", "cat", "tail",
                 "dirname", "sha256sum", "cut", "env", "sleep", "printf"):
        real = shutil.which(tool)
        if real:
            (bindir / tool).symlink_to(real)
    for stand in ("A", "B"):
        run_dir = tmp / f"prodrun-{stand}"
        log = tmp / f"patch-{stand}.log"
        env = {"PATH": str(bindir), "PRODRUN_DIR": str(run_dir), "PROBE_STAND": stand,
               "PROBE_LOG": str(log), "HOME": str(tmp)}
        proc = subprocess.run(["bash", str(fix / "prodrun.sh"), "vpc"], env=env,
                              capture_output=True, text=True, timeout=60)
        merged = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
        mine = run_dir / "matrix.json"
        got = json.loads(mine.read_text()).get("stand") if mine.exists() else None
        check(f"D{stand}: prodrun.sh кладёт матрицу в каталог своего прогона",
              proc.returncode == 0 and got == stand,
              f"rc={proc.returncode}, в {mine} стенд {got!r}; {proc.stderr.strip()[-300:]}")
        check(f"D{stand}: расширение под prodrun.sh видит матрицу своего прогона",
              {"extSaw": stand} in merged and {"stand": stand} in merged,
              f"окружению слито {merged}")
        check(f"D{stand}: кэш расширения — в каталоге своего прогона",
              (run_dir / "matrix-vpc-ext.json").exists(),
              f"в {run_dir} нет matrix-vpc-ext.json")
    # D'. Каталог по умолчанию различается по стенду: одна рабочая копия против двух
    # стендов (разные адреса края) держит две матрицы, а не одну на двоих.
    for stand in ("A", "B"):
        env = {"PATH": str(bindir), "PROBE_STAND": stand, "HOME": str(tmp),
               "PROBE_LOG": str(tmp / f"patch-default-{stand}.log"),
               "BASE_URL": f"http://stand-{stand}.invalid"}
        proc = subprocess.run(["bash", str(fix / "prodrun.sh"), "vpc"], env=env,
                              capture_output=True, text=True, timeout=60)
        check(f"D'{stand}: прогонщик без PRODRUN_DIR отрабатывает", proc.returncode == 0,
              f"rc={proc.returncode}; {proc.stderr.strip()[-300:]}")
    dirs = sorted((fix / "out").glob("prodrun-*")) if (fix / "out").is_dir() else []
    stands = sorted(json.loads((d / "matrix.json").read_text()).get("stand") for d in dirs
                    if (d / "matrix.json").exists())
    check("D': каталоги по умолчанию двух стендов различны, у каждого своя матрица",
          len(dirs) == 2 and stands == ["A", "B"], f"каталоги {[d.name for d in dirs]}, стенды {stands}")


def case_f() -> None:
    needle = SHARED_DIR + "/" + SHARED_NAME
    own = pathlib.Path(__file__).name
    files = [p for p in sorted(HERE.iterdir()) if p.is_file() and p.suffix in (".py", ".sh", ".md")]
    hits = [f"{p.name}:{i}" for p in files if p.name != own
            for i, line in enumerate(p.read_text().splitlines(), 1) if needle in line]
    check(f"F: ни один файл посева не называет общий файл матрицы (осмотрено {len(files)})",
          bool(files) and not hits, f"файлов {len(files)}, вхождения {hits}")


def main() -> int:
    print("гейт: матрица посева передаётся в каталоге своего прогона")
    with tempfile.TemporaryDirectory(prefix="prodseed-handoff-") as t:
        tmp = pathlib.Path(t)
        case_a_b(tmp)
        case_c_e(tmp)
        case_d(tmp)
    case_f()
    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
