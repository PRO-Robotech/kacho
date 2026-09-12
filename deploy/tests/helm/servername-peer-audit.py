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
    SCOPE <n> <n> <n> <n> <n>    — осмотрено · сверено с SAN · сверено с адресом ·
                                   без адреса · из них прочитано формой файла настроек

Судит УЗЛЫ разобранных документов, а не расположение символов в строке.

# ЧЕТЫРЕ ЗАКОННЫЕ ФОРМЫ ЗАПИСИ ПАРЫ, И ФОРМА, О КОТОРОЙ РАЗБОР НЕ ЗНАЕТ, МОЛЧИТ

Пара «адрес ребра + имя для сверки» писана в дереве ЧЕТЫРЬМЯ формами, и число
здесь не круглое, а переписанное по пяти потребителям домена величин:

  А. обе половины — переменные окружения контейнера (compute, registry);
  Б. адрес — ключ `data` у ConfigMap, приезжающий контейнеру через `envFrom`,
     имя — переменная окружения (storage);
  В. адрес — ключ внутри ФАЙЛА НАСТРОЕК, смонтированного из ConfigMap, имя —
     переменная окружения (vpc);
  Г. обе половины — ключи внутри файла настроек (nlb).

Прежняя редакция знала только форму А. Три остальные давали НЕ красное и не
зелёное, а МОЛЧАНИЕ: три потребителя из пяти попадали в счётчик «адреса ребра
рядом нет», и «ноль находок» читалось шире, чем было. Замер, которым это
установлено: тридцать пар (шесть профилей × пять потребителей) объявляли
`quota.authority: not-deployed` вместе с именем `kaname-internal.kacho.svc`;
гейт называл двенадцать из тридцати.
"""

import re
import sys

import yaml

# Каноническая форма имени для сверки: короткая служебная `<служба>.<ns>.svc`.
# Она не зависит от домена кластера и её пишет подавляющее большинство рёбер.
CANON = re.compile(r"^[a-z0-9]([-a-z0-9]*[a-z0-9])?\.[a-z0-9]([-a-z0-9]*[a-z0-9])?\.svc$")

# Хвост имени ПЕРЕМЕННОЙ, объявляющей АДРЕС ребра. Узнаётся по хвосту, а не по
# перечню полных имён: выписанный перечень разошёлся бы с деревом молча.
ADDR_TAIL = ("_ADDR", "_URL", "_ENDPOINT", "_AUTHORITY")

# Тот же хвост у КЛЮЧА файла настроек. Отдельный перечень, потому что регистр и
# разделитель у ключей другие (`internal-addr`, `iam-endpoint`, `authority`), а
# сводить их к одному написанию значило бы принимать решение о чужом ключе.
ADDR_TAIL_KEY = ("addr", "url", "endpoint", "authority", "address")

# Объявление отсутствия соседа (`corelib/quota.NotDeployed`). Своё значение, а не
# пустая строка: «не настроили» и «настроили на отсутствие» обязаны различаться.
NOT_DEPLOYED = "not-deployed"

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
    if not isinstance(value, str) or not value:
        return ""
    value = value.strip()
    value = re.sub(r"^[a-z0-9+.-]+://", "", value)
    value = value.split("/", 1)[0]
    if value.count(":") == 1:
        value = value.split(":", 1)[0]
    return value


def shorten(host):
    return host[: -len(CLUSTER_SUFFIX)] if host.endswith(CLUSTER_SUFFIX) else host


def flatten(node, path, out):
    """Плоский вид разобранного документа: `a.b.c` → скаляр."""
    if isinstance(node, dict):
        for key, value in node.items():
            flatten(value, path + [str(key)], out)
    elif isinstance(node, list):
        for i, value in enumerate(node):
            flatten(value, path + [str(i)], out)
    else:
        out[".".join(path)] = node


def config_documents(data):
    """Файлы настроек внутри `ConfigMap.data`: (ключ, плоский вид).

    Судится ЛЮБОЕ значение, разобравшееся в отображение, а не только `config.yaml`:
    перечень имён файлов разошёлся бы с деревом молча, а служба вправе назвать свой
    файл как угодно.
    """
    out = []
    for key, value in (data or {}).items():
        if not isinstance(value, str) or "\n" not in value:
            continue
        try:
            doc = yaml.safe_load(value)
        except yaml.YAMLError:
            continue
        if isinstance(doc, dict):
            flat = {}
            flatten(doc, [], flat)
            out.append((key, flat))
    return out


def env_tokens(base):
    """Имена ребра, какими его мог назвать файл настроек, — от длинного к короткому.

    `KACHO_VPC_QUOTA_AUTHORITY` → `quota-authority`, `authority`. Берутся СУФФИКСЫ
    имени переменной, потому что приставка — это имя службы, а не ребра, и её длина
    у трёх служб разная (`KACHO_COMPUTE_`, `KACHO_VPC_`, `KANAME_`). Выписанный
    перечень приставок разошёлся бы с деревом при первой новой службе.
    """
    parts = [p for p in base.split("_") if p]
    return ["-".join(parts[i:]).lower() for i in range(len(parts))]


def path_words(path):
    """Слова пути: разделители `.` и `-` равноправны.

    Одно ребро писано в разных местах разными разделителями: ключ удостоверения —
    `mtls.quota-authority`, ключ адреса — `quota.authority`. Сведение к СЛОВАМ
    убирает выбор между двумя написаниями одного имени; сравнение по целому
    сегменту молчало бы ровно на этой паре, и молчание было бы неотличимо от
    чистого дерева (проверено инъекцией: находок 4 из 5, пятая — nlb).
    """
    words = set()
    for segment in path.split("."):
        words.update(w for w in segment.split("-") if w)
    return words


def config_addr_candidates(flat, tokens):
    """Адреса ребра, объявленные КЛЮЧАМИ файла настроек.

    Ключ признаётся адресом ЭТОГО ребра, когда ВСЕ слова имени ребра есть в словах
    пути, а последний сегмент оканчивается хвостом адреса. Отношение по словам, а не
    по подстроке: подстрока `iam` нашлась бы в `trusted-forwarder-sans`, то есть в
    круге доверия, который адресом не является.

    Имена ребра перебираются от ДОЛГОГО к короткому, и берётся первое давшее
    находку: долгое имя точнее — `iam-authz` отбирает `authz.iam-endpoint`, тогда
    как `authz` забрал бы вместе с ним и соседний `authz.authorize-endpoint`, то
    есть адрес ДРУГОГО слушателя.
    """
    for token in tokens:
        want = {w for w in token.split("-") if w}
        if not want:
            continue
        found = {}
        for path, value in flat.items():
            segments = path.split(".")
            if not want <= path_words(path):
                continue
            if not any(segments[-1].endswith(tail) for tail in ADDR_TAIL_KEY):
                continue
            if host_of(value) or str(value).strip() == NOT_DEPLOYED:
                found[path] = value
        if found:
            return found
    return {}


def judge(coord, value, san, addrs, findings):
    """Три утверждения об ОДНОМ объявлении имени. Возвращает (сверено-с-SAN, сверено-с-адресом)."""
    san_judged = addr_judged = 0

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
    # Пир, чей сертификат в профиле не рендерится, находкой не является: сверять
    # не с чем. Поэтому условие — наличие сертификатов вообще, а не конкретного.
    if san:
        if value in san or shorten(value) in san or value + CLUSTER_SUFFIX in san:
            san_judged = 1
        else:
            findings.append(
                f"{coord} = {value!r} — имени НЕТ ни в одном "
                f"`Certificate.spec.dnsNames` этого профиля. Рукопожатие "
                f"оборвётся на сверке, а объявление выглядит настроенным"
            )

    # В. ИМЯ СОВПАДАЕТ С АДРЕСОМ РЕБРА — там, где адрес объявлен.
    if not addrs:
        return san_judged, addr_judged
    addr_judged = 1

    # Адрес, объявивший ОТСУТСТВИЕ соседа, — не «другой хост», а отсутствие
    # хоста вовсе, и отказ обязан называть это своим словом: оператор иначе
    # ищет опечатку в имени вместо лишней половины пары.
    if all(str(v).strip() == NOT_DEPLOYED for v in addrs.values()):
        findings.append(
            f"{coord} = {value!r} — адрес ребра объявляет, что соседа в этой "
            f"установке НЕТ ({sorted(addrs)!r} = {NOT_DEPLOYED!r}), а имя для сверки "
            f"задано. Обращаться не к кому, поэтому имя называет ПИРА, К КОТОРОМУ "
            f"РЕБРО НЕ ИДЁТ. Половина пары хуже отсутствия обеих: она выглядит "
            f"настроенной. Снимается удостоверение к ребру целиком, а не одна строка"
        )
        return san_judged, addr_judged

    hosts = {shorten(host_of(v)) for v in addrs.values() if host_of(v)}

    # Адрес объявлен и ПУСТ — это не «адреса рядом нет» и не совпадение. Прежняя
    # редакция давала здесь находку с текстом «ребро идёт к []»; нынешняя
    # называет предмет прямо. Молчать нельзя: пустой набор хостов делал бы
    # отношение «имя ∈ хосты» выполнимым подстановкой, то есть не сужающим
    # ничего.
    if not hosts:
        findings.append(
            f"{coord} = {value!r} — адрес ребра ОБЪЯВЛЕН ({sorted(addrs)!r}), но хоста в "
            f"нём нет: сверять имя не с чем, а объявление выглядит настроенным. Либо "
            f"адрес задан пустым, либо он не адрес"
        )
        return san_judged, addr_judged

    if shorten(value) not in hosts:
        findings.append(
            f"{coord} = {value!r} — ребро идёт к {sorted(hosts)!r} "
            f"(объявлено {sorted(addrs)!r}), а сверяется имя ДРУГОГО пира. "
            f"Рукопожатие пройдёт с тем, чьё имя подставлено, а не с тем, "
            f"к кому шли"
        )
    return san_judged, addr_judged


def audit(docs, stack):
    """Возвращает (список находок, перепись). Чистая функция от разобранных документов."""
    findings = []
    seen = san_judged = addr_judged = addr_absent = from_config = 0

    san = set()
    for doc in docs:
        if doc.get("kind") == "Certificate":
            san.update(doc["spec"].get("dnsNames") or [])

    configmaps = {
        doc["metadata"]["name"]: (doc.get("data") or {})
        for doc in docs
        if doc.get("kind") == "ConfigMap"
    }

    # ── формы А, Б, В: имя объявлено ПЕРЕМЕННОЙ окружения ────────────────────
    for doc in docs:
        spec = pod_spec(doc)
        if not spec:
            continue
        owner = doc["metadata"]["name"]

        # Файлы настроек, смонтированные ЭТОМУ поду: форма В ищет адрес в них.
        mounted = []
        for volume in spec.get("volumes") or []:
            ref = (volume.get("configMap") or {}).get("name")
            if ref in configmaps:
                mounted.extend(flat for _, flat in config_documents(configmaps[ref]))

        for container in (spec.get("containers") or []) + (spec.get("initContainers") or []):
            # Форма Б: `envFrom` доносит ключи ConfigMap до процесса КАК env, и
            # объявление адреса живёт там, а не в перечне `env`. Своё `env`
            # перекрывает пришедшее по `envFrom` — тот же порядок, что у kubelet.
            env = {}
            for source in container.get("envFrom") or []:
                ref = (source.get("configMapRef") or {}).get("name")
                if ref in configmaps:
                    env.update(
                        {k: v for k, v in configmaps[ref].items() if isinstance(v, str)}
                    )
            env.update(
                {e["name"]: e.get("value") for e in (container.get("env") or []) if "name" in e}
            )

            for name, value in sorted(env.items()):
                if not name.endswith("_SERVERNAME"):
                    continue
                seen += 1
                coord = f"{stack} · {owner}/{container['name']} · {name}"

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
                    tokens = env_tokens(base)
                    for flat in mounted:
                        addrs = config_addr_candidates(flat, tokens)
                        if addrs:
                            break

                sj, aj = judge(coord, value, san, addrs, findings)
                san_judged += sj
                addr_judged += aj
                if not aj:
                    addr_absent += 1

    # ── форма Г: обе половины объявлены КЛЮЧАМИ файла настроек ───────────────
    #
    # Судится по ConfigMap, а не по каждому поду, который его монтирует: один
    # файл настроек монтируют и рабочий контейнер, и накатчик миграций, и одна
    # находка приходила бы дважды.
    for cm_name, data in sorted(configmaps.items()):
        for key, flat in config_documents(data):
            for path, value in sorted(flat.items()):
                segments = path.split(".")
                if segments[-1] != "servername":
                    continue
                seen += 1
                from_config += 1
                coord = f"{stack} · {cm_name}:{key} · {path}"
                tokens = ["-".join(segments[i:-1]) for i in range(len(segments) - 1)]
                addrs = config_addr_candidates(flat, [t for t in tokens if t])
                sj, aj = judge(coord, str(value), san, addrs, findings)
                san_judged += sj
                addr_judged += aj
                if not aj:
                    addr_absent += 1

    return findings, (seen, san_judged, addr_judged, addr_absent, from_config)


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
