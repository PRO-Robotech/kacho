#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""profile_launches — РЕНДЕРИТ ли профиль стенда рабочую нагрузку, запускающую процесс.

ПРЕДМЕТ (kacho#2915, решение Д91)
--------------------------------
Знаменатель живых гейтов — носители, ОТРЕНДЕРЕННЫЕ профилем стенда. notify,
которого профиль не рендерит (до полосы D2 таблица источников пуста, и чарт не
даёт ни одного объекта), в знаменатель не входит — но гейт обязан ПРОВЕРИТЬ сам
факт «не отрендерен», а не вывести его из отсутствия пода: при отрендеренном
notify отсутствие пода — красный. Этот модуль отвечает на вопрос рендером, а не
чтением таблицы или текста шаблона.

Рендер — `helm template` зонтика цепочкой профиля из `deploy/stacks.txt`
(единственный читатель — `deploy/tests/helm/stacks.sh --args`). Запуск —
по разобранному документу, а не по подстроке: контейнер (в том числе init)
рабочей нагрузки запускает процесс, когда базовое имя первого элемента `command`
либо `args`, или последний сегмент репозитория образа (без тега и дайджеста)
равны имени процесса. Имя процесса в SAN, адресе или имени Service запуском не
является.

ВЫЗОВ
-----
  profile_launches.py --stack <имя> --process <процесс> [--manifest <файл>]

`--manifest` — уже отрендеренный текст вместо рендера (самопроверки гейтов-
потребителей). Печать — одна строка итога и по строке на нагрузку:

  профиль <имя>: документов D, рабочих нагрузок W, запускают <процесс>: N
  НАГРУЗКА <Kind>/<имя> <ключ=значение,…>   ← метки селектора подов

Исходы: 0 — ответ получен (N может быть 0); 2 — не выполнилось (имя профиля не
задано, цепочка не прочитана, helm отказал или отсутствует, рендер не разобран,
документов ноль). Ноль документов — не «не рендерит»: рендер, не давший ничего,
ничего и не осмотрел.
"""
from __future__ import annotations

import argparse
import pathlib
import shutil
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
DEPLOY = HERE.parent
UMBRELLA = DEPLOY / "helm" / "umbrella"
STACKS_SH = DEPLOY / "tests" / "helm" / "stacks.sh"
RELEASE = "kacho-umbrella"

WORKLOAD_KINDS = {"Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob", "Pod"}


class NotRun(Exception):
    """Ответа нет — третья категория, а не «не рендерит»."""


def _image_name(image: str) -> str:
    v = str(image).split("@", 1)[0]
    last = v.rsplit("/", 1)[-1]
    return last.split(":", 1)[0]


def _first(seq) -> str:
    if isinstance(seq, list) and seq:
        return str(seq[0])
    return ""


def container_launches(c: dict, proc: str) -> bool:
    for argv0 in (_first(c.get("command")), _first(c.get("args"))):
        if argv0 and argv0.rsplit("/", 1)[-1] == proc:
            return True
    return bool(c.get("image")) and _image_name(c["image"]) == proc


def _pod_spec(doc: dict) -> dict:
    kind = doc.get("kind")
    spec = doc.get("spec") or {}
    if kind == "Pod":
        return spec
    if kind == "CronJob":
        spec = ((spec.get("jobTemplate") or {}).get("spec") or {})
    return ((spec.get("template") or {}).get("spec") or {})


def _selector(doc: dict) -> dict:
    if doc.get("kind") == "Pod":
        return dict((doc.get("metadata") or {}).get("labels") or {})
    spec = doc.get("spec") or {}
    if doc.get("kind") == "CronJob":
        spec = ((spec.get("jobTemplate") or {}).get("spec") or {})
    sel = (spec.get("selector") or {}).get("matchLabels")
    if sel:
        return dict(sel)
    return dict(((spec.get("template") or {}).get("metadata") or {}).get("labels") or {})


def launches(manifest: str, proc: str) -> tuple[int, int, list[tuple[str, str, dict]]]:
    """→ (документов, рабочих нагрузок, [(kind, имя, селектор)] запускающих `proc`)."""
    import yaml
    try:
        docs = [d for d in yaml.safe_load_all(manifest) if isinstance(d, dict) and d.get("kind")]
    except yaml.YAMLError as err:
        raise NotRun(f"рендер профиля не разобран: {err}") from err
    workloads = [d for d in docs if d.get("kind") in WORKLOAD_KINDS]
    hits = []
    for d in workloads:
        ps = _pod_spec(d)
        conts = list(ps.get("containers") or []) + list(ps.get("initContainers") or [])
        if any(isinstance(c, dict) and container_launches(c, proc) for c in conts):
            hits.append((d["kind"], str((d.get("metadata") or {}).get("name") or ""), _selector(d)))
    return len(docs), len(workloads), hits


def render(stack: str) -> str:
    if not stack:
        raise NotRun("профиль стенда не назван — чей рендер судить, неизвестно")
    if not shutil.which("helm"):
        raise NotRun("helm не в PATH — профиль рендерить нечем")
    a = subprocess.run(["bash", str(STACKS_SH), "--args", stack, str(UMBRELLA)],
                       capture_output=True, text=True, timeout=60)
    args = a.stdout.split()
    if a.returncode != 0 or not args:
        raise NotRun(f"цепочка профиля '{stack}' не прочитана (код {a.returncode}): "
                     f"{(a.stderr or a.stdout).strip()[:300]}")
    r = subprocess.run(["helm", "template", RELEASE, str(UMBRELLA), "-n", "kacho", *args],
                       capture_output=True, text=True, timeout=600)
    if r.returncode != 0:
        raise NotRun(f"рендер профиля '{stack}' отказал (код {r.returncode}): "
                     f"{r.stderr.strip()[:400]}")
    return r.stdout


def answer(stack: str, proc: str, manifest: str | None = None) -> tuple[str, list[tuple[str, str, dict]]]:
    """→ (строка итога, нагрузки). Бросает NotRun."""
    text = manifest if manifest is not None else render(stack)
    ndocs, nwl, hits = launches(text, proc)
    if ndocs == 0:
        raise NotRun(f"рендер профиля '{stack}' не дал ни одного документа — осмотрено ноль")
    return (f"профиль {stack}: документов {ndocs}, рабочих нагрузок {nwl}, "
            f"запускают {proc}: {len(hits)}"), hits


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--stack", default="")
    ap.add_argument("--process", required=True)
    ap.add_argument("--manifest", default=None)
    a = ap.parse_args(argv)
    try:
        manifest = (pathlib.Path(a.manifest).read_text(encoding="utf-8")
                    if a.manifest else None)
        line, hits = answer(a.stack, a.process, manifest)
    except (NotRun, OSError, subprocess.SubprocessError) as err:
        print(f"НЕ ВЫПОЛНИЛОСЬ: {err}", file=sys.stderr)
        return 2
    print(line)
    for kind, name, sel in hits:
        print(f"НАГРУЗКА {kind}/{name} " + ",".join(f"{k}={v}" for k, v in sorted(sel.items())))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
