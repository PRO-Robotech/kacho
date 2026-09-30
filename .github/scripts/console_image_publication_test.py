#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Решение о публикации образов консоли: исполняется шаг `gate` из `ui.yml`.

ПРЕДМЕТ (kacho#2881)
--------------------
Образы консоли публиковались только по `push`, а `push` у файла объявлен лишь
для ствола. У веток волны и эпика пути публикации не было вовсе: запрос в линию
и ручной запуск собирали девять образов и выбрасывали. У образов служб путь для
ветки есть — ручной запуск `docker-build.yml` публикует любую ветку.

ЧТО ЗДЕСЬ СУДИТСЯ
-----------------
Не текст шага, а его ИСХОД: шаг `gate` берётся из разобранного `ui.yml`,
исполняется той же оболочкой, что у провайдера (`defaults.run.shell: bash` —
`bash --noprofile --norc -eo pipefail`), на синтетическом событии, и судятся
его выводы `push`, `ns`, `tag`. Выражения `${{ … }}` подставляются так же, как
у провайдера, — текстом, до исполнения, — но только из закрытого перечня
KNOWN_EXPRESSIONS: выражение вне перечня — отказ пробы, а не молчаливая пустая
строка, иначе проба судила бы шаг на условии, которого у провайдера нет.

Положительная сторона — ствол и ручной запуск на ветке линии публикуют.
Отрицательная — запрос на слияние, событие вне перечня публикующих и прогон без
учётных данных реестра не публикуют. Отрицание без положительного контроля
зеленело бы на шаге, который не публикует ничего никогда.

ЧЕГО ПРОБА НЕ СУДИТ — СКАЗАНО ПРЯМО
-----------------------------------
  * кто вправе запустить процесс вручную: это право площадки (запись в
    репозиторий), а не свойство дерева;
  * что учётные данные реестра не доходят до прогона запроса из чужой копии —
    это тоже свойство площадки; проба судит, что шаг не публикует запрос даже
    тогда, когда учётные данные ему доступны;
  * что реестр принял образ: публикацию исполняет действие сборки, а не шаг.
"""

from __future__ import annotations

import os
import re
import subprocess
import tempfile
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "ui.yml"

SHA = "0123456789abcdef0123456789abcdef01234567"
REGISTRY_USER = "prorobotech"
# Проверка, которую применяет к тегу сам сборщик образа.
TAG_GRAMMAR = re.compile(r"^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$")

EXPR = re.compile(r"\$\{\{\s*(.*?)\s*\}\}")
KNOWN_EXPRESSIONS = (
    "secrets.DOCKERHUB_USERNAME",
    "secrets.DOCKERHUB_TOKEN",
    "github.event_name",
    "github.ref_name",
    "github.sha",
)


def _load() -> dict:
    doc = yaml.safe_load(WORKFLOW.read_text(encoding="utf-8"))
    assert isinstance(doc, dict) and isinstance(doc.get("jobs"), dict), (
        f"{WORKFLOW}: не разобрался как процесс с заданиями — судить нечего"
    )
    return doc


def _image_job(doc: dict) -> tuple[str, dict]:
    """Задание, которое публикует образы: в нём есть шаг действия сборки образа."""
    found = [
        (name, job) for name, job in doc["jobs"].items()
        if any(str(s.get("uses", "")).startswith("docker/build-push-action@")
               for s in job.get("steps", []))
    ]
    assert len(found) == 1, (
        f"{WORKFLOW}: заданий с действием сборки образа {len(found)}, ожидалось ровно 1 — "
        "предпосылка пробы не выполняется"
    )
    return found[0]


def _gate_step(job: dict) -> dict:
    steps = [s for s in job.get("steps", []) if s.get("id") == "gate"]
    assert len(steps) == 1, f"шагов с id gate: {len(steps)}, ожидался ровно 1"
    step = steps[0]
    assert isinstance(step.get("run"), str) and step["run"].strip(), "у шага gate нет тела run"
    return step


def _shell(doc: dict, step: dict) -> list[str]:
    shell = step.get("shell") or doc.get("defaults", {}).get("run", {}).get("shell")
    # Иная оболочка — иная семантика кода возврата; проба её не воспроизводит.
    assert shell == "bash", f"оболочка шага gate «{shell}», проба воспроизводит только bash"
    return ["bash", "--noprofile", "--norc", "-eo", "pipefail"]


def _render(text: str, values: dict[str, str]) -> str:
    def sub(m: re.Match[str]) -> str:
        expr = m.group(1)
        assert expr in KNOWN_EXPRESSIONS, (
            f"выражение «{expr}» вне закрытого перечня пробы — условие, которого "
            "проба не умеет создать; расширь KNOWN_EXPRESSIONS доводом"
        )
        return values[expr]
    return EXPR.sub(sub, text)


def run_gate(event: str, ref_name: str, *, credentials: bool = True) -> dict[str, str]:
    """Исполняет шаг gate на синтетическом событии и возвращает его выводы."""
    doc = _load()
    _, job = _image_job(doc)
    step = _gate_step(job)
    values = {
        "secrets.DOCKERHUB_USERNAME": REGISTRY_USER if credentials else "",
        "secrets.DOCKERHUB_TOKEN": "token" if credentials else "",
        "github.event_name": event,
        "github.ref_name": ref_name,
        "github.sha": SHA,
    }
    with tempfile.TemporaryDirectory() as tmp:
        out = Path(tmp) / "output"
        out.write_text("", encoding="utf-8")
        script = Path(tmp) / "step.sh"
        script.write_text(_render(step["run"], values), encoding="utf-8")
        env = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": tmp,
            "GITHUB_OUTPUT": str(out),
        }
        for key, val in (step.get("env") or {}).items():
            env[str(key)] = _render(str(val), values)
        proc = subprocess.run(
            [*_shell(doc, step), str(script)],
            cwd=tmp, env=env, capture_output=True, text=True, timeout=60,
        )
        assert proc.returncode == 0, (
            f"шаг gate ({event}, ветка «{ref_name}») вышел кодом {proc.returncode}:\n"
            f"{proc.stdout}{proc.stderr}"
        )
        outputs: dict[str, str] = {}
        for line in out.read_text(encoding="utf-8").splitlines():
            key, sep, val = line.partition("=")
            assert sep, f"строка вывода без «=»: «{line}»"
            outputs[key] = val
    for key in ("push", "ns", "tag"):
        assert key in outputs, f"шаг gate ({event}) не выдал вывод {key}: {outputs}"
    assert TAG_GRAMMAR.match(outputs["tag"]), f"негодный тег «{outputs['tag']}»"
    return outputs


def test_trunk_push_publishes() -> None:
    """Положительный контроль: без него отрицания ниже зеленели бы на немом шаге."""
    o = run_gate("push", "main")
    assert (o["push"], o["ns"], o["tag"]) == ("true", REGISTRY_USER, f"main-{SHA}"), o


def test_manual_run_on_a_line_branch_publishes() -> None:
    """Ветка эпика или волны получает образ ручным запуском — как образы служб."""
    o = run_gate("workflow_dispatch", "2564")
    assert (o["push"], o["ns"], o["tag"]) == ("true", REGISTRY_USER, f"2564-{SHA}"), o


def test_pull_request_does_not_publish_even_with_credentials() -> None:
    """Близнец: то же дерево, учётные данные доступны, событие — запрос."""
    o = run_gate("pull_request", "2903/merge")
    assert (o["push"], o["ns"]) == ("false", "local"), o


def test_event_outside_the_publishing_set_does_not_publish() -> None:
    """Перечень публикующих событий закрыт: новое событие не публикует, пока не названо."""
    for event in ("schedule", "workflow_run", "pull_request_target"):
        o = run_gate(event, "main")
        assert (o["push"], o["ns"]) == ("false", "local"), (event, o)


def test_without_registry_credentials_nothing_publishes() -> None:
    o = run_gate("workflow_dispatch", "2564", credentials=False)
    assert (o["push"], o["ns"]) == ("false", "local"), o


def test_any_legal_branch_name_yields_a_valid_tag() -> None:
    """Имя ветки — данные: законное имя с кавычкой даёт годный тег, а не отказ шага."""
    name = "wip/o'brien"
    legal = subprocess.run(["git", "check-ref-format", "--branch", name],
                           capture_output=True, text=True)
    assert legal.returncode == 0, f"предпосылка: «{name}» — законное имя ветки git"
    o = run_gate("workflow_dispatch", name)
    assert o["tag"] == f"wip-o-brien-{SHA}", o


def test_publication_is_keyed_on_the_gate() -> None:
    """Вход в реестр и публикация зависят от вывода шага gate, а не от своего условия."""
    doc = _load()
    _, job = _image_job(doc)
    steps = job["steps"]
    login = [s for s in steps if str(s.get("uses", "")).startswith("docker/login-action@")]
    build = [s for s in steps if str(s.get("uses", "")).startswith("docker/build-push-action@")]
    assert len(login) == 1 and len(build) == 1, (len(login), len(build))
    assert str(login[0].get("if", "")).strip() == "steps.gate.outputs.push == 'true'", login[0]
    w = build[0].get("with") or {}
    assert str(w.get("push", "")).strip() == "${{ steps.gate.outputs.push == 'true' }}", w
    assert "steps.gate.outputs.tag" in str(w.get("tags", "")), w
    assert "steps.gate.outputs.ns" in str(w.get("tags", "")), w
