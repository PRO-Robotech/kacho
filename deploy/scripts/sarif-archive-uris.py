#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Перевод координат SARIF IaC-скана в пути checkout'а — до выгрузки в Code Scanning.

ПРЕДМЕТ. Находку внутри архива-чарта trivy адресует как `<архив>:<шаблон>`
(`cert-manager-approver-policy-v0.28.0.tgz:templates/deployment.yaml`) относительно
базы `ROOTPATH` = каталог scan-ref. Для разбора URI это строка со СХЕМОЙ
`cert-manager-approver-policy-v0.28.0.tgz`, и Code Scanning отвергает весь файл:
«SARIF URI scheme … did not match the checkout URI scheme "file"». Замер — задание
trivy волны #2977 (run 36867890066, job 110387905580): восемь таких отказов, а следом
НЕ исполнился гейтовый шаг fs. Шаблона внутри архива в checkout'е нет вовсе, так что
«правильного» пути к нему не существует; существует путь к самому архиву.

ЧТО ДЕЛАЕТСЯ. Каждая физическая координата формы `<архив>:<внутренний путь>`
переводится в путь архива ОТ КОРНЯ checkout'а (`deploy/helm/vendor/<архив>`), база
`ROOTPATH` с неё снимается, а `region` заменяется привязкой к началу файла
(`startLine: 1`): строки в исходном считаны по ОТРЕНДЕРЕННОМУ шаблону, к байтам архива
они отношения не имеют, и номер строки 34 у двоичного файла был бы ложью. Внутренний
путь и настоящая строка не теряются: прежняя область переносится в сообщение
координаты, а текст находки (`Artifact: …`) остаётся как был.

ПОЧЕМУ ПЕРЕВОД, А НЕ ОТКАЗ ОТ ВЫГРУЗКИ. Без выгрузки находки второго прохода
существовали бы только в журнале гейтового шага, который судит лишь CRITICAL/HIGH;
LOW/MEDIUM внешнего чарта (их сейчас восемь) не были бы видны нигде. Перевод
сохраняет их во вкладке Security и привязывает к файлу, который действительно лежит
в дереве.

ПОСТУСЛОВИЕ ПРОВЕРЯЕТСЯ, А НЕ ПРЕДПОЛАГАЕТСЯ. После перевода КАЖДАЯ координата
файла обязана: не нести схемы (кроме `file:`) и указывать на существующий файл
checkout'а. Иначе — код 1 с координатой: выгрузка отказала бы всё равно, только
позже и без имени виноватой строки. Нет файла отчёта или он не JSON — код 2.

Объём печатается: файлов, результатов, координат, переведённых.
"""
import json
import pathlib
import re
import sys
import urllib.parse

# Архивная часть координаты МОЖЕТ нести каталоги: гейт покрытия признаёт архив-чарт
# в любом подкаталоге `deploy/helm/vendor/`, и trivy пишет его как `sub/<архив>:…`.
# Первая редакция запрещала `/` в архивной части (H6): такая координата не
# переводилась, и шаг перевода краснел на архиве, который гейт счёл законным.
ARCHIVE_URI = re.compile(r"^(?P<archive>[^:]+?\.(?:tgz|tar\.gz|tar)):(?P<inner>.+)$",
                         re.IGNORECASE)
SCHEME = re.compile(r"^[A-Za-z][A-Za-z0-9+.-]*:")


def base_dir(run, base_id, checkout):
    """→ каталог базы `uriBaseId` относительно checkout'а (None — база не в нём)."""
    if not base_id:
        return pathlib.Path(".")
    uri = ((run.get("originalUriBaseIds") or {}).get(base_id) or {}).get("uri") or ""
    if not uri.startswith("file://"):
        return None
    path = pathlib.Path(urllib.parse.unquote(urllib.parse.urlparse(uri).path))
    try:
        return path.resolve().relative_to(checkout)
    except ValueError:
        return None


def _walk(obj):
    if isinstance(obj, dict):
        yield obj
        for v in obj.values():
            yield from _walk(v)
    elif isinstance(obj, list):
        for v in obj:
            yield from _walk(v)


