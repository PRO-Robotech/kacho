#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Печатает текст шага подстановки ТАК, КАК ЕГО ПОЛУЧАЕТ ОБОЛОЧКА В ПОДЕ.

──────────────────────────────────────────────────────────────────────────────
ЗАЧЕМ ЭТОТ ФАЙЛ

Между чартом и оболочкой стоит ЕЩЁ ОДИН подстановщик: Kubernetes разбирает
`command`/`args` сам — подставляет свои ссылки по объявленным переменным
контейнера и трактует удвоенный знак доллара как экран для одинарного.

Доказательства инъекцией подавали извлечённый скрипт ПРЯМО в `sh`, то есть
МИМО этого подстановщика. Фикстура была снисходительнее продукта (`e2e-flow.md`
§5), и класс в ней не воспроизводился НИКОГДА: на машине разработчика зелено, в
поде красно. Задачей #1786 это стоило стенда — шаг подстановки записывал в
конфигурацию ИМЯ переменной вместо её величины, потому что удвоение схлопывалось
до того, как оболочка вообще начинала читать строку.

Отсюда правило: всякий, кто прогоняет извлечённый шаг, берёт его ОТСЮДА.
Одно место на оба доказательства — иначе они разойдутся молча.

ГРАНИЦА НАЗВАНА: этот файл ПРИМЕНЯЕТ подстановку, а не запрещает её. Запрет
(«подстановка Kubernetes на наших командах — тождество») держит гейт
deploy/identity_step_survives_kubernetes_expansion_test.go; здесь его нет
намеренно, чтобы два места об одном предмете не разошлись.

Подстановщик воспроизведён дословно по
k8s.io/kubernetes/third_party/forked/golang/expansion (Expand +
tryReadVariableName) и kubelet MappingFuncFor, оборачивающему нерезолвящееся имя
обратно в исходную форму ссылки.

ВЫЗОВ:  identity-step-as-kubelet-delivers.py <render.yaml> [имя-контейнера]
        stdout — текст довода; stderr — перепись (изменила ли подстановка текст).
"""

import sys

import yaml

OPERATOR = "$"
OPENER = "("
CLOSER = ")"


def _try_read_variable_name(s):
    """Ветви ровно те же, что у платформы."""
    if s[0] == OPERATOR:  # экран: возвращается ОДИН знак
        return s[0], False, 1
    if s[0] == OPENER:
        i = 1
        while i < len(s):
            if s[i] == CLOSER:
                return s[1:i], True, i + 1
            i += 1
        return OPERATOR + OPENER, False, 1  # незакрытая ссылка — дословно
    return OPERATOR + s[0], False, 1


def expand(text, env):
    """Подстановка Kubernetes над `command`/`args`."""
    out = []
    checkpoint = 0
    cursor = 0
    while cursor < len(text):
        if text[cursor] == OPERATOR and cursor + 1 < len(text):
            out.append(text[checkpoint:cursor])
            read, is_var, advance = _try_read_variable_name(text[cursor + 1:])
            if is_var:
                out.append(env[read] if read in env else OPERATOR + OPENER + read + CLOSER)
            else:
                out.append(read)
            cursor += advance
            checkpoint = cursor + 1
        cursor += 1
    out.append(text[checkpoint:])
    return "".join(out)


def main():
    if len(sys.argv) < 2:
        sys.exit("вызов: identity-step-as-kubelet-delivers.py <render.yaml> [контейнер]")
    render = sys.argv[1]
    want = sys.argv[2] if len(sys.argv) > 2 else "identity-config-render"

    with open(render, encoding="utf-8") as fh:
        docs = list(yaml.safe_load_all(fh))

    for doc in docs:
        if not doc or doc.get("kind") not in ("Deployment", "StatefulSet"):
            continue
        for container in doc["spec"]["template"]["spec"].get("initContainers") or []:
            if container.get("name") != want:
                continue
            args = container.get("args") or []
            if not args:
                sys.exit("ОТКАЗ: у шага %s нет довода — прогонять нечего" % want)
            env = {}
            for entry in container.get("env") or []:
                name = entry.get("name")
                if not name:
                    continue
                # Kubelet резолвит ссылку на секрет ДО подстановки, поэтому имя
                # ему известно. Величину сюда не тащим: секрет в вывод не идёт, а
                # предмет здесь — САМ ФАКТ подстановки, а не её содержимое.
                # Случай `$(ИМЯ)` в наших командах запрещён гейтом (см. шапку),
                # поэтому заглушка не может подменить прогоняемый текст молча:
                # гейт покраснеет раньше, чем она понадобится.
                env[name] = entry.get("value", "«ВЕЛИЧИНА " + name + "»")
            delivered = expand(args[0], env)
            if delivered == args[0]:
                sys.stderr.write(
                    "перепись: подстановка Kubernetes текст НЕ изменила "
                    "(%d знаков) — оболочка получит объявленное дословно\n" % len(args[0]))
            else:
                sys.stderr.write(
                    "перепись: подстановка Kubernetes ИЗМЕНИЛА текст (%d → %d знаков) — "
                    "прогон пойдёт по тому, что получит оболочка, а не по объявленному\n"
                    % (len(args[0]), len(delivered)))
            sys.stdout.write(delivered)
            return
    sys.exit("ОТКАЗ: шаг %s не найден в рендере" % want)


if __name__ == "__main__":
    main()
