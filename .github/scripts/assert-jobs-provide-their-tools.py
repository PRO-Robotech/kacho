#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: задание СОЗДАЁТ условие своего прогона, а не берёт его от пула.

ПРЕДМЕТ
-------
Под меткой `ubuntu-latest` в этом репозитории работают ДВА разных окружения:
образ GitHub-hosted и наши `pro-robotech-runner-*`. Кому достанется задание,
решает очередь. Поэтому задание, из шагов которого достижим скрипт, ОТКАЗЫВАЮЩИЙСЯ
работать без инструмента, получает вердикт ПО ЖРЕБИЮ: на одном пуле инструмент в
образе есть, на другом нет, и один и тот же коммит зеленеет и краснеет без единой
правки в дереве.

Отказ при этом приходит КРАСНЫМ и выглядит вердиктом о продукте. Наблюдалось
(#2632, запрос слияния #2633): задание чартов напечатало `PASS: обход
самопроверок`, затем `FATAL: нужен yq (mikefarah v4) …` и упало. Самопроверка
прошла, а цель не исполнилась — то есть вердикта о продукте не выносилось ВОВСЕ,
но круг ранера был потрачен и запрос слияния покраснел следом.

Это третья категория исхода (`testing-verdict.md` §2): «не выполнилось». Она не
вычитается из вердикта, не зачитывается в успех — и не имеет права выглядеть как
красное.

ЧТО ИМЕННО СУДИТ ЭТОТ ГЕЙТ — И ЧЕГО ОН НЕ СУДИТ
-----------------------------------------------
Предмет проверки — ОТКАЗЫВАЮЩЕЕСЯ ПРЕДУСЛОВИЕ: конструкция `command -v X`
(либо `which X`), в чьей ветке провала стоит выход с ненулевым кодом. Это прямое
объявление самого дерева: «без X прогон не состоится». Такое объявление
однозначно, его нельзя спутать с прозой, и оно выводится обходом, а не
выписывается.

Находка — когда такое предусловие ДОСТИЖИМО из шагов задания, а задание
инструмент НЕ СТАВИТ. Достижимость считается транзитивно: шаг → `make <цель>`
(её предпосылки и рецепт) → скрипт → скрипт.

ЧЕГО ГЕЙТ НЕ ЛОВИТ — сказано прямо, потому что молчание тут неотличимо от
чистоты:

  * инструмент, который зовут БЕЗ предусловия. Он даёт `command not found` и код
    127 — тоже «не выполнилось», но дерево о своей нужде нигде не объявляло, и
    вывести её обходом не из чего. Так пропал бы `trivy` (#2630);
  * предусловие, написанное не шеллом: проба Go, падающая `t.Fatalf` без
    инструмента, объявляет то же самое, но другим языком. Так пропал бы `helm`
    в задании юнитов (#2630);
  * версию инструмента. `yq` бывает двух разных программ с одним именем, и
    подмена ловится самим предусловием, а не этим гейтом.

Первые два закрываются установкой инструмента в задании — то есть той же
правкой; гейт их не находит, но и не мешает. Названы они здесь затем, чтобы
«гейт зелёный» не читалось шире, чем есть.

БАЗА ПРОГОНА — И ПОЧЕМУ ЭТО НЕ ТИХОЕ ПОСЛАБЛЕНИЕ
------------------------------------------------
Несколько инструментов исключены: их несёт не образ, а САМА машинерия прогона,
и без них не запустится ни один шаг ни на одном пуле. Перечень мал, назван
поимённо, у каждой записи своё основание (`BASE` ниже).

Исключение обязано ИСТЕКАТЬ САМО: запись базы, на которую в дереве не осталось
ни одного отказывающегося предусловия, — НАХОДКА, а не чистота. Иначе перечень
пережил бы свой предмет и стал слепой зоной.

ПРЕДПОСЫЛКА ГЕЙТА И ОБЪЁМ ОСМОТРЕННОГО
--------------------------------------
Состав берётся обходом ОТСЛЕЖИВАЕМОГО дерева (`git ls-files`) — то же множество,
что увидит CI на свежем checkout'е. Печатается объём осмотренного: процессов,
заданий, шагов, разрешённых целей и скриптов, найденных предусловий. Пустой
обход (ноль процессов, ноль заданий, ноль предусловий) — ОТКАЗ, а не чистота:
он отчитался бы «нарушений нет» ровно тогда, когда сломан сам.

Разбор читает ИСПОЛНЯЕМЫЕ строки: комментарии снимаются ДО разбора. Иначе гейт
краснел бы на собственном объяснении — в этом файле `command -v` встречается, и
командой оно тут не является.

ПЕЧАТАЮТСЯ ДВЕ ВЕЛИЧИНЫ, А НЕ ОДНА
----------------------------------
«Заданий, требующих инструмента» и «из них создают своё условие» — обе. Одно
число скрывает ровно тот случай, ради которого проверка заведена: при пустом
множестве требующих отчёт «нарушений 0» неотличим от исправности.

Запуск:
  python3 .github/scripts/assert-jobs-provide-their-tools.py --self-test
  python3 .github/scripts/assert-jobs-provide-their-tools.py
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import tempfile
from typing import Dict, Iterable, List, Sequence, Set, Tuple

try:
    import yaml
except ImportError:  # pragma: no cover — предпосылка прогона, а не находка
    sys.stderr.write(
        "FATAL: нужен pyyaml — гейт читает РАЗОБРАННЫЙ YAML, а не подстроки.\n"
        "       Прогон НЕ ВЫПОЛНЕН.\n"
    )
    raise SystemExit(2)


# --------------------------------------------------------------------------
# База прогона: инструменты, которые несёт САМА машинерия прогона.
# Запись, на которую не осталось предусловий, объявляется находкой.
# --------------------------------------------------------------------------
BASE: Dict[str, str] = {
    "git": "им работает actions/checkout — без git не стартует ни одно задание",
    "python3": "им исполняются гейты этого каталога, включая этот",
}

# Действия посадки: что именно они кладут на PATH.
SETUP_ACTIONS: Dict[str, Tuple[str, ...]] = {
    "azure/setup-helm": ("helm",),
    "azure/setup-kubectl": ("kubectl",),
    "actions/setup-go": ("go", "gofmt"),
    "actions/setup-node": ("node", "npm", "npx"),
    "actions/setup-python": ("python3", "pip", "pip3"),
    "bufbuild/buf-setup-action": ("buf",),
    "opentofu/setup-opentofu": ("tofu",),
    "hashicorp/setup-terraform": ("terraform",),
    "aquasecurity/trivy-action": ("trivy",),
    "helm/kind-action": ("kind",),
    "engineerd/setup-kind": ("kind",),
}

# Имя двоичного файла не всегда равно пути модуля, которым он ставится.
GO_INSTALL_BINARY: Dict[str, Tuple[str, ...]] = {
    "yq": ("mikefarah/yq",),
    "kubeconform": ("yannh/kubeconform",),
    "golangci-lint": ("golangci/golangci-lint",),
    "grpcurl": ("fullstorydev/grpcurl",),
    "gosec": ("securego/gosec",),
    "govulncheck": ("golang.org/x/vuln",),
}

COMMENT_SH = re.compile(r"(?m)(?<!\$)(?<![\w\"'`])#(?![{(]).*$")

