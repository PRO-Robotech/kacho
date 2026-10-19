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
    адресе по-прежнему пережидается (403 → повтор);
  * пустой ВНУТРЕННИЙ сегмент у адреса с хвостовой косой — отказ;
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

# (метка, шаблон адреса шага, сегменты в момент исполнения, код, ждём отказ стража, ждём повтор)
CASES = [
    ("хвостовая косая адреса, 202", TRAILING,
     ["v2", "reg-1", "app", "blobs", "uploads", ""], 202, False, False),
    ("хвостовая косая адреса, 403 — окно видимости", TRAILING,
     ["v2", "reg-1", "app", "blobs", "uploads", ""], 403, False, True),
    ("хвостовая косая адреса, пустой внутренний сегмент", TRAILING,
     ["v2", "", "blobs", "uploads", ""], 403, True, False),
    ("адрес без хвостовой косой, пустая последняя переменная", STRICT,
     ["registry", "v1", "registries", ""], 403, True, False),
    ("неразрешённая подстановка", STRICT,
     ["registry", "v1", "registries", "%7B%7BregistryId%7D%7D"], 403, True, False),
    ("разрешённый адрес, 403 — окно видимости", STRICT,
     ["registry", "v1", "registries", "regaaaaaaaaaaaaaaaaa"], 403, False, True),
]


def _node() -> str:
    node = shutil.which("node")
    # Без node проба не исполнена — это отказ, а не пропуск: молчание здесь
    # читалось бы зелёным.
    assert node, "node не найден в PATH — голова полосы ожидания не исполнена"
    return node


def _script(gs, path: str, mutate=None) -> List[str]:
    step = _Step(name="push-probe-open", method="POST", path=path, test_script=list(_OWN))
    wrapped = gs.retry_until_authorized(step, budget=3, interval_ms=1, lane_head=True)
    lines = list(wrapped.test_script)
    return mutate(lines) if mutate else lines


def judge(gs, mutate=None) -> List[str]:
    """Находки по всем случаям; пустой список — голова держит обе стороны."""
    problems: List[str] = []
    by_path = {}
    for label, path, segs, code, want_guard, want_retry in CASES:
        by_path.setdefault(path, []).append((label, segs, code, want_guard, want_retry))
    for path, cases in by_path.items():
        lines = _script(gs, path, mutate)
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


def test_mutant_trailing_slash_always_lawful_is_red(monkeypatch):
    # Перешироченная починка: пустой последний сегмент признан законным всегда —
    # и пустая переменная в конце пути снова уходит в повтор.
    gs = _load_gen_shared()
    monkeypatch.setattr(gs, "_ends_with_literal_slash", lambda path: True)
    problems = judge(gs)
    assert any("пустая последняя переменная" in p for p in problems), problems


def test_generated_collections_carry_the_head_of_their_own_address():
    """Коллекции порождены нынешним производителем: у каждого обёрнутого шага с
    головой вариант стража совпадает с хвостом его адреса. Перепись печатается —
    «ноль шагов с хвостовой косой» отличим от «ноль прочитанных шагов»."""
    gs = _load_gen_shared()
    lawful_line = gs._RETRY_LANE_TRAILING_MARK
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
                    segs = ((it.get("request") or {}).get("url") or {}).get("path") or []
                    tail = bool(segs) and segs[-1] == ""
                    trailing += tail
                    if tail != (lawful_line in exec_):
                        problems.append(f"{coll.relative_to(REPO)}: {it['name']}")
        walk(json.loads(coll.read_text(encoding="utf-8")).get("item", []))
    print(f"шагов с головой полосы ожидания: {seen}, из них с хвостовой косой адреса: {trailing}")
    assert seen > 0, "ни одного шага с головой полосы ожидания — предпосылка пробы исчезла"
    assert problems == [], "голова не соответствует адресу шага (коллекции не перегенерированы?):\n" \
        + "\n".join(problems[:20])
