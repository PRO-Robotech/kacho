#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""required-secrets.py — КАКИЕ СЕКРЕТЫ РАСКАТ ТРЕБУЕТ ДО ПРИМЕНЕНИЯ.

Читает отрендеренный стенд (stdin, поток YAML-документов `helm template`) и
печатает имена секретов, без которых под не создаст контейнер, — за вычетом тех,
что производит само это применение. По имени на строку, отсортировано.

─────────────────────────────────────────────────────────────────────────────
ЗАЧЕМ ОДИН ФАЙЛ (задача #891)

Производная жила в двух рецептах раската копиями, и копии разошлись там, где
расхождение опасно. Замер на рендере стенда fe3455 (дерево 1d42a672):

  • копия боевой раскатки НЕ ЧИТАЛА `envFrom` — секрет учётных данных объектного
    хранилища приезжает именно так, и держала его только рукописная таблица;
  • та же копия НЕ ВЫЧИТАЛА то, что чеканит cert-manager по `Certificate` того же
    применения, и требовала до применения 25 секретов, которых на свежем кластере
    быть не может: предполёт отказал бы на законной первой установке;
  • обе копии не читали том `projected` — законную форму, в которой обязательный
    секрет так же не даёт контейнеру стартовать.

─────────────────────────────────────────────────────────────────────────────
ФОРМЫ ПОТРЕБЛЕНИЯ — ВСЕ ЗАКОННЫЕ, НЕ ТОЛЬКО ВСТРЕЧАЮЩИЕСЯ СЕГОДНЯ

  env[].valueFrom.secretKeyRef          (containers и initContainers)
  envFrom[].secretRef                   (containers и initContainers)
  volumes[].secret.secretName
  volumes[].projected.sources[].secret.name

Ссылка с `optional: true` предметом НЕ является: kubelet создаст контейнер и без
секрета. `imagePullSecrets` тоже нет: без него kubelet идёт в реестр анонимно,
и отказ, если он будет, — отказ вытягивания образа, а не создания контейнера.

Носители шаблона пода: Pod · Deployment · StatefulSet · DaemonSet · ReplicaSet ·
Job (`spec.template`) · CronJob (`spec.jobTemplate.spec.template`).

ЧТО ПРОИЗВОДИТ САМО ПРИМЕНЕНИЕ (вычитается):
  • `kind: Secret` рендера;
  • `spec.secretName` каждого `Certificate` — его чеканит cert-manager из того же
    применения;
  • имя, названное в аргументах `Job` того же применения (напр. `--secret-name=…`
    у создателя удостоверения вебхука допуска): его заводит сам выкат.

─────────────────────────────────────────────────────────────────────────────
ИСХОДЫ

  0 — перечень напечатан (может быть пустым: стенду ничего не нужно);
  2 — вход не прочитан либо в нём НОЛЬ документов с шаблоном пода. «Требуется
      ноль» обязано быть отличимо от «прочитано ноль»: предполёт, которому нечего
      было читать, неотличим от предполёта, которому нечего сказать.

--explain печатает «имя<TAB>форма<TAB>вид/объект» по каждой ссылке — для отказа,
который называет не только имя, но и того, кто его требует.
"""
import sys

try:
    import yaml
except ImportError:  # pragma: no cover - предпосылка, а не ветка логики
    sys.stderr.write("required-secrets: нет PyYAML — рендер разобрать нечем\n")
    sys.exit(2)

POD_TEMPLATE_KINDS = ("Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job")


def pod_spec(doc):
    """Спека пода у носителя либо None."""
    kind = doc.get("kind")
    spec = doc.get("spec") or {}
    if kind == "Pod":
        return spec
    if kind in POD_TEMPLATE_KINDS:
        return ((spec.get("template") or {}).get("spec")) or None
    if kind == "CronJob":
        job = (spec.get("jobTemplate") or {}).get("spec") or {}
        return ((job.get("template") or {}).get("spec")) or None
    return None


def references(pod):
    """(имя, форма) каждой ОБЯЗАТЕЛЬНОЙ ссылки на секрет в спеке пода."""
    out = []
    for c in (pod.get("containers") or []) + (pod.get("initContainers") or []):
        for e in c.get("env") or []:
            r = (e.get("valueFrom") or {}).get("secretKeyRef")
            if r and r.get("name") and not r.get("optional", False):
                out.append((r["name"], "env.secretKeyRef"))
        for ef in c.get("envFrom") or []:
            r = ef.get("secretRef")
            if r and r.get("name") and not r.get("optional", False):
                out.append((r["name"], "envFrom.secretRef"))
    for v in pod.get("volumes") or []:
        s = v.get("secret")
        if s and s.get("secretName") and not s.get("optional", False):
            out.append((s["secretName"], "volume.secret"))
        for src in ((v.get("projected") or {}).get("sources") or []):
            s = src.get("secret")
            if s and s.get("name") and not s.get("optional", False):
                out.append((s["name"], "volume.projected.secret"))
    return out


def made_by_the_apply(docs):
    """Имена секретов, которые производит само это применение."""
    made = {d["metadata"]["name"] for d in docs
            if d.get("kind") == "Secret" and (d.get("metadata") or {}).get("name")}
    made |= {(d.get("spec") or {}).get("secretName") for d in docs if d.get("kind") == "Certificate"}
    for d in docs:
        if d.get("kind") != "Job":
            continue
        pod = pod_spec(d) or {}
        for c in (pod.get("containers") or []) + (pod.get("initContainers") or []):
            for a in (c.get("command") or []) + (c.get("args") or []):
                for part in str(a).replace("=", " ").split():
                    made.add(part)
    made.discard(None)
    return made


def required(docs):
    """[(имя, форма, «вид/объект»)] — обязательные ссылки, которых применение не производит."""
    made = made_by_the_apply(docs)
    need = []
    for d in docs:
        pod = pod_spec(d)
        if pod is None:
            continue
        owner = f"{d.get('kind')}/{(d.get('metadata') or {}).get('name', '?')}"
        for name, form in references(pod):
            if name not in made:
                need.append((name, form, owner))
    return need


def carriers(docs):
    return sum(1 for d in docs if pod_spec(d) is not None)


def main(argv):
    explain = "--explain" in argv[1:]
    try:
        docs = [d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]
    except yaml.YAMLError as err:
        sys.stderr.write(f"required-secrets: рендер не разобран как YAML ({err}) — вывести требуемое не из чего\n")
        return 2
    if carriers(docs) == 0:
        sys.stderr.write("required-secrets: в рендере НОЛЬ документов с шаблоном пода — "
                         "прочитано ноль, а не требуется ноль\n")
        return 2
    need = required(docs)
    if explain:
        for name, form, owner in sorted(set(need)):
            print(f"{name}\t{form}\t{owner}")
    else:
        for name in sorted({n for n, _, _ in need}):
            print(name)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