# Отказывающееся предусловие: `command -v X` / `which X`, у которого в ветке
# провала стоит выход с ненулевым кодом. Засчитывается только В ПОЗИЦИИ КОМАНДЫ. Без этого `which` внутри
# английской прозы в строке (`… which would plant …`) читается как предусловие на
# инструмент `would` — наблюдалось на первом же прогоне, находка была ложной.
GUARD = re.compile(
    r"(?:^|[\n;|&(){}@]|\$\(|`|&&|\|\||(?<![\w-])(?:then|else|elif|do|if|while|until)(?![\w-]))"
    r"\s*!?\s*"
    r"(?:command\s+-v|which)\s+(?P<tool>[A-Za-z_][\w.+-]*)",
    re.MULTILINE,
)
REFUSAL = re.compile(
    r"(?:\bexit\s+[1-9]|\bexit\s+\$|\bfatal\b|\bdie\b|FATAL|\breturn\s+[1-9]|"
    r"\bt\.Fatal|ОТКАЗ|НЕ ВЫПОЛНЕН)",
    re.IGNORECASE,
)

MAKE_CALL = re.compile(
    r"(?<![\w./-])make\s+(?P<args>(?:-C\s+\S+\s+)?(?:-[A-Za-z]+\s+)*"
    r"[A-Za-z0-9][\w./-]*(?:\s+[A-Za-z0-9][\w./-]*)*)"
)

