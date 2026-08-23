#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Вердикт по отчёту сквозных проб консоли: исполнено ВСЁ объявленное, упавших нет.

ПРЕДМЕТ. Код возврата прогона отвечает на вопрос «упало ли то, что запускалось».
Он молчит о другом: запускалось ли всё. Проба, выпавшая из набора — переименован
файл, сломался отбор, отвалился каталог с описаниями, — уносит с собой своё
утверждение, а прогон остаётся зелёным. «Ноль упавших» из двух проб и из семи
выглядит одинаково, и различить их можно только сверкой с деревом.

Поэтому здесь два независимых утверждения:

  1. ЧИСЛО. Проб в отчёте столько же, сколько объявлено в дереве. Объявленное
     считается по исходникам проб (`test(` на своей строке), а не выписывается —
     выписанное число разошлось бы с деревом молча, и разошлось бы именно тогда,
     когда пробу добавили и забыли включить.
  2. ИСХОД. Ни одной пробы вне состояния «прошла». Пропуск («skipped») здесь
     ТОЖЕ провал: пропущенная проба — это отсутствующая проверка, и правило
     запрещает её ровно так же, как маску.

Отсутствие отчёта — ПРОВАЛ, а не «нечего проверять»: суита, не оставившая
отчёта, не выполнилась, и эта третья категория из вердикта не вычитается.

Печатается объём осмотренного: сколько исходников прочитано, сколько объявлений
в них найдено, сколько записей разобрано в отчёте. «Ноль находок» обязано быть
отличимо от «ноль прочитанного».

Запуск:
    python3 .github/scripts/assert-console-probes-verdict.py            # вердикт
    python3 .github/scripts/assert-console-probes-verdict.py --self-test # инъекция
