#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: сетевая поверхность сборки образа равна её графу импортов.

ПРЕДМЕТ
-------
`go mod download` без аргументов тянет модули, ОБЪЯВЛЕННЫЕ в go.mod, а не те,
что компилирует собираемый бинарь. Замер на 007e3e99: объявлено 142, а
компилируется от 27 (край, geo, registry, storage) до 82 (iam) — то есть от 60
до 115 модулей на каждый из восьми образов загружались, чтобы не попасть ни в
один бинарь.

Цена не в трафике. Каждая лишняя загрузка — ещё один способ уронить сборку по
причине, к продукту отношения не имеющей, и он сработал: образ упал на модуле
поставщика инструментов инфраструктуры (`hashicorp`), которого его бинарь не
компилирует. Строк `hashicorp` в go.mod — 23; в графе импортов ЛЮБОГО из восьми
образов — 0. Шард сквозного прогона получил тогда «не выполнилось»: стенд не
поднялся, вердикта не осталось ни у одной пробы.

ЧТО ИМЕННО ТРЕБУЕТСЯ
--------------------
Шаг, который в образе тянет модули, обязан называть ПАКЕТЫ, а не молчать:

  * `go mod download` без аргументов — находка. Он не знает, что собирается;
  * `go list -deps <пакеты>` / `go mod download <модули>` — законно;
  * шага загрузки нет вовсе — тоже законно: `go build` дотянет ровно нужное.
    Отдельный шаг существует ради диагностики, а не ради полноты.

И отдельно — РАСХОЖДЕНИЕ СПИСКОВ. Если шаг загрузки называет пакеты, они обязаны
быть теми же, что собирает `go build` ниже. Расхождение не ломает сборку
(недостающее догрузит `go build`), поэтому само себя не обнаружит никогда — и
именно поэтому его проверяет гейт, а не прогон.

ПРЕДПОСЫЛКА ГЕЙТА И ОБЪЁМ ОСМОТРЕННОГО
--------------------------------------
Осматриваются Dockerfile'ы, содержащие `go build` — то есть собирающие Go.
Состав берётся обходом ОТСЛЕЖИВАЕМОГО дерева (`git ls-files`): это то же
множество, что увидит CI на свежем checkout'е. Ноль найденных Dockerfile'ов —
ОТКАЗ, а не чистота: пустой обход отчитался бы «всё в порядке» ровно тогда,
когда сломан он сам.

Разбор читает ИСПОЛНЯЕМЫЕ строки: комментарий, объясняющий этот самый запрет,
содержит слова `go mod download` — и первая редакция гейта краснела на
собственном объяснении. Строки комментариев снимаются до разбора.

Запуск:
  python3 .github/scripts/assert-build-fetch-matches-imports.py --self-test
  python3 .github/scripts/assert-build-fetch-matches-imports.py