# Скрипт считается ВЫЗВАННЫМ только при явном признаке вызова: через
# интерпретатор либо через `./`. Голый путь вызовом НЕ считается — в этом дереве
# он почти всегда УПОМИНАНИЕ: перечень имён гейтов в heredoc'е, путь в тексте
# отказа, координата в документации. Первая редакция засчитывала голый путь и
# втянула в обход весь каталог `.github/scripts/` (183 скрипта вместо 60),
# приписав заданию чартов требование браузера и сканера образов — все находки
# ложные.
#
# `@`, `-`, `+` — префиксы рецепта make (тихо / игнорировать отказ / всегда):
# без них вызов из Makefile не распознаётся, а именно там лежала находка, ради
# которой гейт написан.
SCRIPT_CALL = re.compile(
    r"(?:^|[\n;|&{}@]|\$\(|&&|\|\||(?<![\w-])(?:then|else|elif|do|exec|sudo|time|source)(?![\w-]))"
    r"\s*[-+@]?\s*"
    r"(?:"
    r"(?:/usr/bin/env\s+)?(?:bash|sh|zsh|python3?|py)\s+(?:-[A-Za-z]+\s+)*"
    r"(?P<viainterp>(?:\./)?(?:[\w.-]+/)*[\w.-]+\.(?:sh|bash|py))"
    r"|"
    r"(?P<viadot>\./(?:[\w.-]+/)*[\w.-]+\.(?:sh|bash|py))"
    r")(?![\w/-])",
    re.MULTILINE,
)

# Строка документации Python — проза, а не код: пути в ней суть координаты, а не
# вызовы. Снимается до разбора наравне с комментариями шелла.
PY_DOCSTRING = re.compile(r'(?s)(?<![\w\\])(?P<q>"""|\'\'\').*?(?P=q)')


def run(cmd: Sequence[str], cwd: str) -> str:
    return subprocess.run(
        list(cmd), cwd=cwd, check=True, capture_output=True, text=True
    ).stdout


def tracked_files(root: str) -> List[str]:
    try:
        out = run(["git", "ls-files"], root)
    except (subprocess.CalledProcessError, FileNotFoundError):
        return []
    return [ln for ln in out.splitlines() if ln]


def strip_comments(text: str) -> str:
    return COMMENT_SH.sub("", text or "")


def refusing_guards(text: str) -> Set[str]:
    """Инструменты, без которых этот текст ОТКАЗЫВАЕТСЯ работать.

    Судится ФОРМА отказа, а не близость слова `exit`. Форм ровно две:

      А. `if ! command -v X …; then … exit N … fi` — отрицание плюс выход
         внутри ветки провала;
      Б. `command -v X … || fatal …` — отказ на той же логической строке.

    Всё прочее предусловием НЕ считается, и главный случай назван отдельно:
    `"$(command -v X || true)"` — это ВЫБОР среди кандидатов, отсутствие тут
    проглочено НАМЕРЕННО. Первая редакция judged по окну в 400 символов и
    объявила находкой перебор браузеров в `install-pinned-browser.sh`: `return 1`
    принадлежал функции, а не ветке провала. Три находки из шести были ложными.
    """
    code = strip_comments(text)
    out: Set[str] = set()
    for m in GUARD.finditer(code):
        tool = m.group("tool")
        tail = code[m.end():]
        nl = tail.find("\n")
        same_line = tail[: nl if nl != -1 else len(tail)]

        # Отсутствие проглочено намеренно — это не отказ, а выбор.
        if re.search(r"\|\|\s*(?:true|:|echo|printf)(?![\w-])", same_line):
            continue

        # Форма Б: отказ на той же строке после `||`.
        alt = re.search(r"\|\|\s*(?P<rest>.{0,200})", same_line)
        if alt and REFUSAL.search(alt.group("rest")):
            out.add(tool)
            continue

        # Форма А: отрицание в самом предусловии плюс отказ до закрытия ветки.
        if "!" not in m.group(0):
            continue
        end = re.search(r"\n\s*fi(?![\w-])", tail)
        branch = tail[: end.start()] if end else tail[:400]
        if REFUSAL.search(branch):
            out.add(tool)
    return out


