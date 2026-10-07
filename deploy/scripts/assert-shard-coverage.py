#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""assert-shard-coverage.py — шардирование не должно стать способом не гонять кейс.

ПРЕДМЕТ. До разбиения по раннерам один прогон судил ВСЕ коллекции дерева, и
«потерять» коллекцию было негде: она либо есть в дереве, либо её нет. После
разбиения появляется третье состояние — коллекция ЕСТЬ, а ни один шард её не
берёт. Такое дерево выглядит здоровым с любой стороны: файл на месте, гейт суиты
зелёный у всех, кто её гонял (никто), сводный вердикт складывает нули. Это ровно
тот класс, который правила называют «форма проверки без содержания», и он тем
опаснее, что заводится не правкой теста, а правкой РАСПИСАНИЯ.

ЧТО УТВЕРЖДАЕТСЯ (по дереву, а не по памяти):
  1. каждая суита дерева назначена РОВНО одному шарду;
  2. ни один шард не называет суиту, которой в дереве нет;
  3. сумма коллекций по шардам равна числу коллекций в дереве, и равенство
     держится ПОКОЛЛЕКЦИОННО, а не только итогом (иначе потеря в одной суите
     компенсируется добавкой в другой);
  4. состав суит совпадает с умолчанием SERVICES в newman-parallel.sh — новая
     суита не может появиться в прогоне, минуя это разбиение;
  5. каждый компонент, который шард называет, объявлен в gates, и каждый gate
     реально условен в Chart.yaml зонтичного чарта (условие, которого нет,
     рендерится успешно и не делает ничего — измеренный дефект vpc/compute).

ЕДИНИЦА СЧЁТА — ОТСЛЕЖИВАЕМЫЙ git-элемент (`git ls-files`), а не файл на диске:
`gen.py` кладёт коллекции в тот же каталог, и рабочая копия после прогона
содержит артефакты, которых нет в дереве. Считать диск значило бы получать
разные числа до и после прогона.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль находок» обязано быть отличимо от «ноль
прочитанного»: если предикат перестанет находить коллекции, это будет видно
числом, а не молчанием.

  6. карта «компонент → образ» называет образы, которые знает сборка;
  7. имя, которым шард объявляет отсутствие сервиса, понимает гейт посадки;
  8. объединение доменов ban #6 по шардам покрывает КАЖДЫЙ домен, у которого этот
     запрет имеет ПРЕДМЕТ (проба ban #6 сужается составом стенда — без этой
     проверки домен мог бы не измеряться ни на одном шарде, оставаясь зелёным
     везде).

     ПРЕДМЕТ — не «есть Internal*-контракт», а «контракт кто-то регистрирует».
     Контракт, который не провязан ни одним композиционным корнем, недостижим на
     внешнем листенере by construction, и требовать его измерения значило бы
     требовать зелёного из отсутствия: встречный контроль пробы такой домен не
     подтвердит, а уронит прогон как невыполненное измерение. Принадлежность к
     предмету ВЫВОДИТСЯ ИЗ ДЕРЕВА одним предикатом с пробой
     (`e2e-ban6-domains.py`), поэтому ведомости прощённых здесь нет ни одной
     строки, а послабление истекает само: появится регистрация — домен войдёт в
     охват, и гейт покраснеет, пока его не возьмёт шард. Непровязанный домен при
     этом НАЗЫВАЕТСЯ переписью на каждом прогоне: «нечего измерять» и «забыли
     измерить» обязаны быть различимы.