"""
from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

# Пакет в командной строке go: ./services/vpc/cmd/vpc, ./gateway/cmd/api-gateway.
PKG_RE = re.compile(r"\./[A-Za-z0-9_./-]+")

# Голая загрузка: `go mod download` без единого аргумента после неё.
BARE_DOWNLOAD_RE = re.compile(r"\bgo\s+mod\s+download\s*(?:$|&&|\||;|>)")

# Шаг загрузки, называющий предмет.
TARGETED_FETCH_RE = re.compile(r"\bgo\s+(?:list\s+-deps|mod\s+download)\s+\S")

GO_BUILD_RE = re.compile(r"\bgo\s+build\b")


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def list_dockerfiles(root: Path) -> list[str]:
    """Состав — по содержимому репозитория, с откатом на обход ФС.

    Откат нужен синтетическому дереву самопроверки, где git недоступен; тот же
    приём и по той же причине, что в `.github/scripts/run-python-probes.py`.
    """
    try:
        out = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z", "--", "*Dockerfile"],
            capture_output=True, text=True, timeout=60, check=True).stdout
        names = [n for n in out.split("\0") if n]
        if names:
            return sorted(names)
    except (subprocess.SubprocessError, OSError):
        pass
    found = []
    for dirpath, _dirs, files in os.walk(root):
        for f in files:
            if f == "Dockerfile" or f.endswith("Dockerfile"):
                rel = os.path.relpath(os.path.join(dirpath, f), root)
                found.append(rel.replace(os.sep, "/"))
    return sorted(found)


def executable_lines(body: str) -> list[tuple[int, str]]:
    """Строки Dockerfile БЕЗ комментариев, со склейкой переносов `\\`.

    Комментарий снимается до разбора: объяснение этого запрета само содержит
    `go mod download`, и разбор по сырому тексту краснел бы на нём.
    """
    out: list[tuple[int, str]] = []
    acc, start = "", 0
    for i, raw in enumerate(body.split("\n"), start=1):
        s = raw.strip()
        if s.startswith("#"):
            continue
        if not acc:
            start = i
        if s.endswith("\\"):
            acc += s[:-1] + " "
            continue
        acc += s
        if acc.strip():
            out.append((start, acc.strip()))
        acc = ""
    if acc.strip():
        out.append((start, acc.strip()))
    return out


def inspect(rel: str, body: str) -> tuple[bool, list[str], list[str], list[str]]:
    """Разбирает один Dockerfile.

    Возвращает (собирает ли Go, пакеты сборки, пакеты загрузки, находки).
    """
    lines = executable_lines(body)
    builds: list[str] = []
    fetched: list[str] = []
    findings: list[str] = []
    builds_go = False

    for ln, text in lines:
        if GO_BUILD_RE.search(text):
            builds_go = True
            for p in PKG_RE.findall(text):
                if p not in builds:
                    builds.append(p)
        if BARE_DOWNLOAD_RE.search(text):
            findings.append(
                f"{rel}:{ln}: `go mod download` без аргументов — тянет весь "
                f"объявленный граф модуля, а не то, что компилирует этот образ. "
                f"Назови пакеты (`go list -deps <пакеты>`) либо убери шаг вовсе: "
                f"`go build` дотянет ровно нужное")
        elif TARGETED_FETCH_RE.search(text):
            for p in PKG_RE.findall(text):
                if p not in fetched:
                    fetched.append(p)

    if builds_go and fetched:
        extra = [p for p in fetched if p not in builds]
        missing = [p for p in builds if p not in fetched]
        if extra:
            findings.append(
                f"{rel}: шаг загрузки называет пакеты, которых образ не собирает: "
                f"{', '.join(extra)} — списки разошлись, и сборка об этом молчит")
        if missing:
            findings.append(
                f"{rel}: образ собирает пакеты, которых шаг загрузки не называет: "
                f"{', '.join(missing)} — списки разошлись, и сборка об этом молчит")

    return builds_go, builds, fetched, findings


def execute(root: Path) -> int:
    files = list_dockerfiles(root)

    print("===== сетевая поверхность сборки: перепись =====")
    print(f"Dockerfile'ов в дереве: {len(files)}")

    if not files:
        print("ОТКАЗ: не найдено ни одного Dockerfile — обход сломан либо они "
              "переехали. Пустой обход не является доказательством чистоты.",
              file=sys.stderr)
        return 2

    findings: list[str] = []
    go_images = 0
    targeted = 0
    no_step = 0

    for rel in files:
        try:
            body = (root / rel).read_text(encoding="utf-8")
        except OSError as e:
            findings.append(f"{rel}: не читается: {e}")
            continue
        builds_go, builds, fetched, f = inspect(rel, body)
        findings += f
        if not builds_go:
            continue
        go_images += 1
        if fetched:
            targeted += 1
            print(f"  {rel}: собирает {len(builds)}, загрузка называет {len(fetched)}")
        elif not any("go mod download" in x for x in f):
            no_step += 1
            print(f"  {rel}: собирает {len(builds)}, отдельного шага загрузки нет")

    print()
    print(f"===== ИТОГ: Dockerfile'ов {len(files)}; собирают Go {go_images}; "
          f"загрузка по графу импортов {targeted}; без отдельного шага {no_step}; "
          f"находок {len(findings)} =====")

    # Ноль образов, собирающих Go, — ОТКАЗ. Предмета у переписи нет, значит её
    # «чисто» не является утверждением о дереве.
    if go_images == 0:
        print("ОТКАЗ: ни один Dockerfile не собирает Go — гейту нечего осматривать. "
              "Это не «находок нет», это «прочитано ноль».", file=sys.stderr)
        return 2

    if findings:
        print(f"ПРОВАЛ: {len(findings)} находк(и)", file=sys.stderr)
        for x in findings:
            print(f"  - {x}", file=sys.stderr)
        return 1

    print(f"PASS: все {go_images} образ(ов) тянут ровно то, что компилируют")
    return 0


# ── ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ, В ОБЕ СТОРОНЫ ─────────────────────────────────

_HEAD = "FROM golang:1.26-alpine AS builder\nWORKDIR /src\nCOPY . .\n"
_BUILD = "RUN go build -o /x ./services/x/cmd/x\n"

# Дефект: голая загрузка.
_BARE = _HEAD + "RUN --mount=type=cache,target=/go/pkg/mod go mod download\n" + _BUILD
# Законный близнец ТОЙ ЖЕ формы: загрузка называет тот же пакет.
_TARGETED = _HEAD + "RUN go list -deps ./services/x/cmd/x >/dev/null\n" + _BUILD
# Тоже законно: шага нет вовсе.
_NOSTEP = _HEAD + _BUILD
# Дефект второго рода: списки разошлись.
_DRIFT = _HEAD + "RUN go list -deps ./services/y/cmd/y >/dev/null\n" + _BUILD
# Комментарий, содержащий запрещённую строку. Гейт обязан молчать: он читает
# исполняемые строки, а не прозу — иначе краснел бы на собственном объяснении.
_COMMENT_ONLY = (_HEAD + "# Здесь стоял `go mod download` без аргументов — снят.\n"
                 + "RUN go list -deps ./services/x/cmd/x >/dev/null\n" + _BUILD)
# Перенос строки: голая загрузка, разложенная на две строки обратным слэшем.
_WRAPPED = (_HEAD + "RUN --mount=type=cache,target=/go/pkg/mod \\\n    go mod download\n"
            + _BUILD)
# Образ, не собирающий Go (сайт документации): предмета нет, гейт его не судит.
_NON_GO = "FROM nginx:alpine\nCOPY build /usr/share/nginx/html\n"


def _tree(files: dict[str, str]) -> Path:
    root = Path(tempfile.mkdtemp(prefix="buildfetch-selftest-"))
    for rel, body in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(body)
    return root


def self_test() -> int:
    import contextlib
    import io

    failures: list[str] = []

    def check(label: str, cond: bool, detail: str = "") -> None:
        if cond:
            print(f"  ок     {label}")
        else:
            print(f"  ПРОВАЛ {label}  {detail}")
            failures.append(label)

    def run(files: dict[str, str]) -> tuple[int, str]:
        root = _tree(files)
        buf = io.StringIO()
        try:
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                rc = execute(root)
        finally:
            shutil.rmtree(root, ignore_errors=True)
        return rc, buf.getvalue()

    print("(a) дефект внесён — гейт обязан покраснеть и назвать координату")
    rc, out = run({"services/x/Dockerfile": _BARE})
    check("краснеет на голой загрузке", rc == 1, out)
    check("называет файл и строку", "services/x/Dockerfile:4" in out, out)

    print("(b) законный близнец той же формы — гейт обязан молчать")
    rc, out = run({"services/x/Dockerfile": _TARGETED})
    check("молчит на адресной загрузке", rc == 0, out)
    check("перепись печатает объём", "собирают Go 1" in out, out)

    print("(c) шага загрузки нет вовсе — тоже законно")
    rc, out = run({"services/x/Dockerfile": _NOSTEP})
    check("молчит, когда шага нет", rc == 0, out)
    check("перепись считает такой образ", "без отдельного шага 1" in out, out)

    print("(d) списки разошлись — находка, которую сборка не обнаружит сама")
    rc, out = run({"services/x/Dockerfile": _DRIFT})
    check("краснеет на расхождении", rc == 1, out)
    check("называет обе стороны расхождения",
          "./services/y/cmd/y" in out and "./services/x/cmd/x" in out, out)

    print("(e) запрещённая строка В КОММЕНТАРИИ — гейт читает код, а не прозу")
    rc, out = run({"services/x/Dockerfile": _COMMENT_ONLY})
    check("молчит на объяснении запрета", rc == 0, out)

    print("(f) голая загрузка, разложенная переносом строки — тот же дефект")
    rc, out = run({"services/x/Dockerfile": _WRAPPED})
    check("краснеет на перенесённой строке", rc == 1, out)

    print("(g) образ, не собирающий Go, предметом гейта не является")
    rc, out = run({"ui/x/Dockerfile": _NON_GO, "services/x/Dockerfile": _TARGETED})
    check("судит только собирающих Go", rc == 0, out)
    check("перепись отличает состав от предмета",
          "Dockerfile'ов в дереве: 2" in out and "собирают Go 1" in out, out)

    print("(h) беспредметная перепись — ОТКАЗ, а не успех")
    rc, out = run({"ui/x/Dockerfile": _NON_GO})
    check("отвергает дерево без единого образа на Go", rc == 2, out)
    check("говорит «прочитано ноль»", "прочитано ноль" in out, out)
    rc, out = run({"README.md": "x"})
    check("отвергает пустой обход", rc == 2, out)

    print()
    if failures:
        print(f"САМОПРОВЕРКА ПРОВАЛЕНА: {len(failures)} — {', '.join(failures)}",
              file=sys.stderr)
        return 1
    print("ДОКАЗАНО: гейт краснеет на голой загрузке и на расхождении списков, "
          "молчит на адресной загрузке, на её отсутствии и на комментарии, "
          "отвергает беспредметную перепись.")
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--root", default=None, help="корень обхода")
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией: краснеет на дефекте, молчит на законной форме")
    args = ap.parse_args(argv)
    if args.self_test:
        return self_test()
    return execute(Path(args.root).resolve() if args.root else repo_root())


if __name__ == "__main__":
    sys.exit(main())