# --------------------------------------------------------------------------
# Makefile: цель -> (предпосылки, рецепт)
# --------------------------------------------------------------------------
class MakeDb:
    def __init__(self, root: str, files: Iterable[str]) -> None:
        self.by_dir: Dict[str, Dict[str, Tuple[List[str], str]]] = {}
        for rel in files:
            if os.path.basename(rel) not in ("Makefile", "makefile", "GNUmakefile"):
                continue
            self.by_dir[os.path.dirname(rel)] = self._parse(os.path.join(root, rel))

    @staticmethod
    def _parse(path: str) -> Dict[str, Tuple[List[str], str]]:
        targets: Dict[str, Tuple[List[str], str]] = {}
        try:
            lines = open(path, encoding="utf-8", errors="replace").read().splitlines()
        except OSError:
            return targets
        rule = re.compile(r"^([A-Za-z0-9][\w./%-]*(?:\s+[A-Za-z0-9][\w./%-]*)*)\s*:(?!=)\s*(.*)$")
        cur: Tuple[List[str], List[str]] | None = None
        recipe: List[str] = []

        def flush() -> None:
            if not cur:
                return
            for name in cur[0]:
                prev = targets.get(name, ([], ""))
                targets[name] = (prev[0] + cur[1], prev[1] + "\n".join(recipe) + "\n")

        for ln in lines:
            if ln.startswith("\t"):
                if cur:
                    recipe.append(ln[1:])
                continue
            flush()
            cur, recipe = None, []
            if ln.lstrip().startswith("#"):
                continue
            m = rule.match(ln)
            if m:
                names = [n for n in m.group(1).split() if not n.startswith("$")]
                prereq = [p for p in m.group(2).split() if not p.startswith("$")]
                cur = (names, prereq)
        flush()
        return targets

    def lookup(self, directory: str, target: str):
        return self.by_dir.get(directory, {}).get(target)


# --------------------------------------------------------------------------
# Достижимость. Множество посещённого — СВОЁ НА КАЖДОЕ ЗАДАНИЕ: общее сделало бы
# приписывание зависимым от порядка обхода (второе задание не получило бы
# ничего от скрипта, который уже посетило первое).
# --------------------------------------------------------------------------
class Resolver:
    def __init__(self, root: str, files: List[str], makedb: MakeDb) -> None:
        self.root = root
        self.makedb = makedb
        self.file_set = set(files)
        self.by_base: Dict[str, List[str]] = {}
        for rel in files:
            self.by_base.setdefault(os.path.basename(rel), []).append(rel)
        self.scripts_total: Set[str] = set()
        self.targets_total: Set[Tuple[str, str]] = set()

    def _read(self, rel: str) -> str:
        try:
            text = open(os.path.join(self.root, rel), encoding="utf-8", errors="replace").read()
        except OSError:
            return ""
        if rel.endswith(".py"):
            text = PY_DOCSTRING.sub("", text)
        return text

    def _resolve(self, token: str, wd: str) -> str | None:
        token = token.lstrip("./")
        cands = []
        if wd:
            cands.append(os.path.normpath(os.path.join(wd, token)))
        cands.append(token)
        for c in cands:
            c = c.lstrip("./")
            if c in self.file_set:
                return c
        hits = self.by_base.get(os.path.basename(token), [])
        return hits[0] if len(hits) == 1 else None

    def requirements(self, text: str, wd: str, seen_s: Set[str], seen_t: Set[Tuple[str, str]]) -> Dict[str, str]:
        out: Dict[str, str] = {}
        self._walk(text, wd, f"шаг (wd={wd or '.'})", out, seen_s, seen_t, 0)
        return out

    def _walk(self, text, wd, where, out, seen_s, seen_t, depth) -> None:
        if depth > 12 or not text:
            return
        code = strip_comments(text)
        for tool in refusing_guards(text):
            out.setdefault(tool, where)

        for m in MAKE_CALL.finditer(code):
            args = m.group("args")
            d = wd
            cm = re.search(r"-C\s+(\S+)", args)
            skip: Set[str] = set()
            if cm:
                raw = cm.group(1)
                skip.add(raw)
                d = os.path.normpath(os.path.join(wd, raw)) if wd else os.path.normpath(raw)
                d = d.strip("/") if d != "." else ""
            for tgt in args.split():
                if tgt.startswith("-") or "=" in tgt or tgt in skip:
                    continue
                self._make_target(d, tgt, out, seen_s, seen_t, depth)

        for m in SCRIPT_CALL.finditer(code):
            rel = self._resolve(m.group("viainterp") or m.group("viadot"), wd)
            if not rel or rel in seen_s:
                continue
            seen_s.add(rel)
            self.scripts_total.add(rel)
            self._walk(self._read(rel), os.path.dirname(rel), rel, out, seen_s, seen_t, depth + 1)

    def _make_target(self, d, tgt, out, seen_s, seen_t, depth) -> None:
        key = (d, tgt)
        if key in seen_t:
            return
        found = self.makedb.lookup(d, tgt)
        if not found:
            return
        seen_t.add(key)
        self.targets_total.add(key)
        prereqs, recipe = found
        self._walk(recipe, d, f"{d or '.'}/Makefile::{tgt}", out, seen_s, seen_t, depth + 1)
        for p in prereqs:
            self._make_target(d, p, out, seen_s, seen_t, depth + 1)


