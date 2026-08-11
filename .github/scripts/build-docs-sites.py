#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""build-docs-sites — собрать КАЖДЫЙ сайт документации и судить по коду возврата.

ЧТО ЛОВИТ. Восемь сайтов документации (по одному у края и каждого сервиса; каталог
сайта опознаётся своим `docusaurus.config.ts`, а не выписанным именем) не собирал
никто: ни один workflow их не упоминал, цели в Makefile не было, образы
собирались только для сервисов. Правки содержания уезжали в ствол, не пройдя ни одной
проверки, — а у Docusaurus проверок как раз достаточно, чтобы это было обидно:
`onBrokenLinks`/`onBrokenAnchors: 'throw'` роняют сборку на ссылке в никуда.

ТРИ ТРЕБОВАНИЯ, каждое выведено воспроизведением, а не соображением.

1. ВЕРДИКТ — КОД ВОЗВРАТА, НИКОГДА НЕ НАЛИЧИЕ `build/`. Провалившаяся сборка
   Docusaurus **оставляет полный каталог результата**: замерено — `rc=1`,
   «Docusaurus found broken links!», и при этом в `build/` лежат готовые страницы
   на мегабайты. Конвейер, который копирует `build/` «раз он есть», опубликует
   непроверенное и не заметит. Поэтому здесь единственный источник вердикта —
   `returncode` процесса сборки; существование каталога не спрашивается вовсе.

2. «0 БИТЫХ ССЫЛОК» ПОКРЫВАЕТ МЕНЬШЕ, ЧЕМ ЗВУЧИТ. Docusaurus проверяет
   markdown-ссылки и `<Link>`. Сырой `<a href="/чего-нет">` в MDX собирается
   **зелёным и молча** — это обычный HTML-атрибут, до роутера он не доходит.
   Такие якоря в дереве есть (их пишут в таблицах и кнопках hero), поэтому их
   цели сверяются здесь: путь — с построенным деревом маршрутов, фрагмент — с
   идентификаторами на своей же странице.

3. НОЛЬ НАХОДОК ОБЯЗАН БЫТЬ ОТЛИЧИМ ОТ НУЛЯ ПРОЧИТАННОГО. Печатается объём
   осмотренного: сколько сайтов найдено и собрано, сколько страниц выдал каждый,
   сколько сырых якорей проверено. Пустой перечень сайтов — ОТКАЗ, а не успех:
   «собрали ноль сайтов» иначе неотличимо от «всё зелено».

ПРОВЕРКА СВОЕЙ ПРЕДПОСЫЛКИ. Пункт 2 осмыслен ровно пока сборка вообще падает на
битых ссылках. Поэтому перед сборкой сверяется, что каждый конфиг несёт
`onBrokenLinks: 'throw'` И `onBrokenAnchors: 'throw'`: понизить их до `'warn'`
можно одной строкой, и тогда весь остальной гейт останется зелёным, проверяя
воздух. Понижение — находка.

ЧЕГО НЕ ПРОВЕРЯЕТ (осознанно): внешние ссылки (сеть в гейте — источник флейка,
а недоступность чужого сайта не есть дефект нашего дерева); фактическую верность
текста относительно кода (это работа ревью, у неё нет механического предиката).