Самопроверка (`--self-test`) вносит дефекты по одному и требует красного на
каждом, плюс держит рядом ЗАКОННЫХ близнецов. Состав СЧИТАЕТСЯ, а не выписывается:
`--self-test | grep -c 'ждали красного'` и то же про зелёный. Гейт, который не
покраснел на внесённом дефекте, — не гейт; гейт, у которого нет молчащей стороны,
доказывает лишь чувствительность к правке.
"""
from __future__ import annotations

import importlib.util
import json
import pathlib
import re
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
DEPLOY = HERE.parent
ROOT = DEPLOY.parent
MANIFEST = DEPLOY / "e2e-shards.json"
PARALLEL_SH = DEPLOY / "scripts" / "newman-parallel.sh"
UMBRELLA_CHART = DEPLOY / "helm" / "umbrella" / "Chart.yaml"

COLLECTION_GLOBS = (
    "services/*/tests/newman/collections/*.postman_collection.json",
    "gateway/tests/newman/collections/*.postman_collection.json",
)


# SHARD_DOC — документ, из которого выведено разбиение. Его таблица §2 несёт вес
# каждой суиты, а §4 берёт из неё долю раннера.
SHARD_DOC = "deploy/E2E-SHARDS.md"

# SHARD_DOC_ROW — размеченная строка таблицы весов: имя суиты, коллекции, запросы.
# Итоговая строка выделена жирным и потому разбирается отдельным образцом: одна
# грамматика на обе дала бы «итого» суитой.
SHARD_DOC_ROW = re.compile(r'^\|\s*([a-z][a-z0-9-]*)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|')
SHARD_DOC_TOTAL = re.compile(r'^\|\s*\*\*итого\*\*\s*\|\s*\*\*(\d+)\*\*\s*\|\s*\*\*(\d+)\*\*\s*\|')


def shard_doc_weights(text: str) -> tuple[dict[str, tuple[int, int]], tuple[int, int] | None]:
    """Строки таблицы весов документа: suite -> (коллекций, запросов) плюс итог."""
    rows: dict[str, tuple[int, int]] = {}
    total: tuple[int, int] | None = None
    for line in text.splitlines():
        m = SHARD_DOC_ROW.match(line)
        if m:
            rows[m.group(1)] = (int(m.group(2)), int(m.group(3)))
            continue
        m = SHARD_DOC_TOTAL.match(line)
        if m:
            total = (int(m.group(1)), int(m.group(2)))
    return rows, total


def _transports_module():
    """Тот же вывод спроса, что исполняет прогонщик, — не вторая его реализация.

    Гейт и прогонщик обязаны отвечать на «кто набирает этот транспорт» ОДНИМ
    предикатом. Две реализации разошлись бы молча и разошлись бы именно там, где
    расхождение не видно: обе отвечают «да» на очевидном входе.
    """
    path = HERE / "e2e-optional-transports.py"
    spec = importlib.util.spec_from_file_location("e2e_optional_transports", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def tracked_collections(root: pathlib.Path) -> dict[str, list[str]]:
    """suite -> отсортированный список stem'ов коллекций, ОТСЛЕЖИВАЕМЫХ git."""
    out: dict[str, list[str]] = {}
    r = subprocess.run(["git", "-C", str(root), "ls-files", "--", *COLLECTION_GLOBS],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit(f"FATAL: git ls-files не отработал в {root}: {r.stderr.strip()}")
    for line in r.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split("/")
        suite = parts[1] if parts[0] == "services" else "api-gateway"
        stem = parts[-1][: -len(".postman_collection.json")]
        out.setdefault(suite, []).append(stem)
    for k in out:
        out[k].sort()
    return out


def parallel_default_services(path: pathlib.Path) -> list[str]:
    """Умолчание SERVICES из newman-parallel.sh — что прогон возьмёт, если не сузить."""
    text = path.read_text(encoding="utf-8")
    m = re.search(r'^SERVICES="\$\{SERVICES:-([^}]*)\}"', text, re.M)
    if not m:
        raise SystemExit(f"FATAL: в {path} не найдено умолчание SERVICES — "
                         "предпосылка гейта отпала, чинить надо гейт, а не молчать")
    return m.group(1).split()


def gated_deps(path: pathlib.Path) -> set[str]:
    """Имена зависимостей зонтичного чарта, у которых ЕСТЬ `condition: <имя>.enabled`."""
    gated: set[str] = set()
    for m in re.finditer(r'^\s*condition:\s*([A-Za-z0-9_.-]+)\.enabled\s*$',
                         path.read_text(encoding="utf-8"), re.M):
        gated.add(m.group(1))
    return gated


_BAN6_MOD = None


def _ban6_module():
    """Тот же предикат популяции ban #6, что исполняет проба, — не вторая его реализация.

    Гейт и проба обязаны отвечать на «у каких доменов ban #6 имеет ПРЕДМЕТ» ОДНИМ
    предикатом. Здесь это не теория: до сведения гейт обходил каталог домена
    целиком (9 доменов), проба адресовала только `*/v1/*.proto` (8), и девятый не
    измерял НИКТО — заметить это можно было лишь сложением двух чисел, которых
    рядом никто не печатал.
    """
    global _BAN6_MOD
    if _BAN6_MOD is None:
        path = HERE / "e2e-ban6-domains.py"
        spec = importlib.util.spec_from_file_location("e2e_ban6_domains", path)
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        _BAN6_MOD = mod
    return _BAN6_MOD


# ЛИДИРУЮЩАЯ пара «N коллекций / M запросов» в пояснении шарда.
#
# ЯКОРЬ `^` — НЕСУЩИЙ, а не косметика. Дальше в тех же пояснениях стоят ЧУЖИЕ
# числа той же формы: «(122 запроса в коллекциях)» у nlb, «vpc 21, storage 11,
# nlb 3 запроса» у iam. Разборщик без якоря схватил бы их и объявил находку там,
# где число верно и относится к другому предмету.
#
# Слева бывает СУММА («7+9», «7+4+1») — по одному слагаемому на суиту шарда, в
# порядке их объявления. Форм именно три, и все выведены из дерева, а не из
# памяти; форма, о которой разборщик не знает, уходит из-под наблюдения молча,
# поэтому нераспознанное пояснение — НАХОДКА, а не тишина.
SHARD_WEIGHT = re.compile(
    r'^(?P<colls>\d+(?:\s*\+\s*\d+)*)\s+коллекц\S*\s*/\s*(?P<reqs>\d+)\s+запрос')

# Число коллекций, названное в прозе НЕ парой, а знаменателем предиката:
# «→ 0 при 88 коллекциях». Тот же класс — выписанное число о дереве без
# владельца, — только оно свидетельствует об ОБЪЁМЕ ОСМОТРЕННОГО, и устаревший
# знаменатель говорит о полноте предиката неправду.
PROSE_TOTAL = re.compile(r'при\s+(?P<total>\d+)\s+коллекц')


def _requests_in(path: pathlib.Path) -> int:
    """Листья коллекции — элементы, несущие `request` (та же единица, что у
    прочих читателей коллекций дерева; папки вложены произвольно)."""
    def walk(items) -> int:
        n = 0
        for it in items or []:
            if "request" in it:
                n += 1
            if "item" in it:
                n += walk(it["item"])
        return n
    return walk(json.loads(path.read_text(encoding="utf-8")).get("item", []))


def tracked_requests(root: pathlib.Path, tree: dict[str, list[str]]) -> dict[str, int]:
    """suite -> число запросов в её ОТСЛЕЖИВАЕМЫХ коллекциях."""
    out: dict[str, int] = {}
    for suite, stems in tree.items():
        base = (root / "gateway" if suite == "api-gateway" else root / "services" / suite)
        base = base / "tests" / "newman" / "collections"
        out[suite] = sum(_requests_in(base / f"{st}.postman_collection.json") for st in stems)
    return out


def chart_templates(root: pathlib.Path) -> dict:
    """путь → текст каждого ОТСЛЕЖИВАЕМОГО шаблона чарта (`*/templates/*`).

    Единица — элемент индекса git, а не файл на диске: под деревом лежат
    распакованные архивы зависимостей и копии полос.
    """
    r = subprocess.run(["git", "-C", str(root), "ls-files", "-z", "--", "*/templates/*"],
                       capture_output=True, text=True)
    if r.returncode != 0:
        return {}
    out = {}
    for rel in filter(None, r.stdout.split("\0")):
        try:
            out[rel] = (root / rel).read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
    return out


_LAUNCH_KEY = re.compile(r'^(?P<ind>\s*)(?:-\s+)?(?P<key>command|args|image):\s*(?P<val>.*)$')


def _argv0(val: str) -> str:
    """Первый элемент значения `command:`/`args:` в форме потока (`["a", "b"]`) либо
    скаляра; пустая строка — значение не задано в строке (блочный список)."""
    v = val.split(" #", 1)[0].strip()
    if v.startswith("["):
        v = v[1:].split(",", 1)[0].rstrip("]")
    return v.strip().strip("'\"").strip()


def _image_name(val: str) -> str:
    """Последний сегмент пути репозитория образа без тега и дайджеста."""
    v = val.split(" #", 1)[0].strip().strip("'\"")
    v = v.split("@", 1)[0]
    last = v.rsplit("/", 1)[-1]
    return last.split(":", 1)[0]


def chart_values(root: pathlib.Path) -> dict:
    """путь → текст каждого ОТСЛЕЖИВАЕМОГО файла значений и `Chart.yaml` чартов.

    Нужны распознавателю запуска образом в ШАБЛОННОЙ форме: строка `image:` чарта
    почти всегда ссылается на значения (`{{ .Values.image.repository }}:…`,
    `{{ include "<чарт>.image" . }}`), и имя репозитория стоит в values, а не в
    шаблоне. Единица — элемент индекса git, как у шаблонов.
    """
    r = subprocess.run(["git", "-C", str(root), "ls-files", "-z", "--",
                        "*/values*.yaml", "*/Chart.yaml"],
                       capture_output=True, text=True)
    if r.returncode != 0:
        return {}
    out = {}
    for rel in filter(None, r.stdout.split("\0")):
        try:
            out[rel] = (root / rel).read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
    return out


_ACTION = re.compile(r'\{\{-?(.*?)-?\}\}', re.S)
_SOURCE = re.compile(r'(?<![\w"])(\$[A-Za-z_]\w*|\$|)((?:\.[A-Za-z_][\w-]*)+)')
_INCLUDE_REF = re.compile(r'\b(?:include|template)\s+"([^"]+)"')
_DEFAULT_LIT = re.compile(r'\bdefault\s+"([^"]*)"')
_DEFINE = re.compile(r'\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}')


def _yaml_load(text: str):
    import yaml
    try:
        return yaml.safe_load(text)
    except yaml.YAMLError:
        return None


class ImageResolver:
    """Кандидаты имени образа для строки `image:` в ШАБЛОННОЙ форме.

    Строка, несущая `{{`, сама имени образа не называет — его называют значения
    чарта. Разрешение идёт по ССЫЛКАМ действия шаблона, а не по тексту рядом:

      * `.Values.<путь>` (и `$.Values.<путь>`) — путь в файлах значений чарта
        (`<чарт>/values*.yaml`) и в поддеревьях зонтичных чартов, подключающих его
        `file://` (ключ — `alias` либо `name` зависимости);
      * `$имя.<поля>` — присваивание `$имя := <выражение>` (в том числе
        `range … $имя := <выражение>` — тогда элементы списка) в тексте того же
        файла либо того `define`, где стоит строка; выражение разрешается тем же
        способом, поля дописываются к его пути;
      * `fromYaml (include "<имя>" .)` с полями — ключ `"<поле>"` словаря,
        который собирает `define` (форма объекта настроек);
      * `include "<имя>" .` без полей — ссылки текста этого `define`, транзитивно,
        в пределах шаблонов ТОГО ЖЕ чарта;
      * `default "<литерал>"` — литерал тоже кандидат.

    Значение-строка — кандидат; значение-таблица — её `repository` либо `image`.
    Строка со схемой (`spiffe://…`, `https://…`) кандидатом не бывает: ссылка на
    образ схемы не несёт, а сегмент SAN `sa/<процесс>` иначе читался бы образом.

    Ссылка, которую разрешить не удалось, — НЕ «не запускает»: она считается и
    называется переписью (`unresolved`), чтобы слепое пятно было видно, а не
    молчаливо.
    """

    def __init__(self, templates: dict, values: dict) -> None:
        self.templates = templates
        self.values = values
        self.stats = {"templated": 0, "resolved": 0, "unresolved": []}
        self._parsed: dict[str, object] = {}
        self._umbrella_keys = self._parents()
        self._seen_lines: set[tuple[str, str]] = set()

    def _doc(self, path: str):
        if path not in self._parsed:
            text = self.values.get(path)
            self._parsed[path] = _yaml_load(text) if text is not None else None
        return self._parsed[path]

    def _parents(self) -> dict[str, list[tuple[str, str]]]:
        """каталог подчарта → [(каталог зонтика, ключ значений)] по `file://`."""
        out: dict[str, list[tuple[str, str]]] = {}
        for path in self.values:
            if not path.endswith("/Chart.yaml"):
                continue
            doc = self._doc(path)
            parent = path[: -len("/Chart.yaml")]
            deps = (doc.get("dependencies") or []) if isinstance(doc, dict) else []
            for dep in deps:
                repo = str(dep.get("repository") or "")
                if not repo.startswith("file://"):
                    continue
                parts: list[str] = []
                for seg in f"{parent}/{repo[len('file://'):]}".split("/"):
                    if seg == "..":
                        if parts:
                            parts.pop()
                    elif seg not in ("", "."):
                        parts.append(seg)
                key = str(dep.get("alias") or dep.get("name") or "")
                if key:
                    out.setdefault("/".join(parts), []).append((parent, key))
        return out

    def _values_files(self, chart: str) -> list[str]:
        return [p for p in sorted(self.values)
                if p.startswith(chart + "/") and "/" not in p[len(chart) + 1:]
                and p.rsplit("/", 1)[-1].startswith("values") and p.endswith(".yaml")]

    def _value_roots(self, chart: str) -> list:
        roots = [self._doc(p) for p in self._values_files(chart)]
        for parent, key in self._umbrella_keys.get(chart, []):
            for p in self._values_files(parent):
                doc = self._doc(p)
                if isinstance(doc, dict) and key in doc:
                    roots.append(doc[key])
        return [r for r in roots if r is not None]

    def _defines(self, chart: str) -> dict[str, str]:
        """имя define → его текст (до следующего define либо конца файла)."""
        out: dict[str, str] = {}
        for path, text in self.templates.items():
            if not path.startswith(chart + "/templates/"):
                continue
            marks = list(_DEFINE.finditer(text))
            for k, m in enumerate(marks):
                end = marks[k + 1].start() if k + 1 < len(marks) else len(text)
                out[m.group(1)] = text[m.end():end]
        return out

    @staticmethod
    def _assignment(scope: str, var: str) -> tuple[str, bool] | None:
        """Выражение присваивания `$var` в тексте области; второе — элементы списка."""
        rng = re.search(r'range\s+(?:\$\w+\s*,\s*)?\$' + re.escape(var) + r'\s*:=\s*(.*?)-?\}\}',
                        scope, re.S)
        if rng:
            return rng.group(1), True
        m = re.search(r'\$' + re.escape(var) + r'\s*:?=\s*(.*?)-?\}\}', scope, re.S)
        return (m.group(1), False) if m else None

    def _expr_paths(self, expr: str, scope: str, defines: dict, fields: list[str],
                    depth: int) -> tuple[list[list[str]], list[str], bool]:
        """→ (пути в значениях, литералы, всё ли разрешено) для выражения."""
        if depth > 8:
            return [], [], False
        paths: list[list[str]] = []
        lits = list(_DEFAULT_LIT.findall(expr))
        ok = True
        head = expr.split("|", 1)[0]
        inc = _INCLUDE_REF.search(head)
        if inc and fields:
            body = defines.get(inc.group(1))
            if body is None:
                return paths, lits, False
            km = re.search(r'"' + re.escape(fields[0]) + r'"\s+\(?\s*([^)\n]*)', body)
            if not km:
                return paths, lits, False
            p2, l2, ok2 = self._expr_paths(km.group(1), body, defines, fields[1:], depth + 1)
            return paths + p2, lits + l2, ok2
        if inc:
            body = defines.get(inc.group(1))
            if body is None:
                return paths, lits, False
            # Действия управления (`if`, `else`, `end`) источников не несут — они
            # не провал разрешения. Провал — ссылка, которую разрешить не удалось,
            # либо ни одного пути и литерала на всё тело.
            for a in _ACTION.findall(body):
                if not _SOURCE.search(a.split("|", 1)[0]) and not _INCLUDE_REF.search(a):
                    lits += _DEFAULT_LIT.findall(a)
                    continue
                p2, l2, ok2 = self._expr_paths(a, body, defines, [], depth + 1)
                paths += p2
                lits += l2
                ok = ok and ok2
            return paths, lits, ok and bool(paths or lits)
        found = False
        for var, dotted in _SOURCE.findall(head):
            keys = dotted.strip(".").split(".")
            if var in ("", "$"):
                if keys[0] != "Values":
                    continue
                paths.append(keys[1:] + fields)
                found = True
                continue
            asg = self._assignment(scope, var[1:])
            if asg is None:
                ok = False
                continue
            sub, is_range = asg
            p2, l2, ok2 = self._expr_paths(sub, scope, defines,
                                           (["[]"] if is_range else []) + keys + fields,
                                           depth + 1)
            paths += p2
            lits += l2
            ok = ok and ok2
            found = True
        if not found and not lits:
            ok = False
        return paths, lits, ok

    @staticmethod
    def _lookup(node, keys: list[str]) -> list:
        if not keys:
            return [node]
        k, rest = keys[0], keys[1:]
        if k == "[]":
            items = node if isinstance(node, list) else (
                list(node.values()) if isinstance(node, dict) else [])
            out = []
            for it in items:
                out += ImageResolver._lookup(it, rest)
            return out
        if isinstance(node, dict) and k in node:
            return ImageResolver._lookup(node[k], rest)
        return []

    @staticmethod
    def _strings(node) -> list[str]:
        if isinstance(node, str):
            return [node]
        if isinstance(node, dict):
            for k in ("repository", "image"):
                if isinstance(node.get(k), str):
                    return [node[k]]
        return []

    def candidates(self, template_path: str, val: str) -> list[str]:
        if "{{" not in val:
            return [val]
        chart = template_path.split("/templates/", 1)[0]
        defines = self._defines(chart)
        text = self.templates.get(template_path, "")
        # Область присваиваний — define, внутри которого стоит строка, иначе файл.
        scope = text
        for body in defines.values():
            if val in body and body in text:
                scope = body
                break
        roots = self._value_roots(chart)
        found: list[str] = []
        ok = True
        for action in _ACTION.findall(val):
            paths, lits, a_ok = self._expr_paths(action, scope, defines, [], 0)
            ok = ok and a_ok
            found += lits
            for keys in paths:
                for root in roots:
                    for node in self._lookup(root, keys):
                        found += self._strings(node)
        line = (template_path, val)
        if line not in self._seen_lines:
            self._seen_lines.add(line)
            self.stats["templated"] += 1
            if ok:
                self.stats["resolved"] += 1
            else:
                self.stats["unresolved"].append(f"{template_path}: {val}")
        return [c for c in found if "://" not in c]


def launches(text: str, proc: str, path: str = "", resolver: ImageResolver | None = None) -> bool:
    """Шаблон ЗАПУСКАЕТ процесс `proc`: первый элемент `command:` либо `args:`
    контейнера (поток, скаляр или блочный список) называет исполняемый файл с
    базовым именем `proc`, либо образ контейнера — репозиторий с последним сегментом
    `proc`. Образ в ШАБЛОННОЙ форме (`{{ .Values.… }}`, `{{ include "….image" . }}`)
    разрешается значениями чарта (`ImageResolver`); без распознавателя шаблонная
    строка не называет ничего. Подстрока имени где-либо ещё (SAN, адрес, имя
    Service, таблица источников) запуском не является (Д86)."""
    lines = text.splitlines()
    for i, ln in enumerate(lines):
        m = _LAUNCH_KEY.match(ln)
        if not m:
            continue
        val = m.group("val")
        if m.group("key") == "image":
            raw = val.split(" #", 1)[0].strip()
            names = ([raw] if "{{" not in raw or resolver is None
                     else resolver.candidates(path, raw))
            if any(_image_name(c) == proc for c in names):
                return True
            continue
        first = _argv0(val)
        if not first:
            ind = len(m.group("ind"))
            for nxt in lines[i + 1:]:
                if not nxt.strip() or nxt.lstrip().startswith("#"):
                    continue
                stripped = nxt.lstrip()
                if len(nxt) - len(stripped) < ind or not stripped.startswith("- "):
                    break
                first = _argv0(stripped[2:])
                break
        if first and first.rsplit("/", 1)[-1] == proc:
            return True
    return False


def undeployed_carriers(root: pathlib.Path, records: dict, b6: dict, union_b6: set,
                        templates: dict | None = None,
                        values: dict | None = None) -> tuple[dict, list, dict]:
    """Записи «носитель не развёрнут» (`ban6_undeployed_carriers` манифеста).

    ПРЕДМЕТ. Домен входит в популяцию ban #6, когда его контракт регистрирует
    композиционный корень. Бывает носитель, чей корень регистрирует службу, а
    НИ ОДИН чарт дерева этот процесс не запускает: экземпляра нет ни в одном
    профиле, и ни один шард домен измерить не может — встречный контроль спросил
    бы листенер, которого нет нигде. Требовать от шарда такой домен значило бы
    требовать зелёного из отсутствия; молча снять его — завести невидимое
    послабление. Поэтому запись ЕСТЬ, с причиной, и ВЫВОДИТСЯ ОБРАТНО из дерева:

      * носитель записи обязан служить хотя бы один провязанный домен — иначе
        запись без предмета (находка);
      * процесс записи не ЗАПУСКАЕТ ни один отслеживаемый шаблон чарта (команда
        либо образ контейнера, `launches`) — иначе носитель развёрнут, и запись
        ИСТЕКЛА (находка: домен обязан взять шард). Подстрока имени в тексте
        шаблона запуском НЕ считается: таблица источников notify называет
        процесс пробы в SAN и адресе, не запуская его (Д86);
      * домен засчитывается «не измеряемым» только когда ВСЕ его носители — в
        записях; поперечный домен с развёрнутым носителем судится как прежде;
      * домен записи, который уже берёт шард, — запись лишняя (находка).

    Пустой обход шаблонов — отказ, а не «процесс нигде не назван».

    → (домен → {carrier, process, reason, lifted_by}, находки, перепись).
    """
    findings: list[str] = []
    hosts = b6.get("hosts", {})
    served = set(b6.get("served", set()))
    tpl = chart_templates(root) if templates is None else templates
    vals = chart_values(root) if values is None else values
    resolver = ImageResolver(tpl, vals)
    stats = {"records": len(records), "templates_read": len(tpl), "values_read": len(vals),
             "image_stats": resolver.stats}
    if records and not tpl:
        findings.append("записи «носитель не развёрнут» есть, а шаблонов чартов не прочитано "
                        "НИ ОДНОГО — истечение записей проверить не на чем; это отказ, а не "
                        "«процесс нигде не назван»")
        return {}, findings, stats
    if records and not vals:
        findings.append("записи «носитель не развёрнут» есть, а файлов значений чартов не "
                        "прочитано НИ ОДНОГО — образ в шаблонной форме разрешить нечем, и "
                        "запуск образом остался бы невидимым; это отказ, а не «не запускает»")
        return {}, findings, stats
    valid: dict[str, dict] = {}
    for carrier, rec in sorted(records.items()):
        proc = str(rec.get("process") or "")
        doms = sorted(d for d in served if carrier in hosts.get(d, []))
        if not proc or not rec.get("reason") or not rec.get("lifted_by"):
            findings.append(f"запись «носитель не развёрнут» '{carrier}' без процесса, причины "
                            f"или предиката снятия — запись, которую нельзя проверить, не заводится")
            continue
        if not doms:
            findings.append(f"запись «носитель не развёрнут» '{carrier}' без предмета: носитель "
                            f"не служит ни одного провязанного домена — удали запись")
            continue
        named = sorted(path for path, text in tpl.items()
                       if launches(text, proc, path, resolver))
        if named:
            findings.append(f"запись «носитель не развёрнут» '{carrier}' ИСТЕКЛА: процесс {proc} "
                            f"запускает шаблон {', '.join(named)} — домен(ы) "
                            f"{', '.join(doms)} обязан взять шард, а запись снимается")
            continue
        valid[carrier] = dict(rec, carrier=carrier, domains=doms)
    pending: dict[str, dict] = {}
    for d in sorted(served):
        hs = hosts.get(d, [])
        if hs and all(h in valid for h in hs):
            info = valid[hs[0]]
            if d in union_b6:
                findings.append(f"домен '{d}' в записи «носитель не развёрнут», а шард его уже "
                                f"измеряет — запись лишняя, удали её")
                continue
            pending[d] = {"carrier": info["carrier"], "process": info["process"],
                          "reason": info["reason"], "lifted_by": info["lifted_by"]}
    return pending, findings, stats


def check(root: pathlib.Path, manifest_path: pathlib.Path, *,
          ban6: dict | None = None,
          shard_doc: pathlib.Path | None = None,
          templates: dict | None = None,
          values: dict | None = None) -> tuple[list[str], dict]:
    """`ban6` — перепись популяции запрета #6; по умолчанию берётся из дерева.

    Параметр существует ради самопроверки: инъекция на СИНТЕТИЧЕСКОЙ переписи
    не привязана к сегодняшнему состоянию дерева и переживёт тот день, когда
    сегодняшний непровязанный домен провяжут. Фикстура, привязанная к
    снимаемому предмету, истекает вместе с ним и уносит доказательство.
    Сам предикат по дереву доказывается отдельной парой — на синтетическом
    дереве, см. `_self_test_population`.
    """
    findings: list[str] = []
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    shards = manifest["shards"]
    gates = set(manifest["gates"])

    tree = tracked_collections(root)
    tree_suites = set(tree)
    tree_total = sum(len(v) for v in tree.values())

    if tree_total == 0:
        findings.append("ПРОЧИТАНО НОЛЬ КОЛЛЕКЦИЙ — предикат не нашёл предмет; "
                        "это отказ, а не «всё покрыто»")

    # 1-2. каждая суита ровно у одного шарда; чужих суит нет
    seen: dict[str, list[str]] = {}
    for sh in shards:
        for suite in sh["suites"]:
            seen.setdefault(suite, []).append(sh["id"])
    for suite, owners in sorted(seen.items()):
        if suite not in tree_suites:
            findings.append(f"шард(ы) {owners} называют суиту '{suite}', которой нет в дереве")
        if len(owners) > 1:
            findings.append(f"суита '{suite}' задвоена между шардами {owners} — "
                            f"её коллекции посчитаются дважды")
    for suite in sorted(tree_suites - set(seen)):
        findings.append(f"суита '{suite}' ({len(tree[suite])} коллекций) НЕ НАЗНАЧЕНА "
                        f"ни одному шарду — её кейсы не гоняет никто")

    # 3. равенство поколлекционное, а не только итогом
    assigned_total = 0
    for suite, owners in sorted(seen.items()):
        if suite in tree_suites and len(owners) == 1:
            assigned_total += len(tree[suite])
    if assigned_total != tree_total:
        findings.append(f"сумма коллекций по шардам {assigned_total} != {tree_total} в дереве")

    # 4. состав суит == умолчание прогонщика
    default_services = set(parallel_default_services(PARALLEL_SH))
    for suite in sorted(default_services - set(seen)):
        findings.append(f"newman-parallel.sh гонит суиту '{suite}' по умолчанию, "
                        f"а ни один шард её не берёт")
    for suite in sorted(set(seen) - default_services):
        findings.append(f"шард берёт суиту '{suite}', которой нет в умолчании "
                        f"SERVICES прогонщика — она не будет исполнена")

    # 5. компоненты объявлены и реально условны
    chart_gated = gated_deps(UMBRELLA_CHART)
    for g in sorted(gates):
        if g not in chart_gated:
            findings.append(f"компонент '{g}' объявлен переключаемым, но в Chart.yaml "
                            f"зонтичного чарта у него НЕТ `condition: {g}.enabled` — "
                            f"`--set {g}.enabled=false` отрендерится и не сделает ничего")
    for sh in shards:
        for c in sh["components"]:
            if c not in gates:
                findings.append(f"шард '{sh['id']}' включает компонент '{c}', "
                                f"которого нет в gates — выключить его нечем")

    # 6. образы: карта gate→образ обязана называть образы, которые сборка знает
    make_images = set()
    mk = (DEPLOY / "Makefile").read_text(encoding="utf-8")
    m = re.search(r'^SERVICES := (.+)$', mk, re.M)
    if not m:
        findings.append("в deploy/Makefile не найден список SERVICES — "
                        "сверить карту образов не с чем; это отказ, а не «сошлось»")
    else:
        make_images = set(m.group(1).split())
    for img in list(manifest.get("core_images", [])) + list(manifest.get("gate_images", {}).values()):
        if make_images and img not in make_images:
            findings.append(f"манифест называет образ '{img}', которого нет в SERVICES "
                            f"сборки — шард попытается собрать несуществующее")
    # Компонент, который не является go-сервисом продукта, образа в SERVICES не
    # имеет by construction (его собирает другая цель). Исключение ОБЪЯВЛЕНО в
    # манифесте и проверяется на живость ниже, а не зашито здесь именем.
    non_service = manifest.get("non_service_gates", {})
    for g in sorted(gates):
        if g.startswith("pg-") or g in non_service:
            continue
        if g not in manifest.get("gate_images", {}):
            findings.append(f"компонент '{g}' переключаемый, но образа за ним не закреплено — "
                            f"шард, который его включит, не соберёт его образ")

    # 6а. САМОИСТЕЧЕНИЕ исключения «не сервис». Запись, которой больше нечего
    #     исключать, — находка: иначе она переживёт свой предмет и следующий
    #     читатель примет её за действующее свойство компонента.
    mk_targets = set(re.findall(r'^([A-Za-z0-9_-]+):', mk, re.M))
    for g, target in sorted(non_service.items()):
        if g not in gates:
            findings.append(f"'{g}' объявлен не-сервисом, но переключаемым компонентом "
                            f"не является (нет в gates) — исключать нечего")
        if make_images and g in make_images:
            findings.append(f"'{g}' объявлен не-сервисом, но он ЕСТЬ в SERVICES сборки — "
                            f"исключение пережило свой предмет: образ у него теперь есть, "
                            f"и пункты 6/8 обязаны его судить")
        if target not in mk_targets:
            findings.append(f"'{g}' объявлен собираемым целью '{target}', которой в "
                            f"deploy/Makefile НЕТ — названа команда, которой не существует")

    # 7. имя, которым шард ОБЪЯВЛЯЕТ отсутствие сервиса, обязано быть тем именем,
    #    которое понимает гейт посадки. Он держит жёсткий список ожидаемых
    #    сервисов намеренно (не развернувшийся сервис обязан быть красным), и
    #    исключение принимает ТОЛЬКО по этому имени. Разъезд имён означал бы, что
    #    шард выключил компонент, а гейт посадки покраснел на его отсутствии — и
    #    покраснел бы верно.
    posture = DEPLOY / "scripts" / "assert-production-posture.sh"
    ptext = posture.read_text(encoding="utf-8")
    pm = re.search(r'^SERVICES="\n((?:.*\n)*?)"\s*$', ptext, re.M)
    if not pm:
        findings.append("в assert-production-posture.sh не найден список SERVICES — "
                        "сверить имена исключений не с чем; это отказ, а не «сошлось»")
    else:
        posture_names = {ln.split("|")[1] for ln in pm.group(1).splitlines()
                         if ln.strip() and "|" in ln}
        for g, img in sorted(manifest.get("gate_images", {}).items()):
            if img not in posture_names:
                findings.append(
                    f"компонент '{g}' объявляет себя как '{img}', но гейт посадки "
                    f"такого сервиса не знает — POSTURE_SKIP='{img}' его не исключит, "
                    f"и стенд шарда покраснет на законном отсутствии")

    # 8. ban #6: объединение доменов по шардам обязано покрывать ВСЕ домены,
    #    у которых есть внутренний листенер. Проба ban #6 теперь сужается составом
    #    стенда (`--domains`), и без этой проверки домен мог бы не измеряться НИ НА
    #    ОДНОМ шарде, оставаясь при этом зелёным везде — ровно то послабление,
    #    ради невозможности которого сужение и сделано явным.
    core_b6 = set(manifest.get("core_ban6_domains", []))
    # Значение — СПИСОК доменов: один компонент способен дать больше одного.
    # `subscription` служат пять носителей сразу, и своего сервиса у домена нет
    # ни одного — при значении-строке его нельзя было приписать никому, не отняв
    # компонент у собственного домена.
    gate_b6 = {c: list(v) for c, v in manifest.get("gate_ban6_domains", {}).items()}
    union_b6 = set(core_b6)
    for sh in shards:
        for c in sh["components"]:
            union_b6 |= set(gate_b6.get(c, []))

    # ПОПУЛЯЦИЯ — «домены, у которых ban #6 имеет ПРЕДМЕТ», а не «домены, у
    # которых есть Internal*-контракт». Разница не формальная: контракт, который
    # не регистрирует НИ ОДИН композиционный корень, недостижим на внешнем
    # листенере by construction, и потребовать его измерения значит потребовать
    # зелёного из отсутствия — проба на таком домене не подтвердит изоляцию, а
    # уронит прогон встречным контролем («метода нет на внешнем» неотличимо от
    # «метода нет нигде»).
    #
    # Ведомости прощённых при этом НЕТ НИ ОДНОЙ СТРОКИ: принадлежность к
    # популяции ВЫВОДИТСЯ ИЗ ДЕРЕВА, поэтому послабление истекает само — в тот
    # день, когда регистрация появится в прод-коде, домен войдёт в популяцию, и
    # этот гейт покраснеет, пока его не возьмёт шард.
    b6 = ban6 if ban6 is not None else _ban6_module().census(root)
    contract_b6 = set(b6["services"])
    want_b6 = set(b6["served"])
    unserved_b6 = dict(b6["unserved"])
    if not contract_b6:
        findings.append("не удалось перечислить домены с Internal*-контрактом — "
                        "сверить охват ban #6 не с чем; это отказ, а не «сошлось»")
    if contract_b6 and not want_b6:
        findings.append("ни один Internal*-контракт дерева не регистрируется прод-кодом — "
                        "предикат провязки не нашёл предмет; это отказ, а не «нечего измерять»")
    pending_b6, pending_findings, pending_stats = undeployed_carriers(
        root, manifest.get("ban6_undeployed_carriers", {}), b6, union_b6, templates, values)
    findings += pending_findings
    for d in sorted(want_b6 - union_b6 - set(pending_b6)):
        findings.append(f"домен '{d}' несёт Internal*-контракт, ПРОВЯЗАННЫЙ прод-кодом, "
                        f"но ban #6 для него не измеряет НИ ОДИН шард — метод остался бы "
                        f"«изолированным» просто потому, что его никто не спрашивал")
    for d in sorted(union_b6 - contract_b6):
        findings.append(f"шарды заявляют ban #6 для домена '{d}', у которого нет "
                        f"Internal*-контракта в proto — измерять нечего")
    for d in sorted((union_b6 & contract_b6) - want_b6):
        findings.append(f"шарды заявляют ban #6 для домена '{d}', чей Internal*-контракт "
                        f"({', '.join(unserved_b6.get(d, []))}) не регистрирует НИ ОДИН "
                        f"композиционный корень: встречный контроль пробы его не "
                        f"подтвердит и уронит прогон как НЕВЫПОЛНЕННОЕ ИЗМЕРЕНИЕ, "
                        f"а не как изоляцию")
    for g in sorted(set(gates) - {x for x in gates if x.startswith("pg-")} - set(non_service)):
        if not gate_b6.get(g):
            findings.append(f"компонент '{g}' переключаемый, но домена ban #6 за ним "
                            f"не закреплено — включивший его шард не проверит его изоляцию")

    # 8а. ПРИПИСКА СВЕРЯЕТСЯ С ДЕРЕВОМ. Домен, закреплённый за компонентом, обязан
    #     СЛУЖИТЬСЯ сервисом этого компонента — иначе шард объявит его измеряемым,
    #     а встречный контроль пробы пойдёт на листенер, где такой службы нет, и
    #     уронит прогон как невыполненное измерение. Носители выводятся из путей
    #     регистраций (`e2e-ban6-domains.py`), поэтому выписанное здесь не может
    #     разойтись с деревом молча: припишут домен не тому компоненту — находка;
    #     перестанет носитель монтировать службу — тоже находка.
    hosts_b6 = b6.get("hosts", {})
    gate_images_map = manifest.get("gate_images", {})
    for comp, doms in sorted(gate_b6.items()):
        carrier = gate_images_map.get(comp)
        if carrier is None:
            findings.append(f"компоненту '{comp}' приписаны домены ban #6, но образа "
                            f"(а значит и сервиса-носителя) за ним не закреплено — "
                            f"сверить приписку с деревом не с чем")
            continue
        for d in doms:
            if d not in want_b6:
                # Домен без контракта и домен без провязки судят проверки выше,
                # каждая своим текстом. Сверять приписку у того, чей предмет ещё
                # не существует, значило бы ронять прогон дважды за одно.
                continue
            if carrier not in hosts_b6.get(d, []):
                findings.append(
                    f"компоненту '{comp}' приписан домен ban #6 '{d}', но сервис "
                    f"'{carrier}' его НЕ служит (служат: "
                    f"{', '.join(hosts_b6.get(d, [])) or '—'}) — встречный контроль "
                    f"пойдёт на листенер, где этой службы нет, и уронит прогон")
    # Та же сверка для ядра: домен ядра обязан служиться одним из образов ядра.
    core_images_set = set(manifest.get("core_images", []))
    for d in sorted(core_b6):
        if d not in want_b6:
            continue
        if not (set(hosts_b6.get(d, [])) & core_images_set):
            findings.append(
                f"домен ban #6 '{d}' объявлен ядерным, но ни один образ ядра "
                f"({', '.join(sorted(core_images_set)) or '—'}) его не служит "
                f"(служат: {', '.join(hosts_b6.get(d, [])) or '—'}) — на стенде без "
                f"этих компонентов его встречный контроль подтвердить нечем")

    # 9. ТРАНСПОРТ К КОМПОНЕНТУ — ТОЛЬКО НА ШАРДАХ, ЭТОТ КОМПОНЕНТ ПОДНИМАЮЩИХ.
    #
    # Часть адресов прогона ведёт не к ядру, а к переключаемому компоненту
    # (`optional_transports`). Суита, чья коллекция такой адрес НАБИРАЕТ, обязана
    # исполняться только там, где адресат поднят. Без этой пары дефект принимает
    # форму, которую ни один из прежних восьми пунктов не видел: коллекция на месте,
    # суита назначена ровно одному шарду, счёт коллекций сходится — и всё равно
    # полоса проверяется транспортом, которого на стенде нет.
    #
    # Наблюдалось: полоса docker жила в наборе iam, а `registry` шард iam снимает.
    # Проброс к отсутствующему сервису не встал, прогонщик объявил прогон
    # недействительным, и ЧЕТЫРЕ шарда из пяти не запустили ни одной суиты
    # (31344367968) — включая три, ни одна коллекция которых этот адрес не набирает.
    #
    # Предикат берётся ИЗ ТОГО ЖЕ модуля, что исполняет прогонщик (см. выше).
    transports = manifest.get("optional_transports", {})
    tmod = _transports_module()
    suite_owner = {s: sh["id"] for sh in shards for s in sh["suites"]}
    shard_components = {sh["id"]: set(sh["components"]) for sh in shards}
    transport_dialers: dict[str, list[str]] = {}
    for var, spec in sorted(transports.items()):
        comp = spec.get("component")
        if comp not in gates:
            findings.append(
                f"транспорт '{var}' объявлен опциональным, но его компонент "
                f"'{comp}' не переключаемый (нет в gates) — тогда он есть на каждом "
                f"стенде, и условность транспорта скрывает, что условия нет")
        dialers = sorted(s for s in tree_suites
                         if tmod.transports_dialled_by(s, [var])[0])
        transport_dialers[var] = dialers
        # САМОИСТЕЧЕНИЕ: объявленный транспорт, которого никто не набирает, —
        # находка, а не запас. Иначе объявление переживёт свой предмет и следующий
        # читатель примет его за действующее требование к стенду.
        if not dialers:
            findings.append(
                f"транспорт '{var}' объявлен, но его не набирает НИ ОДНА коллекция "
                f"дерева — объявлению нечего обслуживать; снять его либо назвать "
                f"кейс, ради которого он держится")
        for suite in dialers:
            owner = suite_owner.get(suite)
            if owner is None:
                continue  # покрыто п.1 — суита вообще никем не взята
            if comp not in shard_components[owner]:
                findings.append(
                    f"суита '{suite}' набирает '{var}' (адресат — компонент "
                    f"'{comp}'), но шард '{owner}', который её гоняет, этот "
                    f"компонент НЕ поднимает: полоса проверялась бы транспортом, "
                    f"которого на стенде нет")

    # 10. ЧИСЛА, ВЫПИСАННЫЕ В ПРОЗЕ МАНИФЕСТА, СХОДЯТСЯ С ДЕРЕВОМ.
    #
    # Пояснение шарда открывается его весом («N коллекций / M запросов»), и вес
    # этот читается как факт о СЕГОДНЯШНЕМ дереве: по нему судят, стоит ли шард
    # отдельного раннера и не пора ли его делить. Выписанное число владельца не
    # имеет — правит его тот, кто наткнётся, — поэтому стареет молча и утаскивает
    # за собой выводы, сделанные из него.
    #
    # Замер, из которого пункт заведён (#2207): число КОЛЛЕКЦИЙ разошлось у трёх
    # шардов из пяти, число ЗАПРОСОВ — у ПЯТИ из пяти.
    reqs_tree = tracked_requests(root, tree)
    weights_read = 0
    for sh in shards:
        why = sh.get("why", "")
        m = SHARD_WEIGHT.match(why)
        if not m:
            findings.append(
                f"шард '{sh['id']}': пояснение не открывается весом вида "
                f"«N коллекций / M запросов» — форма, о которой разборщик не знает, "
                f"уходит из-под наблюдения молча, поэтому это находка, а не тишина")
            continue
        weights_read += 1
        parts = [int(x) for x in re.split(r'\s*\+\s*', m.group("colls"))]
        want_parts = [len(tree.get(su, [])) for su in sh["suites"]]
        want_colls, want_reqs = sum(want_parts), sum(reqs_tree.get(su, 0) for su in sh["suites"])
        # Слагаемые судятся ПОРАЗДЕЛЬНО, если их несколько: сумма сходится и при
        # том, что одна суита выросла ровно на столько, на сколько усохла другая.
        if len(parts) > 1 and parts != want_parts:
            findings.append(
                f"шард '{sh['id']}': пояснение называет коллекции по суитам "
                f"{'+'.join(map(str, parts))}, дерево — {'+'.join(map(str, want_parts))} "
                f"({', '.join(sh['suites'])})")
        elif sum(parts) != want_colls:
            findings.append(
                f"шард '{sh['id']}': пояснение называет {sum(parts)} коллекций, "
                f"в дереве {want_colls}")
        if int(m.group("reqs")) != want_reqs:
            findings.append(
                f"шард '{sh['id']}': пояснение называет {m.group('reqs')} запросов, "
                f"в дереве {want_reqs}")
    if shards and weights_read == 0:
        findings.append("ни у одного шарда вес не прочитан — предикат ослеп, "
                        "и «ноль находок» здесь было бы свойством разборщика")

    # То же для знаменателя предиката в прозе манифеста: «→ 0 при N коллекциях».
    # Числитель предиката прогоняется его автором; знаменатель — объём
    # осмотренного, и устаревший он говорит о полноте предиката неправду.
    # Обход РЕКУРСИВНЫЙ, а не по верхнему уровню. Сегодня знаменатель в манифесте
    # один и лежит наверху, поэтому перепись от этого не меняется — сказано
    # затем, чтобы расширение не приняли за находку. Обход верхнего уровня был бы
    # слепой зоной by construction: знаменатель, записанный в пояснении
    # транспорта, не попал бы под наблюдение НИ КРАСНЫМ, НИ ЗЕЛЁНЫМ.
    def _prose(node, path=""):
        if isinstance(node, dict):
            for k, v in node.items():
                yield from _prose(v, f"{path}.{k}" if path else k)
        elif isinstance(node, list):
            for i, v in enumerate(node):
                yield from _prose(v, f"{path}[{i}]")
        elif isinstance(node, str):
            yield path, node

    prose_totals = 0
    for where, line in _prose(manifest):
        m = PROSE_TOTAL.search(line)
        if m:
            prose_totals += 1
            if int(m.group("total")) != tree_total:
                findings.append(
                    f"поле '{where}': предикат объявляет знаменатель {m.group('total')} "
                    f"коллекций, в дереве {tree_total} — объём осмотренного назван "
                    f"неверно, и полнота предиката читается шире, чем есть")

    # Поперечный домен НАЗЫВАЕТСЯ переписью: у него нет своего сервиса, и
    # «на каком шарде он измеряется» иначе восстанавливается только чтением
    # манифеста вместе с картой носителей — то есть не восстанавливается.
    cross_b6 = {}
    for d in sorted(want_b6):
        hs = hosts_b6.get(d, [])
        if len(hs) > 1:
            cross_b6[d] = {
                "hosts": list(hs),
                "shards": [sh["id"] for sh in shards
                           if any(d in gate_b6.get(c, []) for c in sh["components"])],
            }

    # 11. ТАБЛИЦА ВЕСОВ В ДОКУМЕНТЕ РАЗБИЕНИЯ СХОДИТСЯ С ДЕРЕВОМ.
    #
    # Документ честнее прочих: он называет ревизию своего замера, поэтому его
    # числа читаются как свидетельство о ней. Но таблица §2 подписана «Числа —
    # запросы коллекций суиты, не догадка», а §4 выводит из неё долю раннера —
    # то есть из свидетельства делается вывод о СЕГОДНЯШНЕМ стенде. Владельца у
    # выписанного числа нет: правит его тот, кто наткнётся.
    #
    # Замер, из которого пункт заведён (#2226): расходились ВСЕ ВОСЕМЬ строк
    # таблицы и итог. Исходов у задачи было два — вывести таблицу из дерева либо
    # объявить её свидетельством явно; выбран первый: тогда у неё появляется
    # держатель, а не дата.
    #
    # Судится РАЗМЕЧЕННАЯ строка таблицы, а не текст: имена суит стоят в этом же
    # документе прозой и в перечнях, и поиск по подстроке считал бы объяснение
    # предметом.
    # Путь документа — ПАРАМЕТР ради инъекции: подменить его нельзя ничем, кроме
    # явного аргумента, поэтому боевой прогон читает ровно то, что объявлено.
    doc_path = shard_doc if shard_doc is not None else root / SHARD_DOC
    if not doc_path.is_file():
        findings.append(
            f"документ разбиения {SHARD_DOC} не прочитан: таблица весов осталась бы "
            f"вне наблюдения молча, а «ноль находок» здесь означало бы «ноль прочитанного»")
        doc_rows: dict[str, tuple[int, int]] = {}
        doc_total: tuple[int, int] | None = None
    else:
        doc_rows, doc_total = shard_doc_weights(doc_path.read_text(encoding="utf-8"))
        if not doc_rows:
            findings.append(
                f"в {SHARD_DOC} не прочитано НИ ОДНОЙ строки таблицы весов — разборщик "
                f"перестал её видеть, и молчание пункта ничего не означает")
    for suite in sorted(doc_rows):
        colls, reqs = doc_rows[suite]
        if suite not in tree:
            findings.append(
                f"{SHARD_DOC}: таблица называет суиту '{suite}', которой в дереве нет — "
                f"строка пережила свой предмет")
            continue
        want_c, want_r = len(tree[suite]), reqs_tree.get(suite, 0)
        if (colls, reqs) != (want_c, want_r):
            findings.append(
                f"{SHARD_DOC}: суита '{suite}' объявлена как {colls}/{reqs} "
                f"(коллекций/запросов), в дереве {want_c}/{want_r} — §4 выводит из этой "
                f"таблицы долю раннера, то есть из устаревшего числа делается вывод о "
                f"сегодняшнем стенде")
    for suite in sorted(tree):
        if suite not in doc_rows:
            findings.append(
                f"{SHARD_DOC}: суита '{suite}' есть в дереве и НЕ названа таблицей — "
                f"её вес не участвует ни в одном выводе документа")
    if doc_total is not None:
        want = (sum(len(v) for v in tree.values()), sum(reqs_tree.values()))
        if doc_total != want:
            findings.append(
                f"{SHARD_DOC}: итог таблицы {doc_total[0]}/{doc_total[1]}, "
                f"в дереве {want[0]}/{want[1]}")
    elif doc_rows:
        findings.append(
            f"{SHARD_DOC}: строка итога таблицы не прочитана — сумма, сходящаяся с "
            f"самой собой при разошедшихся строках, и есть тот случай, ради которого "
            f"итог судится отдельно")

    stats = {
        "ban6_pending": pending_b6,
        "ban6_pending_stats": pending_stats,
        "ban6_cross_domains": cross_b6,
        "transports_declared": len(transports),
        "transport_dialers": transport_dialers,
        "ban6_domains_with_contract": len(contract_b6),
        "ban6_domains_needed": len(want_b6),
        "ban6_domains_covered": len(union_b6 & want_b6),
        "ban6_domains_unserved": {d: list(v) for d, v in sorted(unserved_b6.items())},
        "ban6_proto_files_read": b6["proto_files_read"],
        "ban6_registrations_found": b6["registrations_found"],
        "suites_tree": len(tree_suites),
        "suites_assigned": len([s for s in seen if s in tree_suites]),
        "collections_tree": tree_total,
        "collections_assigned": assigned_total,
        "shards": len(shards),
        "gates": len(gates),
        "per_suite": {s: len(tree[s]) for s in sorted(tree)},
        "requests_tree": sum(reqs_tree.values()),
        "shard_weights_read": weights_read,
        "prose_totals_read": prose_totals,
        "doc_weight_rows_read": len(doc_rows),
    }
    return findings, stats


def report(root: pathlib.Path, manifest_path: pathlib.Path) -> int:
    findings, st = check(root, manifest_path)
    print("=== покрытие шардов (единица счёта — отслеживаемая git коллекция) ===")
    print(f"осмотрено: суит {st['suites_tree']}, коллекций {st['collections_tree']}, "
          f"шардов {st['shards']}, переключаемых компонентов {st['gates']}")
    # Строки таблицы весов НАЗЫВАЮТСЯ числом: «расхождений ноль» обязано быть
    # отличимо от «таблицу не прочитали».
    print(f"вес суит: строк таблицы {SHARD_DOC} прочитано {st['doc_weight_rows_read']}, "
          f"запросов в дереве {st['requests_tree']}")
    print(f"ban #6: прочитано .proto {st['ban6_proto_files_read']}, регистраций "
          f"Internal*-служб в прод-коде {st['ban6_registrations_found']}")
    print(f"ban #6: доменов с Internal*-контрактом {st['ban6_domains_with_contract']}, "
          f"из них провязано прод-кодом {st['ban6_domains_needed']}, "
          f"покрыто шардами {st['ban6_domains_covered']}")
    # Домен, чей контракт приземлён, но не провязан, НАЗЫВАЕТСЯ на каждом прогоне.
    # Умолчать его значило бы завести невидимое послабление: разница между «нечего
    # измерять» и «забыли измерить» видна только когда обе величины напечатаны.
    for dom, svcs in st["ban6_domains_unserved"].items():
        print(f"   {dom:12s} контракт приземлён ({', '.join(svcs)}), но НЕ провязан ни "
              f"одним композиционным корнем — у ban #6 нет предмета; провяжут → домен "
              f"войдёт в охват сам, и этот гейт покраснеет, пока его не возьмёт шард")
    # Домен, чей единственный носитель не развёрнут НИ ОДНИМ шаблоном дерева,
    # называется на каждом прогоне вместе с причиной и предикатом снятия.
    ps = st["ban6_pending_stats"]
    im = ps.get("image_stats", {})
    unres = im.get("unresolved", [])
    print(f"ban #6: записей «носитель не развёрнут» {ps['records']}; шаблонов чартов "
          f"осмотрено {ps['templates_read']}; файлов значений {ps.get('values_read', 0)}; "
          f"строк образа в шаблонной форме {im.get('templated', 0)}, из них разрешено "
          f"значениями {im.get('resolved', 0)}, НЕ разрешено {len(unres)}")
    for u in unres:
        print(f"   образ не разрешён (запуск по нему не судим): {u}")
    for dom, info in sorted(st["ban6_pending"].items()):
        print(f"   {dom:12s} НЕ ИЗМЕРЯЕТСЯ НИ ОДНИМ ШАРДОМ: носитель '{info['carrier']}' "
              f"(процесс {info['process']}) не развёрнут ни одним шаблоном чарта — "
              f"{info['reason']}; снимается: {info['lifted_by']}")
    # Домен, служимый НЕСКОЛЬКИМИ носителями, называется отдельно: у него нет
    # своего сервиса, поэтому «кто его измеряет» не выводится из имени.
    for dom, info in st["ban6_cross_domains"].items():
        print(f"   {dom:12s} поперечный: служат {len(info['hosts'])} носителей "
              f"({', '.join(info['hosts'])}); измеряют шарды: "
              f"{', '.join(info['shards']) if info['shards'] else '— НИКТО'}")
    print(f"транспортов к компонентам объявлено {st['transports_declared']}:")
    for var, dialers in sorted(st["transport_dialers"].items()):
        print(f"   {var} ← набирают: {', '.join(dialers) if dialers else '—'}")
    print(f"назначено: суит {st['suites_assigned']}, коллекций {st['collections_assigned']}")
    print(f"числа прозы манифеста: весов шардов прочитано {st['shard_weights_read']} "
          f"из {st['shards']}, знаменателей предиката {st['prose_totals_read']}; "
          f"в дереве коллекций {st['collections_tree']}, запросов {st['requests_tree']}")
    for s, n in st["per_suite"].items():
        owner = next((sh["id"] for sh in json.loads(manifest_path.read_text())["shards"]
                      if s in sh["suites"]), "—")
        print(f"   {s:12s} {n:3d} коллекций → шард {owner}")
    if findings:
        print()
        for f in findings:
            print(f"НАХОДКА: {f}")
        print(f"\nFAIL: находок {len(findings)}")
        return 1
    print("\nOK: каждая коллекция дерева назначена ровно одному шарду; "
          f"сумма по шардам = {st['collections_assigned']} = коллекций в дереве")
    return 0


def _self_test_population() -> bool:
    """Пара для ПРЕДИКАТА ПОПУЛЯЦИИ — на синтетическом дереве, а не на сегодняшнем.

    Предмет здесь другой, чем у инъекций манифеста ниже: там проверяется, что
    пункт 8 реагирует на перепись, здесь — что сама перепись читает дерево. Без
    этой пары «домен не провязан» держалось бы на честном слове модуля.

    Дерево СИНТЕТИЧЕСКОЕ и лежит вне репозитория (`tempfile` + явный `git -C`):
    проба не имеет права писать в индекс, настройки и дерево репозитория, из
    которого запущена. Привязать фикстуру к живому `subscription` было бы
    ошибкой того же рода, что и ведомость прощённых: она истекла бы в день, когда
    его провяжут, и унесла бы доказательство с собой.

    Утверждается ЧЕТЫРЕ вещи, и каждая — сторона пары:
      alpha  — контракт в `v1/` + вызов регистрации в прод-коде  ⇒ ПРОВЯЗАН;
      beta   — контракт ВНЕ `v1/` + регистрация только в `_test.go` и объявление
               в `pkg/api/`                                       ⇒ НЕ ПРОВЯЗАН
               (обе половины важны: раскладка вне `v1/` обязана быть видна, а
               внутрипроцессный харнесс и сгенерированное объявление обязаны НЕ
               считаться провязкой);
      gamma  — контракт без `Internal*`-службы                    ⇒ ВНЕ популяции;
      delta  — контракт + регистрация ВТОРОЙ законной формой
               (`RegisterService(&…_ServiceDesc, …)`) при том, что объявление
               дескриптора лежит в `pkg/api/`                      ⇒ ПРОВЯЗАН
               (предикат обязан отвечать про регистрацию, а не про сегодняшнюю
               привычку её записывать: второй формы в дереве нет ни одной, и
               непризнание её сделало бы сужение маской);
      beta после появления прод-регистрации                       ⇒ ПРОВЯЗАН
               (самоистечение: послабление снимается появлением предмета).
    """
    import tempfile

    mod = _ban6_module()
    ok = True

    def say(label: str, good: bool, detail: str) -> None:
        nonlocal ok
        ok = ok and good
        print(f"  [{'ok ' if good else 'FAIL'}] {label} — {detail}")

    with tempfile.TemporaryDirectory(prefix="kacho-ban6-population-") as tmp:
        root = pathlib.Path(tmp)

        def put(rel: str, body: str) -> None:
            f = root / rel
            f.parent.mkdir(parents=True, exist_ok=True)
            f.write_text(body, encoding="utf-8")

        put("proto/kacho/cloud/alpha/v1/alpha.proto",
            "package kacho.cloud.alpha.v1;\nservice InternalAlphaService {\n"
            "  rpc Peek(Req) returns (Res);\n}\n")
        put("proto/kacho/cloud/beta/beta.proto",
            "package kacho.cloud.beta;\nservice InternalBetaService {\n"
            "  rpc Peek(Req) returns (Res);\n}\n")
        put("proto/kacho/cloud/gamma/v1/gamma.proto",
            "package kacho.cloud.gamma.v1;\nservice GammaService {\n"
            "  rpc Get(Req) returns (Res);\n}\n")
        put("services/alpha/cmd/alpha/main.go",
            "package main\nfunc wire(srv S, h H) {\n"
            "\talphav1.RegisterInternalAlphaServiceServer(srv, h)\n}\n")
        put("pkg/beta/harness_test.go",
            "package beta\nfunc harness(srv S, h H) {\n"
            "\tbetav1.RegisterInternalBetaServiceServer(srv, h)\n}\n")
        put("pkg/api/kacho/cloud/beta/beta_grpc.pb.go",
            "package betav1\nfunc RegisterInternalBetaServiceServer(s grpc.ServiceRegistrar, "
            "srv InternalBetaServiceServer) {\n\ts.RegisterService(&x, srv)\n}\n")
        put("proto/kacho/cloud/delta/v1/delta.proto",
            "package kacho.cloud.delta.v1;\nservice InternalDeltaService {\n"
            "  rpc Peek(Req) returns (Res);\n}\n")
        put("services/delta/cmd/delta/main.go",
            "package main\nfunc wire(srv grpc.ServiceRegistrar, h H) {\n"
            "\tsrv.RegisterService(&deltav1.InternalDeltaService_ServiceDesc, h)\n}\n")
        put("pkg/api/kacho/cloud/delta/v1/delta_grpc.pb.go",
            "package deltav1\nvar InternalDeltaService_ServiceDesc = grpc.ServiceDesc{}\n")

        for args in (("init", "-q"), ("add", "-A")):
            r = subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True)
            if r.returncode != 0:
                say("синтетическое дерево заведено", False, r.stderr.strip())
                return False

        c = mod.census(root)
        say("alpha: контракт в v1/ + прод-регистрация ⇒ ПРОВЯЗАН",
            "alpha" in c["served"], f"провязано={sorted(c['served'])}")
        say("beta: контракт ВНЕ v1/ виден предикатом",
            "beta" in c["services"], f"доменов={sorted(c['services'])}")
        say("beta: регистрация в _test.go и объявление в pkg/api ⇒ НЕ провязан",
            "beta" in c["unserved"] and "beta" not in c["served"],
            f"не провязано={sorted(c['unserved'])}")
        say("gamma: контракт без Internal*-службы ⇒ вне популяции",
            "gamma" not in c["services"], f"доменов={sorted(c['services'])}")
        say("delta: регистрация второй законной формой ⇒ ПРОВЯЗАН",
            "delta" in c["served"], f"провязано={sorted(c['served'])}")
        say("объём осмотренного напечатан, а не подразумевается",
            c["proto_files_read"] == 4 and c["registrations_found"] == 2,
            f"прочитано .proto={c['proto_files_read']} регистраций={c['registrations_found']}")

        # САМОИСТЕЧЕНИЕ: появился прод-вызов ⇒ домен обязан войти в популяцию сам.
        put("services/beta/cmd/beta/main.go",
            "package main\nfunc wire(srv S, h H) {\n"
            "\tbetav1.RegisterInternalBetaServiceServer(srv, h)\n}\n")
        subprocess.run(["git", "-C", str(root), "add", "-A"], capture_output=True, text=True)
        mod.invalidate()
        c2 = mod.census(root)
        say("beta провязали ⇒ домен вошёл в популяцию САМ (послабление истекло)",
            "beta" in c2["served"] and "beta" not in c2["unserved"],
            f"провязано={sorted(c2['served'])}")
    return ok