def locations(run):
    """→ (location, physicalLocation) КАЖДОЙ координаты файла в результатах — не только
    `results[].locations`, но и `relatedLocations`, `codeFlows`, `fixes` и прочих мест,
    где SARIF несёт `physicalLocation`: Code Scanning проверяет их все."""
    for node in _walk(run.get("results") or []):
        phys = node.get("physicalLocation")
        if isinstance(phys, dict) and isinstance(phys.get("artifactLocation"), dict):
            yield node, phys


def all_artifact_locations(run):
    """→ каждый `artifactLocation` прогона (и `artifacts[].location`) — для постусловия."""
    for node in _walk(run):
        art = node.get("artifactLocation")
        if isinstance(art, dict):
            yield art
    for item in run.get("artifacts") or []:
        if isinstance(item, dict) and isinstance(item.get("location"), dict):
            yield item["location"]


def postcondition(doc, checkout):
    """→ [нарушения] по ЗАПИСАННОМУ документу: каждая координата файла без схемы
    (кроме `file:`) и указывает на существующий файл checkout'а."""
    bad = []
    for run in doc.get("runs") or []:
        for art in all_artifact_locations(run):
            uri, base_id = art.get("uri") or "", art.get("uriBaseId")
            if SCHEME.match(uri) and not uri.startswith("file:"):
                bad.append("%s — координата несёт схему «%s», Code Scanning её отвергнет"
                           % (uri, uri.split(":", 1)[0]))
                continue
            base = base_dir(run, base_id, checkout)
            target = None if base is None else checkout / base / uri
            if target is None or not target.is_file():
                bad.append("%s — файла по этой координате в checkout'е нет" % uri)
    return bad


def rewrite(doc, checkout):
    """→ (координат, переведено, [отказы перевода]). Постусловие — `postcondition`,
    и судится оно по документу, ПРОЧИТАННОМУ из записанного файла (F1): прежняя
    редакция судила по переменным перевода и не видела координат вне
    `results[].locations` — они уходили в выгрузку непереведёнными."""
    seen, moved, bad = 0, 0, []
    for run in doc.get("runs") or []:
        for loc, phys in locations(run):
            art = phys["artifactLocation"]
            seen += 1
            uri, base_id = art.get("uri") or "", art.get("uriBaseId")
            m = ARCHIVE_URI.match(uri)
            if not m:
                continue
            base = base_dir(run, base_id, checkout)
            if base is None:
                bad.append("%s — база %s вне checkout'а, перевести не во что" % (uri, base_id))
                continue
            art["uri"] = (base / m.group("archive")).as_posix()
            art.pop("uriBaseId", None)
            old = phys.pop("region", None) or {}
            phys["region"] = {"startLine": 1}
            loc["message"] = {"text": "%s:%s, строки %s–%s отрендеренного шаблона" % (
                m.group("archive"), m.group("inner"),
                old.get("startLine", "?"), old.get("endLine", "?"))}
            moved += 1
    return seen, moved, bad


def main(argv):
    if len(argv) < 2:
        print("ОТКАЗ: укажи файлы SARIF", file=sys.stderr)
        return 2
    checkout = pathlib.Path.cwd().resolve()
    total_seen = total_moved = total_results = 0
    findings = []
    for name in argv[1:]:
        path = pathlib.Path(name)
        try:
            doc = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as err:
            print("ОТКАЗ: %s не прочитан как SARIF: %s" % (name, err), file=sys.stderr)
            return 2
        seen, moved, bad = rewrite(doc, checkout)
        results = sum(len(r.get("results") or []) for r in doc.get("runs") or [])
        try:
            path.write_text(json.dumps(doc, ensure_ascii=False, indent=2), encoding="utf-8")
            written = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as err:
            print("ОТКАЗ: %s не записан или не перечитан: %s" % (name, err), file=sys.stderr)
            return 2
        bad += postcondition(written, checkout)
        total_seen, total_moved, total_results = (total_seen + seen, total_moved + moved,
                                                  total_results + results)
        findings += ["%s: %s" % (name, b) for b in bad]
        print("  %s: результатов %d; координат %d; переведено из архивной формы %d"
              % (name, results, seen, moved))
    print("sarif-archive-uris: файлов %d; результатов %d; координат %d; переведено %d; "
          "нарушений постусловия %d" % (len(argv) - 1, total_results, total_seen,
                                        total_moved, len(findings)))
    if findings:
        print("\nНАХОДКИ:")
        for f in findings:
            print("  " + f)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