"""

from __future__ import annotations

import json
import re
import sys
import tempfile
from pathlib import Path

# Объявление пробы: вызов `test(` в начале логической строки. Форма `test.describe(`
# и `test.skip(` под неё намеренно не подпадают: первая — группа, а не проба;
# вторая обязана ловиться отдельным запретом на пропуски, а не считаться пробой.
DECL = re.compile(r"^[ \t]*test\(", re.MULTILINE)

# Управляющие последовательности цвета из вывода playwright. В журнале шага они
# читаются как мусор и разрывают строку диагноза, поэтому снимаются перед
# печатью — но НЕ перед сверкой: сверка идёт по подстрокам, которых цвет не
# касается.
ANSI = re.compile(r"\x1b\[[0-9;]*m")


def plain(text: str) -> str:
    return ANSI.sub("", text)


# КОДЫ ВОЗВРАТА. Третья категория обязана иметь СВОЙ, иначе она неотличима от
# красноты продукта для всякого, кто читает исход, а не журнал (#1041).
#
#   0 — зелено
#   1 — КРАСНОЕ: вердикт вынесен и он о продукте
#   3 — НЕ ВЫПОЛНИЛОСЬ: вердикта нет ни у одной пробы
#
# Ненулевым остаётся и 3: вычета нет, прогон зелёным не становится. Различается
# не СТРОГОСТЬ, а ТО, ЧТО РАЗБИРАТЬ.
#
# ГРАНИЦА ПРОВЕДЕНА УЗКО, И ЭТО НЕ ОСТОРОЖНИЧАНЬЕ. Код 3 выдаётся ровно там, где
# есть ПОЛОЖИТЕЛЬНОЕ и конкретное свидетельство, что до продукта не дошла ни одна
# проба. Ранняя остановка набора под него НЕ подпадает, хотя тоже зовётся третьей
# категорией в своей строке: останавливает прогон предел падений, а падения эти
# бывают настоящими, продуктовыми. Объявив такой исход «условие не создано», мы
# спрятали бы дефект продукта за словами о недостижимости стенда — то есть
# сделали бы послабление маской, ровно то, что запрещено.
RC_GREEN = 0
RC_RED = 1
RC_UNMET = 3

SPECS_GLOB = "*.spec.ts"


def declared_probes(specs_dir: Path) -> tuple[int, int]:
    """Сколько проб объявлено в дереве и сколько файлов для этого прочитано."""
    files = sorted(specs_dir.glob(SPECS_GLOB))
    return sum(len(DECL.findall(f.read_text(encoding="utf-8"))) for f in files), len(files)


def walk_specs(node: dict) -> list[dict]:
    """Плоский список записей проб из отчёта playwright (вложенность произвольна)."""
    out: list[dict] = []
    for spec in node.get("specs", []) or []:
        out.append(spec)
    for child in node.get("suites", []) or []:
        out.extend(walk_specs(child))
    return out


def outcomes(report: dict) -> list[tuple[str, str, str, int]]:
    """Четвёрки «имя пробы → исход → текст отказа → сколько раз запускалась».

    Текст нужен третьей категории (#935): падение НА НАВИГАЦИИ означает, что
    проба до продукта не дошла, и вердикт о продукте по ней не выносится.

    Число запусков — ЕДИНСТВЕННЫЙ признак, отличающий пробу, которая не
    стартовала вовсе (прогон остановился раньше неё), от намеренного
    `test.skip`: исход у обеих один и тот же — `skipped`, — а запусков у первой
    НОЛЬ, у второй ровно один. Признак замерен на playwright 1.56.1, а не
    выведен из документации: сравнивать пришлось два отчёта, отличающихся
    только этим (#1050).
    """
    res: list[tuple[str, str, str, int]] = []
    for spec in walk_specs(report):
        title = spec.get("title") or "(без имени)"
        status = "не исполнена"
        message = ""
        started = 0
        for t in spec.get("tests", []) or []:
            runs = t.get("results", []) or []
            started += len(runs)
            if runs:
                last = runs[-1]
                status = last.get("status") or "не исполнена"
                # САМЫЙ СОДЕРЖАТЕЛЬНЫЙ из доступных текстов, а не первый.
                # Замерено (#1050): при снятии пробы по времени `error.message`
                # несёт голое «Test timeout of Nms exceeded», а журнал ожидания
                # с ИМЕНЕМ ЛОКАТОРА лежит во ВТОРОЙ записи `errors[]`. Взяв
                # первую, гейт печатает диагноз, по которому ничего не
                # разобрать, — то есть выполняет форму и не даёт содержания.
                candidates = [(last.get("error") or {}).get("message") or ""]
                candidates += [
                    (e or {}).get("message") or "" for e in (last.get("errors") or [])
                ]
                message = max(candidates, key=len, default="")
            elif t.get("status"):
                status = t["status"]
        res.append((title, status, message, started))
    return res


# Отказы, при которых запрос до продукта НЕ ДОШЁЛ: имя не разрешилось, соединение
# отвергнуто, стенд недостижим. Список закрытый и узкий намеренно — расширять его
# «похожими» словами значит превращать третью категорию в маску для красного.
_UNREACHED = (
    "net::ERR_NAME_NOT_RESOLVED",
    "net::ERR_CONNECTION_REFUSED",
    "net::ERR_CONNECTION_RESET",
    "net::ERR_ADDRESS_UNREACHABLE",
)


def unreached(message: str) -> bool:
    return any(tok in message for tok in _UNREACHED)


def verdict(report_path: Path, specs_dir: Path) -> tuple[int, list[str]]:
    """Возвращает (код возврата, строки вывода)."""
    log: list[str] = []
    declared, files = declared_probes(specs_dir)
    log.append(f"=== осмотрено: исходников проб {files}, объявлений в них {declared} ===")
    if files == 0 or declared == 0:
        log.append(
            "ПРОВАЛ: в дереве не найдено ни одного объявления пробы. Гейт судил бы о "
            "наборе, которого не читал, и его «ноль упавших» означало бы «ноль "
            f"прочитанного». Каталог: {specs_dir}"
        )
        return 1, log

    if not report_path.exists():
        log.append(
            f"ПРОВАЛ: отчёта нет ({report_path}). Суита без отчёта НЕ ВЫПОЛНИЛАСЬ — это "
            "третья категория исхода, и она не вычитается из вердикта и не зачитывается "
            "в зелёное."
        )
        return 1, log

    try:
        report = json.loads(report_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        log.append(f"ПРОВАЛ: отчёт не разбирается ({exc}) — исход прогона неизвестен.")
        return 1, log

    got = outcomes(report)
    started = [g for g in got if g[3] > 0]
    not_started = [g for g in got if g[3] == 0]
    log.append(f"=== разобрано записей отчёта: {len(got)} ===")
    log.append(f"=== исполнено проб {len(started)} из {declared} объявленных ===")
    rc = 0

    if len(got) != declared:
        log.append(
            f"ПРОВАЛ: объявлено проб {declared}, в отчёте {len(got)}. Расхождение значит, "
            "что часть проб не исполнялась, а прогон об этом молчал: «ноль упавших» из "
            "двух и из семи выглядит одинаково."
        )
        rc = 1

    bad = [(n, s, m) for n, s, m, _ in got if s != "passed"]
    bad_started = [(n, s, m) for n, s, m, r in got if s != "passed" and r > 0]
    for name, status, _, runs in got:
        mark = "✓" if status == "passed" else ("·" if runs == 0 else "✗")
        shown = "не стартовала" if runs == 0 else status
        log.append(f"  {mark} {shown:<14} {name}")

    # ТРЕТЬЯ КАТЕГОРИЯ (#935): ни одна проба не дошла до продукта.
    #
    # Красное означает «продукт ответил не то». Если КАЖДАЯ упавшая проба
    # отвалилась на самой навигации — стенд недостижим для браузера, и о
    # продукте не сказано ничего. Код возврата остаётся ненулевым (вычета нет,
    # прогон не зелёный), но причина названа своя: иначе разбор уходит в
    # продукт, которого запрос не касался.
    if bad_started and len(bad_started) == len(started) and all(
        unreached(m) for _, _, m in bad_started
    ):
        log.append(
            f"НЕ ВЫПОЛНИЛОСЬ: все {len(started)} стартовавших проб отвалились на навигации — до "
            "продукта не дошла ни одна. Это НЕ вердикт по продукту: разбирать "
            "надо достижимость стенда для браузера. Шаг «браузер видит консоль» "
            "задаёт тот же вопрос ДО прогона и обязан был поймать это раньше."
        )
        return RC_UNMET, log

    # ТРЕТЬЯ КАТЕГОРИЯ: прогон ОСТАНОВИЛСЯ САМ, не дойдя до конца набора (#1050).
    #
    # Проба с нулём запусков не стартовала вовсе. Это ни «пропуск» (тот несёт
    # ровно один запуск с исходом `skipped` — и остаётся провалом), ни «выпала
    # из набора» (той нет в отчёте совсем — это ловит сверка числа выше). Это
    # набор, не исполненный целиком, и вердикта по продукту у непрогнанных проб
    # нет: зелёным такой исход не становится, но и красным ПО ПРОДУКТУ не
    # называется — иначе разбор уйдёт в продукт, которого проба не касалась.
    if not_started:
        cap = (report.get("config") or {}).get("maxFailures") or 0
        first = next(((n, st, m) for n, st, m, r in got
                      if r > 0 and st != "passed"), None)
        log.append(
            f"НЕ ВЫПОЛНИЛОСЬ: исполнено {len(started)} проб из {declared} "
            f"объявленных, не стартовало {len(not_started)}. Прогон остановлен "
            f"ранней остановкой (предел падений: {cap or 'не объявлен'}). Набор "
            "не исполнен целиком — это ТРЕТЬЯ категория исхода, она не "
            "вычитается из вердикта и не зачитывается в зелёное."
        )
        if first:
            log.append(f"  диагноз — первое падение: {first[0]}")
            for line in plain(first[2] or "(текст отказа пуст)").strip().splitlines()[:6]:
                log.append(f"    {line}")
        else:
            log.append(
                "  диагноз назвать НЕЧЕМ: ни одна стартовавшая проба не упала, "
                "а прогон всё же оборвался — разбирать надо сам прогон."
            )
        return 1, log

    if bad:
        log.append(f"ПРОВАЛ: проб не в состоянии «прошла»: {len(bad)}.")
        log.append(
            "  Пропуск здесь — тоже провал: пропущенная проба это отсутствующая проверка."
        )
        rc = 1

    if rc == 0:
        log.append(f"✓ исполнены все {declared} объявленных проб, упавших и пропущенных нет")
    return rc, log


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: гейт обязан покраснеть на настоящем дефекте и смолчать на
# законном близнеце. Не доказавший обоих — не доказательство.
# ─────────────────────────────────────────────────────────────────────────────

_SPEC = 'test("одна", async () => {});\ntest("две", async () => {});\n'
_SPEC_WITH_GROUP = (
    'test.describe("группа", () => {\n  test("три", async () => {});\n});\n'
)


def _spec(title: str, status: str, message: str = "", runs: int = 1) -> dict:
    """Запись пробы. `runs=0` — проба не стартовала (ранняя остановка прогона);
    `runs=1, status="skipped"` — намеренный `test.skip`. Различие замерено на
    настоящем отчёте playwright, а не придумано (#1050)."""
    t: dict = {"status": status, "results": []}
    if runs:
        t["results"] = [{"status": status, "error": {"message": message}}]
    return {"title": title, "tests": [t]}


def _report_of(*specs: dict, max_failures: int = 0) -> str:
    return json.dumps({
        "config": {"maxFailures": max_failures},
        "suites": [{"title": "s", "specs": list(specs)}],
    })


def _report(*statuses: str, message: str = "") -> str:
    specs = [
        {
            "title": f"проба-{i}",
            "tests": [{"results": [{"status": s, "error": {"message": message}}]}],
        }
        for i, s in enumerate(statuses)
    ]
    return json.dumps({"suites": [{"title": "s", "specs": specs}]})


def self_test() -> int:
    rc = 0
    cases: list[tuple[str, bool, object, object]] = []

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        specs = root / "specs"
        specs.mkdir()
        (specs / "a.spec.ts").write_text(_SPEC, encoding="utf-8")
        (specs / "b.spec.ts").write_text(_SPEC_WITH_GROUP, encoding="utf-8")
        # Объявлено 3: две на верхнем уровне и одна внутри группы. Сама группа
        # (`test.describe(`) пробой не считается — иначе законный близнец
        # завышал бы объявленное и гейт краснел бы на исправном прогоне.
        declared, _ = declared_probes(specs)
        cases.append(("объявленное считается по дереву", declared == 3, 3, declared))

        rep = root / "results.json"

        # МОЛЧИТ: исполнено всё объявленное, все прошли.
        rep.write_text(_report("passed", "passed", "passed"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("законный прогон принят", got == 0, 0, got))

        # КРАСНЕЕТ: одна проба упала.
        rep.write_text(_report("passed", "failed", "passed"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("упавшая проба ловится", got == 1, 1, got))

        # КРАСНЕЕТ: проба пропущена — отсутствующая проверка, а не «не считается».
        rep.write_text(_report("passed", "passed", "skipped"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("пропуск ловится", got == 1, 1, got))

        # КРАСНЕЕТ: проба молча выпала из набора. Ровно тот случай, ради которого
        # гейт и существует: код возврата прогона здесь был бы нулевым.
        rep.write_text(_report("passed", "passed"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("недосчёт исполненного ловится", got == 1, 1, got))

        # КРАСНЕЕТ: отчёта нет вовсе — «не выполнилось» не зачитывается в зелёное.
        rep.unlink()
        got, _ = verdict(rep, specs)
        cases.append(("отсутствие отчёта ловится", got == 1, 1, got))

        # ТРЕТЬЯ КАТЕГОРИЯ: все пробы отвалились на навигации — вердикт остаётся
        # ненулевым (вычета нет), но причина названа своя.
        rep.write_text(
            _report("failed", "failed", "failed",
                    message="page.goto: net::ERR_NAME_NOT_RESOLVED at http://x/"),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        cases.append(("недостижимость названа своей причиной",
                      got == RC_UNMET and any("НЕ ВЫПОЛНИЛОСЬ" in ln for ln in log),
                      f"{RC_UNMET} + «НЕ ВЫПОЛНИЛОСЬ»", got))
        cases.append(("недостижимость даёт СВОЙ код, отличный от красного",
                      got != RC_RED and got != RC_GREEN, f"не {RC_RED} и не {RC_GREEN}", got))

        # ЗАКОННЫЙ БЛИЗНЕЦ: те же три падения, но по существу продукта — третья
        # категория объявляться НЕ должна, иначе она станет маской для красного.
        rep.write_text(
            _report("failed", "failed", "failed",
                    message="expect(received).toBeVisible() — элемент не найден"),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        cases.append(("падение по существу третьей категорией НЕ считается",
                      got == RC_RED and not any("НЕ ВЫПОЛНИЛОСЬ" in ln for ln in log),
                      f"{RC_RED} без «НЕ ВЫПОЛНИЛОСЬ»", got))

        # ЗАКОННЫЙ БЛИЗНЕЦ: часть проб дошла до продукта — значит стенд достижим,
        # и сетевое падение остальных третьей категорией не объявляется.
        rep.write_text(
            json.dumps({"suites": [{"title": "s", "specs": [
                {"title": "прошла", "tests": [{"results": [{"status": "passed"}]}]},
                {"title": "упала", "tests": [{"results": [
                    {"status": "failed",
                     "error": {"message": "net::ERR_NAME_NOT_RESOLVED"}}]}]},
                {"title": "прошла-2", "tests": [{"results": [{"status": "passed"}]}]},
            ]}]}),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        # ПРОТИВОПОЛОЖНАЯ ИНЪЕКЦИЯ (#1041): одиночная сетевая ошибка при прочих
        # зелёных обязана остаться КРАСНОЙ и получить код красного. Иначе
        # послабление становится маской: любой сетевой сбой одной пробы уводил бы
        # весь прогон в «условие не создано».
        cases.append(("частичная недостижимость третьей категорией НЕ считается",
                      got == RC_RED and not any("НЕ ВЫПОЛНИЛОСЬ" in ln for ln in log),
                      f"{RC_RED} без «НЕ ВЫПОЛНИЛОСЬ»", got))

        # ТРЕТЬЯ КАТЕГОРИЯ (#1050): прогон остановился сам, не дойдя до конца
        # набора. Гейт обязан назвать исход «не выполнилось» И дать диагноз —
        # текст первого падения. Без диагноза ранняя остановка не лучше снятия
        # по времени: отчёт есть, а разбирать по нему нечего.
        rep.write_text(
            _report_of(
                _spec("упала-1", "failed", 'waiting for locator("input[name=x]")'),
                _spec("упала-2", "failed", 'waiting for locator("input[name=x]")'),
                _spec("не-стартовала", "skipped", runs=0),
                max_failures=2,
            ),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        joined = "\n".join(log)
        cases.append((
            "ранняя остановка названа третьей категорией",
            got == 1 and "НЕ ВЫПОЛНИЛОСЬ: исполнено 2 проб из 3" in joined,
            "1 + «исполнено 2 из 3»", got))
        cases.append((
            "ранняя остановка несёт ДИАГНОЗ первого падения",
            'waiting for locator("input[name=x]")' in joined,
            "текст первого отказа в выводе", "нет" if 'locator' not in joined else "есть"))
        cases.append((
            "ранняя остановка называет объявленный предел",
            "предел падений: 2" in joined, "предел падений: 2",
            "назван" if "предел падений: 2" in joined else "НЕ назван"))

        # ДИАГНОЗ БЕРЁТСЯ САМЫЙ СОДЕРЖАТЕЛЬНЫЙ, а не первый: при снятии по
        # времени имя локатора лежит во второй записи `errors[]`, и гейт,
        # печатающий первую, даёт форму без содержания. Цвет снимается.
        rep.write_text(
            json.dumps({
                "config": {"maxFailures": 1},
                "suites": [{"title": "s", "specs": [
                    {"title": "снята по времени", "tests": [{"status": "timedOut", "results": [{
                        "status": "timedOut",
                        "error": {"message": "\x1b[31mTest timeout of 10000ms exceeded.\x1b[39m"},
                        "errors": [
                            {"message": "Test timeout of 10000ms exceeded."},
                            {"message": 'Error: page.fill: Test timeout.\nCall log:\n  - waiting for locator(\'input[name="traits.email"]\')'},
                        ],
                    }]}]},
                    {"title": "не-стартовала", "tests": [{"status": "skipped", "results": []}]},
                ]}],
            }),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        joined = "\n".join(log)
        cases.append((
            "диагноз берёт самый содержательный текст, а не первый",
            'waiting for locator' in joined,
            "имя локатора в выводе",
            "есть" if 'waiting for locator' in joined else "НЕТ"))
        cases.append((
            "цвет из чужого вывода снят",
            "\x1b[" not in joined, "без управляющих последовательностей",
            "чисто" if "\x1b[" not in joined else "остались"))

        # ЗАКОННЫЙ БЛИЗНЕЦ: намеренный `test.skip` несёт РОВНО ОДИН запуск.
        # Он остаётся провалом (отсутствующая проверка) и третьей категорией
        # НЕ объявляется — иначе ранняя остановка станет маской для пропусков.
        rep.write_text(
            _report_of(
                _spec("прошла", "passed"),
                _spec("намеренно пропущена", "skipped"),
                _spec("прошла-2", "passed"),
            ),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        joined = "\n".join(log)
        cases.append((
            "намеренный пропуск третьей категорией НЕ считается",
            got == 1 and "НЕ ВЫПОЛНИЛОСЬ" not in joined,
            "1 без «НЕ ВЫПОЛНИЛОСЬ»", got))

        # ЗАКОННЫЙ БЛИЗНЕЦ: полный зелёный прогон — перепись исполненного
        # печатается и здесь, иначе «ноль находок» неотличимо от «ноль
        # прочитанного».
        rep.write_text(
            _report_of(_spec("а", "passed"), _spec("б", "passed"), _spec("в", "passed")),
            encoding="utf-8",
        )
        got, log = verdict(rep, specs)
        joined = "\n".join(log)
        cases.append((
            "перепись исполненного печатается и на зелёном",
            got == 0 and "исполнено проб 3 из 3" in joined,
            "0 + «исполнено проб 3 из 3»", got))

        # КРАСНЕЕТ: дерево проб пусто — «ноль упавших» из ничего.
        empty = root / "empty"
        empty.mkdir()
        rep.write_text(_report(), encoding="utf-8")
        got, _ = verdict(rep, empty)
        cases.append(("пустое дерево проб ловится", got == 1, 1, got))

    for name, ok, want, have in cases:
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {name} (ждали {want}, получили {have})")
        if not ok:
            rc = 1
    print(f"=== самопроверка: случаев {len(cases)}, провалов {sum(1 for c in cases if not c[1])} ===")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()
    unknown = [a for a in sys.argv[1:] if a != "--self-test"]
    if unknown:
        # Неизвестный ввод — явный отказ. Опечатка в шаге конвейера иначе дала бы
        # зелёное, ничего не проверив.
        print(f"неизвестный аргумент: {unknown}; допустимо: без аргументов | --self-test",
              file=sys.stderr)
        return 2

    root = Path(__file__).resolve().parents[2]
    e2e = root / "ui-future" / "e2e"
    rc, log = verdict(e2e / "results.json", e2e / "specs")
    print("\n".join(log))
    return rc


if __name__ == "__main__":
    sys.exit(main())