# --------------------------------------------------------------------------
# Что задание ПРЕДОСТАВЛЯЕТ. Окружения заданий раздельны: установка в соседнем
# задании не считается, даже если оно стоит в `needs`.
# --------------------------------------------------------------------------
def job_provides(job: dict) -> Set[str]:
    provided: Set[str] = set()
    for step in job.get("steps") or []:
        uses = (step.get("uses") or "").split("@")[0].strip()
        for prefix, tools in SETUP_ACTIONS.items():
            if uses == prefix or uses.startswith(prefix + "/"):
                provided.update(tools)
        text = strip_comments(step.get("run") or "")
        if not text:
            continue
        for m in re.finditer(
            r"(?:apt-get|apt|apk|dnf|yum)\s+(?:-[^\s]+\s+)*install\s+(?P<pkgs>[^\n&|;]+)", text
        ):
            for tok in m.group("pkgs").split():
                if not tok.startswith("-"):
                    provided.add(tok.split("=")[0])
        for m in re.finditer(r"go\s+install\s+(?P<mod>\S+)", text):
            mod = m.group("mod")
            provided.add(mod.split("@")[0].rstrip("/").split("/")[-1])
            for binary, needles in GO_INSTALL_BINARY.items():
                if any(n in mod for n in needles):
                    provided.add(binary)
        for m in re.finditer(
            r"(?:pip3?|python3?\s+-m\s+pip)\s+install\s+(?P<pkgs>[^\n&|;]+)", text
        ):
            for tok in m.group("pkgs").split():
                if not tok.startswith("-"):
                    provided.add(tok.split("==")[0])
        for m in re.finditer(r"npm\s+(?:i|install)\s+(?:-g|--global)\s+(?P<pkgs>[^\n&|;]+)", text):
            for tok in m.group("pkgs").split():
                if not tok.startswith("-"):
                    provided.add(tok.split("@")[0] or tok)
        for m in re.finditer(r"(?:curl|wget)[^\n]*?(?:-o|--output)\s+(?P<dst>\S+)", text):
            provided.add(os.path.basename(m.group("dst").strip("\"'")))
        for m in re.finditer(
            r"(?:install|mv|cp|ln\s+-s\w*)\s+(?:-[^\s]+\s+)*\S+\s+(?P<dst>\S*/bin/\S*)", text
        ):
            base = os.path.basename(m.group("dst").rstrip("/"))
            if base:
                provided.add(base)
    return provided


def load_jobs(root: str) -> List[Tuple[str, str, dict]]:
    out: List[Tuple[str, str, dict]] = []
    wf_dir = os.path.join(root, ".github", "workflows")
    if not os.path.isdir(wf_dir):
        return out
    for name in sorted(os.listdir(wf_dir)):
        if not name.endswith((".yml", ".yaml")):
            continue
        try:
            doc = yaml.safe_load(open(os.path.join(wf_dir, name), encoding="utf-8"))
        except Exception:
            continue
        if isinstance(doc, dict):
            for jid, job in (doc.get("jobs") or {}).items():
                if isinstance(job, dict):
                    out.append((name, jid, job))
    return out


