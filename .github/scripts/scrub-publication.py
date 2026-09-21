#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""scrub-publication.py — между прогоном и выкладкой стоит шаг, ЧИТАЮЩИЙ содержимое.

ПРЕДМЕТ (#1810)
---------------
Между шагом прогона и шагом `upload-artifact` не было НИ ОДНОГО шага, который
смотрит в файлы. Отчёты Newman и журналы стенда уезжали в артефакт публичного
репозитория как есть — вместе с тем, что в них записал сам инструмент: значения
заголовка `Authorization`, cookie, тела с токенами.

Канал был fail-open и НЕОБРАТИМЫЙ: выложенное удалением не отзывается — его
успевают прочитать, а доказать обратное нельзя. Удаление уже выложенного
применялось и причину не снимало ни разу: производитель оставался прежним.
Координаты удалённого здесь не приводятся намеренно: читатель публичен, перечень
адресов сказал бы ему, что именно скачивать, а выложенное до этой правки живёт до
истечения своего срока хранения.

Здесь сделано не третье удержание, а ЕДИНСТВЕННОЕ, что снимает причину: выкладка
стала fail-closed. Артефакт публикуется ТОЛЬКО если чистка сошлась; не сошлась —
не публикуется ничего.

ТРИ ИСХОДА, И ТРЕТИЙ НЕ ЗАЧИТЫВАЕТСЯ В УСПЕХ
--------------------------------------------
    0  ЧИСТО           — входы осмотрены целиком, изъятие сошлось, публиковать можно;
    1  НАХОДКА         — материал удостоверения остался после изъятия либо файл
                         нельзя переписать доказуемо чисто. Публикация запрещена;
    3  НЕ ВЫПОЛНИЛОСЬ  — осматривать нечего либо не по чему: ноль входов, нет
                         объявления, нет разборщика. Публикация тоже запрещена —
                         «не смотрели» не есть «чисто».

Ни один из трёх не печатает найденного ЗНАЧЕНИЯ. Печатаются координата файла,
имя распознанной формы и ЧИСЛО — этого хватает разбирающему и не хватает тому,
кто пришёл за удостоверением.

ПОЧЕМУ ПЕРЕЧЕНЬ ПУТЕЙ БЕРЁТСЯ ИЗ ОБЪЯВЛЕНИЯ ВЫКЛАДКИ, А НЕ ПОВТОРЯЕТСЯ ЗДЕСЬ
---------------------------------------------------------------------------
Скрипт читает ТОТ ЖЕ узел YAML, из которого выкладка берёт свои `path:`. Значит
«чистят одно, выкладывают другое» невозможно by construction: добавленная в
выкладку строка пути в тот же момент становится входом чистки. Выписанный рядом
перечень разошёлся бы молча — этот класс дерево уже ловило не раз.

Связь в обратную сторону — «у каждой выкладки есть своя чистка, и выкладка
погашена её исходом» — держит `.github/scripts/assert-uploads-are-scrubbed.py`.

ИЗЪЯТИЕ — НА МЕСТЕ, А НЕ В КОПИЮ
--------------------------------
Копия рядом оставила бы сырой файл по прежнему адресу, и он уехал бы ЛЮБОЙ
другой выкладкой, чьи маски его накрывают (у шарда сквозных проб таких масок
четыре). Поэтому файл переписывается на месте: после чистки сырого представления
в дереве не остаётся вовсе.

Вердикт выносится по ЗАПИСАННЫМ БАЙТАМ, а не по намерению: после записи файл
читается заново и сканируется ещё раз. Не сошлось — находка.

ФОРМЫ, КОТОРЫЕ РАСПОЗНАЮТСЯ (каждая доказана инъекцией в `--self-test`)
-----------------------------------------------------------------------
    key      — ключ JSON из закрытого словаря удостоверений (authorization,
               cookie, set-cookie, x-api-key, token и родня);
    pair     — пара Postman `{"key": "Authorization", "value": …}`: имя поля
               лежит в ЗНАЧЕНИИ соседнего ключа, а не в имени своего;
    header   — строка текста `Authorization: Bearer …` / `Cookie: …`;
    jwt      — три сегмента base64url через точку с заголовком JSON-веба;
    pem      — блок закрытого ключа;
    query    — удостоверение в параметрах адреса (`?access_token=…`);
    base64   — обёртка, под которой лежит любая из форм выше (глубина 1).

Словарь закрыт намеренно: распознаватель, который «ищет похожее на секрет»,
молчит ровно там, где форма ему незнакома, и молчание это неотличимо от чистоты.
Незнакомая форма здесь не выдаётся за чистую — она просто не заявляется
распознанной, и потому рядом стоит вторая половина: файл, который нельзя
прочитать и переписать доказуемо (двоичный с попаданием, ссылка за пределы
дерева), публикацию запрещает, а не пропускает.

ЗАПУСК
------
    scrub-publication.py --workflow <файл> --job <работа> --step-id <id выкладки>
                         [--root <корень>] [--scan-only]
    scrub-publication.py --self-test

`--scan-only` судит, не переписывая: это режим доказательства (инъекция канарейки
обязана краснить) и режим повторной сверки. Рабочий режим — изъятие и сверка.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import glob as globmod
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

# Пометка изъятия — ОДНО СЛОВО без пробелов, и это несущее свойство, а не вкус.
# Изъятие подставляется ВНУТРЬ строки (`?access_token=<сюда>`), а распознаватель
# значения обрывается на пробеле: с пометкой из трёх слов он видел бы после
# изъятия огрызок «[ИЗЪЯТО», не узнавал бы в нём свою же работу и объявлял
# находку на собственном результате. Поймано самопроверкой, а не вычиткой.
REDACTED = "[ИЗЪЯТО-ПРИ-ВЫКЛАДКЕ]"

CLEAN, FINDING, NOT_EXECUTED = 0, 1, 3

# Один файл читается целиком: потоковый обход с перекрытием усложнил бы разбор
# JSON и ничего не дал бы — предмет чистки измерен и лежит в мегабайтах.
# Больший файл не объявляется чистым: не осмотрен — не публикуется.
MAX_FILE_BYTES = 256 * 1024 * 1024

# ─── ЗАКРЫТЫЙ СЛОВАРЬ ИМЁН ───────────────────────────────────────────────────
# Имя нормализуется: регистр снимается, `-` и `_` схлопываются. Иначе
# `Set-Cookie`, `set_cookie` и `SET-COOKIE` были бы тремя разными именами, и
# распознаватель молчал бы на двух из трёх.
AUTH_KEYS = frozenset(
    {
        "authorization",
        "proxyauthorization",
        "wwwauthenticate",
        "cookie",
        "setcookie",
        "xapikey",
        "apikey",
        "xauthtoken",
        "token",
        "accesstoken",
        "idtoken",
        "refreshtoken",
        "bearertoken",
        "sessiontoken",
        "clientsecret",
        "clientassertion",
        "privatekey",
        "password",
        "passwd",
        "secret",
        "credential",
        "credentials",
    }
)


def norm_key(name: str) -> str:
    return re.sub(r"[-_\s]", "", str(name)).lower()


# ─── ФОРМЫ ЗНАЧЕНИЙ ──────────────────────────────────────────────────────────
# Заголовок JSON-веба: `eyJ` — это `{"` в base64url, то есть признак структуры,
# а не угадывание по длине.
RE_JWT = re.compile(rb"eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}")
RE_PEM = re.compile(rb"-----BEGIN [A-Z ]*PRIVATE KEY-----")
RE_HEADER = re.compile(
    rb"(?im)^[ \t>|\"']*(authorization|proxy-authorization|cookie|set-cookie|"
    rb"x-api-key|x-auth-token)[ \t]*[:=][ \t]*(?P<v>[^\r\n]+)"
)
RE_SCHEME = re.compile(rb"(?i)\b(bearer|basic)[ \t]+(?P<v>[A-Za-z0-9._~+/=-]{8,})")
RE_QUERY = re.compile(
    rb"(?i)[?&](access_token|id_token|refresh_token|token|api_key|apikey|"
    rb"client_secret|password)=(?P<v>[^&\s\"'<>#]{4,})"
)
# Обёртка base64: кандидат берётся по форме, раскрывается один раз и судится теми
# же правилами. Потолок на длину — чтобы обход не превращался в раскрытие всего
# файла по каждому смещению.
RE_B64 = re.compile(rb"[A-Za-z0-9+/=_-]{24,4096}")

TEXT_FORMS = (
    ("header", RE_HEADER),
    ("jwt", RE_JWT),
    ("pem", RE_PEM),
    ("query", RE_QUERY),
    ("scheme", RE_SCHEME),
)


def _b64_carries_secret(blob: bytes) -> bool:
    """Раскрывает обёртку ОДИН раз и судит раскрытое теми же формами."""
    if len(blob) % 4:
        pad = b"=" * (-len(blob) % 4)
    else:
        pad = b""
    for dec in (base64.b64decode, base64.urlsafe_b64decode):
        try:
            inner = dec(blob + pad)
        except (binascii.Error, ValueError):
            continue
        if not inner or len(inner) < 8:
            continue
        for _, rx in TEXT_FORMS:
            if rx.search(inner):
                return True
        # Раскрытое может оказаться JSON с ключом из словаря.
        try:
            doc = json.loads(inner.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            continue
        if _json_carries_secret(doc):
            return True
    return False


def _json_carries_secret(node: object) -> bool:
    """Судит РАЗОБРАННЫЙ документ. Знает обе формы: имя ключа и пару Postman.

    Пара — не частный случай первой формы, а отдельная: имя поля лежит в
    ЗНАЧЕНИИ соседнего ключа (`{"key": "Set-Cookie", "value": …}`), поэтому обход
    по именам её не видит вовсе. Поймано на настоящем отчёте newman: cookie
    ответа уцелела бы и в осмотре, и в ПОВТОРНОЙ сверке после изъятия — то есть
    проверка объявила бы чистым то, что сама же не умеет прочитать.
    """
    if isinstance(node, dict):
        pair_key = node.get("key")
        if isinstance(pair_key, str) and norm_key(pair_key) in AUTH_KEYS:
            for f in ("value", "values"):
                if f in node and _nonempty(node.get(f)):
                    return True
        for k, v in node.items():
            if norm_key(k) in AUTH_KEYS and _nonempty(v):
                return True
            if _json_carries_secret(v):
                return True
        return False
    if isinstance(node, list):
        return any(_json_carries_secret(v) for v in node)
    return False


def _nonempty(v: object) -> bool:
    """Отсутствие удостоверения выражается ровно двумя значениями.

    `null` и пустая строка — законное «его тут нет»; изъятая пометка — уже
    изъятое. Всё прочее считается материалом.
    """
    if v is None:
        return False
    if isinstance(v, str):
        return v.strip() not in ("", REDACTED)
    if isinstance(v, (list, dict)):
        return bool(v)
    return True


def scan_bytes(data: bytes) -> dict[str, int]:
    """Сколько попаданий каждой формы в этих байтах. Значения не возвращаются."""
    hits: dict[str, int] = {}
    for name, rx in TEXT_FORMS:
        n = 0
        for m in rx.finditer(data):
            val = m.groupdict().get("v")
            if val is not None and val.strip() in (b"", REDACTED.encode()):
                continue
            n += 1
        if n:
            hits[name] = hits.get(name, 0) + n
    n64 = 0
    for m in RE_B64.finditer(data):
        if _b64_carries_secret(m.group(0)):
            n64 += 1
    if n64:
        hits["base64"] = n64
    return hits


# ─── ИЗЪЯТИЕ ─────────────────────────────────────────────────────────────────


def redact_json(node: object, counts: dict[str, int]) -> object:
    """Обходит разобранный документ и изымает материал удостоверения."""
    if isinstance(node, dict):
        out: dict[str, object] = {}
        # Пара Postman: имя поля лежит в ЗНАЧЕНИИ ключа `key`, поэтому обычный
        # обход по именам её не видит — это отдельная форма, а не частный случай.
        pair_key = node.get("key")
        is_auth_pair = isinstance(pair_key, str) and norm_key(pair_key) in AUTH_KEYS
        for k, v in node.items():
            if norm_key(k) in AUTH_KEYS and _nonempty(v):
                counts["key"] = counts.get("key", 0) + 1
                out[k] = REDACTED
                continue
            if is_auth_pair and k in ("value", "values") and _nonempty(v):
                counts["pair"] = counts.get("pair", 0) + 1
                out[k] = REDACTED
                continue
            out[k] = redact_json(v, counts)
        return out
    if isinstance(node, list):
        return [redact_json(v, counts) for v in node]
    if isinstance(node, str):
        red, n = redact_text(node.encode("utf-8"), counts)
        if n:
            return red.decode("utf-8", "replace")
        return node
    return node


def redact_text(data: bytes, counts: dict[str, int]) -> tuple[bytes, int]:
    """Изымает материал в тексте. Возвращает (байты, сколько изъято)."""
    total = 0
    rep = REDACTED.encode("utf-8")

    def sub_group(name: str, rx: re.Pattern[bytes], buf: bytes) -> bytes:
        nonlocal total

        def _one(m: re.Match[bytes]) -> bytes:
            nonlocal total
            val = m.groupdict().get("v")
            if val is None:
                total += 1
                counts[name] = counts.get(name, 0) + 1
                return rep
            if val.strip() in (b"", rep):
                return m.group(0)
            total += 1
            counts[name] = counts.get(name, 0) + 1
            return m.group(0).replace(val, rep, 1)

        return rx.sub(_one, buf)

    for name, rx in TEXT_FORMS:
        data = sub_group(name, rx, data)

    def _b64(m: re.Match[bytes]) -> bytes:
        nonlocal total
        if _b64_carries_secret(m.group(0)):
            total += 1
            counts["base64"] = counts.get("base64", 0) + 1
            return rep
        return m.group(0)

    data = RE_B64.sub(_b64, data)
    return data, total


# ─── ОБХОД ───────────────────────────────────────────────────────────────────


RE_EXPR = re.compile(r"\$\{\{(?P<e>[^}]*)\}\}")


def expr_key(text: str) -> str:
    """Ключ выражения объявления: пробелы внутри скобок значения не несут."""
    return re.sub(r"\s+", "", text)


def substitute(pattern: str, values: dict[str, str], strict: bool = True) -> str:
    """Подставляет значения выражений объявления в маску пути.

    Выражение (`${{ matrix.dir }}`) вычисляет провайдер, а не мы: прочитанный из
    файла текст маски несёт его ДОСЛОВНО. Значение подаёт вызывающий — тем же
    выражением, которое провайдер уже вычислил для самой выкладки. Не подано —
    третий исход: маска, по которой не найдено ничего, и маска, которую нечем
    вычислить, обязаны различаться, иначе вторая молча выглядела бы как чистое
    дерево.
    """

    def one(m: re.Match[str]) -> str:
        key = expr_key(m.group("e"))
        if key not in values:
            # Нестрогий режим — для ЯРЛЫКА артефакта: он идёт в строку вывода и
            # ни одного файла не выбирает. Требовать значения и для него значило
            # бы отказываться от чистки из-за невычисленной подписи — исход,
            # пойманный на настоящем объявлении шарда (`newman-shard-${{ … }}`).
            if not strict:
                return m.group(0)
            raise LookupError(
                f"маска пути несёт выражение объявления ${{{{ {key} }}}}, "
                f"значение которого не подано ключом --set {key}=<значение>"
            )
        return values[key]

    return RE_EXPR.sub(one, pattern)


def declared_paths(
    workflow: Path, job: str, step_id: str, values: dict[str, str]
) -> tuple[list[str], list[str], str]:
    """Маски выкладки — из ТОГО ЖЕ узла, из которого их берёт сама выкладка."""
    import yaml  # локально: отсутствие разборщика — «не выполнилось», а не отказ

    doc = yaml.safe_load(workflow.read_text(encoding="utf-8"))
    jobs = (doc or {}).get("jobs") or {}
    spec = jobs.get(job)
    if spec is None:
        raise LookupError(f"в {workflow} нет работы {job!r}")
    for step in spec.get("steps") or []:
        if str(step.get("id") or "") != step_id:
            continue
        uses = str(step.get("uses") or "")
        if "upload-artifact" not in uses:
            raise LookupError(f"шаг {step_id!r} не является выкладкой (uses: {uses!r})")
        with_ = step.get("with") or {}
        raw = str(with_.get("path") or "")
        include, exclude = [], []
        for line in raw.splitlines():
            line = line.strip()
            if not line:
                continue
            if line.startswith("!"):
                exclude.append(substitute(line[1:].strip(), values))
            else:
                include.append(substitute(line, values))
        return include, exclude, substitute(str(with_.get("name") or step_id), values, strict=False)
    raise LookupError(f"в работе {job!r} нет шага с id {step_id!r}")


def expand(root: Path, patterns: list[str]) -> list[Path]:
    found: set[Path] = set()
    for pat in patterns:
        for hit in globmod.glob(str(root / pat), recursive=True):
            p = Path(hit)
            if p.is_dir():
                for sub in p.rglob("*"):
                    if sub.is_file() or sub.is_symlink():
                        found.add(sub)
            elif p.is_file() or p.is_symlink():
                found.add(p)
    return sorted(found)


class Report:
    def __init__(self) -> None:
        self.files = 0
        self.bytes = 0
        self.rewritten = 0
        self.redacted: dict[str, int] = {}
        self.findings: list[str] = []

    def add(self, form_counts: dict[str, int]) -> None:
        for k, v in form_counts.items():
            self.redacted[k] = self.redacted.get(k, 0) + v


def process(path: Path, root: Path, scan_only: bool, rep: Report) -> None:
    """Осматривает и (в рабочем режиме) переписывает ОДИН файл."""
    rel = os.path.relpath(path, root)

    if path.is_symlink():
        target = Path(os.path.realpath(path))
        try:
            target.relative_to(os.path.realpath(root))
        except ValueError:
            rep.findings.append(f"{rel}: ссылка ведёт за пределы дерева — публиковать нельзя")
            return

    try:
        size = path.stat().st_size
    except OSError as exc:
        rep.findings.append(f"{rel}: не прочитан ({exc.__class__.__name__}) — не осмотрен, значит не чист")
        return
    if size > MAX_FILE_BYTES:
        rep.findings.append(f"{rel}: {size} байт — больше потолка осмотра, целиком не прочитан")
        return

    try:
        data = path.read_bytes()
    except OSError as exc:
        rep.findings.append(f"{rel}: не прочитан ({exc.__class__.__name__}) — не осмотрен, значит не чист")
        return

    rep.files += 1
    rep.bytes += len(data)

    if scan_only:
        hits = scan_bytes(data)
        try:
            doc = json.loads(data.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            doc = None
        if doc is not None and _json_carries_secret(doc):
            hits["key"] = hits.get("key", 0) + 1
        if hits:
            rep.add(hits)
            rep.findings.append(
                f"{rel}: материал удостоверения — " + ", ".join(f"{k} ×{v}" for k, v in sorted(hits.items()))
            )
        return

    counts: dict[str, int] = {}
    out: bytes | None = None

    # JSON разбирается КАК ДОКУМЕНТ: имя ключа — единственный признак, по
    # которому видно значение, не похожее на удостоверение ни одной формой
    # (короткий непрозрачный ключ доступа, например).
    try:
        doc = json.loads(data.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        doc = None
    if doc is not None:
        cleaned = redact_json(doc, counts)
        out = json.dumps(cleaned, ensure_ascii=False).encode("utf-8")
    else:
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            # Двоичное содержимое переписать доказуемо чисто нельзя: структуры
            # мы не знаем, а подмена байтов в неизвестном формате ломает файл и
            # не доказывает изъятия. Попадание здесь — запрет публикации.
            hits = scan_bytes(data)
            if hits:
                rep.add(hits)
                rep.findings.append(
                    f"{rel}: двоичный файл несёт материал удостоверения ("
                    + ", ".join(f"{k} ×{v}" for k, v in sorted(hits.items()))
                    + ") — переписать его доказуемо чисто нельзя"
                )
            return
        red, _ = redact_text(text.encode("utf-8"), counts)
        out = red

    if out is not None and out != data:
        tmp = path.with_name(path.name + ".scrub.tmp")
        tmp.write_bytes(out)
        os.replace(tmp, path)
        rep.rewritten += 1
        rep.add(counts)

    # ВЕРДИКТ — ПО ЗАПИСАННЫМ БАЙТАМ. Читается то, что легло на диск, а не то,
    # что мы собирались записать: расхождение между намерением и содержимым —
    # ровно тот класс, ради которого этот шаг и заведён.
    after = path.read_bytes()
    left = scan_bytes(after)
    try:
        doc2 = json.loads(after.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        doc2 = None
    if doc2 is not None and _json_carries_secret(doc2):
        left["key"] = left.get("key", 0) + 1
    if left:
        rep.findings.append(
            f"{rel}: после изъятия остался материал — "
            + ", ".join(f"{k} ×{v}" for k, v in sorted(left.items()))
        )


def run(
    workflow: Path, job: str, step_id: str, root: Path, scan_only: bool, values: dict[str, str]
) -> int:
    try:
        include, exclude, name = declared_paths(workflow, job, step_id, values)
    except ModuleNotFoundError as exc:
        print(f"НЕ ВЫПОЛНИЛОСЬ: нет разборщика объявления ({exc}) — маски выкладки")
        print("    прочитать нечем. Это не «чисто»: публикация запрещена.")
        return NOT_EXECUTED
    except (LookupError, OSError) as exc:
        print(f"НЕ ВЫПОЛНИЛОСЬ: объявление выкладки не прочитано — {exc}")
        print("    Это не «чисто»: публикация запрещена.")
        return NOT_EXECUTED

    print(f"чистка перед выкладкой «{name}» ({workflow}, работа {job}, шаг {step_id})")
    print(f"    масок объявлено: {len(include)}" + (f", исключений {len(exclude)}" if exclude else ""))
    for pat in include:
        print(f"    · {pat}")

    files = expand(root, include)
    if exclude:
        dropped = set(expand(root, exclude))
        files = [f for f in files if f not in dropped]

    if not files:
        print("НЕ ВЫПОЛНИЛОСЬ: по маскам выкладки не нашлось НИ ОДНОГО файла.")
        print("    Ноль входов — это «не смотрели», а не «находок нет»: ноль находок")
        print("    на пустом обходе ничего не означает. Публикация запрещена.")
        return NOT_EXECUTED

    rep = Report()
    for f in files:
        process(f, root, scan_only, rep)

    print(
        f"перепись чистки: осмотрено файлов {rep.files} ({rep.bytes} байт), "
        f"переписано {rep.rewritten}, изъятий "
        + (", ".join(f"{k} ×{v}" for k, v in sorted(rep.redacted.items())) if rep.redacted else "0")
        + f", осталось находок {len(rep.findings)}"
    )
    if rep.files == 0:
        print("НЕ ВЫПОЛНИЛОСЬ: ни один вход не прочитан — судить не о чем.")
        return NOT_EXECUTED

    if rep.findings:
        print("НАХОДКА: публикация запрещена — материал удостоверения не изъят:")
        for line in rep.findings:
            print(f"  · {line}")
        print("    Значения не печатаются нигде: координата, форма и число — всё,")
        print("    что нужно разбирающему, и меньше, чем нужно пришедшему за ними.")
        return FINDING

    print("ЧИСТО: во всех осмотренных входах материала удостоверения не осталось.")
    return CLEAN


# ─── САМОПРОВЕРКА ────────────────────────────────────────────────────────────
# Прогоняется НАСТОЯЩИЙ этот скрипт отдельным процессом: проверка, зовущая свою
# же функцию, доказывает работу функции, а не работу скрипта.

CANARIES = {
    # форма: (имя файла, содержимое)
    "key": ("report.json", json.dumps({"request": {"headers": {"Authorization": "Bearer AAAABBBBCCCCDDDD"}}})),
    "pair": (
        "postman.json",
        json.dumps({"request": {"header": [{"key": "Authorization", "value": "Bearer AAAABBBBCCCCDDDD"}]}}),
    ),
    "header": ("stand.log", "ts=1 msg=req\nAuthorization: Bearer AAAABBBBCCCCDDDD\n"),
    "cookie": ("cookie.log", "Set-Cookie: session=abcdefghijklmnop; Path=/\n"),
    "jwt": ("jwt.log", "token accepted eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJlX2hlcmU\n"),
    "pem": ("key.log", "-----BEGIN RSA PRIVATE KEY-----\nQUJDREVGRw==\n-----END RSA PRIVATE KEY-----\n"),
    "query": ("url.log", "GET /v1/projects?access_token=AAAABBBBCCCCDDDD HTTP/1.1\n"),
    "pair-cookie": (
        "cookie.json",
        json.dumps({"response": {"header": [{"key": "Set-Cookie", "value": "kacho_session=abcdefghijklmnop; Path=/"}]}}),
    ),
    "base64": (
        "wrapped.log",
        "payload "
        + base64.b64encode(b'{"Authorization": "Bearer AAAABBBBCCCCDDDD"}').decode()
        + "\n",
    ),
}

LAWFUL_TWIN = {
    # Законный близнец КАЖДОЙ формы: та же структура, то же место, удостоверения
    # нет. Без этой половины распознаватель, краснеющий всегда, выглядел бы
    # работающим.
    "report.json": json.dumps({"request": {"headers": {"Authorization": None}}}),
    "postman.json": json.dumps({"request": {"header": [{"key": "Content-Type", "value": "application/json"}]}}),
    "stand.log": "ts=1 msg=req\nContent-Type: application/json\n",
    "cookie.log": "Set-Cookie: \n",
    "jwt.log": "token rejected: malformed\n",
    "key.log": "-----BEGIN CERTIFICATE-----\nQUJDREVGRw==\n-----END CERTIFICATE-----\n",
    "url.log": "GET /v1/projects?pageSize=50 HTTP/1.1\n",
    "wrapped.log": "payload " + base64.b64encode(b'{"page": 1, "size": 50}').decode() + "\n",
    # Близнец ПАРЫ: то же место, то же имя поля, удостоверения нет. Законным
    # отсутствием считаются ровно два значения — `null` и пустая строка.
    "cookie.json": json.dumps({"response": {"header": [{"key": "Set-Cookie", "value": None}]}}),
}

SELFTEST_WORKFLOW = """
name: проба
jobs:
  проба:
    steps:
      - id: выкладка
        uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with:
          name: проба
          path: |
            out/*.json
            out/*.log
"""


def self_test() -> int:
    me = str(Path(__file__).resolve())
    ok = True
    cases = 0

    def call(root: Path, extra: list[str]) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, me, "--workflow", str(root / "wf.yml"), "--job", "проба",
             "--step-id", "выкладка", "--root", str(root), *extra],
            capture_output=True, text=True, check=False,
        )

    def make_root(files: dict[str, str]) -> Path:
        d = Path(tempfile.mkdtemp())
        (d / "wf.yml").write_text(SELFTEST_WORKFLOW, encoding="utf-8")
        (d / "out").mkdir()
        for nm, body in files.items():
            (d / "out" / nm).write_text(body, encoding="utf-8")
        return d

    print("=== scrub-publication.py --self-test ===")

    # (1) ИНЪЕКЦИЯ ПО КАЖДОЙ ФОРМЕ: канарейка обязана краснить осмотр.
    for form, (nm, body) in CANARIES.items():
        cases += 1
        root = make_root({nm: body})
        r = call(root, ["--scan-only"])
        if r.returncode != FINDING:
            print(f"  ПРОВАЛ форма {form}: осмотр дал код {r.returncode}, ждали {FINDING}")
            print("        " + (r.stdout + r.stderr).replace("\n", "\n        ")[:900])
            ok = False
        elif "AAAABBBBCCCCDDDD" in (r.stdout + r.stderr) or "abcdefghijklmnop" in (r.stdout + r.stderr):
            print(f"  ПРОВАЛ форма {form}: в выводе напечатано ЗНАЧЕНИЕ канарейки")
            ok = False
        else:
            print(f"  ОК  форма {form} → канарейка найдена, значение не напечатано")

    # (2) ЗАКОННЫЙ БЛИЗНЕЦ КАЖДОЙ ФОРМЫ: та же структура без удостоверения — молчание.
    for nm, body in LAWFUL_TWIN.items():
        cases += 1
        root = make_root({nm: body})
        r = call(root, ["--scan-only"])
        if r.returncode != CLEAN:
            print(f"  ПРОВАЛ близнец {nm}: код {r.returncode}, ждали {CLEAN}")
            print("        " + (r.stdout + r.stderr).replace("\n", "\n        ")[:900])
            ok = False
        else:
            print(f"  ОК  близнец {nm} → молчание")

    # (3) РАБОЧИЙ РЕЖИМ: изъятие сходится, повторный осмотр чист, значение не
    #     осталось в файле, структура отчёта сохранена.
    cases += 1
    root = make_root({nm: body for nm, body in CANARIES.values()})
    r = call(root, [])
    left = [p for p in (root / "out").iterdir() if "AAAABBBBCCCCDDDD" in p.read_text(errors="replace")]
    if r.returncode != CLEAN:
        print(f"  ПРОВАЛ изъятие: код {r.returncode}, ждали {CLEAN}")
        print("        " + (r.stdout + r.stderr).replace("\n", "\n        ")[:1500])
        ok = False
    elif left:
        print(f"  ПРОВАЛ изъятие: значение осталось в файлах {[p.name for p in left]}")
        ok = False
    else:
        doc = json.loads((root / "out" / "report.json").read_text())
        if "request" not in doc:
            print("  ПРОВАЛ изъятие: структура отчёта не сохранена")
            ok = False
        else:
            print("  ОК  изъятие → повторный осмотр чист, значения нет, структура цела")

    # (4) ТРЕТИЙ ИСХОД: ноль входов — не «чисто».
    cases += 1
    root = make_root({})
    r = call(root, [])
    if r.returncode != NOT_EXECUTED:
        print(f"  ПРОВАЛ ноль входов: код {r.returncode}, ждали {NOT_EXECUTED}")
        ok = False
    else:
        print("  ОК  ноль входов → НЕ ВЫПОЛНИЛОСЬ (код 3), публикация запрещена")

    # (5) ТРЕТИЙ ИСХОД: объявления нет.
    cases += 1
    root = make_root({"stand.log": "чисто\n"})
    (root / "wf.yml").unlink()
    r = call(root, [])
    if r.returncode != NOT_EXECUTED:
        print(f"  ПРОВАЛ нет объявления: код {r.returncode}, ждали {NOT_EXECUTED}")
        ok = False
    else:
        print("  ОК  нет объявления → НЕ ВЫПОЛНИЛОСЬ (код 3)")

    # (6а) ВЫРАЖЕНИЕ ОБЪЯВЛЕНИЯ В МАСКЕ: значение не подано → третий исход, а не
    #      «файлов не нашлось». Вторая половина — значение подано, файл найден.
    expr_wf = SELFTEST_WORKFLOW.replace("out/*.json", "${{ matrix.dir }}/*.json")
    cases += 1
    root = make_root({})
    (root / "wf.yml").write_text(expr_wf, encoding="utf-8")
    (root / "out" / "clean.json").write_text('{"ok": true}', encoding="utf-8")
    r = call(root, [])
    if r.returncode != NOT_EXECUTED:
        print(f"  ПРОВАЛ выражение без значения: код {r.returncode}, ждали {NOT_EXECUTED}")
        ok = False
    else:
        print("  ОК  выражение в маске без --set → НЕ ВЫПОЛНИЛОСЬ (код 3)")

    cases += 1
    r = call(root, ["--set", "matrix.dir=out"])
    if r.returncode != CLEAN:
        print(f"  ПРОВАЛ выражение со значением: код {r.returncode}, ждали {CLEAN}")
        print("        " + (r.stdout + r.stderr).replace("\n", "\n        ")[:900])
        ok = False
    else:
        print("  ОК  выражение в маске с --set → маска вычислена, вход прочитан")

    # (6) ДВОИЧНОЕ С ПОПАДАНИЕМ: переписать доказуемо нельзя → запрет публикации.
    cases += 1
    root = make_root({})
    (root / "out" / "dump.log").write_bytes(
        b"\x00\x01\xff\xfe Authorization: Bearer AAAABBBBCCCCDDDD \x00\xff"
    )
    r = call(root, [])
    if r.returncode != FINDING:
        print(f"  ПРОВАЛ двоичное с попаданием: код {r.returncode}, ждали {FINDING}")
        ok = False
    else:
        print("  ОК  двоичное с попаданием → НАХОДКА, публикация запрещена")

    # (7) ЗАКОННЫЙ БЛИЗНЕЦ ШЕСТОГО: двоичное БЕЗ попадания публиковать можно.
    cases += 1
    root = make_root({})
    (root / "out" / "dump.log").write_bytes(b"\x00\x01\xff\xfe corpus \x00\xff")
    r = call(root, [])
    if r.returncode != CLEAN:
        print(f"  ПРОВАЛ двоичное без попадания: код {r.returncode}, ждали {CLEAN}")
        ok = False
    else:
        print("  ОК  двоичное без попадания → ЧИСТО")

    print(f"осмотрено случаев {cases}; самопроверка: {'PASS' if ok else 'FAIL'}")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--workflow")
    ap.add_argument("--job")
    ap.add_argument("--step-id")
    ap.add_argument("--root", default=os.environ.get("GITHUB_WORKSPACE") or ".")
    ap.add_argument("--scan-only", action="store_true", help="судить, не переписывая (режим доказательства)")
    ap.add_argument(
        "--set", action="append", default=[], metavar="ВЫРАЖЕНИЕ=ЗНАЧЕНИЕ",
        help="значение выражения объявления, встречающегося в маске пути выкладки",
    )
    ap.add_argument("--self-test", action="store_true")
    a = ap.parse_args()

    if a.self_test:
        return self_test()
    if not (a.workflow and a.job and a.step_id):
        print("НЕ ВЫПОЛНИЛОСЬ: не названы --workflow/--job/--step-id — чистить нечего и не по чему.")
        return NOT_EXECUTED
    # НЕОЖИДАННЫЙ ОБРЫВ — ЭТО ТРЕТИЙ ИСХОД, А НЕ НАХОДКА. Без этой развилки
    # исключение Python выходило бы кодом 1 — ровно тем, которым здесь называется
    # найденный материал. Наблюдалось в самопроверке: разборщик обёртки падал
    # TypeError и читался как «нашли». Публикация запрещена в обоих случаях,
    # но разбирающему называется РАЗНОЕ.
    values: dict[str, str] = {}
    for item in a.set:
        if "=" not in item:
            print(f"НЕ ВЫПОЛНИЛОСЬ: ключ --set {item!r} не имеет формы ВЫРАЖЕНИЕ=ЗНАЧЕНИЕ")
            return NOT_EXECUTED
        k, v = item.split("=", 1)
        values[expr_key(k)] = v
    try:
        return run(Path(a.workflow), a.job, a.step_id, Path(a.root).resolve(), a.scan_only, values)
    except Exception:  # noqa: BLE001 — предмет развилки именно «любое иное»
        import traceback

        print("НЕ ВЫПОЛНИЛОСЬ: чистка оборвалась — вердикта о содержимом нет.")
        traceback.print_exc()
        print("    Это не «чисто» и не «находка»: публикация запрещена, причина выше.")
        return NOT_EXECUTED


if __name__ == "__main__":
    sys.exit(main())
