#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Состав дерева для гейтов `deploy/scripts`: индекс git — только у верха рабочей копии.

ПРЕДМЕТ (задача #2828)
----------------------
Гейт обходит корень одним из двух способов. В репозитории авторитет — индекс git:
то же множество, что увидит свежий checkout, без локального мусора. В
синтетическом дереве самопроверки индекса нет, и обход идёт по диску.

Выбор между ними делал КОД ВОЗВРАТА `git -C <корень> ls-files`, и это ответ не на
тот вопрос. Внутри объемлющего репозитория команда выходит кодом 0 с ПУСТЫМ
перечнем — каталог в нём просто не отслеживается, — и обход принимал ответ
объемлющего репозитория за состав самого корня. Три самопроверки
(`assert-alt-fixtures-are-another.py`, `assert-posture-branches-can-be-taken.py`,
`assert-report-readers-use-the-summary.py`) выходили кодом 1 ровно тогда, когда
TMPDIR лежал внутри рабочего дерева git, а каталог полос воркспейса лежит именно
там. Перепись самопроверок, зовущих `git ls-files`, при обоих положениях TMPDIR:
краснели эти три, остальные либо не судят синтетический корень индексом, либо
уходят на диск при пустом ответе.

ПРАВИЛО
-------
Индекс авторитетен, только когда корень И ЕСТЬ верх рабочей копии: вывод
`git rev-parse --show-toplevel` совпадает с корнем после разрешения ссылок. Во
всех прочих случаях — корень вне репозитория, внутри чужого, подкаталог
отслеживаемого — обход идёт по диску. Какой способ сработал, называет
`tree_source`: гейт печатает его рядом с объёмом осмотренного, иначе «осмотрено N»
не отличает перечень индекса от диска с локальным мусором. Оба читателя —
`tree_files` и `tree_source` — зовут ОДИН предикат, `is_worktree_top`.

Корень назван верхом рабочей копии, а индекс не читается — это не синтетическое
дерево, и тихо уйти на диск нельзя: исход «не выполнилось», код 2.

ОКРУЖЕНИЕ git ЧИСТИТСЯ. `GIT_DIR` и соседи сильнее рабочего каталога: запущенный
из хука git гейт иначе прочёл бы чужой индекс, а самопроверка ниже, которая
заводит синтетические репозитории, писала бы в него. Перечень переменных тот же,
что у `corelib/gitenv`.

Запуск:
    python3 deploy/scripts/repo_tree.py --self-test
Код возврата: 0 — все случаи выбора способа обхода верны; 1 — нет; 2 — условие
самопроверки не создано (нет git).
"""
from __future__ import annotations

import contextlib
import io
import os
import subprocess
import sys
import tempfile
from typing import Iterator

# Переменные, которые перебивают рабочий каталог при выборе репозитория.
GIT_ENV_VARS = (
    "GIT_DIR",
    "GIT_WORK_TREE",
    "GIT_INDEX_FILE",
    "GIT_OBJECT_DIRECTORY",
    "GIT_ALTERNATE_OBJECT_DIRECTORIES",
    "GIT_COMMON_DIR",
    "GIT_PREFIX",
)

FROM_INDEX = "индекс git"
FROM_DISK = "обход диска"


class ConditionUnmet(SystemExit):
    """Исход «не выполнилось»: вердикт о дереве не вынесен, код 2.

    Наследник SystemExit намеренно: гейт, не перехвативший исключение, выходит
    кодом 2, а не трассой с кодом 1, и прогонщик самопроверок
    (`run-gate-self-tests.sh`) относит его к «условие не создано», а не к
    «самопроверка провалена».
    """

    def __init__(self, why: str) -> None:
        super().__init__(2)
        self.why = why
        print(f"НЕ ВЫПОЛНИЛОСЬ: {why}", file=sys.stderr)


def git_env() -> dict[str, str]:
    return {k: v for k, v in os.environ.items() if k not in GIT_ENV_VARS}


def _git(root: str, *args: str) -> subprocess.CompletedProcess | None:
    """None — git не запустился вовсе (нет бинаря)."""
    try:
        return subprocess.run(["git", "-C", root, *args], capture_output=True,
                              text=True, env=git_env(), check=False)
    except OSError:
        return None


def is_worktree_top(root: str) -> bool:
    """Корень — верх рабочей копии, а не каталог внутри чужой и не вне всякой."""
    r = _git(root, "rev-parse", "--show-toplevel")
    if r is None or r.returncode != 0:
        return False
    top = r.stdout.rstrip("\n")
    return bool(top) and os.path.realpath(top) == os.path.realpath(root)


def tree_source(root: str) -> str:
    return FROM_INDEX if is_worktree_top(root) else FROM_DISK


def tree_files(root: str, prune: tuple[str, ...] = ()) -> list[str]:
    """Пути от корня через `/`, отсортированные.

    `prune` — имена каталогов, которые обход ДИСКА не посещает (`.git` — всегда).
    На индекс не действует: неотслеживаемое в индекс не попадает и так.
    """
    if is_worktree_top(root):
        r = _git(root, "ls-files", "-z")
        if r is None or r.returncode != 0:
            detail = "git не запустился" if r is None else (r.stderr.strip() or f"код {r.returncode}")
            raise ConditionUnmet(
                f"{root} — верх рабочей копии, а `git ls-files` не отработал ({detail}). "
                f"Состав не прочитан; обход диска подменил бы его локальным мусором.")
        return sorted(n for n in r.stdout.split("\0") if n)
    skip = {".git", *prune}
    names = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in skip]
        for fn in filenames:
            names.append(os.path.relpath(os.path.join(dirpath, fn), root).replace(os.sep, "/"))
    return sorted(names)


@contextlib.contextmanager
def nested_fixture_root() -> Iterator[str]:
    """Синтетический корень самопроверки — ВСЕГДА внутри объемлющего репозитория.

    Так самопроверка гейта доказывает независимость его обхода от объемлющего
    репозитория при ЛЮБОМ положении TMPDIR, а не только тогда, когда TMPDIR
    случайно лежит в рабочем дереве. Объемлющий репозиторий заводится свой, в
    том же временном каталоге, и уходит вместе с ним.
    """
    with tempfile.TemporaryDirectory() as td:
        r = _git(td, "init", "-q")
        if r is None or r.returncode != 0:
            detail = "git не запустился" if r is None else r.stderr.strip()
            raise ConditionUnmet(f"объемлющий репозиторий фикстуры не заведён ({detail})")
        root = os.path.join(td, "root")
        os.mkdir(root)
        yield root


# ── самопроверка: каждый способ обхода выбирается там, где должен ────────────
def _write(root: str, rel: str, body: str = "x\n") -> None:
    full = os.path.join(root, rel)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w", encoding="utf-8") as fh:
        fh.write(body)


def _must_git(root: str, *args: str) -> None:
    r = _git(root, *args)
    if r is None or r.returncode != 0:
        raise ConditionUnmet(f"git {' '.join(args)} в {root}: "
                             f"{'git не запустился' if r is None else r.stderr.strip()}")


@contextlib.contextmanager
def _env(**kv: str) -> Iterator[None]:
    saved = {k: os.environ.get(k) for k in kv}
    os.environ.update(kv)
    try:
        yield
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v


def self_test() -> int:
    failures: list[str] = []
    cases = 0

    def check(name: str, got, want) -> None:
        nonlocal cases
        cases += 1
        if got != want:
            failures.append(f"{name}: получено {got!r}, ожидалось {want!r}")

    with tempfile.TemporaryDirectory() as td:
        # Объемлющий репозиторий с отслеживаемым файлом — ему есть что ответить.
        outer = os.path.join(td, "outer")
        os.mkdir(outer)
        _must_git(outer, "init", "-q")
        _write(outer, "outer-tracked.txt")
        _must_git(outer, "add", "outer-tracked.txt")

        # (а) ДЕФЕКТ #2828: синтетический корень внутри объемлющего репозитория.
        #     `git -C <корень> ls-files` отвечает кодом 0 и пустым перечнем; обход
        #     обязан пойти по диску и найти файлы корня.
        nested = os.path.join(outer, "fixture")
        _write(nested, "a.txt")
        _write(nested, "sub/b.txt")
        _write(nested, "node_modules/junk.js")
        check("(а) корень внутри объемлющего: способ", tree_source(nested), FROM_DISK)
        check("(а) корень внутри объемлющего: состав", tree_files(nested),
              ["a.txt", "node_modules/junk.js", "sub/b.txt"])
        check("(а) prune действует на обход диска", tree_files(nested, prune=("node_modules",)),
              ["a.txt", "sub/b.txt"])

        # (б) ЗАКОННЫЙ БЛИЗНЕЦ: корень — сам верх рабочей копии. Авторитет —
        #     индекс: неотслеживаемый файл в состав не входит. Без этой половины
        #     «починка» обходом диска всегда прошла бы (а) и тихо сменила бы
        #     гейтам дерева их множество на диск с мусором.
        top = os.path.join(td, "top")
        os.mkdir(top)
        _must_git(top, "init", "-q")
        _write(top, "tracked.txt")
        _write(top, "dir/tracked2.txt")
        _write(top, "untracked.txt")
        _must_git(top, "add", "tracked.txt", "dir/tracked2.txt")
        check("(б) верх рабочей копии: способ", tree_source(top), FROM_INDEX)
        check("(б) верх рабочей копии: состав", tree_files(top), ["dir/tracked2.txt", "tracked.txt"])

        # (в) Подкаталог отслеживаемого дерева — не верх: диск, вместе с
        #     неотслеживаемым. Граница правила названа случаем, а не шапкой.
        _write(top, "dir/untracked2.txt")
        check("(в) подкаталог рабочей копии: способ", tree_source(os.path.join(top, "dir")), FROM_DISK)
        check("(в) подкаталог рабочей копии: состав", tree_files(os.path.join(top, "dir")),
              ["tracked2.txt", "untracked2.txt"])

        # (г) Корень вне всякого репозитория. Потолок поиска делает случай
        #     детерминированным при любом положении TMPDIR.
        plain = os.path.join(td, "plain")
        _write(plain, "p.txt")
        with _env(GIT_CEILING_DIRECTORIES=td):
            check("(г) корень вне репозитория: способ", tree_source(plain), FROM_DISK)
            check("(г) корень вне репозитория: состав", tree_files(plain), ["p.txt"])

        # (д) GIT_DIR в окружении указывает на ЧУЖОЙ репозиторий (так бывает под
        #     хуком git). Обход обязан прочесть индекс названного корня, а не чужой.
        with _env(GIT_DIR=os.path.join(outer, ".git")):
            check("(д) GIT_DIR чужого репозитория: состав", tree_files(top),
                  ["dir/tracked2.txt", "tracked.txt"])

        # (е) Верх рабочей копии с нечитаемым индексом — «не выполнилось», а не
        #     молчаливый уход на диск.
        broken = os.path.join(td, "broken")
        os.mkdir(broken)
        _must_git(broken, "init", "-q")
        _write(broken, "f.txt")
        _must_git(broken, "add", "f.txt")
        with open(os.path.join(broken, ".git", "index"), "wb") as fh:
            fh.write(b"not an index")
        said = io.StringIO()
        try:
            with contextlib.redirect_stderr(said):
                got = tree_files(broken)
            check("(е) нечитаемый индекс у верха", f"состав {got!r}", "ConditionUnmet")
        except ConditionUnmet as e:
            check("(е) нечитаемый индекс у верха: код", e.code, 2)
            check("(е) отказ называет корень", broken in said.getvalue(), True)

    # (ж) Фикстура самопроверок гейтов действительно вложена в чужой репозиторий
    #     и при этом обходится диском — то, на что опираются их самопроверки.
    with nested_fixture_root() as root:
        _write(root, "x/y.txt")
        parent_top = _git(root, "rev-parse", "--show-toplevel")
        check("(ж) фикстура вложена в объемлющий репозиторий",
              parent_top is not None and parent_top.returncode == 0
              and os.path.realpath(parent_top.stdout.rstrip("\n")) == os.path.realpath(os.path.dirname(root)),
              True)
        check("(ж) фикстура обходится диском", tree_files(root), ["x/y.txt"])

    print(f"===== способ обхода корня: случаев {cases} =====")
    for f in failures:
        print(f"SELF-TEST FAIL {f}", file=sys.stderr)
    if cases == 0:
        print("SELF-TEST FAIL: не проверено ни одного случая", file=sys.stderr)
        return 1
    print("SELF-TEST OK" if not failures else "SELF-TEST FAILED")
    return 0 if not failures else 1


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()
    print("использование: python3 deploy/scripts/repo_tree.py --self-test", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