def analyse(root: str) -> dict:
    files = tracked_files(root)
    makedb = MakeDb(root, files)
    jobs = load_jobs(root)
    resolver = Resolver(root, files, makedb)

    findings: List[Tuple[str, str, str, str]] = []
    steps = 0
    required_any = 0
    satisfied = 0
    demanded: Set[str] = set()

    for wf, jid, job in jobs:
        provided = job_provides(job)
        need: Dict[str, str] = {}
        seen_s: Set[str] = set()
        seen_t: Set[Tuple[str, str]] = set()
        for step in job.get("steps") or []:
            steps += 1
            text = step.get("run")
            if not text:
                continue
            wd = (step.get("working-directory") or "").strip().strip("/")
            if wd == ".":
                wd = ""
            for tool, where in resolver.requirements(text, wd, seen_s, seen_t).items():
                need.setdefault(tool, where)
        demanded |= set(need)
        watched = {t: w for t, w in need.items() if t not in BASE}
        if watched:
            required_any += 1
            missing = sorted(t for t in watched if t not in provided)
            if missing:
                findings.extend((wf, jid, t, watched[t]) for t in missing)
            else:
                satisfied += 1

    stale = sorted(b for b in BASE if b not in demanded)
    return dict(
        files=len(files),
        workflows=len({w for w, _, _ in jobs}),
        jobs=len(jobs),
        steps=steps,
        targets=len(resolver.targets_total),
        scripts=len(resolver.scripts_total),
        guards=sorted(demanded),
        required_any=required_any,
        satisfied=satisfied,
        findings=findings,
        stale_base=stale,
    )


def report(res: dict) -> int:
    print("=== задание создаёт условие своего прогона ===")
    print(
        f"перепись: процессов {res['workflows']} · заданий {res['jobs']} · шагов {res['steps']} "
        f"· make-целей разрешено {res['targets']} · скриптов разрешено {res['scripts']} "
        f"· файлов в индексе {res['files']}"
    )
    print(
        f"инструментов с отказывающимся предусловием: {len(res['guards'])}"
        + (": " + ", ".join(res["guards"]) if res["guards"] else "")
    )
    print(
        f"заданий, требующих инструмента: {res['required_any']} · "
        f"из них создают своё условие: {res['satisfied']}"
    )

    if res["workflows"] == 0 or res["jobs"] == 0 or not res["guards"]:
        print("ОТКАЗ: обход пуст — о дереве не прочитано ничего, вердикт беспредметен.")
        return 1

    rc = 0
    if res["findings"]:
        rc = 1
        print(f"\nНАХОДКИ ({len(res['findings'])}): задание берёт инструмент от пула")
        for wf, jid, tool, where in res["findings"]:
            print(f"  {wf}::{jid} — требует `{tool}`, не ставит его. Отказ объявлен здесь: {where}")
    if res["stale_base"]:
        rc = 1
        print(
            f"\nНАХОДКИ: запись БАЗЫ, которой нечего исключать ({len(res['stale_base'])}): "
            + ", ".join(res["stale_base"])
        )
        print("  На инструмент не осталось предусловий — исключение пережило свой предмет.")
    if rc == 0:
        print("\nOK: каждое задание ставит то, без чего его шаги отказываются работать.")
    return rc


# --------------------------------------------------------------------------
# Инъекция в обе стороны.
# --------------------------------------------------------------------------
WF = """\
name: proba
on: [push]
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
{setup}      - name: progon
        run: |
          # {tool} в комментарии командой не является
          bash scripts/probe.sh
"""

SH_REFUSING = """\
#!/usr/bin/env bash
set -euo pipefail
# про {tool} тут только объяснение
if ! command -v {tool} >/dev/null 2>&1; then
  echo "FATAL: нужен {tool}"
  exit 2
fi
"""

SH_SELECTOR = """\
#!/usr/bin/env bash
set -euo pipefail
# перебор кандидатов: отсутствие проглочено НАМЕРЕННО, это выбор, а не отказ
pick() {{
  local cand
  for cand in "$(command -v {tool} || true)"; do
    if [ -n "$cand" ]; then printf '%s' "$cand"; return 0; fi
  done
  return 1
}}
pick
"""

