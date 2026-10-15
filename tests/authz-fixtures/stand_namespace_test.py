#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: посев ходит в кластер только к пространству имён названного стенда (задача #2882).

ПРЕДМЕТ. Пространство имён стенда посев раньше читал из разных переменных:
прогонщики и `setup.sh` — `SETUP_NS`, `mint_rs256.py` и `prodseed_matrix.py` —
`KACHO_NAMESPACE`, `prodrun.sh` держал `kacho` литералом. Стенд не в пространстве
по умолчанию при одной заданной переменной получал часть шагов посева к ЧУЖОМУ
пространству. Теперь пространство разрешается в одном месте — правилом `stand_ns.py` для python и
`stand-ns.sh` для оболочки, равенство которых судит ось P, — и
передаётся всем шагам; явно переданный материал не подменяется повторным
извлечением; заданные и расходящиеся переменные — отказ до первого обращения.

ЧТО УТВЕРЖДАЕТСЯ — ИСХОД. Заглушка `kubectl` пишет пространство имён КАЖДОГО
вызова; проба краснеет, если хоть один ушёл не в целевое. Заглушка отвечает
«не найдено» на любое нецелевое пространство — так красный прогон не пишет
материал поверх чужого. Шаги, которым посев передаёт работу (`prodseed_all.py`,
`prodseed_matrix.py`), подменены записью окружения, с которым их позвали.

ОСИ И БЛИЗНЕЦЫ (у каждого «прошло» есть близнец, обязанный не пройти):
  A. только SETUP_NS — модули посева на python: все вызовы в целевое, вызовов > 0;
  B. только KACHO_NAMESPACE — `setup.sh`: все вызовы в целевое, шаг посева получает
     целевое в обеих переменных;
  C. только SETUP_NS — `prodrun.sh`: все вызовы в целевое, шаг матрицы получает
     целевое в обеих переменных;
  D. обе заданы и расходятся — python, `setup.sh`, `prodrun.sh`: код не 0, ни
     одного вызова кластера, ни одного шага, отказ называет обе переменные;
     близнец D′ — обе заданы и СОВПАДАЮТ: отказа нет;
  E. материал передан явно — файлы не тронуты, его секретов не спрашивали;
     близнец F — не передан: материал извлечён из кластера (положительный контроль);
  P. две реализации правила — `stand_ns.py` (python) и `stand-ns.sh` (оболочка) —
     дают один ответ на шести сочетаниях, включая расхождение и пустое значение;
  G. перепись: чтение SETUP_NS / KACHO_NAMESPACE в дереве посева и прогонщиков —
     только в двух резолверах; осмотрено файлов > 0.

СЕТИ НЕТ. `kubectl`, `curl`, `grpcurl` и передача шагов подменены; весь материал —
во временном каталоге пробы, который снимается и на падении.

КТО ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — обходом по образцу
`tests/authz-fixtures/*_test.py`.
"""

import ast
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parent.parent
TARGET = "t2882-probe-target"
OTHER = "t2882-probe-other"
CLUSTER_MATERIAL = "cluster-material"
EXPLICIT_MATERIAL = "explicit-material"

FAILURES: list[str] = []
CHECKS = [0]


def check(name: str, ok: bool, detail: str = "") -> None:
    CHECKS[0] += 1
    print(f"  {'ok ' if ok else 'ОТКАЗ'} {name}" + ("" if ok else f" — {detail}"))
    if not ok:
        FAILURES.append(f"{name}: {detail}")


KUBECTL_STUB = r"""#!/usr/bin/env bash
ns=""; prev=""
for a in "$@"; do [ "$prev" = "-n" ] && ns="$a"; prev="$a"; done
printf '%s|%s\n' "$ns" "$*" >> "$PROBE_KUBECTL_LOG"
if [ "$ns" != "$PROBE_TARGET_NS" ]; then
  echo "Error from server (NotFound): namespaces \"$ns\" not found" >&2; exit 1
fi
case " $* " in
  *" -o "*|*" -o="*|*"jsonpath="*) printf '%s' "$(printf '%s' "$PROBE_MATERIAL" | base64 -w0)" ;;
esac
exit 0
"""

CURL_STUB = """#!/usr/bin/env bash
printf 401
"""

GRPCURL_STUB = """#!/usr/bin/env bash
echo "grpcurl: probe stub, no network" >&2; exit 1
"""

PYTHON_STUB = """#!/usr/bin/env bash
case "${1:-}" in
  */prodseed_all.py|*/prodseed_matrix.py)
    printf 'STEP %s SETUP_NS=%s KACHO_NAMESPACE=%s\\n' "$(basename "$1")" \\
      "${SETUP_NS-<unset>}" "${KACHO_NAMESPACE-<unset>}" >> "$PROBE_STEPS_LOG"
    case "$1" in */prodseed_matrix.py) echo '{"probe": 1}' ;; esac
    exit 0 ;;
