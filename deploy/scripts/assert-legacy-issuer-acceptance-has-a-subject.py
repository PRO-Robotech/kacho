#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Приём прежнего издателя на КРАЮ обязан иметь ПРЕДМЕТ — живой контур, который
предъявителя этого издателя ещё добывает.

─────────────────────────────────────────────────────────────────────────────
ПРЕДМЕТ

Край принимает двух издателей: нашего и прежнего. Требование держать второго
записано пробой `TestF1b_OurIssuerIsADDEDToTheListNotSubstitutedForIt`
(`gateway/deploy/f1b_token_acceptance_declared_test.go`), и довод у неё был
ПРОЗОЙ: «предъявитель прежней полосы добывается интерактивным входом человека».

Проза не исполняется, поэтому она не может покраснеть. Когда вход человека
переедет на свою чеканку (задача #1122), контур исчезнет — а требование
останется, потому что о контуре оно узнаёт только из собственного комментария.
Тогда блок на снятие прежнего издателя (задача #1123) переживёт своё основание,
и снимут его не по предикату, а как непонятный.

Замер, ради которого гейт заведён (проверено инъекцией на дереве
`release/identity-3`): если убрать из посева церемонии обмен у прежнего
издателя, НЕ КРАСНЕЕТ НИЧТО — ни проба двух издателей, ни ведомость поверхностей
поставщика, ни `ceremony_credentials.py --verify`. Три стража зелены, предмета
нет.

─────────────────────────────────────────────────────────────────────────────
ЧТО ЭТОТ ГЕЙТ УТВЕРЖДАЕТ, А ЧТО — НЕТ

Утверждает РОВНО ОДНО: у требования есть предмет.

  * предмет ЖИВ  ⇒ зелено. Приём прежнего издателя на крае обоснован, задача
    #1123 неисполнима, и это сказано числами, а не памятью;
  * предмета НЕТ ⇒ находка. Записи приёма прежнего издателя пережили свой
    контур; названы поимённо профили, из которых их снимать, и названа задача.

НЕ утверждает, что профиль поднимется: это предмет пробы выше, и вердикт там
выносит НАСТОЯЩИЙ читатель (`config.Config.TokenAcceptance`). Второго разбора
объявления здесь нет намеренно — два разбора одного предмета разошлись бы молча
на вырожденном значении. Записи прежнего издателя здесь только ПЕРЕСЧИТЫВАЮТСЯ,
чтобы находка могла назвать место.

─────────────────────────────────────────────────────────────────────────────
ГРАНИЦА НАЗВАНА ВСЛУХ

Предмет гейта — КРАЙ (`api-gateway`). Приём токена у первой конфигурации
проверяющего (`registry`) — отдельный контур со своим предъявителем и своей
задачей; он здесь не судится, и молчание об нём не означает, что он чист.

Гейт не докажет, что иных производителей предъявителя прежнего издателя нет
вовсе: он смотрит на посев церемонии, объявленный `ceremony_credentials.py`, —
то есть на тот контур, ради которого требование и стоит. Производитель, заведённый
мимо этого объявления, останется невидим, и это держится обзором, а не здесь.

─────────────────────────────────────────────────────────────────────────────
ПОЧЕМУ ПУТЬ ПРЕЖНЕГО ИЗДАТЕЛЯ ОБЪЯВЛЕН ЗДЕСЬ ВТОРОЙ РАЗ — И ЧЕМ ЭТО УДЕРЖАНО

Словарь путей поставщика живёт в `internal/repohygiene/providersurface.go`,
и он на Go: импортировать его отсюда нельзя by construction. Значит объявление
второе, а второе объявление одного предмета расходится с первым молча.

Поэтому гейт ПРОВЕРЯЕТ СВОЮ ПРЕДПОСЫЛКУ: путь ниже обязан стоять в том словаре.
Разошлось — исход 2 («предпосылка сломана»), а не тихое зелёное.

Использование:
    python3 deploy/scripts/assert-legacy-issuer-acceptance-has-a-subject.py --root .
    python3 deploy/scripts/assert-legacy-issuer-acceptance-has-a-subject.py --self-test

Исходы: 0 — предмет жив; 1 — находка; 2 — предпосылка сломана (осматривать нечего).
"""
from __future__ import annotations

import argparse
import ast
import glob
import os
import sys
import tempfile

import yaml

# Объявление церемонии — единственный машиночитаемый источник о том, какой посев
# создаёт условие волны и каких предъявителей машинный посев выковать не может.
CEREMONY_DECL = "tests/authz-fixtures/ceremony_credentials.py"

# Словарь поверхностей поставщика — предпосылка пути ниже.
PROVIDER_SURFACE_DICT = "internal/repohygiene/providersurface.go"

# Путь ВЫДАЧИ токена у прежнего издателя. Обмен по нему и есть производство
# предъявителя: другого способа получить его токен не существует.
PROVIDER_TOKEN_ENDPOINT_PATH = "/oauth2/token"

# Ключ, под которым профиль зонта настраивает КРАЙ. То же имя, что у пробы
# приёма (`edgeChartKey`); расхождение наказуемо само — сравнивать станет нечего.
EDGE_CHART_KEY = "api-gateway"

PROFILE_DIR = "deploy/helm/umbrella"

# Задача, которая станет исполнимой, когда предмет исчезнет.
REMOVAL_TASK = "#1123"


class PremiseBroken(Exception):
    """Предпосылка гейта не выполняется: осматривать нечего."""


def literal_strings(tree: ast.Module) -> list[tuple[int, str]]:
    """Строковые литералы ИСПОЛНЯЕМОЙ части: докстроки исключены.

    Разбор, а не поиск по тексту: путь, названный в комментарии или в докстроке,
    производителем не является, и гейт, считающий его, краснел бы на собственном
    объяснении.
    """
    docstrings: set[int] = set()
    for node in ast.walk(tree):
        if not isinstance(node, (ast.Module, ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            continue
        body = getattr(node, "body", None)
        if not body:
            continue
        first = body[0]
        if isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant) \
                and isinstance(first.value.value, str):
            docstrings.add(id(first.value))
    out: list[tuple[int, str]] = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Constant) and isinstance(node.value, str) \
                and id(node) not in docstrings:
            out.append((node.lineno, node.value))
    return out


def parse_module(root: str, rel: str) -> ast.Module:
    path = os.path.join(root, rel)
    if not os.path.exists(path):
        raise PremiseBroken(f"нет файла {rel} — читать нечего")
    with open(path, encoding="utf-8") as fh:
        src = fh.read()
    try:
        return ast.parse(src, filename=rel)
    except SyntaxError as exc:
        raise PremiseBroken(f"{rel} не разбирается: {exc}") from exc


def read_ceremony_declaration(root: str) -> tuple[str, tuple[str, ...], list[str]]:
    """Посев церемонии, его делегаты и объявленные предъявители — из объявления.

    Пути НЕ выписываются здесь: рукописная копия разошлась бы с объявлением
    молча, а объявление и есть то место, куда смотрит прогонщик волны.
    """
    tree = parse_module(root, CEREMONY_DECL)
    seed: str | None = None
    delegates: tuple[str, ...] = ()
    presenters: list[str] = []
    for node in ast.walk(tree):
        targets = []
        if isinstance(node, ast.Assign):
            targets = node.targets
        elif isinstance(node, ast.AnnAssign):
            targets = [node.target]
        else:
            continue
        name = next((t.id for t in targets if isinstance(t, ast.Name)), None)
        if name == "CEREMONY_SEED" and isinstance(node.value, ast.Constant):
            seed = node.value.value
        elif name == "CEREMONY_SEED_DELEGATES" and isinstance(node.value, (ast.Tuple, ast.List)):
            delegates = tuple(e.value for e in node.value.elts
                              if isinstance(e, ast.Constant) and isinstance(e.value, str))
        elif name == "CEREMONY_ONLY_ENV" and isinstance(node.value, ast.Dict):
            presenters = [k.value for k in node.value.keys
                          if isinstance(k, ast.Constant) and isinstance(k.value, str)]
    if not seed:
        raise PremiseBroken(f"{CEREMONY_DECL} не объявляет CEREMONY_SEED — "
                            "объявление переехало, а предикат остался")
    if not presenters:
        raise PremiseBroken(f"{CEREMONY_DECL} не объявляет ни одного предъявителя церемонии — "
                            "объявление пусто, и «предмета нет» неотличимо от «не прочитали»")
    return seed, delegates, presenters


def check_premise_path_is_in_the_shared_dictionary(root: str) -> None:
    """Путь выдачи обязан стоять в общем словаре поверхностей поставщика."""
    path = os.path.join(root, PROVIDER_SURFACE_DICT)
    if not os.path.exists(path):
        raise PremiseBroken(f"нет словаря поверхностей {PROVIDER_SURFACE_DICT} — "
                            "предпосылка пути не проверяема")
    with open(path, encoding="utf-8") as fh:
        src = fh.read()
    if f'"{PROVIDER_TOKEN_ENDPOINT_PATH}"' not in src:
        raise PremiseBroken(
            f"путь выдачи {PROVIDER_TOKEN_ENDPOINT_PATH!r} больше не стоит в словаре "
            f"{PROVIDER_SURFACE_DICT}: два объявления одного предмета разошлись. "
            "Сверьте путь, а не правьте этот гейт наугад")


def producers_of_a_legacy_presenter(root: str, seed: str,
                                    delegates: tuple[str, ...]) -> list[tuple[str, int]]:
    """Места посева церемонии, обменивающие у ПРЕЖНЕГО издателя.

    Возвращает пары «файл, строка». Пусто ⇒ контур предъявителя прежней полосы в
    дереве больше не производится.
    """
    if not os.path.exists(os.path.join(root, seed)):
        # Посева нет по объявленному пути. Это НЕ «предмета нет»: гейт целиком
        # стоит на его чтении, и молчаливый ноль здесь означал бы «не прочитали»,
        # поданное как «нарушений нет». Объявление и дерево разошлись — чинить
        # надо это, а не приём токена.
        raise PremiseBroken(
            f"посев церемонии {seed}, объявленный в {CEREMONY_DECL}, в дереве отсутствует — "
            "прочитать производителя нечем")
    found: list[tuple[str, int]] = []
    for rel in (seed, *delegates):
        if not os.path.exists(os.path.join(root, rel)):
            # Делегат мог быть снят вместе со своим предметом — это законно, и
            # молчаливым пропуском не является: перепись печатает состав.
            continue
        tree = parse_module(root, rel)
        for lineno, value in literal_strings(tree):
            if PROVIDER_TOKEN_ENDPOINT_PATH in value:
                found.append((rel, lineno))
    return found


def edge_records_of_other_issuers(root: str) -> list[tuple[str, list[str]]]:
    """Профили зонта, чей КРАЙ принимает издателя, отличного от нашего.

    Перечень профилей ВЫВОДИТСЯ из дерева: рукописный разошёлся бы с ним молча.
    Разбор здесь только пересчитывает записи, чтобы находка могла назвать место;
    вердикт о поднимаемости выносит настоящий читатель у пробы приёма.
    """
    out: list[tuple[str, list[str]]] = []
    pattern = os.path.join(root, PROFILE_DIR, "values*.yaml")
    for path in sorted(glob.glob(pattern)):
        with open(path, encoding="utf-8") as fh:
            try:
                tree = yaml.safe_load(fh) or {}
            except yaml.YAMLError as exc:
                raise PremiseBroken(f"профиль {os.path.basename(path)} не разбирается: {exc}") from exc
        edge = tree.get(EDGE_CHART_KEY)
        if not isinstance(edge, dict):
            continue
        acceptance = edge.get("tokenAcceptance")
        if not isinstance(acceptance, dict):
            continue
        ours = str(acceptance.get("platformIssuer") or "").strip()
        raw = str(acceptance.get("issuers") or "")
        others = [i for i in (part.strip() for part in raw.split(",")) if i and i != ours]
        if others:
            out.append((os.path.basename(path), others))
    return out


def evaluate(root: str) -> tuple[int, list[str], list[str]]:
    """Вердикт, находки и перепись. Перепись печатается ВСЕГДА."""
    check_premise_path_is_in_the_shared_dictionary(root)
    seed, delegates, presenters = read_ceremony_declaration(root)
    producers = producers_of_a_legacy_presenter(root, seed, delegates)
    profiles = edge_records_of_other_issuers(root)

    census = [
        f"объявление церемонии: посев {seed}, делегатов {len(delegates)}, "
        f"предъявителей объявлено {len(presenters)}",
        f"мест обмена у прежнего издателя: {len(producers)}"
        + (" (" + ", ".join(f"{f}:{ln}" for f, ln in producers) + ")" if producers else ""),
        f"профилей, чей край принимает не нашего издателя: {len(profiles)}"
        + (" (" + ", ".join(name for name, _ in profiles) + ")" if profiles else ""),
    ]

    findings: list[str] = []
    if producers:
        # Предмет жив. Требование держать прежнего издателя обосновано, и это
        # утверждение о дереве, а не о чьей-то памяти.
        return 0, findings, census

    if not profiles:
        # Предмет исчез И записи сняты — то состояние, к которому линия ведёт.
        census.append("предмета нет и записей нет — приём прежнего издателя снят; "
                      "снимите и это требование вместе с его гейтом")
        return 0, findings, census

    for name, others in profiles:
        findings.append(
            f"{name}: край принимает {', '.join(repr(o) for o in others)}, а контура, "
            f"добывающего предъявителя этого издателя, в дереве больше НЕТ "
            f"(посев {seed} у него не обменивает). Запись приёма пережила свой предмет: "
            f"задача {REMOVAL_TASK} стала исполнимой — снимите издателя из issuers, его "
            f"запись из issuerKeySets и полосу интроспекции вместе с адресом и якорем")
    return 1, findings, census


# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: доказательство инъекцией, в ОБЕ стороны по каждой оси.
# ─────────────────────────────────────────────────────────────────────────────
DECL_TEMPLATE = '''\
"""докстрока объявления: {docnote}"""
CEREMONY_SEED = "tests/authz-fixtures/seed.py"
CEREMONY_SEED_DELEGATES: tuple[str, ...] = ()
CEREMONY_ONLY_ENV: dict[str, str] = {presenters}
'''

SEED_WITH_EXCHANGE = '''\
"""докстрока посева: путь /oauth2/token назван тут ПРОЗОЙ."""
def go():
    return post("https://provider" + "/oauth2/token")
'''

SEED_WITHOUT_EXCHANGE = '''\
"""докстрока посева: путь /oauth2/token назван тут ПРОЗОЙ."""
def go():
    return post("https://kaname.kacho.local" + "/kacho/tokens:mint")
'''

DICT_STUB = 'var ProviderSurfaces = []ProviderSurface{{{{Path: "{path}"}}}}\n'


def _plant(root: str, *, seed_src: str, profile: str, presenters: str = '{"jwtHumanCeremony": "почему"}',
           dict_path: str = PROVIDER_TOKEN_ENDPOINT_PATH, docnote: str = "предмет",
           drop_seed: bool = False) -> None:
    os.makedirs(os.path.join(root, "tests", "authz-fixtures"), exist_ok=True)
    os.makedirs(os.path.join(root, "internal", "repohygiene"), exist_ok=True)
    os.makedirs(os.path.join(root, PROFILE_DIR), exist_ok=True)
    with open(os.path.join(root, CEREMONY_DECL), "w", encoding="utf-8") as fh:
        fh.write(DECL_TEMPLATE.format(presenters=presenters, docnote=docnote))
    if not drop_seed:
        with open(os.path.join(root, "tests", "authz-fixtures", "seed.py"), "w", encoding="utf-8") as fh:
            fh.write(seed_src)
    with open(os.path.join(root, PROVIDER_SURFACE_DICT), "w", encoding="utf-8") as fh:
        fh.write(DICT_STUB.format(path=dict_path))
    with open(os.path.join(root, PROFILE_DIR, "values.probe.yaml"), "w", encoding="utf-8") as fh:
        fh.write(profile)


TWO_ISSUERS = ("api-gateway:\n"
               "  tokenAcceptance:\n"
               "    issuers: \"https://kaname.kacho.local,https://provider\"\n"
               "    platformIssuer: \"https://kaname.kacho.local\"\n")
ONE_ISSUER = ("api-gateway:\n"
              "  tokenAcceptance:\n"
              "    issuers: \"https://kaname.kacho.local\"\n"
              "    platformIssuer: \"https://kaname.kacho.local\"\n")
# Близнец с ПОХОЖИМ именем ручки: предикат обязан читать имя целиком, а не
# вхождением — иначе переименование остаётся незамеченным.
LOOKALIKE_KNOB = ("api-gateway:\n"
                  "  tokenAcceptanceLegacy:\n"
                  "    issuers: \"https://kaname.kacho.local,https://provider\"\n"
                  "    platformIssuer: \"https://kaname.kacho.local\"\n")
# Близнец с похожим именем ЧАРТА: `api-gateway-canary` — не край.
LOOKALIKE_CHART = ("api-gateway-canary:\n"
                   "  tokenAcceptance:\n"
                   "    issuers: \"https://kaname.kacho.local,https://provider\"\n"
                   "    platformIssuer: \"https://kaname.kacho.local\"\n")


def self_test() -> int:
    cases: list[tuple[str, dict, int]] = [
        # ── ось «предмет жив» ────────────────────────────────────────────────
        ("предмет жив + два издателя ⇒ ЗЕЛЕНО (законный близнец)",
         {"seed_src": SEED_WITH_EXCHANGE, "profile": TWO_ISSUERS}, 0),
        ("предмет жив + один издатель ⇒ ЗЕЛЕНО (не наш предмет: приём судит проба приёма)",
         {"seed_src": SEED_WITH_EXCHANGE, "profile": ONE_ISSUER}, 0),
        # ── ось «предмета нет» — то, ради чего гейт заведён ──────────────────
        ("предмета НЕТ + два издателя ⇒ НАХОДКА (приём пережил предмет)",
         {"seed_src": SEED_WITHOUT_EXCHANGE, "profile": TWO_ISSUERS}, 1),
        ("предмета НЕТ + один издатель ⇒ ЗЕЛЕНО (цель достигнута, а не поломка)",
         {"seed_src": SEED_WITHOUT_EXCHANGE, "profile": ONE_ISSUER}, 0),
        # ── ось «граница имени» ─────────────────────────────────────────────
        ("предмета НЕТ + похожая ручка tokenAcceptanceLegacy ⇒ ЗЕЛЕНО (имя читается целиком)",
         {"seed_src": SEED_WITHOUT_EXCHANGE, "profile": LOOKALIKE_KNOB}, 0),
        ("предмета НЕТ + похожий чарт api-gateway-canary ⇒ ЗЕЛЕНО (край читается целиком)",
         {"seed_src": SEED_WITHOUT_EXCHANGE, "profile": LOOKALIKE_CHART}, 0),
        # ── ось «предпосылка» ───────────────────────────────────────────────
        ("предъявителей объявлено ноль ⇒ ПРЕДПОСЫЛКА СЛОМАНА",
         {"seed_src": SEED_WITH_EXCHANGE, "profile": TWO_ISSUERS, "presenters": "{}"}, 2),
        ("посев исчез по объявленному пути ⇒ ПРЕДПОСЫЛКА СЛОМАНА (не «предмета нет»)",
         {"seed_src": SEED_WITH_EXCHANGE, "profile": TWO_ISSUERS, "drop_seed": True}, 2),
        ("путь ушёл из общего словаря ⇒ ПРЕДПОСЫЛКА СЛОМАНА",
         {"seed_src": SEED_WITH_EXCHANGE, "profile": TWO_ISSUERS,
          "dict_path": "/oauth2/renamed"}, 2),
    ]
    failures = 0
    for title, kwargs, want in cases:
        with tempfile.TemporaryDirectory() as root:
            _plant(root, **kwargs)
            try:
                code, _, _ = evaluate(root)
            except PremiseBroken:
                code = 2
        mark = "OK " if code == want else "ОТКАЗ"
        if code != want:
            failures += 1
        print(f"  [{mark}] {title} — ожидали {want}, получили {code}")
    # Отдельная ось: докстрока НЕ считается производителем. Она стоит в обоих
    # посевах самопроверки, и если бы считалась — случай «предмета нет» дал бы 0
    # там, где ждём 1, то есть провалился бы выше. Утверждение печатается, чтобы
    # свойство было названо, а не выведено читателем из чужого случая.
    print("  [OK ] докстрока производителем не считается — иначе случай «предмета НЕТ» "
          "не смог бы покраснеть ни разу")
    print(f"самопроверка: утверждений {len(cases) + 1}, отказов {failures}")
    return 1 if failures else 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--root", default=".", help="корень дерева")
    ap.add_argument("--self-test", action="store_true",
                    help="доказательство инъекцией, в обе стороны")
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    try:
        code, findings, census = evaluate(args.root)
    except PremiseBroken as exc:
        print(f"ПРЕДПОСЫЛКА СЛОМАНА: {exc}", file=sys.stderr)
        print("осматривать нечего — «ноль находок» здесь означало бы «ноль прочитанного»",
              file=sys.stderr)
        return 2

    for line in census:
        print(line)
    if findings:
        print("")
        for f in findings:
            print(f"НАХОДКА: {f}", file=sys.stderr)
        return code
    print("приём прежнего издателя на крае имеет предмет — требование обосновано.")
    return code


if __name__ == "__main__":
    sys.exit(main())
