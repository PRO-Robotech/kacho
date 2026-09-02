#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Величина учётных данных обратного вызова обязана иметь ИСТОЧНИК в поде.

Читает поток отрендеренных документов (stdout `helm template`) со stdin и судит
ПОДЫ, а не значения профиля: предмет — то, что достанется процессу, а не то, что
кто-то объявил намерением.

────────────────────────────────────────────────────────────────────────────
ПРЕДМЕТ

Полоса обратных вызовов к слушателю хуков службы прав подписывается общим
секретом. Секрет не может лежать в карте настроек, поэтому в конфигурации на его
месте стояла ссылка `${ИМЯ}`. Читатель конфигурации подстановки в ЗНАЧЕНИЯХ не
делает — ИЗМЕРЕНО, а не выведено из документации:

  · oryd/hydra:v26.2.0 — ссылка, поставленная значением `urls.self.issuer` при
    ЗАВЕДЁННОЙ в поде переменной того же имени, дожила до проверки схемы
    дословно: «"${…}" is not valid "uri"»;
  · oryd/kratos:v26.2.0 — та же ссылка вернулась в адресе действия потока
    регистрации.

Значит литерал уезжал в заголовок как есть, служба прав отвечала `401`, а край
переводил это арендатору в `502`. Доставка переменной с этим именем предмет бы
НЕ закрыла: читатель её не читает.

Читатель берёт величину только двумя способами, и проверка требует одного из них:

  1. ПЕРЕМЕННАЯ ПО ПУТИ КЛЮЧА — имя переменной есть путь величины в
     конфигурации, заглавными и через подчёркивание. Она перекрывает файл. Путь
     с индексом массива таким именем невыразим — см. (2);
  2. ПОДСТАНОВКА ДО СТАРТА ПРОЦЕССА — конфигурацию готовит отдельный шаг, а
     процесс читает уже готовое. Тогда карты настроек он не читает вовсе, и
     ссылке взяться неоткуда by construction.

────────────────────────────────────────────────────────────────────────────
ЧТО УТВЕРЖДАЕТСЯ

Для каждого контейнера-ЧИТАТЕЛЯ (в командной строке есть `--config <путь>`),
чей путь резолвится в том, backed картой настроек:

  A. текст, который достанется процессу, НЕ несёт ссылок `${ИМЯ}`;
  B. у каждой объявленной в этом тексте полосы обратного вызова к слушателю
     хуков с учётными данными типа `api_key` величина имеет источник:
       · путь выразим переменной → переменная объявлена В ЭТОМ контейнере и её
         значение приходит из `secretKeyRef` (величина в открытом виде рядом с
         объявлением — это тот же секрет в рендере, только другим способом);
       · путь невыразим (величина внутри массива) → источника не существует
         вовсе, и такую конфигурацию процесс не имеет права читать картой
         настроек: её обязан подготовить шаг ДО старта.

────────────────────────────────────────────────────────────────────────────
ЧЕГО НЕ УТВЕРЖДАЕТ (граница названа, чтобы «зелено» не читалось шире)

  · что секрет существует и непуст — это рантайм; выполнимость обязательной
    ссылки держит prerequisite-secrets-test.sh;
  · что отправитель и проверяющая сторона взяли ОДИН И ТОТ ЖЕ секрет — это
    отдельное утверждение, оно ниже и печатается переписью;
  · ничего о конфигурации, которую ГЛАВНЫЙ контейнер не монтирует. Такие карты
    настроек ПЕРЕСЧИТЫВАЮТСЯ отдельной строкой переписи и названы поимённо:
    «ноль находок» обязано быть отличимо от «ноль прочитанного».
    ЧИТАТЬ ЭТУ СТРОКУ КАК «карту никто не читает» НЕЛЬЗЯ, и здесь так и было
    написано. Конфигурация службы личности читается ЧЕРЕЗ ШАГ ПОДСТАНОВКИ:
    карта монтируется init-контейнеру, он пишет отрендеренный файл, и уже его
    читает процесс — поэтому у главного контейнера её и нет by construction.
    Ссылки такой карты судит identity-hook-credential-source-test.sh, число
    монтирующих профилей — deploy/identity_config_mount_census_test.go.