Прогон локально: `python3 .github/scripts/build-docs-sites.py`
Только проверка конфигов, без сборки: `--configs-only`
"""

from __future__ import annotations

import argparse
import os
import pathlib
import re
import shutil
import subprocess
import sys

# ── что считается сайтом ────────────────────────────────────────────────────────
# Перечень ВЫВОДИТСЯ из дерева, а не выписывается: рукописный список разошёлся бы
# с деревом молча, и новый сайт остался бы вне гейта, ничем себя не выдав.
#
# ПРИЗНАК САЙТА — ЕГО КОНФИГ, А НЕ ИМЯ КАТАЛОГА. Прежде здесь стояло имя `docs-site`
# в двух шаблонах, то есть перечень выводился из дерева лишь наполовину: переименование
# каталога документации вывело бы все восемь сайтов из-под гейта, и он отчитался бы
# «сайтов найдено: 0» — отказом, но только потому, что пустой перечень объявлен отказом
# ниже. Опознание по `docusaurus.config.ts` переживает любую раскладку: каталог
# компонента — на глубине 1 (`gateway/`) или 2 (`services/<svc>/`), сайт — сразу под ним.
SITE_GLOBS = ("*/*/docusaurus.config.ts", "services/*/*/docusaurus.config.ts")

REQUIRED_CONFIG = (
    # (ключ, требуемое значение) — оба обязаны стоять на 'throw', см. §ПРЕДПОСЫЛКА.
    ("onBrokenLinks", "throw"),
    ("onBrokenAnchors", "throw"),
)

ANCHOR_RE = re.compile(r"<a\b[^>]*?\bhref=[\"']([^\"']+)[\"']", re.IGNORECASE)
ID_RE = re.compile(r'\bid="([^"]+)"')
FRONTMATTER_SLUG_RE = re.compile(r"^slug:\s*(\S+)\s*$", re.MULTILINE)

# Каталог страниц объявляет сам сайт — ключ `path` пресета docs. Регистр значим:
# `sidebarPath`/`routeBasePath` под `\bpath:` не попадают (там заглавная `P`).
DOCS_PATH_RE = re.compile(r"\bpath:\s*['\"]([^'\"]+)['\"]")
DOCS_PATH_DEFAULT = "docs"  # умолчание Docusaurus, когда ключ не задан

SOURCE_SUFFIXES = (".md", ".mdx", ".tsx", ".jsx")


def find_sites(root: pathlib.Path) -> list[pathlib.Path]:
    sites: set[pathlib.Path] = set()
    for glob in SITE_GLOBS:
        for cfg in root.glob(glob):
            sites.add(cfg.parent)
    return sorted(sites)


def docs_dir(site: pathlib.Path) -> pathlib.Path:
    """Каталог страниц сайта — ЧИТАЕТСЯ ИЗ КОНФИГА, не выписывается здесь.

    Гейт проверяет якори по исходникам страниц, поэтому обязан смотреть туда же,
    куда смотрит сборка. Выписанное имя разошлось бы с конфигом молча: несуществующий
    каталог даёт пустой обход, «сырых якорей проверено 0» и зелёный вердикт — форма
    без содержания. Расхождение ловится проверкой существования в `check_config`.
    """
    cfg = (site / "docusaurus.config.ts").read_text(encoding="utf-8")
    m = DOCS_PATH_RE.search(cfg)
    return site / (m.group(1) if m else DOCS_PATH_DEFAULT)


def check_config(site: pathlib.Path) -> list[str]:
    """Предпосылка гейта: сборка обязана падать на битой ссылке и битом якоре."""
    cfg = (site / "docusaurus.config.ts").read_text(encoding="utf-8")
    findings = []
    for key, want in REQUIRED_CONFIG:
        m = re.search(rf"\b{key}:\s*['\"]([a-z]+)['\"]", cfg)
        if m is None:
            findings.append(
                f"PREMISE {site}/docusaurus.config.ts: нет {key} — "
                f"умолчание Docusaurus лишь предупреждает, гейт проверял бы воздух"
            )
        elif m.group(1) != want:
            findings.append(
                f"PREMISE {site}/docusaurus.config.ts: {key}='{m.group(1)}', требуется '{want}' — "
                f"иначе битая ссылка проезжает сборку молча"
            )
    d = docs_dir(site)
    if not d.is_dir():
        findings.append(
            f"PREMISE {site}/docusaurus.config.ts: каталог страниц '{d.name}' не существует — "
            f"обход якорей прочитал бы ноль файлов и промолчал"
        )
    return findings


def route_of_source(site: pathlib.Path, path: pathlib.Path) -> str | None:
    """Маршрут построенной страницы для исходного файла каталога страниц.

    Возвращает None, если файл лежит вне этого каталога — тогда фрагмент к странице
    не привязать, и это докладывается как отказ предпосылки, а не проглатывается.
    """
    try:
        rel = path.relative_to(docs_dir(site))
    except ValueError:
        return None
    text = path.read_text(encoding="utf-8", errors="replace")
    head = text.split("---", 2)[1] if text.startswith("---") else ""
    m = FRONTMATTER_SLUG_RE.search(head)
    if m:
        slug = m.group(1).strip("'\"")
        return "/" + slug.strip("/")
    parts = list(rel.parts)
    parts[-1] = re.sub(r"\.(md|mdx)$", "", parts[-1])
    if parts[-1] == "index":
        parts.pop()
    return "/" + "/".join(parts)


def page_file(build: pathlib.Path, route: str) -> pathlib.Path | None:
    r = route.split("?")[0].split("#")[0].strip("/")
    cand = build / r / "index.html" if r else build / "index.html"
    if cand.is_file():
        return cand
    direct = build / r
    return direct if direct.is_file() else None


def check_raw_anchors(site: pathlib.Path) -> tuple[int, list[str]]:
    """Сырые <a href> — слепая зона штатного link-checker'а Docusaurus."""
    build = site / "build"
    findings: list[str] = []
    checked = 0
    sources = [
        p
        for sub in (docs_dir(site), site / "src")
        for p in sub.rglob("*")
        if p.is_file() and p.suffix in SOURCE_SUFFIXES
    ]
    for src in sorted(sources):
        text = src.read_text(encoding="utf-8", errors="replace")
        for href in ANCHOR_RE.findall(text):
            if href.startswith(("http://", "https://", "//", "mailto:", "tel:")):
                continue  # внешние — осознанно вне предмета
            if href.startswith("{"):
                continue  # JSX-выражение: значение вычисляется, статически не разрешимо
            checked += 1
            rel = src.relative_to(site)
            if href.startswith("#"):
                route = route_of_source(site, src)
                if route is None:
                    findings.append(
                        f"PREMISE {site}/{rel}: якорь '{href}' вне docs/ — "
                        f"страницу для него не вывести, проверить нечем"
                    )
                    continue
                pg = page_file(build, route)
                if pg is None:
                    findings.append(
                        f"PREMISE {site}/{rel}: страница '{route}' не найдена в build/ — "
                        f"вывод маршрута из пути исходника перестал работать"
                    )
                    continue
                ids = set(ID_RE.findall(pg.read_text(encoding="utf-8", errors="replace")))
                if href[1:] not in ids:
                    findings.append(
                        f"BROKEN-ANCHOR {site}/{rel}: '{href}' — на странице '{route}' нет такого id"
                    )
            elif href.startswith("/"):
                if page_file(build, href) is None:
                    findings.append(
                        f"BROKEN-LINK {site}/{rel}: '{href}' — такого маршрута нет в build/"
                    )
            else:
                findings.append(
                    f"UNCHECKABLE {site}/{rel}: относительный href '{href}' — "
                    f"пиши абсолютный путь от корня сайта, иначе цель ничем не сверить"
                )
    return checked, findings


