#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Сводный вердикт запроса на слияние: решает ли набор проверок пускать в ствол.

ПРЕДМЕТ (#614)
--------------
Обязательная проверка блокирует слияние, только когда она СУЩЕСТВУЕТ. Между
открытием запроса и появлением её check-run есть окно, в котором контекста нет ни
как pending, ни как failure, — и слияние проходит. Защита при этом выглядит
исполненной: список контекстов настроен, обход администратора выключен.

Цена измерена: в ствол уехала ревизия, чья обязательная проверка впоследствии
покраснела, и красное осталось незамеченным трое суток. Хуже прямого следствия
косвенное: из «влито» перестаёт следовать «было зелёным».

ПОЧЕМУ РЕШЕНИЕ ЗДЕСЬ, А НЕ В YAML
---------------------------------
Логика вердикта обязана быть проверяемой: ей подают синтетический набор проверок
и требуют предсказанного исхода. Пока она жила шагом workflow, доказать её можно
было только прогоном в конвейере — то есть тем самым способом, надёжность
которого она и призвана обеспечить.

ИСХОДОВ ТРИ, И ТРЕТИЙ НЕ ЗАЧИТЫВАЕТСЯ В ЗЕЛЁНОЕ
-----------------------------------------------
Исход выносится по СУДИМЫМ прогонам — по одному на проверку (раздел ниже) — и
по ОБЪЯВЛЕННОМУ: джобам, которые запрос обязан завести на голове (раздел
«ОБЪЯВЛЕННОЕ»).

* зелёный   — каждая объявленная проверка появилась, все судимые завершились
              успехом, и успехов ХОТЯ БЫ ОДИН;
* красный   — есть отказ, отмена, снятие по времени или требование действия;
              объявление пусто или не вычислено;
* не готово — что-то ещё идёт, либо объявленная проверка ещё не появилась
              (ждать).

«Ноль проверок» — это НЕ «замечаний нет», это «никто не смотрел», и отличать одно
от другого обязательно: ровно на их неразличении построен дефект, ради которого
написан этот скрипт. «Пришла одна из пятидесяти» — тот же класс на шаг дальше
(#2910): без объявленного она неотличима от «проверка одна».

ОБЪЯВЛЕННОЕ: ЧТО ЗАПРОС ОБЯЗАН ЗАВЕСТИ (#2910)
---------------------------------------------
Пока судились только пришедшие check-runs, вердикт выносился над тем, что успело
появиться. PR #2908, голова dfbbdbc474c: к 14:34:52 UTC на sha пришли два прогона
— этот свод и `review-text`, — и свод сказал «зелено, осмотрено проверок 1».
Задания остальных процессов появились после 14:35:30; в 14:45 check-runs на той же
sha было 65, из них 9 шли и 1 упал.

Поэтому перечень ожидаемого выводится из разобранных процессов дерева, а не из
ответа check-runs (`declare`):

* процесс идёт на запросе, если его `on` называет `pull_request` и фильтр ветки
  берёт базу запроса (`branches` с отменой `!`, `branches-ignore`; `*` не
  переходит `/`, `**` переходит, `?` и `+` — о предыдущем знаке);
* проверка — джоба такого процесса в показе площадки: нога литеральной матрицы —
  отдельная проверка, имя длиннее 100 байт обрезано так же, как его обрезает
  площадка (`provider_cut` берётся у держателя этой формы,
  `assert-review-trigger-scope.py`, второй копии правила в дереве нет);
* матрица из выражения — одна проверка-образец: её доказывает хотя бы одна нога,
  а полноту ног держит сводная джоба того же процесса, объявленная литералом;
* появление доказывает прогон приложения процессов (`github-actions`) — прогон
  другого приложения с тем же именем джобы не доказывает;
* собственная джоба исключается и обязана найтись ровно один раз: иначе свод
  ждал бы сам себя.

Что вычислить нельзя, не угадывается, а даёт нарушение, и вердикта нет (код 2):
фильтр по путям (он зависит от изменения запроса), `types` без хоть одного вида по
умолчанию (свод идёт именно на них), неизвестный ключ фильтра, событие
`pull_request_target`, вызов переиспользуемого процесса, неразобранный процесс,
пустая база, ноль объявленных. Нарушение предпосылки ключа (раздел ниже) — тоже
отказ: объявленная проверка, чьё имя может совпасть с чужим, не доказывается
прогоном однозначно.

Каталог процессов — тот, что исполнил запрос: задание свода берёт его из той же
ревизии слияния, по которой площадка запустила процессы.

Нейтральные и пропущенные считаются отдельно и НЕ зачитываются в успех: молча
зачесть их в зелёное значило бы повторить тот же класс на уровень ниже.

ПО КАЖДОЙ ПРОВЕРКЕ СУДИТСЯ ПОСЛЕДНИЙ ПРОГОН, НЕСУЩИЙ ИСХОД (#2865)
-----------------------------------------------------------------
Check-runs одной sha приходят из разных наборов: событие `edited` запускает
процесс заново, `cancel-in-progress` снимает прежний прогон, и отменённый
остаётся на sha навсегда. Судить все прогоны значит красить вердикт отменой,
у которой есть замена, — и перезапуск свода давал бы то же красное.

Поэтому прогоны одной проверки сводятся к одному:

* «позднее» — пара (`check_suite.id`, `id`), а не один `id`. Набор заводится
  на событие, и повтор попытки его СОХРАНЯЕТ: ui.yml, прогон 35183091753,
  проверка `build compute` — попытки 1 и 2 в одном наборе 95281882850, а id
  прогона у попытки 2 больше (105079283820 против 105082053758). Один `id`
  поставил бы повтор старого набора позже свежего события, а повтор судит
  СТАРЫЙ контекст:
  `review-text` читает текст из полезной нагрузки события. Номер набора
  растёт с событием: на 9672c86fac5 отменённый 108278452901 в наборе
  98030781046, замена 108278551222 в наборе 98030864187. `completed_at` у
  идущей замены пуст, а `started_at` — время с точностью до секунды и порядка
  не повторяет (на 00994f05007 прогон с меньшим `id` стартовал позже);
* судится последний прогон, НЕСУЩИЙ ИСХОД: идущий либо завершённый не
  нейтрально. Поздний пропуск ничего не осмотрел и прежний отказ не стирает;
  если исхода не несёт ни один — судится последний;
* прогоны разных приложений с одним именем — разные проверки, а не замена;
* прогоны одной проверки, у которых нет целого `id` или целого номера набора,
  судятся все: без порядка замену не доказать, и отмена по-прежнему красит.

Отставленные прогоны не исчезают молча: сводка называет каждый, его `id` и набор.

НЕРАСКРЫТЫЙ ШАБЛОН ИМЕНИ: ОТМЕНА ДО МАТРИЦЫ (#3267)
--------------------------------------------------
Прогон, отменённый до вычисления матрицы, площадка показывает ШАБЛОНОМ имени —
`build ${{ matrix.project }}`, — а замена в новом наборе того же процесса
называет ноги раскрытыми именами: ключ у них разный, и правило выше замены не
видит. PR #3268, голова 2b3076f7092: пять шаблонных отмен в ранних наборах трёх
процессов (`build ${{ matrix.project }}` — check-run 118089402988, набор
106387383545), все ноги замены — success в поздних (106387435861 и выше); свод
(прогон 39261316944, попытка 2) был красным только от этих пяти.

Шаблонный прогон отставлен, если разом:

* имя несёт закрытое выражение `${{ … }}` и ни одного незакрытого (обрезанное
  посреди выражения — не шаблон, а неизвестно что);
* исход — `cancelled`: отказ с шаблонным именем — это отказ вычисления матрицы,
  и поздний набор его не стирает;
* шаблон равен в показе имени РОВНО ОДНОЙ джобы объявленного (`Declared.templates`);
* судимый прогон, которым площадка показывает ногу ЭТОЙ джобы (`_proves` по её
  объявленным именам), лежит в наборе с бОльшим номером и несёт исход.

Нога, а не «имя, отвечающее шаблону»: шаблону `build ${{ matrix.project }}`
(ui.yml) отвечает и `build · vet · gofmt` (ci.yaml). Ноги литеральной матрицы
однозначны предпосылкой ключа, образец матрицы из выражения — тоже. Идущая нога
замены — «не готово», а не красное: иначе отмена обрывала бы ожидание на первом
заходе нового набора. Сводка называет каждую такую отмену и ногу, её заменившую.

ПРЕДПОСЫЛКА КЛЮЧА: ИМЯ ПРОВЕРКИ ОДНОЗНАЧНО
-----------------------------------------
Проверка — это имя в пределах приложения (`app`, `name`). В ответе check-runs
нет ни процесса, ни события, ни запроса, из которых пришёл прогон, поэтому
одноимённая джоба ДРУГОГО процесса была бы принята за замену: её успех
отставил бы чужой отказ. Ключ держится предпосылкой, и стережёт её
`premise()` над разобранными процессами, которые запускает запрос на слияние:

* имя каждой джобы с раскрытой литеральной матрицей встречается один раз —
  внутри процесса и между процессами; имя из выражения — образец, и литерал,
  которому он отвечает, считается совпадением;
* имя матричной джобы называет каждое измерение матрицы, а имя матрицы из
  выражения — хотя бы одно; различимость ног такой матрицы держит её план;
* вызов переиспользуемого процесса не вычисляется и однозначным не считается;
* ноль осмотренных джоб — нарушение, а не «нарушений нет».

Имена сравниваются в ПОКАЗЕ GitHub, потому что им ключуется `decide`: имя
длиннее 100 байт GitHub показывает первыми 97 байтами и многоточием `...`
(`ci.yaml`, `pg-outside-selection`: 109 байт в YAML, 100 в прогоне на
b7608fe9750; 98-байтное имя `terraform` на 1ad671ef764 показано целиком).
Два имени, равные в первых 97 байтах, — одна проверка. Форма показа та же, что у
объявления, — `provider_cut` держателя. Разрез посреди многобайтного символа не
измерен: держатель отбрасывает разрезанный символ, и имена, равные до его
границы, считаются совпадением. Имя из выражения бывает любой длины, поэтому
образец сравнивается и обрезанным: для совпадения довольно совместимого начала.

Предикат — `test_the_tree_holds_the_check_name_premise` в `pr_verdict_test.py`;
на 6e39921cfd2 (сведение #2910 с #2891) осмотрено джоб 29, имён 50, нарушений 0,
как и на b7608fe9750. Свод на запуске судит ту же предпосылку над процессами
запроса: нарушение — вердикт не вынесен (код 2).

Граница предпосылки — чего этот обход НЕ стережёт (измерено 2026-09-27):

* другое событие того же процесса на той же sha. Из восьми процессов запроса
  `push` объявляют пять, и у всех он сужен до `main`; `workflow_dispatch`
  объявляют шесть, и ручной запуск на ветке запроса заводит более поздний
  набор тех же имён — он судится как замена;
* другой запрос с той же головой. У `review-text` наборы отдельны на каждый
  запрос, и набор второго запроса судится как замена набора первого.

Вход — JSON от `repos/{repo}/commits/{sha}/check-runs` (или список его элементов)
на stdin, каталог процессов (`--workflows`) и база запроса (`--base`). Имя
собственной джобы (`--self-name`) исключается: она завершается последней by
construction, и без исключения вердикт ждал бы сам себя вечно.

Сводка печатает перепись объявления (процессов прочитано, идут на запросе),
число объявленных, появившихся и осмотренных, поимённо — не появившиеся и
идущие, а на вынесенном вердикте — каждую судимую проверку с её исходом.

Коды: 0 — зелено; 1 — красно; 3 — не готово (спросить снова); 2 — вердикт не
вынесен: вход не разобран либо объявление не вычислено.
"""

from __future__ import annotations

import argparse
import importlib.util
import itertools
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from types import ModuleType

# Без модуля разбора и без держателя формы имени объявления нет. Это «вердикт не
# вынесен» (код 2), а не трасса с кодом 1: ожидание прочло бы 1 как «красно».
try:
    import yaml
except ImportError as _exc:  # pragma: no cover - в задании модуль ставится шагом
    print(f"ОТКАЗ: нет PyYAML, процессы дерева разобрать нечем ({_exc}) — вердикта НЕТ",
          file=sys.stderr)
    sys.exit(2)


def _sibling(file: str, module: str) -> ModuleType:
    """Модуль соседнего скрипта: имя файла с дефисом обычным импортом не берётся."""
    spec = importlib.util.spec_from_file_location(module, Path(__file__).with_name(file))
    if spec is None or spec.loader is None:
        raise ImportError(f"не загружен соседний скрипт {file}")
    mod = importlib.util.module_from_spec(spec)
    sys.modules[module] = mod
    spec.loader.exec_module(mod)
    return mod


# Показ имени джобы площадкой и виды события по умолчанию — у держателя этих
# форм, гейта триггеров запроса. Копия здесь разошлась бы с ним молча.
try:
    _trigger_scope = _sibling("assert-review-trigger-scope.py", "review_trigger_scope")
except (ImportError, OSError, SyntaxError) as _exc:
    print(f"ОТКАЗ: держатель формы имени не загружен ({_exc}) — вердикта НЕТ", file=sys.stderr)
    sys.exit(2)
provider_cut = _trigger_scope.provider_cut
PROVIDER_CUT_BYTES: int = _trigger_scope.PROVIDER_CUT_BYTES
PROVIDER_CUT_TAIL: str = _trigger_scope.PROVIDER_CUT_TAIL
# Бюджет основы в показе: сколько байт имени площадка оставляет перед многоточием.
PROVIDER_CUT_KEPT = PROVIDER_CUT_BYTES - len(PROVIDER_CUT_TAIL.encode("utf-8"))
DEFAULT_REQUEST_TYPES: frozenset[str] = frozenset(_trigger_scope.DEFAULT_REVIEW_TYPES)

# Приложение, от имени которого площадка заводит check-run джобы процесса.
ACTIONS_APP = "github-actions"

# Завершённые исходы, при которых в ствол пускать нельзя.
BLOCKING = {"failure", "cancelled", "timed_out", "action_required", "stale"}
# Завершённые исходы, которые не являются ни успехом, ни отказом.
NEUTRALISH = {"neutral", "skipped"}
# Состояния «ещё не завершилось».
PENDING = {"queued", "in_progress", "waiting", "pending", "requested"}

GREEN, RED, NOT_READY = "зелено", "красно", "не готово"


@dataclass(frozen=True)
class Verdict:
    state: str
    # Осмотрено прогонов, включая отставленные; остальные счётчики — по судимым.
    total: int
    green: int
    blocking: int
    neutralish: int
    pending: int
    # Объявлено проверок, из них появилось; не появившиеся — поимённо.
    declared: int
    present: int
    missing: tuple[str, ...]
    # Судимые прогоны: «имя — исход».
    judged: tuple[str, ...]
    reason: str
    offenders: tuple[str, ...] = ()
    # Прогоны, отставленные ради другого прогона той же проверки — последнего,
    # несущего исход: «имя — исход, check-run id, набор».
    superseded: tuple[str, ...] = ()

    @property
    def exit_code(self) -> int:
        return 0 if self.state == GREEN else 1


def _runs(payload: object) -> list[dict]:
    if isinstance(payload, dict):
        payload = payload.get("check_runs", [])
    if not isinstance(payload, list):
        raise ValueError("вход не похож на ответ check-runs: ожидался объект или список")
    return [r for r in payload if isinstance(r, dict)]


def _check_key(r: dict) -> tuple[str, str]:
    """Проверка — это имя в пределах приложения, которое о ней сообщает.

    Однозначность имени — предпосылка, а не свойство входа (см. шапку, `premise`)."""
    app = r.get("app")
    owner = (app.get("slug") or app.get("id")) if isinstance(app, dict) else None
    return (str(owner), str(r.get("name")))


def _is_int(v: object) -> bool:
    # `bool` в python — подкласс `int`, но `true` в JSON — не число и порядка не несёт.
    return isinstance(v, int) and not isinstance(v, bool)


def _order(r: dict) -> tuple[int, int] | None:
    """(набор, id): сначала порядок событий, внутри набора — порядок попыток.
    Нет целого номера набора или целого id — порядка нет."""
    suite = r.get("check_suite")
    sid = suite.get("id") if isinstance(suite, dict) else None
    rid = r.get("id")
    return (sid, rid) if _is_int(sid) and _is_int(rid) else None


def _carries_outcome(r: dict) -> bool:
    """Идёт (исход будет) либо завершился не нейтрально (исход есть)."""
    status = str(r.get("status"))
    if status in PENDING:
        return True
    return status == "completed" and str(r.get("conclusion")) in BLOCKING | {"success"}


def _outcome(r: dict) -> str:
    status = str(r.get("status"))
    return str(r.get("conclusion")) if status == "completed" else status


def _describe(r: dict) -> str:
    suite = r.get("check_suite")
    sid = suite.get("id") if isinstance(suite, dict) else None
    return f"{r.get('name')} — {_outcome(r)}, check-run {r.get('id')}, набор {sid}"


def _latest_per_check(runs: list[dict]) -> tuple[list[dict], list[dict]]:
    """Судимые прогоны и отставленные: по одному на проверку (см. шапку)."""
    groups: dict[tuple[str, str], list[dict]] = {}
    for r in runs:
        groups.setdefault(_check_key(r), []).append(r)

    judged: list[dict] = []
    superseded: list[dict] = []
    for group in groups.values():
        orders = [_order(r) for r in group]
        if len(group) == 1 or any(o is None for o in orders):
            judged.extend(group)
            continue
        ordered = [r for _, r in sorted(zip(orders, group), key=lambda pair: pair[0])]
        bearing = [r for r in ordered if _carries_outcome(r)]
        pick = (bearing or ordered)[-1]
        judged.append(pick)
        superseded.extend(r for r in ordered if r is not pick)
    return judged, superseded


def _unexpanded(name: str) -> bool:
    """Шаблон имени: есть закрытое выражение и нет незакрытого (см. шапку)."""
    return _EXPR.search(name) is not None and "${{" not in "".join(_EXPR.split(name))


def _set_aside_unexpanded(judged: list[dict], set_aside: list[dict], declared: Declared
                          ) -> tuple[list[dict], list[tuple[dict, dict | None]]]:
    """Шаблонные отмены, заменённые ногой той же джобы в более новом наборе,
    уходят из судимых в отставленные вместе с заменой (см. шапку, #3267)."""
    templates = dict(declared.templates)
    keep: list[dict] = []
    aside: list[tuple[dict, dict | None]] = [(r, None) for r in set_aside]
    for r in judged:
        name, order = str(r.get("name")), _order(r)
        legs = templates.get(name)
        replacement = None
        if (legs and order is not None and _unexpanded(name)
                and str(r.get("status")) == "completed" and str(r.get("conclusion")) == "cancelled"
                and isinstance(r.get("app"), dict) and r["app"].get("slug") == ACTIONS_APP):
            replacement = next((
                x for x in judged
                if x is not r and not _unexpanded(str(x.get("name")))
                and _order(x) is not None and _order(x)[0] > order[0]
                and _carries_outcome(x) and any(_proves(c, x) for c in legs)), None)
        if replacement is None:
            keep.append(r)
        else:
            aside.append((r, replacement))
    return keep, aside


def _judged_and_set_aside(examined: list[dict], declared: Declared
                          ) -> tuple[list[dict], list[tuple[dict, dict | None]]]:
    """Судимые прогоны и отставленные — с заменой у шаблонной отмены, `None` у прочих."""
    judged, set_aside = _latest_per_check(examined)
    return _set_aside_unexpanded(judged, set_aside, declared)


def decide(payload: object, declared: Declared, self_name: str = "",
           gated: frozenset[str] = frozenset()) -> Verdict:
    """Вердикт по набору проверок против объявленного. Чистая функция: ни сети,
    ни времени, ни файлов."""
    examined = [r for r in _runs(payload) if r.get("name") != self_name]
    runs, set_aside = _judged_and_set_aside(examined, declared)

    pending = [r for r in runs if str(r.get("status")) in PENDING]
    done = [r for r in runs if str(r.get("status")) == "completed"]

    blocking = [r for r in done if str(r.get("conclusion")) in BLOCKING]
    neutralish = [r for r in done if str(r.get("conclusion")) in NEUTRALISH]
    green = [r for r in done if str(r.get("conclusion")) == "success"]

    # Задание, которое карта выбора умеет пропускать (`gated`, kacho#3148), пропуском
    # своего появления НЕ доказывает: пропуск черновика остаётся на той же голове, и без
    # этого правила свод полного прогона, стартовав раньше заданий процессов, принял бы
    # его за «появилось» — ровно класс #2910. Появление такого задания доказывает только
    # прогон с исходом, а не `skipped`.
    def present(c: JobName) -> bool:
        need_run = _job_key(c) in gated
        return any(_proves(c, r) and not (need_run and str(r.get("conclusion")) == "skipped")
                   for r in runs)

    missing = tuple(c.text for c in declared.checks if not present(c))

    counts = dict(
        total=len(examined), green=len(green), blocking=len(blocking),
        neutralish=len(neutralish), pending=len(pending),
        declared=len(declared.checks), present=len(declared.checks) - len(missing),
        missing=missing,
        judged=tuple(sorted(f"{r.get('name')} — {_outcome(r)}" for r in runs)),
        superseded=tuple(sorted(
            _describe(r) + ("" if leg is None else
                            f"; имя не раскрыто — отменён до матрицы, замена — нога "
                            f"«{leg.get('name')}» набора {_order(leg)[0]}")
            for r, leg in set_aside)),
    )

    # Объявления нет — судить присутствие не по чему. Это «не прочитано», а не
    # «проверять нечего», и пришедшие зелёные прогоны его не заменяют.
    if declared.breaches:
        return Verdict(RED, **counts, reason="объявление проверок запроса не вычислено: "
                                             + "; ".join(declared.breaches))
    if not declared.checks:
        return Verdict(RED, **counts, reason="объявлено ноль проверок — это не «проверять "
                                             "нечего», а «объявление не прочитано»")
    # Отказ решает СРАЗУ, не дожидаясь остальных: держать ранеры ради заведомо
    # красного вердикта — расход без предмета.
    if blocking:
        return Verdict(RED, **counts, reason="есть не-зелёные проверки",
                       offenders=tuple(sorted(str(r.get("name")) for r in blocking)))
    if pending or missing:
        if not runs:
            reason = ("не появилось НИ ОДНОЙ проверки — это не «замечаний нет», "
                      "а «никто не смотрел»")
        else:
            reason = "; ".join(part for part in (
                "часть проверок ещё идёт" if pending else "",
                f"не появилось объявленных проверок {len(missing)}" if missing else "",
            ) if part)
        return Verdict(NOT_READY, **counts, reason=reason,
                       offenders=tuple(sorted(str(r.get("name")) for r in pending)))
    if not green:
        return Verdict(RED, **counts,
                       reason="ни одной зелёной проверки: все нейтральны или пропущены, "
                              "то есть предмета проверки не было")
    return Verdict(GREEN, **counts,
                   reason="каждая объявленная проверка появилась, все завершились успехом")


def render(v: Verdict) -> str:
    head = (f"вердикт: {v.state} — {v.reason}\n"
            f"объявлено проверок {v.declared} (джобы процессов, которые запрос запускает "
            f"на этой голове; матрица из выражения — одна проверка), появилось {v.present}, "
            f"не появилось {len(v.missing)}\n"
            f"осмотрено проверок {v.total}, отставлено {len(v.superseded)} "
            f"(у той же проверки судится последний по набору и id прогон, несущий исход; "
            f"отмена с нераскрытым шаблоном имени — ногой той же джобы в более новом наборе); "
            f"из судимых зелёных {v.green}, "
            f"не-зелёных {v.blocking}, нейтральных или пропущенных {v.neutralish}, "
            f"идущих {v.pending}")
    if v.offenders:
        head += "\n" + "\n".join(f"   • {n}" for n in v.offenders)
    if v.missing:
        head += "\nне появились:\n" + "\n".join(f"   ◌ {n}" for n in v.missing)
    # Поимённый список судимых — на вынесенном вердикте: заход «не готово»
    # повторяется до ста семидесяти раз, и список тонул бы в журнале.
    if v.state != NOT_READY:
        head += f"\nсудимые ({len(v.judged)}):\n" + "\n".join(f"   · {j}" for j in v.judged)
    if v.superseded:
        head += "\nотставлены:\n" + "\n".join(f"   ◦ {d}" for d in v.superseded)
    return head


# ── Предпосылка ключа (см. шапку) ────────────────────────────────────────────

PR_TRIGGERS = {"pull_request", "pull_request_target"}
_EXPR = re.compile(r"\$\{\{.*?\}\}", re.S)
_MATRIX_REF = re.compile(r"\bmatrix\.([A-Za-z_][A-Za-z0-9_-]*)")
_BARE_MATRIX_REF = re.compile(r"^\s*matrix\.([A-Za-z_][A-Za-z0-9_-]*)\s*$")


@dataclass(frozen=True)
class Premise:
    examined: int                 # джоб процессов запроса
    names: int                    # имён: литеральная матрица раскрыта, образец — одно имя
    breaches: tuple[str, ...]


@dataclass(frozen=True)
class JobName:
    where: str                    # «<процесс>: jobs.<id>»
    text: str                     # имя либо шаблон образца
    parts: tuple[str, ...] | None  # у образца — литеральные куски между выражениями


def _triggers(doc: dict) -> set[str]:
    # YAML 1.1 читает голый ключ `on` как `True`.
    on = doc.get("on", doc.get(True))
    if isinstance(on, str):
        return {on}
    if isinstance(on, (list, dict)):
        return {str(t) for t in on}
    return set()


def _scalar(v: object) -> str:
    return ("true" if v else "false") if isinstance(v, bool) else str(v)


def _job_names(where: str, job_id: str, job: object) -> tuple[list[JobName], list[str]]:
    if not isinstance(job, dict):
        return [], [f"{where}: джоба не разобрана как объект — её имя не вычислено"]
    if "uses" in job:
        return [], [f"{where}: вызов переиспользуемого процесса — имена его джоб "
                    f"(«вызывающий / вызванный») этот обход не вычисляет"]
    template = str(job.get("name", job_id))
    exprs = [m.group(0)[3:-2] for m in _EXPR.finditer(template)]
    parts = tuple(_EXPR.split(template))
    pattern = [JobName(where, template, parts)] if exprs else [JobName(where, template, None)]
    strategy = job.get("strategy")
    matrix = strategy.get("matrix") if isinstance(strategy, dict) else None
    if matrix is None:
        return pattern, []

    refs = {d for e in exprs for d in _MATRIX_REF.findall(e)}
    if not isinstance(matrix, dict):
        if refs:
            return pattern, []
        return pattern, [f"{where}: матрица из выражения, а имя «{template}» не называет "
                         f"ни одного её измерения — ноги получат одно имя"]

    dims = [str(k) for k in matrix if k not in ("include", "exclude")]
    for extra in matrix.get("include") or []:
        if isinstance(extra, dict):
            dims += [str(k) for k in extra if str(k) not in dims]
    if not dims:
        return pattern, [f"{where}: у матрицы нет ни одного измерения — ноги не вычислены"]
    missing = [d for d in dims if d not in refs]
    if missing:
        return pattern, [f"{where}: имя «{template}» не называет измерение матрицы "
                         f"{', '.join(missing)} — ноги по нему получат одно имя"]

    literal = (
        "include" not in matrix and "exclude" not in matrix
        and all(isinstance(matrix[d], list) and matrix[d]
                and all(not isinstance(v, (dict, list)) for v in matrix[d]) for d in dims)
        and all(_BARE_MATRIX_REF.match(e) for e in exprs)
        and refs <= set(dims)
    )
    if not literal:
        return pattern, []
    legs = []
    for values in itertools.product(*(matrix[d] for d in dims)):
        leg = dict(zip(dims, values))
        text = _EXPR.sub(lambda m: _scalar(leg[_BARE_MATRIX_REF.match(m.group(0)[3:-2]).group(1)]),
                         template)
        legs.append(JobName(where, text, None))
    return legs, []


def _shown(n: JobName) -> tuple[JobName, ...]:
    """Формы, в которых площадка может показать имя (см. шапку). Форму обрезки
    даёт её держатель, `provider_cut`. Литерал — одна форма. Имя из выражения
    бывает любой длины, поэтому у образца две: как есть и обрезанная — начало
    образца, что угодно и многоточие. Начало длиннее бюджета основы обрезается
    само: показ любого имени с таким началом, длиннее предела, определён одним
    началом, и держатель даёт его на начале с хвостом — оно уже длиннее предела."""
    if n.parts is None:
        return (JobName(n.where, provider_cut(n.text), None),)
    head = n.parts[0]
    if len(head.encode("utf-8")) > PROVIDER_CUT_KEPT:
        return n, JobName(n.where, provider_cut(head + PROVIDER_CUT_TAIL), None)
    return n, JobName(n.where, head + "${{ … }}" + PROVIDER_CUT_TAIL, (head, PROVIDER_CUT_TAIL))


def _fits(text: str, parts: tuple[str, ...]) -> bool:
    return re.fullmatch(".*".join(re.escape(p) for p in parts), text, re.S) is not None


def _may_equal(a: JobName, b: JobName) -> bool:
    """Литерал с литералом — равенство; литерал с образцом — соответствие;
    образцы — совместимые начало и конец. Последнее грубее истины, но не
    пропускает ни одного совпадения: у разных строк разные начало или конец."""
    if b.parts is None:
        a, b = b, a
    if a.parts is None:
        return a.text == b.text if b.parts is None else _fits(a.text, b.parts)
    head_a, head_b, tail_a, tail_b = a.parts[0], b.parts[0], a.parts[-1], b.parts[-1]
    return ((head_a.startswith(head_b) or head_b.startswith(head_a))
            and (tail_a.endswith(tail_b) or tail_b.endswith(tail_a)))


def premise(workflows: dict[str, object]) -> Premise:
    """Однозначно ли имя проверки в процессах запроса. Чистая функция над
    разобранными процессами: `{путь: документ}`."""
    names: list[JobName] = []
    breaches: list[str] = []
    examined = 0
    for path, doc in sorted(workflows.items()):
        if not isinstance(doc, dict):
            breaches.append(f"{path}: процесс не разобран как объект")
            continue
        if not PR_TRIGGERS & _triggers(doc):
            continue
        jobs = doc.get("jobs")
        if not isinstance(jobs, dict):
            breaches.append(f"{path}: у процесса запроса нет разобранных джоб")
            continue
        for job_id, job in jobs.items():
            examined += 1
            got, bad = _job_names(f"{path}: jobs.{job_id}", str(job_id), job)
            names += got
            breaches += bad
    if not examined:
        breaches.append("осмотрено ноль джоб процессов запроса — это не «нарушений нет», "
                        "а «никто не смотрел»")
    for a, b in itertools.combinations(names, 2):
        hit = next(((x, y) for x in _shown(a) for y in _shown(b) if _may_equal(x, y)), None)
        if hit is None:
            continue
        x, y = hit
        shown = ("" if (x, y) == (a, b) else
                 f" в показе GitHub «{x.text}» и «{y.text}» (имя длиннее "
                 f"{PROVIDER_CUT_BYTES} байт обрезается до {PROVIDER_CUT_KEPT} байт и "
                 f"многоточия)")
        breaches.append(f"имена «{a.text}» ({a.where}) и «{b.text}» ({b.where}) могут "
                        f"совпасть{shown} — успех одной джобы отставил бы отказ другой")
    return Premise(examined=examined, names=len(names), breaches=tuple(breaches))


# ── Объявленное: джобы, которые запрос заводит на голове (см. шапку) ─────────

REQUEST_EVENT = "pull_request"
# Ключи фильтра события запроса, которые разбор знает. Прочий ключ мог бы сузить
# запуск так, что разбор этого не увидит, — поэтому он нарушение, а не молчание.
_FILTER_KEYS = {"types", "branches", "branches-ignore", "paths", "paths-ignore"}


class NotEvaluated(Exception):
    """Разбор не вычислил, идёт ли процесс на запросе. Это не «не идёт»."""


@dataclass(frozen=True)
class Declared:
    checks: tuple[JobName, ...]   # литерал в показе площадки либо образец
    workflows: int                # процессов прочитано
    on_request: int               # из них идут на запросе в эту базу
    breaches: tuple[str, ...]
    # Шаблон имени (в показе площадки) джобы с выражением в имени → её
    # объявленные имена; шаблон двух джоб — `None`: замену не доказать (#3267).
    templates: tuple[tuple[str, tuple[JobName, ...] | None], ...] = ()


def _branch_pattern(pattern: str) -> re.Pattern[str]:
    """Образец фильтра ветки площадки как регулярное выражение по всему имени."""
    out: list[str] = []
    i = 0
    while i < len(pattern):
        c = pattern[i]
        if c == "\\" and i + 1 < len(pattern):
            out.append(re.escape(pattern[i + 1]))
            i += 2
            continue
        if pattern.startswith("**", i):
            out.append(".*")
            i += 2
            continue
        if c == "[":
            end = pattern.find("]", i + 1)
            if end < 0:
                raise NotEvaluated(f"образец ветки «{pattern}»: класс без закрывающей скобки")
            out.append(pattern[i:end + 1])
            i = end + 1
            continue
        out.append("[^/]*" if c == "*" else c if c in "?+" else re.escape(c))
        i += 1
    try:
        return re.compile("".join(out))
    except re.error as exc:
        raise NotEvaluated(f"образец ветки «{pattern}» не разобран: {exc}") from exc


def _patterns(key: str, value: object) -> list[str]:
    """Образцы фильтра строками. Скаляр не строкой (`2798` без кавычек) YAML-разбор
    здесь и у площадки читают по-разному — вычислять по нему нельзя."""
    if isinstance(value, str):
        return [value]
    if isinstance(value, list) and value and all(isinstance(v, str) for v in value):
        return value
    raise NotEvaluated(f"`{REQUEST_EVENT}.{key}` не разобран как список строк-образцов "
                       f"(образец не строкой записывается в кавычках)")


def _runs_on_request(doc: dict, base: str) -> bool:
    """Идёт ли процесс на запросе в базу `base` при видах события по умолчанию."""
    on = doc.get("on", doc.get(True))
    body = on.get(REQUEST_EVENT) if isinstance(on, dict) else None
    if body is None:
        return True
    if not isinstance(body, dict):
        raise NotEvaluated(f"фильтр `{REQUEST_EVENT}` не разобран как объект")
    unknown = sorted(set(map(str, body)) - _FILTER_KEYS)
    if unknown:
        raise NotEvaluated(f"ключ фильтра {', '.join(unknown)} этот разбор не знает, а он "
                           f"может сужать запуск")
    for key in ("paths", "paths-ignore"):
        if key in body:
            raise NotEvaluated(f"фильтр `{key}` зависит от изменения запроса, а его "
                               f"этот разбор не читает")
    if "types" in body:
        kinds = set(_patterns("types", body["types"]))
        lost = sorted(DEFAULT_REQUEST_TYPES - kinds)
        if lost:
            raise NotEvaluated(f"`types` без {', '.join(lost)}, а свод идёт на видах по "
                               f"умолчанию: на новой голове процесс может не пойти")
    if "branches" in body and "branches-ignore" in body:
        raise NotEvaluated(f"`branches` и `branches-ignore` в одном фильтре — площадка "
                           f"такого процесса не принимает")
    if "branches-ignore" in body:
        ignored = _patterns("branches-ignore", body["branches-ignore"])
        if any(p.startswith("!") for p in ignored):
            raise NotEvaluated("`!` в `branches-ignore` этот разбор не вычисляет")
        return not any(_branch_pattern(p).fullmatch(base) for p in ignored)
    if "branches" not in body:
        return True
    # Отмена `!` снимает то, что взял более ранний образец: решает последний.
    selected = False
    for p in _patterns("branches", body["branches"]):
        negated = p.startswith("!")
        if _branch_pattern(p[1:] if negated else p).fullmatch(base):
            selected = not negated
    return selected


def declare(workflows: dict[str, object], base: str, self_name: str) -> Declared:
    """Проверки, которые запрос в базу `base` обязан завести на голове. Чистая
    функция над разобранными процессами: `{путь: документ}`."""
    checks: list[JobName] = []
    breaches: list[str] = []
    templates: dict[str, tuple[JobName, ...] | None] = {}
    on_request = own = 0
    if not base:
        breaches.append("база запроса не передана — какие процессы запрос запускает, "
                        "не вычислено")
    if not self_name:
        breaches.append("имя собственной джобы не передано — свод ждал бы сам себя")
    for path, doc in sorted(workflows.items()):
        if not isinstance(doc, dict):
            breaches.append(f"{path}: процесс не разобран как объект")
            continue
        triggers = _triggers(doc)
        if "pull_request_target" in triggers:
            breaches.append(f"{path}: событие pull_request_target исполняется в контексте "
                            f"базы — на какой sha ложатся его прогоны, не вычислено")
            continue
        if REQUEST_EVENT not in triggers:
            continue
        try:
            if not _runs_on_request(doc, base):
                continue
        except NotEvaluated as exc:
            breaches.append(f"{path}: {exc} — идёт ли процесс на запросе, не вычислено")
            continue
        on_request += 1
        jobs = doc.get("jobs")
        if not isinstance(jobs, dict):
            breaches.append(f"{path}: у процесса запроса нет разобранных джоб")
            continue
        for job_id, job in jobs.items():
            got, bad = _job_names(f"{path}: jobs.{job_id}", str(job_id), job)
            breaches += bad
            mine: list[JobName] = []
            for n in got:
                if n.parts is not None:
                    mine.append(n)
                    continue
                shown = provider_cut(n.text)
                if shown == self_name:
                    own += 1
                else:
                    mine.append(JobName(n.where, shown, None))
            checks += mine
            template = str(job.get("name", job_id)) if isinstance(job, dict) else ""
            if _EXPR.search(template) and mine:
                key = provider_cut(template)
                templates[key] = None if key in templates else tuple(mine)
    if self_name and own != 1:
        breaches.append(f"собственная джоба «{self_name}» найдена среди джоб запроса {own} "
                        f"раз, а не один — исключать себя не из чего, и свод ждал бы сам себя")
    if not checks:
        breaches.append("объявлено ноль проверок — это не «проверять нечего», а «никто не "
                        "смотрел»")
    return Declared(checks=tuple(checks), workflows=len(workflows), on_request=on_request,
                    breaches=tuple(breaches), templates=tuple(sorted(templates.items())))


def _is_cut(name: str) -> bool:
    """Имя в форме обрезки площадкой: основа заполнила бюджет так, что следующий
    знак (до четырёх байт) уже не вошёл, и дописано многоточие."""
    if not name.endswith(PROVIDER_CUT_TAIL):
        return False
    stem = len(name[: -len(PROVIDER_CUT_TAIL)].encode("utf-8"))
    return PROVIDER_CUT_KEPT - 4 < stem <= PROVIDER_CUT_KEPT


def _proves(check: JobName, r: dict) -> bool:
    """Доказывает ли прогон, что объявленная проверка появилась."""
    app = r.get("app")
    name = r.get("name")
    if not (isinstance(app, dict) and app.get("slug") == ACTIONS_APP and isinstance(name, str)):
        return False
    if check.parts is None:
        return name == check.text
    if _fits(name, check.parts):
        return True
    if not _is_cut(name):
        return False
    # Обрезанное имя — начало имени ноги. Началом образца оно служит, если
    # обрезано внутри первого литерала либо после него: остальное поглощает
    # выражение, стоящее следом.
    stem, head = name[: -len(PROVIDER_CUT_TAIL)], check.parts[0]
    return head.startswith(stem) or stem.startswith(head)


def load_workflows(root: Path) -> tuple[dict[str, object], list[str]]:
    """Разобранные процессы каталога и то, что разобрать не удалось."""
    files = sorted({*root.glob("*.yml"), *root.glob("*.yaml")})
    docs: dict[str, object] = {}
    errors: list[str] = []
    if not files:
        errors.append(f"в каталоге {root} нет ни одного процесса — объявлять не из чего")
    for f in files:
        try:
            docs[f.name] = yaml.safe_load(f.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, yaml.YAMLError) as exc:
            errors.append(f"{f.name}: процесс не прочитан ({type(exc).__name__}: {exc})")
    return docs, errors


# ─────────────────────────────────────────────────────────────────────────────
# ЧЕРНОВИК: ЦЕЛЕВОЙ ПРОГОН, И ОН НЕ ДАЁТ ЗЕЛЁНОГО (kacho#3148)
#
# Решение владельца 2026-10-11: на мелкую правку — только затронутое, полный прогон —
# когда всё собирают воедино. На черновике запроса в ветку эпика процессы исполняют
# лишь задания, выбранные картой `.github/ci-paths.json` (`affected.py`), прочие — `skipped`.
#
# Свод на черновике судит ВЫБРАННЫЕ (каждая выбранная объявленная проверка обязана
# появиться и завершиться) и выносит КРАСНОЕ при любом их исходе: зелёным черновика
# вливать нельзя по построению. Защита ветки эпика требует ровно этот контекст, поэтому
# вливание возможно только после перевода в ready — событие `ready_for_review` заводит
# полный прогон того же состава, и свод по нему судит всё объявленное, как прежде.
# Строка «целевой прогон: N заданий из M» — для читателя (`ci-read.sh` воркспейса).
# ─────────────────────────────────────────────────────────────────────────────


@dataclass(frozen=True)
class Drafted:
    full: bool          # выбор вернул «всё» (тронута карта, неопознанный путь, база main)
    selected: int       # объявленных проверок в выборе
    declared: int       # объявленных проверок всего


def _job_key(n: JobName) -> str:
    """«<процесс>:<id>» из `where` объявления («ci.yaml: jobs.lint»)."""
    path, _, job = n.where.partition(": jobs.")
    return f"{path}:{job}"


def narrow_to_draft(d: Declared, selection: object) -> tuple[Declared, Drafted]:
    """Объявление, суженное до выбранного. Выбор без ключей — отказ, а не «всё»."""
    if not isinstance(selection, dict) or not isinstance(selection.get("full"), bool) \
            or not isinstance(selection.get("jobs"), list) \
            or not isinstance(selection.get("always"), list):
        raise ValueError("выбор не в форме {full, jobs, always}")
    if selection["full"]:
        return d, Drafted(True, len(d.checks), len(d.checks))
    keep = set(map(str, selection["jobs"])) | set(map(str, selection["always"]))
    checks = tuple(c for c in d.checks if _job_key(c) in keep)
    narrowed = Declared(checks=checks, workflows=d.workflows, on_request=d.on_request,
                        breaches=d.breaches, templates=d.templates)
    return narrowed, Drafted(False, len(checks), len(d.checks))


def load_gated(path: Path) -> frozenset[str]:
    """Задания, которые карта выбора умеет пропускать. Нет карты — нет и пропусков."""
    if not path.exists():
        return frozenset()
    raw = json.loads(path.read_text(encoding="utf-8"))
    return frozenset(j for g in raw.get("groups", []) for j in g.get("jobs", []))


def draft_verdict(v: Verdict, drafted: Drafted) -> Verdict:
    """«Не готово» остаётся ожиданием; любой вынесенный исход черновика — красный."""
    if v.state == NOT_READY:
        return v
    picked = "все объявленные (выбор вернул полный набор)" if drafted.full else "выбранные картой"
    inner = ("целевой зелёный — выбранные завершились успехом" if v.state == GREEN
             else f"целевой КРАСНЫЙ — {v.reason}")
    reason = (f"целевой прогон: {drafted.selected} заданий из {drafted.declared} ({picked}); "
              f"{inner}; черновик не вливается — полный прогон заводит перевод запроса в "
              f"ready")
    return Verdict(RED, total=v.total, green=v.green, blocking=v.blocking,
                   neutralish=v.neutralish, pending=v.pending, declared=v.declared,
                   present=v.present, missing=v.missing, judged=v.judged, reason=reason,
                   offenders=v.offenders, superseded=v.superseded)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--self-name", required=True,
                    help="имя собственной джобы (исключается из счёта)")
    ap.add_argument("--workflows", required=True, type=Path,
                    help="каталог процессов ревизии, которую исполнил запрос")
    ap.add_argument("--base", required=True, help="база запроса на слияние")
    ap.add_argument("--draft-selection", type=Path, default=None,
                    help="запрос — черновик: выбор затронутого (`affected.py "
                         "--draft-selection`), вердикт красный по построению (kacho#3148)")
    args = ap.parse_args()

    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"ОТКАЗ: вход не разобран как JSON ({exc}) — ни одна проверка не рассмотрена, "
              f"и это НЕ «зелено»", file=sys.stderr)
        return 2

    root = args.workflows.resolve()
    docs, errors = load_workflows(root)
    d = declare(docs, args.base, args.self_name)
    refusals = (errors + [f"предпосылка ключа: {b}" for b in premise(docs).breaches]
                + [f"объявление: {b}" for b in d.breaches])
    print(f"объявление: каталог {root}, процессов {d.workflows}, идут на запросе в "
          f"«{args.base}» {d.on_request}")
    if refusals:
        print("ОТКАЗ: объявление проверок запроса не вычислено — вердикта НЕТ, и это НЕ "
              "«зелено»:\n" + "\n".join(f"   • {r}" for r in refusals), file=sys.stderr)
        return 2

    gated = load_gated(args.workflows.resolve().parent / "ci-paths.json")
    drafted: Drafted | None = None
    if args.draft_selection is not None:
        try:
            d, drafted = narrow_to_draft(d, json.loads(args.draft_selection.read_text(
                encoding="utf-8")))
        except (OSError, json.JSONDecodeError, ValueError) as exc:
            print(f"ОТКАЗ: выбор черновика не прочитан ({exc}) — вердикта НЕТ, и это НЕ "
                  f"«зелено»", file=sys.stderr)
            return 2

    try:
        v = decide(payload, d, args.self_name, gated)
        if drafted is not None:
            v = draft_verdict(v, drafted)
    except ValueError as exc:
        print(f"ОТКАЗ: {exc} — ни одна проверка не рассмотрена, и это НЕ «зелено»",
              file=sys.stderr)
        return 2
    print(render(v))
    if drafted is not None and v.state == RED:
        # Аннотация — признак стража для читателей вердикта (`ci-read.sh` воркспейса):
        # красное черновика по построению отличимо от отказа по содержимому.
        print(f"::error title=черновик не вливается::{v.reason}")
    # «Не готово» — это не вердикт: вызывающий обязан подождать и спросить снова.
    if v.state == NOT_READY:
        return 3
    return v.exit_code


if __name__ == "__main__":
    sys.exit(main())
