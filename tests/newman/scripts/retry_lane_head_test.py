# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Проба стража незахваченной переменной в голове полосы ожидания (kacho#3324).

ПРЕДМЕТ. `retry_until_authorized(..., lane_head=True)` вписывает в обёрнутый шаг
голову, которая отказывает, если адрес шага собран из НЕЗАХВАЧЕННОЙ переменной:
пустой сегмент пути либо уцелевшая подстановка `{{…}}`. Пустой сегмент бывает и
законным — хвостовая косая самого адреса шага (`/v2/<repo>/blobs/uploads/`,
открытие загрузки Registry v2): с kacho#3257 генератор отправляет её как есть,
последним пустым сегментом. Страж, судивший ЛЮБОЙ пустой сегмент, отказал шести
шагам `push-*-open` сборки #3309 при ответе 202.

ИСХОДЫ, КОТОРЫЕ ПРОБА УТВЕРЖДАЕТ — обе стороны:
  * хвостовая косая АДРЕСА ШАГА — не отказ, и окно видимости прав на таком
    адресе по-прежнему пережидается (403 → повтор); так же — у шага, чей
    предварительный скрипт лишь подставляет СВОЙ шаблон на базовый адрес;
  * шаг, чей предварительный скрипт ПЕРЕПИСЫВАЕТ адрес на захваченное значение
    (`push-*-put` — заливка по `Location`), со строгим вариантом: незахваченный
    `Location` оставляет шаблон с косой, и это отказ, а не 81 холостой повтор
    (приёмка #3324, check-verifier);
  * пустой ВНУТРЕННИЙ сегмент у адреса с хвостовой косой — отказ, в том числе
    ПРЕДПОСЛЕДНИЙ (мутант «пустой законен на двух последних местах»);
  * пустой ПОСЛЕДНИЙ сегмент у адреса БЕЗ хвостовой косой (переменная в конце
    пути пуста) — отказ: это ровно та форма, ради которой страж заведён;
  * неразрешённая подстановка (newman отдаёт её процентно-кодированной) — отказ;
  * разрешённый адрес с 403 — повтор, без отказа.

ГОЛОВА ИСПОЛНЯЕТСЯ, А НЕ ЧИТАЕТСЯ. Скрипт шага строит НАСТОЯЩИЙ производитель
(`gen_shared.retry_until_authorized`), а исполняет node на подставном `pm` — так
же, как исполнит его newman. Сверка по тексту доказала бы свойство текста.

МУТАНТЫ — инъекцией в производитель, а не переписанной копией: снятый страж,
хвостовая косая, не признаваемая никогда (дефект #3324), и хвостовая косая,
признаваемая всегда (перешироченная починка). Каждый обязан краснеть судьёй пробы.

КТО ИСПОЛНЯЕТ: `.github/scripts/run-python-probes.py` — образец
`tests/newman/scripts/*_test.py`; node в задании ставит `actions/setup-node`.
"""
from __future__ import annotations

import importlib.util
import json
import shutil
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import List

HERE = Path(__file__).resolve().parent
NEWMAN = HERE.parent
REPO = NEWMAN.parent.parent
GEN_SHARED = NEWMAN / "kacholib" / "gen_shared.py"

GUARD_TEST = "step addresses a captured variable"


def _load_gen_shared():
    spec = importlib.util.spec_from_file_location("gen_shared_lane_head_probe", GEN_SHARED)
    mod = importlib.util.module_from_spec(spec)
    # Модуль объявляет датаклассы: разбор их аннотаций ищет модуль в sys.modules.
    sys.modules[spec.name] = mod
    spec.loader.exec_module(mod)
    return mod


@dataclass
class _Step:
    name: str
    method: str
    path: str
    test_script: List[str] = field(default_factory=list)
    pre_script: List[str] = field(default_factory=list)


# Собственное утверждение шага: 202 — исход открытия загрузки. Предмет пробы —
# голова, поэтому собственный вердикт шага отделён именем.
_OWN = [
    "pm.test('own outcome', function () {",
    "  if (pm.response.code !== 202) { throw new Error('own: ' + pm.response.code); }",
    "});",
]

# Подставной `pm`: ровно те вызовы, что делает голова и шаг. Незнакомый вызов
# бросает, и случай вернётся с `threw` — тихо зачтённым он не станет.
_HARNESS = r"""
const input = JSON.parse(require('fs').readFileSync(0, 'utf8'));
const out = [];
for (const c of input.cases) {
  const env = new Map();
  const r = { retried: false, failed: [], passed: [], threw: null };
  const pm = {
    info: { requestName: input.name },
    request: { url: { path: c.segs.map((v) => ({ value: v })) } },
    response: { code: c.code },
    environment: {
      get: (k) => env.get(k), set: (k, v) => env.set(k, String(v)), unset: (k) => env.delete(k),
    },
    execution: { setNextRequest: (n) => { if (n === input.name) { r.retried = true; } } },
    test: (n, fn) => { try { fn(); r.passed.push(n); } catch (e) { r.failed.push(n); } },
    expect: Object.assign(() => { throw new Error('pm.expect(...) is not in the harness'); },
      { fail: (m) => { throw new Error(m); } }),
  };
  try {
    new Function('pm', 'console', input.script)(pm, { log() {}, warn() {}, error() {} });
  } catch (e) { r.threw = String(e && e.message || e); }
  out.push(r);
}
process.stdout.write(JSON.stringify(out));
"""

TRAILING = "/v2/{{_rpRepoPath}}/blobs/uploads/"
STRICT = "/registry/v1/registries/{{registryId}}"


def _dp(path: str) -> List[str]:
    """Страж адреса плоскости данных — та же форма, что у набора registry:
    подставляет СВОЙ шаблон на базовый адрес."""
    return [
        "const __cfgUrl = pm.environment.get('registryDataPlaneBaseUrl') || '';",
        "if (__cfgUrl) {",
        "  pm.request.url = __cfgUrl + pm.variables.replaceIn(" + json.dumps(path).replace('"', "'") + ");",
        "}",
    ]


# Заливка блоба: адрес ПЕРЕПИСАН на захваченный `Location` — слепок `push-*-put`.
_PUT_REWRITE = _dp(TRAILING) + [
    "const _l = pm.environment.get('_rpUploadLoc') || '';",
    "const _b = pm.environment.get('registryDataPlaneBaseUrl') || '';",
    "if (_b && _l) {",
    "  pm.request.url = _b + _l + (_l.indexOf('?') >= 0 ? '&' : '?') + 'digest=x';",
    "}",
]

# Шаги: имя → (шаблон адреса, предварительный скрипт).
STEPS = {
    "trailing": (TRAILING, []),
    "trailing-dp": (TRAILING, _dp(TRAILING)),
    "put-rewrite": (TRAILING, _PUT_REWRITE),
    "strict": (STRICT, []),
    # Косая — в значении ЗАПРОСА, а не в конце пути: адрес хвостовой косой не несёт.
    "query-slash": ("/a/{{id}}?next=x/", []),
}

# (метка, шаг, сегменты в момент исполнения, код, ждём отказ стража, ждём повтор)
CASES = [
    ("хвостовая косая адреса, 202", "trailing",
     ["v2", "reg-1", "app", "blobs", "uploads", ""], 202, False, False),
    ("хвостовая косая адреса, 403 — окно видимости", "trailing",
     ["v2", "reg-1", "app", "blobs", "uploads", ""], 403, False, True),
    ("открытие загрузки за стражем адреса, 202", "trailing-dp",
     ["v2", "reg-1", "app", "blobs", "uploads", ""], 202, False, False),
    ("хвостовая косая адреса, пустой внутренний сегмент", "trailing",
     ["v2", "", "blobs", "uploads", ""], 403, True, False),
    ("хвостовая косая адреса, пустой предпоследний сегмент", "trailing",
     ["v2", "reg-1", "app", "blobs", "", ""], 403, True, False),
    ("заливка по Location, Location не захвачен, 404", "put-rewrite",
     ["v2", "reg-1", "app", "blobs", "uploads", ""], 404, True, False),
    ("заливка по Location, Location захвачен, 201", "put-rewrite",
     ["v2", "reg-1", "app", "blobs", "uploads", "u-123"], 201, False, False),
    ("косая в запросе, пустая переменная перед запросом", "query-slash",
     ["a", "?next=x", ""], 403, True, False),
    ("адрес без хвостовой косой, пустая последняя переменная", "strict",
     ["registry", "v1", "registries", ""], 403, True, False),
    ("неразрешённая подстановка", "strict",
     ["registry", "v1", "registries", "%7B%7BregistryId%7D%7D"], 403, True, False),
    ("разрешённый адрес, 403 — окно видимости", "strict",
     ["registry", "v1", "registries", "regaaaaaaaaaaaaaaaaa"], 403, False, True),
]


def _node() -> str:
    node = shutil.which("node")
    # Без node проба не исполнена — это отказ, а не пропуск: молчание здесь
    # читалось бы зелёным.
    assert node, "node не найден в PATH — голова полосы ожидания не исполнена"
    return node


def _script(gs, step_key: str, mutate=None) -> List[str]:
    path, pre = STEPS[step_key]
    step = _Step(name="push-probe-open", method="POST", path=path, test_script=list(_OWN),
                 pre_script=list(pre))
    wrapped = gs.retry_until_authorized(step, budget=3, interval_ms=1, lane_head=True)
    lines = list(wrapped.test_script)
    return mutate(lines) if mutate else lines


def judge(gs, mutate=None) -> List[str]:
    """Находки по всем случаям; пустой список — голова держит обе стороны."""
    problems: List[str] = []
    by_step = {}
    for label, key, segs, code, want_guard, want_retry in CASES:
        by_step.setdefault(key, []).append((label, segs, code, want_guard, want_retry))
    for key, cases in by_step.items():
        lines = _script(gs, key, mutate)
        name = "push-probe-open-rya"
        payload = {"name": name, "script": "\n".join(lines),
                   "cases": [{"segs": s, "code": c} for _, s, c, _, _ in cases]}
        proc = subprocess.run([_node(), "-e", _HARNESS], input=json.dumps(payload),
                              capture_output=True, text=True, timeout=60)
        assert proc.returncode == 0, f"исполнитель головы упал: {proc.stderr}"
        results = json.loads(proc.stdout)
        assert len(results) == len(cases), "исполнено не столько случаев, сколько подано"
        for (label, _, _, want_guard, want_retry), r in zip(cases, results):
            if r["threw"]:
                problems.append(f"{label}: скрипт шага бросил — {r['threw']}")
                continue
            guarded = GUARD_TEST in r["failed"]
            if guarded != want_guard:
                problems.append(f"{label}: отказ стража {guarded}, ждали {want_guard}")
            if r["retried"] != want_retry:
                problems.append(f"{label}: повтор {r['retried']}, ждали {want_retry}")
    return problems


def test_lane_head_holds_both_sides():
    gs = _load_gen_shared()
    problems = judge(gs)
    assert problems == [], "\n".join(problems)


def test_mutant_guard_removed_is_red():
    gs = _load_gen_shared()
    site = "const _rblank = _rpath.some("

    def drop_guard(lines):
        hit = [i for i, l in enumerate(lines) if site in l]
        assert len(hit) == 1, f"место мутации «{site}» найдено {len(hit)} раз"
        lines = list(lines)
        lines[hit[0]] = lines[hit[0]].replace(site, "const _rblank = false && _rpath.some(")
        return lines

    problems = judge(gs, drop_guard)
    assert any("пустая последняя переменная" in p for p in problems), problems
    assert any("неразрешённая подстановка" in p for p in problems), problems


def test_mutant_trailing_slash_never_lawful_is_red(monkeypatch):
    # Дефект #3324: хвостовая косая адреса шага судится незахваченной переменной.
    gs = _load_gen_shared()
    monkeypatch.setattr(gs, "_ends_with_literal_slash", lambda path: False)
    problems = judge(gs)
    assert any(p.startswith("хвостовая косая адреса, 202") for p in problems), problems


def test_mutant_lenient_for_rewritten_address_is_red(monkeypatch):
    # Приёмка #3324: мягкий вариант у шага, переписывающего адрес (`push-*-put`).
    gs = _load_gen_shared()
    monkeypatch.setattr(gs, "_address_is_step_template", lambda path, pre: True)
    problems = judge(gs)
    assert any(p.startswith("заливка по Location, Location не захвачен") for p in problems), problems


def test_mutant_own_template_assignment_counted_as_rewrite_is_red(monkeypatch):
    # Законный близнец: подстановка СВОЕГО шаблона на базовый адрес — не перезапись.
    gs = _load_gen_shared()
    monkeypatch.setattr(gs, "_address_is_step_template", lambda path, pre: not pre)
    problems = judge(gs)
    assert any(p.startswith("открытие загрузки за стражем адреса") for p in problems), problems


def test_mutant_empty_lawful_at_two_last_places_is_red():
    # Мутант X5 приёмки: пустой сегмент законен на ДВУХ последних местах.
    gs = _load_gen_shared()
    site = gs._RETRY_LANE_LENIENT_EMPTY

    def widen(lines):
        assert lines.count(site) <= 1
        return [l.replace("_ri !== _rpath.length - 1", "_ri < _rpath.length - 2") if l == site else l
                for l in lines]

    problems = judge(gs, widen)
    assert any(p.startswith("хвостовая косая адреса, пустой предпоследний") for p in problems), problems


# Формы предварительного скрипта: запись адреса (кроме подстановки своего шаблона)
# лишает шаг мягкого варианта; чтение — нет.
_PATH = TRAILING
_FORMS = [
    ("пустой скрипт", [], True),
    ("подстановка своего шаблона", _dp(_PATH), True),
    ("чтение адреса", ["try { _u = pm.request.url.toString(); } catch (e) {}"], True),
    ("подстановка ЧУЖОГО шаблона", _dp("/v2/{{other}}/blobs/uploads/"), False),
    ("присваивание строкой", ["pm.request.url = _b + _l;"], False),
    ("присваивание полю", ["pm.request.url.path = ['v2', _l];"], False),
    ("метод записи", ["pm.request.url.update(_b + _l);"], False),
    ("метод пути", ["pm.request.url.query.add({key: 'digest', value: _d});"], False),
    ("псевдоним", ["const _u = pm.request.url;", "_u.path = [];"], False),
    ("обновление запроса с адресом", ["pm.request.update({url: _b + _l});"], False),
    ("подмена запроса", ["pm.request = new sdk.Request(_b + _l);"], False),
]


def test_address_rewrite_forms_both_sides():
    gs = _load_gen_shared()
    wrong = [f"{label}: {gs._address_is_step_template(_PATH, pre)}, ждали {want}"
             for label, pre, want in _FORMS if gs._address_is_step_template(_PATH, pre) != want]
    assert wrong == [], "\n".join(wrong)


def test_mutant_trailing_slash_always_lawful_is_red(monkeypatch):
    # Перешироченная починка: пустой последний сегмент признан законным всегда —
    # и пустая переменная в конце пути снова уходит в повтор.
    gs = _load_gen_shared()
    monkeypatch.setattr(gs, "_ends_with_literal_slash", lambda path: True)
    problems = judge(gs)
    assert any("пустая последняя переменная" in p for p in problems), problems


def test_generated_collections_carry_the_head_of_their_own_address():
    """Коллекции порождены нынешним производителем: у каждого обёрнутого шага с
    головой ИСПОЛНЯЕМАЯ строка стража (не метка-комментарий) — та, что следует
    из адреса шага и его предварительного скрипта. Перепись печатается — «ноль
    мягких шагов» отличим от «ноль прочитанных шагов»."""
    gs = _load_gen_shared()
    lenient_line, strict_line = gs._RETRY_LANE_LENIENT_EMPTY, gs._RETRY_LANE_STRICT_EMPTY
    seen = trailing = 0
    problems: List[str] = []
    for coll in sorted(REPO.glob("services/*/tests/newman/collections/*.postman_collection.json")):
        def walk(items):
            nonlocal seen, trailing
            for it in items:
                if "item" in it:
                    walk(it["item"])
                    continue
                for ev in it.get("event", []) or []:
                    exec_ = (ev.get("script") or {}).get("exec") or []
                    if ev.get("listen") != "test" or not any("_rblank" in l for l in exec_):
                        continue
                    seen += 1
                    url = (it.get("request") or {}).get("url") or {}
                    host = (url.get("host") or [""])[0]
                    path = (url.get("raw") or "")[len(host):]
                    pre = next(((e.get("script") or {}).get("exec") or []
                                for e in it.get("event", []) or [] if e.get("listen") == "prerequest"), [])
                    want = gs._lenient_trailing_slash(path, pre)
                    trailing += want
                    got_lenient, got_strict = lenient_line in exec_, strict_line in exec_
                    if (got_lenient, got_strict) != (want, not want):
                        problems.append(f"{coll.relative_to(REPO)}: {it['name']} "
                                        f"(ждали {'мягкий' if want else 'строгий'})")
        walk(json.loads(coll.read_text(encoding="utf-8")).get("item", []))
    print(f"шагов с головой полосы ожидания: {seen}, из них с мягким вариантом: {trailing}")
    assert seen > 0, "ни одного шага с головой полосы ожидания — предпосылка пробы исчезла"
    assert problems == [], "голова не соответствует адресу шага (коллекции не перегенерированы?):\n" \
        + "\n".join(problems[:20])