esac
exec "@REAL_PYTHON@" "$@"
"""

# Модули посева на python: каждый путь, что обращается к кластеру. Матрица мимо —
# она чеканит при импорте и без стенда не импортируется; её чтение пространства
# судит перепись G, а её шаг — оси B и C через передачу окружения.
PY_UNITS = r"""
import sys
sys.path.insert(0, sys.argv[1])
try:
    import prodseed_all as s
    import mint_rs256 as m
except SystemExit as e:
    print(f"REFUSED {e}", file=sys.stderr)
    sys.exit(2)
for f in (s.ensure_certs, m.ensure_iam_internal_cert, m.mint_bootstrap,
          lambda: m.ensure_iam_server_ca()):
    try:
        f()
    except (SystemExit, Exception) as e:
        print(f"step {getattr(f, '__name__', 'f')}: {e}", file=sys.stderr)
"""


class World:
    def __init__(self) -> None:
        self.dir = pathlib.Path(tempfile.mkdtemp(prefix="stand-ns-probe-"))
        self.bin = self.dir / "bin"
        self.bin.mkdir()
        for name, body in (("kubectl", KUBECTL_STUB), ("curl", CURL_STUB),
                           ("grpcurl", GRPCURL_STUB),
                           ("python3", PYTHON_STUB.replace("@REAL_PYTHON@", sys.executable))):
            p = self.bin / name
            p.write_text(body)
            p.chmod(0o755)
        (self.dir / "units.py").write_text(PY_UNITS)

    def close(self) -> None:
        shutil.rmtree(self.dir, ignore_errors=True)

    def run(self, argv: list[str], env_ns: dict[str, str], extra: dict[str, str] | None = None):
        run_dir = pathlib.Path(tempfile.mkdtemp(prefix="run-", dir=self.dir))
        klog, slog = run_dir / "kubectl.log", run_dir / "steps.log"
        klog.touch()
        slog.touch()
        env = {k: v for k, v in os.environ.items() if k not in ("SETUP_NS", "KACHO_NAMESPACE")}
        for k in list(env):
            # Материал и адреса вызывающего не доезжают до пробы: она судит своё.
            if k.startswith(("BOOTSTRAP_", "IAM_", "INTERNAL_REST_", "PRODRUN_")):
                env.pop(k)
        env.update({
            "PATH": f"{self.bin}:{os.environ.get('PATH', '')}",
            "PROBE_KUBECTL_LOG": str(klog), "PROBE_STEPS_LOG": str(slog),
            "PROBE_TARGET_NS": TARGET, "PROBE_MATERIAL": CLUSTER_MATERIAL,
            "BOOTSTRAP_OPERATOR_CERT_DIR": str(run_dir / "operator"),
            "IAM_MTLS_CERT_DIR": str(run_dir / "iam-mtls"),
            "IAM_SERVER_CA_DIR": str(run_dir / "server-ca"),
            "PRODRUN_CACHE_DIR": str(run_dir / "cache"),
            "OUT_DIR": str(run_dir / "out"),
            "BASE_URL": "http://127.0.0.1:9",
            "POSTURE_PROBE_RETRY_DELAY": "0",
        })
        env.update(env_ns)
        env.update(extra or {})
        proc = subprocess.run(argv, env=env, capture_output=True, text=True, timeout=120)
        calls = [ln.split("|", 1)[0] for ln in klog.read_text().splitlines() if ln]
        raw = klog.read_text().splitlines()
        steps = slog.read_text().splitlines()
        return proc, calls, raw, steps, run_dir

    def py_units(self, env_ns, extra=None):
        return self.run([sys.executable, str(self.dir / "units.py"), str(HERE)], env_ns, extra)

    def setup_sh(self, env_ns, extra=None):
        return self.run(["bash", str(HERE / "setup.sh")], env_ns, extra)

    def prodrun_sh(self, env_ns, extra=None):
        return self.run(["bash", str(HERE / "prodrun.sh"), "probe-absent-service"], env_ns, extra)


def all_target(calls: list[str]) -> bool:
    return bool(calls) and all(c == TARGET for c in calls)


def stray(calls: list[str]) -> str:
    return f"вызовов {len(calls)}, нецелевые: {sorted(set(c for c in calls if c != TARGET)) or '—'}"


def step_carries_target(steps: list[str], step: str) -> bool:
    want = f"STEP {step} SETUP_NS={TARGET} KACHO_NAMESPACE={TARGET}"
    return any(s == want for s in steps)


# ── перепись G ──────────────────────────────────────────────────────────────
NS_VARS = ("SETUP_NS", "KACHO_NAMESPACE")
RESOLVERS = ("stand_ns.py", "stand-ns.sh")


def py_env_reads(path: pathlib.Path) -> list[int]:
    """Строки, где код ЧИТАЕТ одну из двух переменных (разбор, не греп по прозе).

    Запись (`os.environ[...] = ...`) — передача ответа дальше, не чтение.
    """
    tree = ast.parse(path.read_text(), str(path))
    lines = []
    for node in ast.walk(tree):
        key = None
        if isinstance(node, ast.Call) and node.args and isinstance(node.args[0], ast.Constant):
            fn = ast.unparse(node.func)
            if fn in ("os.environ.get", "os.getenv", "environ.get", "getenv"):
                key = node.args[0].value
        elif (isinstance(node, ast.Subscript) and isinstance(node.ctx, ast.Load)
              and ast.unparse(node.value) in ("os.environ", "environ")):
            if isinstance(node.slice, ast.Constant):
                key = node.slice.value
        if key in NS_VARS:
            lines.append(node.lineno)
    return lines


SH_READ = re.compile(r"\$\{?(?:%s)\b" % "|".join(NS_VARS))


def sh_env_reads(path: pathlib.Path) -> list[int]:
    out = []
    for i, ln in enumerate(path.read_text().splitlines(), 1):
        code = ln.split("#", 1)[0] if not ln.lstrip().startswith("#") else ""
        if SH_READ.search(code):
            out.append(i)
    return out


def census() -> tuple[int, list[str]]:
    files = sorted(p for p in HERE.iterdir()
                   if p.suffix in (".py", ".sh") and not p.name.endswith("_test.py"))
    files += [ROOT / "deploy/scripts/newman-parallel.sh", ROOT / "deploy/scripts/newman-e2e.sh"]
    found = []
    for p in files:
        if p.name in RESOLVERS:
            continue
        lines = py_env_reads(p) if p.suffix == ".py" else sh_env_reads(p)
        found += [f"{p.relative_to(ROOT)}:{n}" for n in lines]
    return len(files), found


def resolver_answer(argv: list[str], env_ns: dict[str, str]) -> tuple[int, str]:
    env = {k: v for k, v in os.environ.items() if k not in NS_VARS}
    env.update(env_ns)
    proc = subprocess.run(argv, env=env, capture_output=True, text=True, timeout=30)
    return proc.returncode, proc.stdout.strip()


def main() -> int:
    print("гейт: посев ходит в кластер только к пространству имён стенда (#2882)")
    w = World()
    try:
        # A. только SETUP_NS — модули на python.
        _p, calls, _r, _s, _d = w.py_units({"SETUP_NS": TARGET})
        check("A: python, только SETUP_NS — все вызовы kubectl в целевое", all_target(calls), stray(calls))

        # B. только KACHO_NAMESPACE — setup.sh.
        p, calls, _r, steps, _d = w.setup_sh({"KACHO_NAMESPACE": TARGET})
        check("B: setup.sh, только KACHO_NAMESPACE — все вызовы в целевое", all_target(calls), stray(calls))
        check("B: setup.sh передаёт шагу посева целевое в обеих переменных",
              step_carries_target(steps, "prodseed_all.py"), f"rc={p.returncode} шаги={steps}")

        # C. только SETUP_NS — prodrun.sh.
        p, calls, _r, steps, _d = w.prodrun_sh({"SETUP_NS": TARGET})
        check("C: prodrun.sh, только SETUP_NS — все вызовы в целевое", all_target(calls), stray(calls))
        check("C: prodrun.sh передаёт шагу матрицы целевое в обеих переменных",
              step_carries_target(steps, "prodseed_matrix.py"), f"rc={p.returncode} шаги={steps}")

        # D. расхождение — отказ до первого обращения; D′ — совпадение не отказ.
        clash = {"SETUP_NS": TARGET, "KACHO_NAMESPACE": OTHER}
        for label, fn in (("python", w.py_units), ("setup.sh", w.setup_sh), ("prodrun.sh", w.prodrun_sh)):
            p, calls, _r, steps, _d = fn(clash)
            named = "SETUP_NS" in p.stderr and "KACHO_NAMESPACE" in p.stderr
            check(f"D: {label}, переменные расходятся — отказ до кластера",
                  p.returncode != 0 and not calls and not steps and named,
                  f"rc={p.returncode} вызовов={len(calls)} шагов={len(steps)} обе названы={named}")
        p, calls, _r, _s, _d = w.py_units({"SETUP_NS": TARGET, "KACHO_NAMESPACE": TARGET})
        check("D′: python, переменные совпадают — отказа нет, вызовы в целевое",
              p.returncode == 0 and all_target(calls), f"rc={p.returncode} {stray(calls)}")

        # E. явный материал не подменяется; F — без него материал извлечён (контроль).
        exp = w.dir / "explicit"
        exp.mkdir(exist_ok=True)
        explicit = {}
        for var in ("BOOTSTRAP_MINT_MTLS_CERT", "BOOTSTRAP_MINT_MTLS_KEY",
                    "IAM_INTERNAL_GRPC_MTLS_CERT", "IAM_INTERNAL_GRPC_MTLS_KEY", "IAM_SERVER_CA_FILE"):
            f = exp / var.lower()
            f.write_text(EXPLICIT_MATERIAL)
            explicit[var] = str(f)
        _p, calls, raw, _s, _d = w.py_units({"SETUP_NS": TARGET}, explicit)
        kept = all(pathlib.Path(v).read_text() == EXPLICIT_MATERIAL for v in explicit.values())
        asked = [r for r in raw if " get secret " in f" {r.split('|', 1)[1]} "]
        check("E: явно переданный материал не подменён и его секреты не спрошены",
              kept and not asked, f"файлы целы={kept}, обращений к секретам={len(asked)}")
        _p, calls, _r, _s, d = w.py_units({"SETUP_NS": TARGET})
        written = [d / "operator/client.crt", d / "iam-mtls/client.crt", d / "server-ca/ca.crt"]
        got = [f.exists() and f.read_text() == CLUSTER_MATERIAL for f in written]
        check("F: без явного материала он извлечён из кластера (положительный контроль)",
              all(got), f"извлечено {sum(got)} из {len(got)}")

        # P. две реализации одного правила дают один ответ на всех сочетаниях.
        diverged = []
        for env_ns in ({}, {"SETUP_NS": TARGET}, {"KACHO_NAMESPACE": TARGET},
                       {"SETUP_NS": TARGET, "KACHO_NAMESPACE": TARGET},
                       {"SETUP_NS": TARGET, "KACHO_NAMESPACE": OTHER},
                       {"SETUP_NS": "", "KACHO_NAMESPACE": TARGET}):
            py = resolver_answer([sys.executable, str(HERE / "stand_ns.py")], env_ns)
            sh = resolver_answer(["bash", "-c", f'. "{HERE / "stand-ns.sh"}" && stand_ns_resolve '
                                  '&& printf "%s|%s|%s\\n" "$STAND_NS" "$SETUP_NS" "$KACHO_NAMESPACE"'],
                                 env_ns)
            sh_ns = sh[1].split("|")[0] if sh[0] == 0 else ""
            sh_prop = sh[0] != 0 or len(set(sh[1].split("|"))) == 1
            if (py[0] == 0) != (sh[0] == 0) or (py[0] == 0 and py[1] != sh_ns) or not sh_prop:
                diverged.append(f"{env_ns}: python={py} shell={sh}")
        check("P: stand_ns.py и stand-ns.sh дают один ответ (6 сочетаний)", not diverged, "; ".join(diverged))

        # G. перепись чтений.
        n, found = census()
        have = all((HERE / r).exists() for r in RESOLVERS)
        check("G: SETUP_NS / KACHO_NAMESPACE читают только stand_ns.py и stand-ns.sh",
              n > 0 and not found and have,
              f"осмотрено {n}, резолверы есть={have}, чтения вне них: {found}")
        print(f"  перепись G: файлов осмотрено {n}, чтений вне резолвера {len(found)}")
    finally:
        w.close()

    print(f"\nперепись: проверок исполнено {CHECKS[0]}, отказов {len(FAILURES)}")
    if not CHECKS[0]:
        print("ОТКАЗ: не исполнено ни одной проверки — это немота, а не чистота")
        return 1
    for f in FAILURES:
        print(f"  ОТКАЗ: {f}")
    return 1 if FAILURES else 0


if __name__ == "__main__":
    sys.exit(main())