def _census_plus(base: dict, domain: str, *, served: bool) -> dict:
    """Синтетическая перепись: базовая плюс один домен в заданном состоянии.

    Нужна затем, чтобы инъекции пункта 8 не зависели от того, какие домены дерева
    сегодня провязаны: фикстура, привязанная к сегодняшнему непровязанному домену,
    истекла бы вместе с ним.
    """
    import copy

    svc = "Internal" + domain[:1].upper() + domain[1:] + "Service"
    out = copy.deepcopy(base)
    out["services"] = dict(out["services"], **{domain: [svc]})
    out["served"] = set(out["served"])
    out["unserved"] = dict(out["unserved"])
    if served:
        out["served"].add(domain)
        out["unserved"].pop(domain, None)
    else:
        out["served"].discard(domain)
        out["unserved"][domain] = [svc]
    return out


def _self_test() -> int:
    """Инъекция: четыре дефекта по одному ⇒ красный на каждом; законный близнец ⇒ зелёный."""
    import copy
    import tempfile

    base = json.loads(MANIFEST.read_text(encoding="utf-8"))
    ok = True

    def run(m: dict, label: str, want_red: bool, expect: str | None = None,
            ban6: dict | None = None, shard_doc: pathlib.Path | None = None,
            templates: dict | None = None, values: dict | None = None) -> None:
        """`expect` — подстрока, которая ОБЯЗАНА встретиться среди находок.

        Без неё инъекция доказывает лишь чувствительность гейта к правке манифеста,
        а не то, что покраснел ИМЕННО проверяемый пункт. Здесь это не теория:
        первая редакция инъекции (з) снимала компонент у шарда и краснела —
        но на пункте про ban #6, потому что снятый компонент уносил с собой и охват
        домена. Пункт 9 при этом мог бы вообще отсутствовать, а самопроверка
        осталась бы зелёной.
        """
        nonlocal ok
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as fh:
            json.dump(m, fh)
            p = pathlib.Path(fh.name)
        try:
            findings, _ = check(ROOT, p, ban6=ban6, shard_doc=shard_doc,
                                templates=templates, values=values)
        finally:
            p.unlink(missing_ok=True)
        red = bool(findings)
        good = red == want_red
        matched = None
        if good and expect is not None:
            matched = next((f for f in findings if expect in f), None)
            good = matched is not None
        ok = ok and good
        state = "КРАСНЫЙ" if red else "зелёный"
        want = "красного" if want_red else "зелёного"
        why = matched or (findings[0] if red else "")
        note = "" if (expect is None or matched) else f" [ЖДАЛИ находку про: {expect}]"
        print(f"  [{'ok ' if good else 'FAIL'}] {label}: {state} (ждали {want}){note}"
              + (f" — {why}" if why else ""))

    print("=== самопроверка гейта (инъекция в обе стороны) ===")
    run(base, "законный близнец: дерево как есть", want_red=False)

    # (п.11) РАСПОЗНАВАТЕЛЬ таблицы весов — сперва он сам, потом сравнение.
    # Форма, о которой он не знает, даёт не красное и не зелёное, а молчание:
    # строки просто не попадают в перепись, и «ноль расхождений» становится
    # свойством разборщика.
    sample = ("| суита | коллекций | запросов |\n"
              "|---|---:|---:|\n"
              "| vpc | 18 | 3709 |\n"
              "| **итого** | **98** | **9391** |\n")
    rows, total = shard_doc_weights(sample)
    good = rows == {"vpc": (18, 3709)} and total == (98, 9391)
    ok = ok and good
    print(f"  [{'ok ' if good else 'FAIL'}] (п.11) разборщик читает строку суиты и "
          f"строку итога раздельно — строк={rows}, итог={total}")
    # Итог выделен жирным и суитой быть не должен: одна грамматика на обе дала бы
    # «итого» суитой, и перепись выросла бы на строку, которой нет.
    good = "итого" not in rows
    ok = ok and good
    print(f"  [{'ok ' if good else 'FAIL'}] (п.11) строка итога суитой не считается")

    def _doc_with(rel_changes: dict[str, tuple[int, int]]) -> pathlib.Path:
        """Копия боевого документа с подменёнными числами названных суит."""
        body = (ROOT / SHARD_DOC).read_text(encoding="utf-8")
        out = []
        for line in body.splitlines():
            m = SHARD_DOC_ROW.match(line)
            if m and m.group(1) in rel_changes:
                c, r = rel_changes[m.group(1)]
                line = re.sub(r'^(\|\s*[a-z][a-z0-9-]*\s*\|\s*)\d+(\s*\|\s*)\d+(\s*\|)',
                              rf'\g<1>{c}\g<2>{r}\g<3>', line)
            out.append(line)
        fh = tempfile.NamedTemporaryFile("w", suffix=".md", delete=False, encoding="utf-8")
        fh.write("\n".join(out) + "\n")
        fh.close()
        return pathlib.Path(fh.name)

    stale = _doc_with({"vpc": (16, 3384)})
    try:
        run(base, "(п.11) вес суиты в документе разошёлся с деревом", want_red=True,
            expect="суита 'vpc' объявлена как 16/3384", shard_doc=stale)
    finally:
        stale.unlink(missing_ok=True)

    missing = pathlib.Path(tempfile.mkdtemp()) / "нет-такого.md"
    run(base, "(п.11) документа разбиения нет — отказ, а не тишина", want_red=True,
        expect="осталась бы вне наблюдения молча", shard_doc=missing)

    empty = tempfile.NamedTemporaryFile("w", suffix=".md", delete=False, encoding="utf-8")
    empty.write("# без таблицы\n")
    empty.close()
    try:
        run(base, "(п.11) таблицы в документе нет — разборщик ослеп, и это находка",
            want_red=True, expect="НИ ОДНОЙ строки таблицы весов",
            shard_doc=pathlib.Path(empty.name))
    finally:
        pathlib.Path(empty.name).unlink(missing_ok=True)

    run(base, "(п.11) законный близнец: боевой документ сходится с деревом",
        want_red=False, shard_doc=ROOT / SHARD_DOC)

    # (а) шард потерял суиту целиком
    m = copy.deepcopy(base)
    m["shards"] = [s for s in m["shards"] if s["id"] != "edge"]
    run(m, "(а) суиты edge-шарда никем не взяты", want_red=True)

    # (б) суита задвоена между двумя шардами
    m = copy.deepcopy(base)
    m["shards"][0]["suites"] = m["shards"][0]["suites"] + ["geo"]
    run(m, "(б) суита geo задвоена", want_red=True)

    # (в) шард называет несуществующую суиту
    m = copy.deepcopy(base)
    m["shards"][0]["suites"] = m["shards"][0]["suites"] + ["dns"]
    run(m, "(в) шард берёт суиту, которой нет в дереве", want_red=True)

    # (г) компонент объявлен включённым, но выключить его нечем
    m = copy.deepcopy(base)
    m["shards"][0]["components"] = m["shards"][0]["components"] + ["kaname"]
    run(m, "(г) компонент вне gates", want_red=True)

    # (д) предпосылка гейта: gates обязаны быть условны в Chart.yaml. Вход —
    # НАСТОЯЩАЯ зависимость зонта без условия (`pg-geo`), а не имя, которого в
    # Chart.yaml нет: прежний вход (подчарт экрана входа поставщика) снят вместе
    # со своей зависимостью (#1276), и отсутствующее имя судило бы уже не ту
    # форму — «нет зависимости», а не «зависимость без условия».
    m = copy.deepcopy(base)
    m["gates"] = m["gates"] + ["pg-geo"]
    run(m, "(д) gate без condition в Chart.yaml", want_red=True,
        expect="НЕТ `condition:")

    # (д1) ТОТ ЖЕ ПУНКТ НА НОВОМ КЛЮЧЕ: `uif` гасит консоль на всех шардах, и это
    # работает ТОЛЬКО если условие у подчарта есть — `--set uif.enabled=false` без
    # него отрендерится успешно и не сделает ничего. Инъекция берёт компонент,
    # который в Chart.yaml условен, и делает его безусловным, подменяя имя на
    # соседнее необусловленное; законный близнец — (д2) ниже.
    m = copy.deepcopy(base)
    m["gates"] = [g for g in m["gates"] if g != "uif"] + ["api-gateway"]
    m["non_service_gates"] = {"api-gateway": "build-ui"}
    run(m, "(д1) не-сервисный gate без condition в Chart.yaml", want_red=True,
        expect="НЕТ `condition:")

    # (д2) ЗАКОННЫЙ БЛИЗНЕЦ: `uif` как есть — условен в Chart.yaml, не назван ни
    # одним шардом, образа в SERVICES не имеет и домена ban #6 не имеет. Гейт
    # обязан МОЛЧАТЬ: иначе (д1) доказывал бы лишь чувствительность к правке
    # манифеста, а не то, что предикат различает существо.
    run(base, "(д2) законный близнец: uif условен и объявлен не-сервисом",
        want_red=False)

    # (д3) САМОИСТЕЧЕНИЕ исключения «не сервис»: компонент, ставший сервисом,
    # обязан выпасть из исключения находкой, а не молча остаться неподсудным
    # пунктам 6 и 8.
    m = copy.deepcopy(base)
    m["gates"] = m["gates"] + ["vpc-extra"]
    m["non_service_gates"] = dict(m["non_service_gates"], vpc="build-ui")
    run(m, "(д3) исключение на компоненте, который ЕСТЬ в SERVICES", want_red=True,
        expect="пережило свой предмет")

    # (д4) названная цель сборки обязана существовать: «названа команда, которой
    # нет» — отдельный класс, и он ловится здесь, а не при первом прогоне стенда.
    m = copy.deepcopy(base)
    m["non_service_gates"] = {"uif": "build-console-that-does-not-exist"}
    run(m, "(д4) исключение называет несуществующую цель сборки", want_red=True,
        expect="которой в deploy/Makefile НЕТ")

    # (е) ban #6: домен выпал из охвата ВСЕХ шардов. Это и есть послабление, ради
    # невозможности которого сужение пробы сделано явным: без этой проверки метод
    # такого домена остался бы «изолированным» просто потому, что его не спросили.
    m = copy.deepcopy(base)
    m["gate_ban6_domains"] = {k: v for k, v in m["gate_ban6_domains"].items() if k != "registry"}
    run(m, "(е) домен registry не измеряет ни один шард", want_red=True,
        expect="не измеряет НИ ОДИН шард")

    # (ж) КОНТРОЛЬ: домен, который шарды заявляют, а Internal*-контракта у него нет.
    m = copy.deepcopy(base)
    m["core_ban6_domains"] = m["core_ban6_domains"] + ["operation"]
    run(m, "(ж) заявлен домен без Internal*-контракта", want_red=True,
        expect="измерять нечего")

    # (ж1) ПРИПИСКА СВЕРЯЕТСЯ С ДЕРЕВОМ: домен закреплён за компонентом, чей
    # сервис его не служит. Домен при этом остаётся в популяции и остаётся
    # покрытым своим настоящим компонентом, поэтому пункт 8 промолчит — покраснеть
    # может ТОЛЬКО новая сверка. Без неё шард объявил бы домен измеряемым, а
    # встречный контроль пробы пошёл бы на листенер, где такой службы нет.
    m = copy.deepcopy(base)
    m["gate_ban6_domains"] = dict(
        m["gate_ban6_domains"],
        registry=m["gate_ban6_domains"]["registry"] + ["vpc"])
    run(m, "(ж1) домен приписан компоненту, чей сервис его НЕ служит", want_red=True,
        expect="НЕ служит")

    # (ж2) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: у компонента ДВА домена, и оба его
    # сервис действительно служит. Без этой стороны (ж1) доказывал бы лишь
    # чувствительность к второму имени в списке, а не к неверной приписке.
    # Поперечный домен оставлен ровно у одного носителя: охват держится (его
    # берёт шард edge), приписка верна — гейт обязан МОЛЧАТЬ.
    m = copy.deepcopy(base)
    m["gate_ban6_domains"] = {
        c: [d for d in doms if d != "subscription"] + (["subscription"] if c == "registry" else [])
        for c, doms in m["gate_ban6_domains"].items()}
    run(m, "(ж2) близнец: два домена у компонента, приписка верна", want_red=False)

    # (ж3) ТА ЖЕ СВЕРКА ДЛЯ ЯДРА: домен объявлен ядерным, а ни один образ ядра
    # его не служит. На стенде без соответствующего компонента подтвердить его
    # встречный контроль нечем — то есть «ядерный» здесь означало бы «есть
    # везде» про домен, которого местами нет.
    m = copy.deepcopy(base)
    m["core_ban6_domains"] = m["core_ban6_domains"] + ["vpc"]
    run(m, "(ж3) домен объявлен ядерным, но образы ядра его не служат", want_red=True,
        expect="ни один образ ядра")

    # (з) ТОТ САМЫЙ ДЕФЕКТ, воспроизведённый: шард гоняет суиту, которая набирает
    # транспорт к компоненту, а компонент не поднимает. Именно в этой форме четыре
    # шарда из пяти не запустили ни одной суиты (31344367968).
    # Суита-потребитель ПЕРЕЕЗЖАЕТ на шард без компонента, а составы компонентов
    # остаются нетронутыми: тогда ни охват ban #6, ни счёт коллекций не меняются, и
    # покраснеть может ТОЛЬКО пункт 9.
    m = copy.deepcopy(base)
    for sh in m["shards"]:
        if sh["id"] == "edge":
            sh["suites"] = [x for x in sh["suites"] if x != "registry"]
        if sh["id"] == "vpc":
            sh["suites"] = sh["suites"] + ["registry"]
    run(m, "(з) суита набирает транспорт, компонента на её шарде нет", want_red=True,
        expect="этот компонент НЕ поднимает")

    # (и) САМОИСТЕЧЕНИЕ: объявленный транспорт, которого не набирает никто.
    # Послабление, которому больше нечего обслуживать, обязано быть находкой —
    # иначе объявление переживёт свой предмет.
    m = copy.deepcopy(base)
    m["optional_transports"] = dict(m["optional_transports"])
    m["optional_transports"]["nobodyDialsThisBaseUrl"] = {
        "component": "registry", "service": "registry", "target_port": 8080,
        "port_env": "NOBODY_PORT", "default_port": 18581, "scheme": "http",
        "why": "инъекция самопроверки",
    }
    run(m, "(и) объявлен транспорт, которого никто не набирает", want_red=True,
        expect="объявлению нечего обслуживать")

    # (к) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: транспорт, чей компонент поднимают ВСЕ
    # шарды, гоняющие его потребителей. Без этого пункта (з)/(и) доказывали бы лишь
    # чувствительность к правке манифеста, а не то, что предикат различает существо.
    m = copy.deepcopy(base)
    for sh in m["shards"]:
        if "registry" not in sh["components"]:
            sh["components"] = sh["components"] + ["registry", "pg-registry"]
    run(m, "(к) близнец: компонент поднят ВЕЗДЕ, где его набирают", want_red=False)

    # ── популяция ban #6: пара на СИНТЕТИЧЕСКОЙ переписи ────────────────────
    #
    # Пункт 8 спрашивает у переписи, а не у дерева, поэтому его инъекции идут
    # переписью. Домен `alpha` в дереве не существует ни в каком виде — это и
    # нужно: фикстура не привязана к тому, что завтра провяжут.
    live = _ban6_module().census(ROOT)

    # (л) провязанный домен, которого не берёт НИ ОДИН шард — то самое послабление,
    # ради невозможности которого пункт 8 и написан.
    run(base, "(л) провязанный домен не берёт ни один шард", want_red=True,
        expect="домен 'alpha' несёт Internal*-контракт, ПРОВЯЗАННЫЙ прод-кодом",
        ban6=_census_plus(live, "alpha", served=True))

    # (м) ЗАКОННЫЙ БЛИЗНЕЦ: контракт приземлён, но не провязан ни одним
    # композиционным корнем — у ban #6 нет предмета, и гейт обязан МОЛЧАТЬ.
    # Без этой стороны (л) доказывал бы лишь чувствительность к переписи.
    run(base, "(м) близнец: контракт приземлён, но не провязан — предмета нет",
        want_red=False, ban6=_census_plus(live, "alpha", served=False))

    # (н) САМОИСТЕЧЕНИЕ послабления: тот же домен ПРОВЯЗАЛИ. Молчание (м) обязано
    # кончиться в тот же миг — иначе послабление пережило бы свой предмет.
    run(base, "(н) тот же домен провязали ⇒ молчание кончилось", want_red=True,
        expect="домен 'alpha' несёт Internal*-контракт, ПРОВЯЗАННЫЙ прод-кодом",
        ban6=_census_plus(live, "alpha", served=True))

    # (о) обратная сторона: шард ЗАЯВЛЯЕТ домен, которого не провязал никто.
    # Это не «покрыто», а невыполненное измерение: встречный контроль пробы такой
    # домен не подтвердит и уронит прогон.
    m = copy.deepcopy(base)
    m["core_ban6_domains"] = m["core_ban6_domains"] + ["alpha"]
    run(m, "(о) шард заявляет домен, которого не провязал никто", want_red=True,
        expect="НЕВЫПОЛНЕННОЕ ИЗМЕРЕНИЕ",
        ban6=_census_plus(live, "alpha", served=False))

    # (п) ПРЕДПОСЫЛКА ПРЕДИКАТА: «ноль доменов» — отказ, а не «всё покрыто».
    empty = {"services": {}, "registrations": {}, "served": set(), "unserved": {},
             "proto_files_read": 0, "domains_with_contract": 0, "registrations_found": 0}
    run(base, "(п) популяция пуста ⇒ отказ, а не «сошлось»", want_red=True,
        expect="это отказ, а не «сошлось»", ban6=empty)

    # (р) ВТОРАЯ ПОЛОВИНА той же предпосылки: контракты есть, а провязки не нашлось
    # ни одной — предикат провязки сломался, и это тоже отказ, а не «нечего мерить».
    broken = dict(empty, services={d: list(v) for d, v in live["services"].items()},
                  domains_with_contract=len(live["services"]),
                  unserved={d: list(v) for d, v in live["services"].items()},
                  proto_files_read=live["proto_files_read"])
    run(base, "(р) контракты есть, провязок ноль ⇒ отказ предиката", want_red=True,
        expect="предикат провязки не нашёл предмет", ban6=broken)

    # ── (т…) ПУНКТ 10: числа прозы манифеста сходятся с деревом.
    #
    # Инъекции правят ТОЛЬКО пояснение шарда, а прочие пункты читают `suites`,
    # `components` и `gates` — значит красное здесь не может прийти от соседа
    # by construction, и ожидание у каждой инъекции ИМЕНОВАННОЕ.
    #
    # ВЕРНАЯ ПАРА БЕРЁТСЯ У ДЕРЕВА, А НЕ ВЫПИСЫВАЕТСЯ. Прежде фикстуры несли
    # «18 коллекций / 3709 запросов» литералом — числа дня записи. Девятнадцатая
    # коллекция vpc (kacho#2684) сделала красными ОБА законных близнеца (т4, т6)
    # при верном дереве и верном манифесте: самопроверка судила не гейт, а
    # свежесть собственной константы (`testing.md` §«Гейт на класс», п. 5 —
    # фикстура, привязанная к живому предмету, истекает вместе с ним). Теперь
    # верная пара выводится тем же читателем, что у самого пункта; дефектная
    # получается из неё подстановкой заведомо чужого числа.
    weights_tree = tracked_collections(ROOT)
    weights_reqs = tracked_requests(ROOT, weights_tree)
    vpc_colls, vpc_reqs = len(weights_tree.get("vpc", [])), weights_reqs.get("vpc", 0)
    assert vpc_colls > 0 and vpc_reqs > 0, "фикстуре пункта 10 нужен непустой вес суиты vpc в дереве"
    cs_suites = next(sh["suites"] for sh in base["shards"] if sh["id"] == "compute-storage")
    cs_parts = [len(weights_tree.get(su, [])) for su in cs_suites]
    cs_reqs = sum(weights_reqs.get(su, 0) for su in cs_suites)
    # Сумма та же, слагаемые сдвинуты на единицу: перестановка равных частей
    # дефектом не была бы, сдвиг — всегда.
    cs_shifted = [cs_parts[0] + 1, cs_parts[1] - 1] + cs_parts[2:]

    def _rewrite_why(m: dict, shard_id: str, why: str) -> dict:
        for sh in m["shards"]:
            if sh["id"] == shard_id:
                sh["why"] = why
        return m

    # (т1) число КОЛЛЕКЦИЙ разошлось.
    m = copy.deepcopy(base)
    _rewrite_why(m, "vpc", f"999 коллекций / {vpc_reqs} запросов — вес выписан неверно.")
    run(m, "(т1) вес шарда называет чужое число коллекций", want_red=True,
        expect="называет 999 коллекций")

    # (т2) число ЗАПРОСОВ разошлось. Коллекции при этом верны — значит красное
    # приходит от второй половины пары, а не от первой.
    m = copy.deepcopy(base)
    _rewrite_why(m, "vpc", f"{vpc_colls} коллекций / 999 запросов — вес выписан неверно.")
    run(m, "(т2) вес шарда называет чужое число запросов", want_red=True,
        expect="называет 999 запросов")

    # (т3) СУММА СХОДИТСЯ, а слагаемые переставлены. Без поразрядной сверки это
    # молчало бы: одна суита выросла ровно на столько, на сколько усохла соседняя.
    m = copy.deepcopy(base)
    _rewrite_why(m, "compute-storage",
                 f"{'+'.join(map(str, cs_shifted))} коллекций / {cs_reqs} запроса — слагаемые сдвинуты.")
    run(m, "(т3) сумма верна, слагаемые по суитам — нет", want_red=True,
        expect=f"по суитам {'+'.join(map(str, cs_shifted))}")

    # (т4) ЗАКОННЫЙ БЛИЗНЕЦ: вес переписан ДРУГИМИ СЛОВАМИ, числа верны. Гейт
    # обязан молчать — иначе он ловил бы правку прозы, а не расхождение с деревом.
    m = copy.deepcopy(base)
    _rewrite_why(m, "vpc", f"{vpc_colls} коллекций / {vpc_reqs} запросов — довод переписан целиком, "
                           "числа не тронуты.")
    run(m, "(т4) близнец: та же пара, другая проза", want_red=False)

    # (т5) ФОРМА, О КОТОРОЙ РАЗБОРЩИК НЕ ЗНАЕТ, — находка, а не тишина. Иначе
    # достаточно было бы переписать пояснение иначе, чтобы вес ушёл из-под
    # наблюдения, и «ноль находок» стало бы свойством разборщика.
    m = copy.deepcopy(base)
    _rewrite_why(m, "vpc", "самая тяжёлая суита по объёму, отдельный раннер.")
    run(m, "(т5) пояснение не открывается весом", want_red=True,
        expect="уходит из-под наблюдения молча")

    # (т6) ЧУЖИЕ ЧИСЛА ТОЙ ЖЕ ФОРМЫ В ХВОСТЕ пояснения гейт не судит — они про
    # другой предмет («122 запроса в коллекциях» у nlb — это запросы В ЧУЖОЙ
    # домен). Разборщик без якоря `^` схватил бы их и объявил находку там, где
    # число верно.
    m = copy.deepcopy(base)
    _rewrite_why(m, "vpc", f"{vpc_colls} коллекций / {vpc_reqs} запросов — из них 7 коллекций / "
                           "12 запросов ходят в geo.")
    run(m, "(т6) близнец: числа той же формы в хвосте не судятся", want_red=False)

    # (т7) ЗНАМЕНАТЕЛЬ ПРЕДИКАТА в прозе манифеста. Числитель прогоняет автор;
    # знаменатель — объём осмотренного, и устаревший он говорит о полноте
    # предиката неправду.
    #
    # Число берётся у САМОГО манифеста через PROSE_TOTAL, а не выписывается
    # («98») рядом: выписанное разошлось бы с деревом молча ровно тем же
    # способом, каким разошёлся продуктовый знаменатель — правка дерева (98→57
    # коллекций, #1111) превратила бы `.replace("при 98 …", …)` в no-op, и
    # инъекция стала бы неотличима от отсутствия дефекта. Так и произошло на
    # первом прогоне после снятия суиты iam: замена не находила «98» в уже
    # исправленном манифесте, вписанное число «7» совпадало с деревом только
    # случайно расходилось, и т7 молчал там, где обязан краснеть.
    m = copy.deepcopy(base)
    uif_idx = next(i for i, ln in enumerate(m["_uif"]) if PROSE_TOTAL.search(ln))
    real = int(PROSE_TOTAL.search(m["_uif"][uif_idx]).group("total"))
    m["_uif"][uif_idx] = PROSE_TOTAL.sub(f"при {real + 1} коллекц", m["_uif"][uif_idx])
    run(m, "(т7) знаменатель предиката разошёлся с деревом", want_red=True,
        expect="объём осмотренного назван неверно")

    # (т8) СЛЕПАЯ ЗОНА, СНЯТАЯ РЕКУРСИВНЫМ ОБХОДОМ. Знаменатель, записанный не
    # на верхнем уровне манифеста, а в пояснении вложенного объявления. Обход
    # верхнего уровня не увидел бы его НИ КРАСНЫМ, НИ ЗЕЛЁНЫМ — то есть молчание
    # было бы свойством разборщика. Сегодня такого знаменателя в дереве нет,
    # поэтому перепись расширение не меняет; способность его увидеть держит
    # ЭТА инъекция, а не наличие предмета.
    m = copy.deepcopy(base)
    m["optional_transports"] = dict(m["optional_transports"])
    first = sorted(m["optional_transports"])[0]
    m["optional_transports"][first] = dict(m["optional_transports"][first],
                                           why="набирают его при 7 коллекциях")
    run(m, "(т8) знаменатель в ГЛУБИНЕ манифеста тоже судится", want_red=True,
        expect="объём осмотренного назван неверно")

    # ── записи «носитель не развёрнут» (ban6_undeployed_carriers) ───────────
    #
    # Запись notify сняло само дерево: шаблон зонта
    # deploy/helm/umbrella/templates/notify-probe.yaml запускает процесс пробы
    # (kacho#2915, полоса D3), и домен notify взяло ядро манифеста. Механизм
    # записи при этом остаётся и обязан доказывать себя, поэтому стороны записи
    # судятся на ВОССТАНОВЛЕННОМ состоянии до D3: та же запись notify, домен и
    # образ notify вне ядра, а шаблона пробы в обходе нет. Каждая сторона ниже —
    # одним фактом против этого близнеца.
    #
    # (у0) — суд самого дерева: запись, возвращённая в манифест при шаблоне пробы
    # как есть, ИСТЕКАЕТ. Это доказательство того, что снятие записи — следствие
    # дерева, а не правки манифеста.
    probe_tpl_path = "deploy/helm/umbrella/templates/notify-probe.yaml"
    real_tpl = dict(chart_templates(ROOT))
    if probe_tpl_path not in real_tpl:
        print(f"  [FAIL] шаблона пробы {probe_tpl_path} в обходе нет — снятие записи notify "
              f"держится ни на чём, и стороны записи не судятся")
        ok = False
    elif base.get("ban6_undeployed_carriers"):
        print("  [FAIL] в манифесте есть записи «носитель не развёрнут» "
              f"{sorted(base['ban6_undeployed_carriers'])} при развёрнутом носителе notify — "
              "самопроверка ждала пустую ведомость; пересмотри её состав")
        ok = False
    else:
        pre_tpl = {k: v for k, v in real_tpl.items() if k != probe_tpl_path}
        rec_base = copy.deepcopy(base)
        rec_base["ban6_undeployed_carriers"] = {"notify": {
            "process": "kacho-notify-probe",
            "reason": "восстановленное состояние до D3: развёртывания пробы нет",
            "lifted_by": "развёртывание notify-probe шаблоном зонта"}}
        rec_base["core_ban6_domains"] = [d for d in base["core_ban6_domains"] if d != "notify"]
        rec_base["core_images"] = [i for i in base["core_images"] if i != "notify"]
        run(rec_base, "(у0) дерево как есть: шаблон зонта запускает пробу → запись ИСТЕКЛА",
            want_red=True, expect="ИСТЕКЛА")
        run(rec_base, "(у00) близнец: восстановленное состояние до D3 → запись законна",
            want_red=False, templates=pre_tpl)
        base = rec_base

        m = copy.deepcopy(base)
        m["ban6_undeployed_carriers"] = {}
        run(m, "(у1) запись notify снята → домен не измеряет НИ ОДИН шард", want_red=True,
            expect="домен 'notify' несёт Internal*-контракт", templates=pre_tpl)

        tpl = dict(pre_tpl)
        proc = base["ban6_undeployed_carriers"]["notify"]["process"]
        tpl["deploy/helm/umbrella/templates/notify-probe.yaml"] = (
            f'command: ["/usr/local/bin/{proc}", "serve"]\n')
        run(base, "(у2) процесс записи запускает шаблон → запись ИСТЕКЛА", want_red=True,
            expect="ИСТЕКЛА", templates=tpl)

        # (у2б) запуск блочным списком команды — тот же факт другой формой.
        tpl = dict(pre_tpl)
        tpl["deploy/helm/umbrella/templates/notify-probe.yaml"] = (
            f"      containers:\n        - name: probe\n          command:\n"
            f"            - /usr/local/bin/{proc}\n            - serve\n")
        run(base, "(у2б) запуск блочным списком команды → запись ИСТЕКЛА", want_red=True,
            expect="ИСТЕКЛА", templates=tpl)

        # (у2в) запуск образом, чей репозиторий назван процессом.
        tpl = dict(pre_tpl)
        tpl["deploy/helm/umbrella/templates/notify-probe.yaml"] = (
            f'          image: "docker.io/prorobotech/{proc}:1.0.0"\n')
        run(base, "(у2в) запуск образом процесса → запись ИСТЕКЛА", want_red=True,
            expect="ИСТЕКЛА", templates=tpl)

        # (у2г) запуск первым элементом args (образ без своей точки входа).
        tpl = dict(pre_tpl)
        tpl["deploy/helm/umbrella/templates/notify-probe.yaml"] = (
            f'          args: ["{proc}", "serve"]\n')
        run(base, "(у2г) запуск первым элементом args → запись ИСТЕКЛА", want_red=True,
            expect="ИСТЕКЛА", templates=tpl)

        # (у2д) запуск образом в ШАБЛОННОЙ форме: строка `image:` ссылается на
        # значения, репозиторий процесса назван в values зонтика. Близнец (у2е) —
        # та же строка и те же ключи, репозиторий называет ДРУГОЙ процесс: запись
        # не истекла. Между ними различие РОВНО одно — значение `repository`.
        probe_tpl = ('      containers:\n        - name: probe\n'
                     '          image: "{{ .Values.notifyProbe.image.repository }}:'
                     '{{ .Values.notifyProbe.image.tag }}"\n')
        vals = dict(chart_values(ROOT))
        uv = "deploy/helm/umbrella/values.yaml"

        def with_probe_repo(repo: str) -> dict:
            v = dict(vals)
            v[uv] = vals[uv] + (f"\nnotifyProbe:\n  image:\n    repository: "
                                f"docker.io/prorobotech/{repo}\n    tag: \"1.0.0\"\n")
            return v
        tpl = dict(pre_tpl)
        tpl["deploy/helm/umbrella/templates/notify-probe.yaml"] = probe_tpl
        run(base, "(у2д) образ шаблонной формы, репозиторий процесса в values → ИСТЕКЛА",
            want_red=True, expect="ИСТЕКЛА", templates=tpl, values=with_probe_repo(proc))
        run(base, "(у2е) близнец: та же форма, репозиторий другого процесса → НЕ истекла",
            want_red=False, templates=tpl, values=with_probe_repo("kacho-notify"))
        # (у2ж) образ через помощник подчарта (`include "<чарт>.image"`), значение — в
        # поддереве зонтика под ключом зависимости. Форма чарта notify в дереве.
        tpl = dict(pre_tpl)
        tpl["deploy/helm/notify/templates/_probe_image.tpl"] = (
            '{{- define "notify.probeImage" -}}'
            '{{ .Values.probe.image.repository }}:{{ .Values.probe.image.tag }}'
            '{{- end -}}\n')
        tpl["deploy/helm/notify/templates/probe.yaml"] = (
            '      containers:\n        - name: probe\n'
            '          image: {{ include "notify.probeImage" . | quote }}\n')
        v = dict(vals)
        v[uv] = vals[uv] + (f"\nnotify:\n  probe:\n    image:\n      repository: "
                            f"docker.io/prorobotech/{proc}\n      tag: \"1.0.0\"\n")
        run(base, "(у2ж) образ через include помощника, значение в поддереве зонтика → ИСТЕКЛА",
            want_red=True, expect="ИСТЕКЛА", templates=tpl, values=v)
        # (у2и) близнец SAN: помощник образа читает рядом и идентичность пробы
        # (`spiffe://…/sa/<процесс>`), репозиторий — другой процесс. Сегмент SAN
        # образом не читается: запись НЕ истекла.
        tpl = dict(pre_tpl)
        tpl["deploy/helm/notify/templates/_probe_image.tpl"] = (
            '{{- define "notify.probeImage" -}}'
            '{{ printf "%s:%s" .Values.probe.image.repository .Values.probe.image.tag }}'
            '{{- /* {{ .Values.probe.identity }} */ -}}'
            '{{- end -}}\n')
        tpl["deploy/helm/notify/templates/probe.yaml"] = (
            '      containers:\n        - name: probe\n'
            '          image: {{ include "notify.probeImage" . | quote }}\n')
        v = dict(vals)
        v[uv] = vals[uv] + ("\nnotify:\n  probe:\n    identity: "
                            f"spiffe://kacho.cloud/ns/kacho/sa/{proc}\n    image:\n"
                            "      repository: docker.io/prorobotech/kacho-notify\n"
                            "      tag: \"1.0.0\"\n")
        run(base, "(у2и) близнец: SAN процесса рядом в помощнике образа → НЕ истекла",
            want_red=False, templates=tpl, values=v)
        # (у2з) пустая ведомость значений при записи → отказ, а не «не запускает».
        run(base, "(у2з) файлов значений не прочитано при записи → отказ", want_red=True,
            expect="файлов значений чартов не прочитано", templates=pre_tpl, values={})

        # БЛИЗНЕЦЫ ПОДСТРОКИ (Д86): имя процесса в тексте шаблона без контейнера,
        # который его запускает, — запись НЕ истекла, дерево зелёное. Прежний
        # предикат (`proc in text`) красил оба.
        # (у6) сегмент SAN и адрес пробы в таблице источников notify.
        tpl = dict(pre_tpl)
        tpl["deploy/helm/notify/templates/sources-twin.yaml"] = (
            "data:\n  KACHO_NOTIFY_SOURCES: '[{\"module\":\"probe\",\"feedAddr\":"
            f"\"{proc}:9091\",\"san\":\"spiffe://kacho.cloud/ns/kacho/sa/{proc}\"}}]'\n"
            f"      containers:\n        - name: notify\n"
            f'          command: ["/usr/local/bin/kacho-notify", "serve", "--peer={proc}"]\n'
            f'          image: "docker.io/prorobotech/kacho-notify:1.0.0"\n')
        run(base, "(у6) SAN пробы в таблице источников без контейнера → запись НЕ истекла",
            want_red=False, templates=tpl)
        # (у7) Service с именем процесса без контейнера.
        tpl = dict(pre_tpl)
        tpl["deploy/helm/umbrella/templates/notify-probe-svc.yaml"] = (
            f"kind: Service\nmetadata:\n  name: {proc}\nspec:\n  selector:\n"
            f"    app: {proc}\n  ports:\n    - port: 9091\n")
        run(base, "(у7) Service с именем процесса без контейнера → запись НЕ истекла",
            want_red=False, templates=tpl)

        m = copy.deepcopy(base)
        m["ban6_undeployed_carriers"] = dict(
            m["ban6_undeployed_carriers"],
            dns={"process": "kacho-dns", "reason": "x", "lifted_by": "y"})
        run(m, "(у3) запись о носителе без провязанного домена → без предмета",
            want_red=True, expect="без предмета", templates=pre_tpl)

        m = copy.deepcopy(base)
        m["shards"][0]["components"] = m["shards"][0]["components"] + ["notify-probe"]
        m["gates"] = sorted(set(m["gates"]) | {"notify-probe"})
        m["gate_ban6_domains"] = dict(m["gate_ban6_domains"], **{"notify-probe": ["notify"]})
        m.setdefault("gate_images", {})
        m["gate_images"] = dict(m["gate_images"], **{"notify-probe": "notify"})
        run(m, "(у4) домен записи уже берёт шард → запись лишняя", want_red=True,
            expect="запись лишняя", templates=pre_tpl)

        run(base, "(у5) пустой обход шаблонов при записи → отказ", want_red=True,
            expect="шаблонов чартов не прочитано", templates={})

    print("\n=== самопроверка предиката популяции (синтетическое дерево) ===")
    ok = _self_test_population() and ok

    print("самопроверка:", "OK" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    if "--self-test" in sys.argv:
        sys.exit(_self_test())
    sys.exit(report(ROOT, MANIFEST))