Исходы: 0 — находок нет; 1 — находки; 2 — обходить нечего (предпосылка сломана).
"""

import re
import sys

import yaml

HOOKS_ROUTE = "/iam/v1/hooks/"
PLACEHOLDER = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")
WORKLOADS = ("Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet", "Pod")


def pod_specs(doc):
    """Спецификации подов документа — по всем известным оболочкам."""
    kind = doc.get("kind")
    if kind == "Pod":
        return [doc.get("spec") or {}]
    spec = doc.get("spec") or {}
    tpl = spec.get("template")
    if isinstance(tpl, dict) and tpl.get("spec"):
        return [tpl["spec"]]
    job = ((spec.get("jobTemplate") or {}).get("spec") or {}).get("template") or {}
    if job.get("spec"):
        return [job["spec"]]
    return []


def config_paths(container):
    """Пути конфигураций, названные командной строкой контейнера."""
    argv = list(container.get("command") or []) + list(container.get("args") or [])
    out = []
    for i, a in enumerate(argv):
        a = str(a)
        if a in ("--config", "-c") and i + 1 < len(argv):
            out.append(str(argv[i + 1]))
        elif a.startswith("--config="):
            out.append(a.split("=", 1)[1])
    return out


def backing_configmap(container, volumes, path):
    """Карта настроек и ключ, из которых процесс прочтёт `path`, либо None.

    Том, не являющийся картой настроек (например, подготовленный шагом до
    старта), возвращает None НАМЕРЕННО: ссылке там взяться неоткуда, и это
    законная форма (2), а не пропуск.
    """
    best = None
    for m in container.get("volumeMounts") or []:
        mp = str(m.get("mountPath") or "")
        if not mp:
            continue
        if path == mp or path.startswith(mp.rstrip("/") + "/"):
            if best is None or len(mp) > len(best[0].get("mountPath") or ""):
                best = (m, mp)
    if best is None:
        return None
    mount, mp = best
    vol = volumes.get(mount.get("name"))
    if not vol or "configMap" not in vol:
        return None
    if mount.get("subPath"):
        key = str(mount["subPath"])
    else:
        key = path[len(mp.rstrip("/")) + 1:] if path != mp else ""
    return (vol["configMap"].get("name"), key)


def walk_lanes(node, trail=()):
    """Полосы обратного вызова: узлы, чей адрес указывает на маршрут хуков."""
    if isinstance(node, dict):
        url = node.get("url")
        if isinstance(url, str) and HOOKS_ROUTE in url:
            yield trail, node
        for k, v in node.items():
            yield from walk_lanes(v, trail + (str(k),))
    elif isinstance(node, list):
        for i, v in enumerate(node):
            yield from walk_lanes(v, trail + (i,))


def env_name_for(trail):
    """Имя переменной по пути величины; None — путь невыразим (индекс массива)."""
    segs = list(trail) + ["auth", "config", "value"]
    if any(isinstance(s, int) for s in segs):
        return None
    return "_".join(str(s).upper() for s in segs)


def main():
    docs = [d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]
    configmaps = {}
    for d in docs:
        if d.get("kind") == "ConfigMap":
            configmaps[d.get("metadata", {}).get("name")] = d.get("data") or {}

    findings = []
    readers = 0
    files_read = 0
    lanes_checked = 0
    refs_checked = 0
    read_cms = set()

    for d in docs:
        if d.get("kind") not in WORKLOADS:
            continue
        owner = f"{d.get('kind')}/{d.get('metadata', {}).get('name')}"
        for spec in pod_specs(d):
            volumes = {v.get("name"): v for v in (spec.get("volumes") or [])}
            containers = list(spec.get("initContainers") or []) + list(spec.get("containers") or [])
            for c in containers:
                paths = config_paths(c)
                if not paths:
                    continue
                readers += 1
                envs = {e.get("name"): e for e in (c.get("env") or [])}
                where = f"{owner}/{c.get('name')}"
                for p in paths:
                    backing = backing_configmap(c, volumes, p)
                    if backing is None:
                        continue
                    cm, key = backing
                    data = configmaps.get(cm)
                    if data is None:
                        continue
                    text = data.get(key) if key else None
                    if text is None:
                        if len(data) == 1:
                            text = next(iter(data.values()))
                        else:
                            continue
                    files_read += 1
                    read_cms.add(cm)

                    # (A) ссылка не имеет права дожить до процесса
                    for name in sorted(set(PLACEHOLDER.findall(text))):
                        refs_checked += 1
                        findings.append(
                            f"{where}: конфигурация {cm}:{key or '(единственный ключ)'} несёт ссылку "
                            f"${{{name}}}, а процесс подстановки в значениях НЕ делает — строка "
                            f"достанется ему дословно"
                        )

                    # (B) у каждой полосы обязан быть источник величины
                    try:
                        parsed = yaml.safe_load(text)
                    except yaml.YAMLError as exc:
                        findings.append(f"{where}: конфигурация {cm} не разбирается: {exc}")
                        continue
                    for trail, lane in walk_lanes(parsed):
                        auth = lane.get("auth") or {}
                        if str(auth.get("type") or "") != "api_key":
                            continue
                        lanes_checked += 1
                        var = env_name_for(trail)
                        if var is None:
                            findings.append(
                                f"{where}: полоса {'.'.join(str(s) for s in trail)} держит величину "
                                f"внутри массива — переменной такой путь НЕ выразим, источника не "
                                f"существует; конфигурацию обязан подготовить шаг ДО старта процесса, "
                                f"а не карта настроек"
                            )
                            continue
                        e = envs.get(var)
                        if e is None:
                            findings.append(
                                f"{where}: полоса {'.'.join(str(s) for s in trail)} объявлена, но "
                                f"переменной {var} в контейнере нет — величина пуста, служба прав "
                                f"ответит 401"
                            )
                        elif "secretKeyRef" not in (e.get("valueFrom") or {}):
                            findings.append(
                                f"{where}: величина полосы {'.'.join(str(s) for s in trail)} задана "
                                f"переменной {var} НЕ из secretKeyRef — общий секрет оказался бы в "
                                f"рендере открытым текстом"
                            )

    # Карты настроек, объявляющие полосу и НЕ смонтированные главному контейнеру.
    # Это не значит «карту никто не читает»: она может доезжать до процесса через
    # шаг подстановки, монтирующий её init-контейнеру.
    declared_unread = sorted(
        n for n, data in configmaps.items()
        if n not in read_cms and any(HOOKS_ROUTE in str(v) for v in data.values())
    )

    print(
        f"перепись: документов {len(docs)} · карт настроек {len(configmaps)} · "
        f"контейнеров-читателей {readers} · прочитано конфигураций {files_read} · "
        f"полос проверено {lanes_checked} · ссылок найдено {refs_checked}"
    )
    if declared_unread:
        print(
            "  вне области: конфигурации объявляют полосу и НЕ смонтированы главному "
            "контейнеру — " + ", ".join(declared_unread)
            + " (это ГРАНИЦА разбора, а не вердикт «карту никто не читает»: она может "
              "доезжать до процесса через шаг подстановки — его ссылки судит "
              "identity-hook-credential-source-test.sh)"
        )

    if readers == 0:
        print("ОТКАЗ: контейнеров-читателей ноль — обходить нечего, предпосылка сломана", file=sys.stderr)
        return 2
    if findings:
        for f in findings:
            print("НАХОДКА: " + f, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
