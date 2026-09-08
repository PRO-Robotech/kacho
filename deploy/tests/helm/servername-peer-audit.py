#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""servername-peer-audit.py — разбор ОДНОГО рендера на предмет имён для сверки TLS.

Отдельным файлом, а не строкой внутри гейта, ОСОЗНАННО: доказательство инъекцией
кормит эту же функцию синтетическим входом, поэтому доказанное на синтетике верно
для дерева. Встроенный в скрипт разбор пришлось бы воспроизводить в инъекции — то
есть завести второе место об одном предмете, которое разойдётся молча.

Печатает построчно:
    FINDING <текст>              — находка о дереве
    SCOPE <n> <n> <n> <n>        — осмотрено · сверено с SAN · сверено с адресом · без адреса

Судит УЗЛЫ разобранных документов, а не расположение символов в строке.
"""

import re
import sys

import yaml

# Каноническая форма имени для сверки: короткая служебная `<служба>.<ns>.svc`.
# Она не зависит от домена кластера и её пишет подавляющее большинство рёбер.
CANON = re.compile(r"^[a-z0-9]([-a-z0-9]*[a-z0-9])?\.[a-z0-9]([-a-z0-9]*[a-z0-9])?\.svc$")

# Хвост имени переменной, объявляющей АДРЕС ребра. Узнаётся по хвосту, а не по
# перечню полных имён: выписанный перечень разошёлся бы с деревом молча.
ADDR_TAIL = ("_ADDR", "_URL", "_ENDPOINT", "_AUTHORITY")

CLUSTER_SUFFIX = ".cluster.local"


def pod_spec(doc):
    kind = doc.get("kind")
    if kind in ("Deployment", "StatefulSet", "DaemonSet", "Job"):
        return doc["spec"]["template"]["spec"]
    if kind == "CronJob":
        return doc["spec"]["jobTemplate"]["spec"]["template"]["spec"]
    return None


def host_of(value):
    """Хост из адреса: схема, путь и порт отбрасываются."""
    if not value:
        return ""
    value = value.strip()
    value = re.sub(r"^[a-z0-9+.-]+://", "", value)
    value = value.split("/", 1)[0]
    if value.count(":") == 1:
        value = value.split(":", 1)[0]
    return value


def shorten(host):
    return host[: -len(CLUSTER_SUFFIX)] if host.endswith(CLUSTER_SUFFIX) else host


def audit(docs, stack):
    """Возвращает (список находок, перепись). Чистая функция от разобранных документов."""
    findings = []
    seen = san_judged = addr_judged = addr_absent = 0

    san = set()
    for doc in docs:
        if doc.get("kind") == "Certificate":
            san.update(doc["spec"].get("dnsNames") or [])

    for doc in docs:
        spec = pod_spec(doc)
        if not spec:
            continue
        owner = doc["metadata"]["name"]
        for container in (spec.get("containers") or []) + (spec.get("initContainers") or []):
            env = {e["name"]: e.get("value") for e in (container.get("env") or []) if "name" in e}
            for name, value in sorted(env.items()):
                if not name.endswith("_SERVERNAME"):
                    continue
                seen += 1
                coord = f"{stack} · {owner}/{container['name']} · {name}"

                # А. ФОРМА ОДНА НА ДЕРЕВО.
                if not CANON.match(value or ""):
                    findings.append(
                        f"{coord} = {value!r} — форма НЕ каноническая. Канон дерева — "
                        f"короткая служебная `<служба>.<пространство>.svc`: она не зависит "
                        f"от домена кластера и её пишут остальные рёбра. Обе формы сегодня "
                        f"работают, поэтому расхождение тихое — и сломается ровно одно "
                        f"ребро в день, когда состав SAN сузят"
                    )

                # Б. ИМЯ ЛЕЖИТ В SAN сертификата ЭТОГО профиля.
                #
                # Пир, чей сертификат в профиле не рендерится, находкой не
                # является: сверять не с чем. Поэтому условие — наличие
                # сертификатов вообще, а не наличие конкретного.
                if san:
                    if value in san or shorten(value) in san or value + CLUSTER_SUFFIX in san:
                        san_judged += 1
                    else:
                        findings.append(
                            f"{coord} = {value!r} — имени НЕТ ни в одном "
                            f"`Certificate.spec.dnsNames` этого профиля. Рукопожатие "
                            f"оборвётся на сверке, а объявление выглядит настроенным"
                        )

                # В. ИМЯ СОВПАДАЕТ С АДРЕСОМ РЕБРА — там, где адрес объявлен.
                head = name[: -len("_SERVERNAME")]
                base = head[: head.rfind("_MTLS")] if "_MTLS" in head else head
                addrs = {
                    k: v
                    for k, v in env.items()
                    if k != name
                    and (k == base or k.startswith(base + "_"))
                    and any(k.endswith(t) for t in ADDR_TAIL)
                }
                if not addrs:
                    addr_absent += 1
                    continue
                addr_judged += 1
                hosts = {shorten(host_of(v)) for v in addrs.values() if host_of(v)}
                if shorten(value) not in hosts:
                    findings.append(
                        f"{coord} = {value!r} — ребро идёт к {sorted(hosts)!r} "
                        f"(объявлено {sorted(addrs)!r}), а сверяется имя ДРУГОГО пира. "
                        f"Рукопожатие пройдёт с тем, чьё имя подставлено, а не с тем, "
                        f"к кому шли"
                    )

    return findings, (seen, san_judged, addr_judged, addr_absent)


def main():
    path, stack = sys.argv[1], sys.argv[2]
    with open(path, encoding="utf-8") as fh:
        docs = [d for d in yaml.safe_load_all(fh) if d]
    findings, scope = audit(docs, stack)
    for finding in findings:
        print("FINDING", finding)
    print("SCOPE", *scope)


if __name__ == "__main__":
    main()