SH_PIPE_REFUSAL = """\
#!/usr/bin/env bash
set -euo pipefail
command -v {tool} >/dev/null 2>&1 || fatal "нет {tool}"
"""

SH_OPTIONAL = """\
#!/usr/bin/env bash
set -euo pipefail
if command -v {tool} >/dev/null 2>&1; then
  echo "нашёлся, пользуемся"
else
  echo "не нашёлся — пропускаем, это законно"
fi
"""


def _tree(base: str, script: str, wf: str) -> str:
    root = tempfile.mkdtemp(dir=base)
    os.makedirs(os.path.join(root, ".github", "workflows"))
    os.makedirs(os.path.join(root, "scripts"))
    open(os.path.join(root, "scripts", "probe.sh"), "w").write(script)
    open(os.path.join(root, ".github", "workflows", "probe.yml"), "w").write(wf)
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    subprocess.run(["git", "add", "-A"], cwd=root, check=True)
    return root


def self_test() -> int:
    ok = True
    n = 0
    setup = "      - uses: azure/setup-helm@v4\n"
    with tempfile.TemporaryDirectory() as tmp:
        # (а) ДЕФЕКТ: отказывающееся предусловие достижимо, инструмент не ставится.
        r = analyse(_tree(tmp, SH_REFUSING.format(tool="helm"), WF.format(setup="", tool="helm")))
        n += 1
        hit = [f for f in r["findings"] if f[2] == "helm"]
        if not hit:
            ok = False
            print("ПРОВАЛ (а): дефект внесён, гейт молчит.")
        elif "probe.sh" not in hit[0][3]:
            ok = False
            print(f"ПРОВАЛ (а): находка не называет координату отказа: {hit[0][3]}")
        else:
            print(f"(а) дефект: НАХОДКА, координата названа — {hit[0][3]}")

        # (б) ЗАКОННЫЙ БЛИЗНЕЦ: та же форма, инструмент ставится действием посадки.
        r = analyse(_tree(tmp, SH_REFUSING.format(tool="helm"), WF.format(setup=setup, tool="helm")))
        n += 1
        if [f for f in r["findings"] if f[2] == "helm"]:
            ok = False
            print("ПРОВАЛ (б): законный близнец объявлен находкой.")
        else:
            print("(б) инструмент ставится посадкой: МОЛЧИТ")

        # (в) ЗАКОННЫЙ БЛИЗНЕЦ: предусловие НЕ отказывается — пропуск законен.
        r = analyse(_tree(tmp, SH_OPTIONAL.format(tool="helm"), WF.format(setup="", tool="helm")))
        n += 1
        if [f for f in r["findings"] if f[2] == "helm"]:
            ok = False
            print("ПРОВАЛ (в): необязательное предусловие объявлено находкой.")
        else:
            print("(в) предусловие без отказа: МОЛЧИТ")

        # (г) КОММЕНТАРИЙ НЕ ЕСТЬ КОМАНДА.
        r = analyse(
            _tree(
                tmp,
                "#!/usr/bin/env bash\n# if ! command -v trivy; then exit 1; fi — это объяснение\necho ok\n",
                WF.format(setup="", tool="trivy"),
            )
        )
        n += 1
        if [f for f in r["findings"] if f[2] == "trivy"]:
            ok = False
            print("ПРОВАЛ (г): предусловие в комментарии засчитано исполняемым.")
        else:
            print("(г) предусловие только в комментарии: МОЛЧИТ")

        # (д) УПОМИНАНИЕ СКРИПТА — не вызов: обход не обязан втягивать всё дерево.
        root = tempfile.mkdtemp(dir=tmp)
        os.makedirs(os.path.join(root, ".github", "workflows"))
        os.makedirs(os.path.join(root, "scripts"))
        open(os.path.join(root, "scripts", "probe.sh"), "w").write(
            SH_REFUSING.format(tool="kubectl")
        )
        open(os.path.join(root, ".github", "workflows", "probe.yml"), "w").write(
            "name: p\non: [push]\njobs:\n  p:\n    runs-on: ubuntu-latest\n    steps:\n"
            '      - name: r\n        run: |\n          echo "подробности в scripts/probe.sh"\n'
        )
        subprocess.run(["git", "init", "-q"], cwd=root, check=True)
        subprocess.run(["git", "add", "-A"], cwd=root, check=True)
        r = analyse(root)
        n += 1
        if [f for f in r["findings"] if f[2] == "kubectl"]:
            ok = False
            print("ПРОВАЛ (д): упоминание пути засчитано вызовом.")
        else:
            print("(д) путь упомянут в сообщении, не вызван: МОЛЧИТ")

        # (в2) ВЫБОР СРЕДИ КАНДИДАТОВ — не отказ: `|| true` глотает отсутствие
        # намеренно, а `return 1` принадлежит функции, а не ветке провала.
        r = analyse(_tree(tmp, SH_SELECTOR.format(tool="chromium"),
                          WF.format(setup="", tool="chromium")))
        n += 1
        if [f for f in r["findings"] if f[2] == "chromium"]:
            ok = False
            print("ПРОВАЛ (в2): перебор кандидатов объявлен отказом.")
        else:
            print("(в2) перебор кандидатов с `|| true`: МОЛЧИТ")

        # (в3) ФОРМА Б: отказ через `|| fatal` на той же строке обязан находиться.
        r = analyse(_tree(tmp, SH_PIPE_REFUSAL.format(tool="kubeconform"),
                          WF.format(setup="", tool="kubeconform")))
        n += 1
        if not [f for f in r["findings"] if f[2] == "kubeconform"]:
            ok = False
            print("ПРОВАЛ (в3): отказ формы `|| fatal` не распознан.")
        else:
            print("(в3) отказ формы `|| fatal`: НАХОДКА")

        # (е) ТРАНЗИТИВНОСТЬ через make: дефект на глубине 2 обязан находиться.
        root = tempfile.mkdtemp(dir=tmp)
        os.makedirs(os.path.join(root, ".github", "workflows"))
        os.makedirs(os.path.join(root, "d", "scripts"))
        open(os.path.join(root, "d", "scripts", "deep.sh"), "w").write(
            SH_REFUSING.format(tool="yq")
        )
        open(os.path.join(root, "d", "Makefile"), "w").write(
            "cel:\n\t@bash scripts/deep.sh\n"
        )
        open(os.path.join(root, ".github", "workflows", "probe.yml"), "w").write(
            "name: p\non: [push]\njobs:\n  p:\n    runs-on: ubuntu-latest\n    steps:\n"
            "      - name: r\n        working-directory: d\n        run: make cel\n"
        )
        subprocess.run(["git", "init", "-q"], cwd=root, check=True)
        subprocess.run(["git", "add", "-A"], cwd=root, check=True)
        r = analyse(root)
        n += 1
        hit = [f for f in r["findings"] if f[2] == "yq"]
        if not hit:
            ok = False
            print("ПРОВАЛ (е): дефект за make-целью не найден — обход не транзитивен.")
        else:
            print(f"(е) дефект на глубине 2 (шаг → make → скрипт): НАХОДКА — {hit[0][3]}")

        # (ж) ПУСТОЙ ОБХОД — ОТКАЗ, а не чистота.
        empty = tempfile.mkdtemp(dir=tmp)
        subprocess.run(["git", "init", "-q"], cwd=empty, check=True)
        r = analyse(empty)
        n += 1
        if r["jobs"] != 0:
            ok = False
            print("ПРОВАЛ (ж): пустое дерево дало непустой обход.")
        elif report(r) == 0:
            ok = False
            print("ПРОВАЛ (ж): пустой обход прошёл как чистый.")
        else:
            print("(ж) пустой обход: ОТКАЗ")

    print(f"\nсамопроверка: утверждений {n}, " + ("все выполнены" if ok else "ЕСТЬ ПРОВАЛЫ"))
    return 0 if ok else 1


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="гейт: задание создаёт условие своего прогона")
    ap.add_argument("--root", default=None, help="корень обхода")
    ap.add_argument("--self-test", action="store_true",
                    help="инъекция в обе стороны: дефект краснеет, законный близнец молчит")
    args = ap.parse_args(argv)
    if args.self_test:
        return self_test()
    return report(analyse(args.root or os.getcwd()))


if __name__ == "__main__":
    sys.exit(main())