def run(cmd: list[str], cwd: pathlib.Path, env: dict[str, str]) -> int:
    print(f"      $ {' '.join(cmd)}", flush=True)
    return subprocess.run(cmd, cwd=cwd, env=env).returncode


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".", help="корень монорепо")
    ap.add_argument("--configs-only", action="store_true", help="только предпосылка, без сборки")
    ap.add_argument(
        "--only",
        default=None,
        help="собрать лишь сайты, чей путь содержит подстроку (для отладки; "
        "перечень и предпосылка всё равно считаются по ВСЕМ — объём осмотренного не сужается)",
    )
    ap.add_argument(
        "--keep-node-modules",
        action="store_true",
        help="не удалять node_modules после сайта (по умолчанию удаляются — держит пик диска ограниченным)",
    )
    args = ap.parse_args()

    root = pathlib.Path(args.root).resolve()
    sites = find_sites(root)

    # Объём осмотренного — ОТДЕЛЬНОЕ утверждение: «ноль сайтов» обязано быть отказом,
    # иначе оно неотличимо от «все зелёные».
    print(f"сайтов найдено: {len(sites)}")
    for s in sites:
        print(f"  · {s.relative_to(root)}")
    if not sites:
        print("ОТКАЗ: не найдено ни одного docs-site — перечень выводится из дерева, "
              "и пустой результат означает сломанный поиск, а не отсутствие работы")
        return 1

    findings: list[str] = []
    for s in sites:
        findings += check_config(s)
    print(f"\nпредпосылка (onBrokenLinks/onBrokenAnchors='throw'): "
          f"проверено конфигов {len(sites)}, находок {len(findings)}")
    for f in findings:
        print(f"  ✗ {f}")

    if args.configs_only:
        return 1 if findings else 0

    env = dict(os.environ)
    env.setdefault("CI", "true")

    targets = [s for s in sites if args.only is None or args.only in str(s)]
    if args.only is not None and not targets:
        print(f"ОТКАЗ: --only '{args.only}' не выбрал ни одного сайта из {len(sites)}")
        return 1

    built = 0
    total_pages = 0
    total_anchors = 0
    for s in targets:
        rel = s.relative_to(root)
        print(f"\n───── {rel} ─────", flush=True)

        rc = run(["npm", "ci", "--no-audit", "--no-fund"], s, env)
        if rc != 0:
            findings.append(f"INSTALL {rel}: npm ci вернул {rc}")
            continue

        # ВЕРДИКТ — ТОЛЬКО ЭТОТ КОД. Наличие build/ не спрашивается: провалившаяся
        # сборка оставляет каталог целиком (замерено), и «раз есть — значит собралось»
        # опубликовало бы непроверенное.
        rc = run(["npm", "run", "build"], s, env)
        if rc != 0:
            findings.append(f"BUILD {rel}: сборка вернула {rc} (каталог build/ при этом мог остаться — он не вердикт)")
            if not args.keep_node_modules:
                shutil.rmtree(s / "node_modules", ignore_errors=True)
            continue

        built += 1
        pages = sum(1 for _ in (s / "build").rglob("index.html"))
        total_pages += pages
        anchors, anchor_findings = check_raw_anchors(s)
        total_anchors += anchors
        findings += anchor_findings
        print(f"      rc=0 · страниц {pages} · сырых якорей проверено {anchors} · "
              f"находок по якорям {len(anchor_findings)}")
        for f in anchor_findings:
            print(f"      ✗ {f}")

        if not args.keep_node_modules:
            shutil.rmtree(s / "node_modules", ignore_errors=True)

    print("\n══════ ИТОГ ══════")
    print(f"сайтов осмотрено : {len(sites)}")
    print(f"сайтов к сборке  : {len(targets)}" + (f"  (--only '{args.only}')" if args.only else ""))
    print(f"сайтов собрано   : {built}  (rc=0)")
    print(f"страниц выдано   : {total_pages}")
    print(f"сырых якорей     : {total_anchors} проверено")
    print(f"находок          : {len(findings)}")
    for f in findings:
        print(f"  ✗ {f}")
    if built != len(targets) or findings:
        print("ОТКАЗ")
        return 1
    print("OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
