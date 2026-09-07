#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Каждый volumeMount обязан иметь свой volume — во ВСЕХ комбинациях тумблеров.

ЗАЧЕМ. `helm template` этого не ловит, и `helm lint` тоже: манифест с монтом на
несуществующий том рендерится успешно и валится только на apiserver — уже при
деплое:

    Deployment.apps "compute" is invalid:
      spec.template.spec.containers[0].volumeMounts[0].name: Not found: "tmp"

Поймано на себе: секция `volumes:` у compute была гейтована
`{{- if or .Values.opa.enabled .Values.mtls.enable }}`, и при обоих false (ровно
профиль umbrella-фазы 1) том исчезал, а монт на него оставался. Standalone-рендер
проблему не показывал — там ДЕФОЛТНЫЕ values, где тумблеры включены. Поэтому
проверяется МАТРИЦА, а не дефолт.

ТОМ БЫВАЕТ НЕ ТОЛЬКО В `volumes:`. У StatefulSet имя тома объявляет ещё и
`volumeClaimTemplates`, и это ОСНОВНАЯ форма для набора с состоянием. Анализатор
про неё не знал, поэтому совершенно корректный StatefulSet реестра читался как
«монт без тома» — то есть распространить гейт на чарт реестра было нельзя, не
починив это. Обратная ошибка («StatefulSet — значит всегда можно») запрещена
тем же предикатом: имя обязано быть объявлено ЛИБО в `volumes`, ЛИБО в
`volumeClaimTemplates`, и мимо обоих списков не проходит ничего.

ОБЪЁМ ОСМОТРЕННОГО ОБЪЯВЛЯЕТСЯ. «Ноль находок» обязано быть отличимо от «ноль
прочитанного»: рендер без единого workload'а — ПРОВАЛ (сломанный набор флагов,
переименованный шаблон), а не чистота. Так же и покрытие: таблица чартов
сверяется с first-party зависимостями умбреллы, поэтому новый сервисный чарт не
может тихо оказаться вне гейта, а запись без предмета не может в нём остаться.

ГЕЙТ ПРОВЕРЯЕТ СВОЮ ПРЕДПОСЫЛКУ. Предикат тут дизъюнктивный — имя тома объявлено
`volumes` ЛИБО `volumeClaimTemplates`, — и вторая его половина держится на одном
единственном наборе с состоянием во всём дереве. Если этот рендер перестанет
происходить (чарт отказал, тумблер убрали из матрицы), про заявки на том гейт
перестанет доказывать что бы то ни было, оставаясь сплошь зелёным: находок ноль,
потому что предмет не читался. Поэтому число осмотренных claim-backed workload'ов
печатается отдельно, а ноль — ПРОВАЛ. Ровно это и случилось: чарт реестра завёл
fail-closed отказ рендера без учётных данных хранилища слоёв, таблица гейта их не
несла, и все рендеры с zot умерли до анализа — набор с состоянием не осматривался
НИ РАЗУ, при живом комментарии о том, что матрица его покрывает.

Вынесен из bash-обёртки отдельным файлом: inline-python в шелле уже дал два
дефекта — `python3 - <<'PY' <<<"$out"` (второй redirect перекрывает первый, python
читает ДАННЫЕ как скрипт) и молчаливое «✓» при падении анализатора.

Запуск:       bash .github/scripts/check-volume-mounts.sh
Один рендер:  helm template … | python3 .github/scripts/check-volume-mounts.py --stdin
Самопроверка: --self-test — инъекции в синтетические манифесты, helm не нужен.
"""
from __future__ import annotations

import pathlib
import subprocess
import sys

import yaml

REPO = pathlib.Path(__file__).resolve().parents[2]
UMBRELLA_CHART = REPO / "deploy/helm/umbrella/Chart.yaml"

WORKLOADS = ("Deployment", "StatefulSet", "Job", "DaemonSet", "CronJob")

# Чарты-склейки: workload'ов с монтами у них нет вовсе, поэтому матрицы им не
# нужно. Это ИСКЛЮЧЕНИЕ, и оно живёт, пока у него есть предмет: пропажа чарта из
# зависимостей умбреллы — находка (см. coverage_findings). Запись подчарта
# начальной настройки движка прав снята вместе с ним (S6 эпика #747) — ровно по
# этому правилу: исключать стало нечего.
NO_WORKLOAD_CHARTS = {"kratos-selfservice-ui"}


class Chart:
    """Один чарт: где лежит, чем рендерится и какие тумблеры гейтят тома.

    `required` — значения, без которых чарт ОСОЗНАННО не рендерится (у kacho-nlb
    нет и не должно быть дефолтного пароля; гейт обязан это удовлетворить, а не
    наказывать за правильный fail-closed дизайн). `toggles` — только те ключи,
    чьи условия реально охватывают `volumes:` / `volumeMounts:` /
    `volumeClaimTemplates:` либо отдельные записи внутри них; они выведены
    разбором шаблонов, а не угаданы, и по ним строится ПОЛНАЯ матрица.
    """

    def __init__(self, name: str, path: str, required: list[str],
                 toggles: list[tuple[str, str, str]]):
        self.name = name
        self.path = path
        self.required = required
        self.toggles = toggles

    def renders(self):
        """Все комбинации тумблеров: (подпись, список аргументов --set)."""
        for mask in range(1 << len(self.toggles)):
            sets, label = list(self.required), []
            for i, (key, off, on) in enumerate(self.toggles):
                val = on if mask >> i & 1 else off
                sets += ["--set", "{}={}".format(key, val)]
                label.append("{}={}".format(key, val if val else '""'))
            yield " ".join(label) or "дефолт", sets


CHARTS = [
    Chart("vpc", "services/vpc/deploy",
          ["--set", "image=x", "--set", "name=vpc"],
          [("opa.enabled", "false", "true"), ("mtls.enable", "false", "true"),
           ("mtls.edges.geo", "false", "true"), ("mtls.edges.iamAuthz", "false", "true"),
           ("mtls.edges.iamProject", "false", "true"),
           ("mtls.edges.iamRegister", "false", "true")]),
    Chart("compute", "services/compute/deploy",
          ["--set", "image=x", "--set", "name=compute"],
          [("opa.enabled", "false", "true"), ("mtls.enable", "false", "true")]),
    Chart("kacho-nlb", "services/nlb/deploy",
          ["--set", "db.password=x"],
          [("mtls.enable", "false", "true"), ("migrator.enable", "false", "true")]),
    Chart("api-gateway", "gateway/deploy",
          ["--set", "image=x", "--set", "name=api-gateway"],
          [("tls.enabled", "false", "true"), ("mtls.enable", "false", "true"),
           # Не булев тумблер: имя секрета с якорем доверия к внутреннему CA —
           # пустое значение убирает том, непустое добавляет.
           ("hydra.adminCa.secretName", "", "internal-ca")]),
    # Учётные данные хранилища слоёв — `required`, ровно по той же причине, что
    # пароль БД у kacho-nlb: дефолта у них нет НАМЕРЕННО (чарт публичный, значит
    # «дефолт» = общеизвестный секрет), и без них чарт fail-closed отказывает в
    # рендере. Гейт обязан это удовлетворить, а не наказывать за правильный
    # отказ: без пароля все рендеры с zot умирают ДО анализа, и единственный в
    # дереве набор с состоянием не осматривается ни разу — при зелёном виде.
    # Значение намеренно выглядит чужим (как renderPassword в zot_authn_test.go),
    # чтобы правдоподобная фикстура не выдавала себя за рабочую настройку.
    # `username` дефолт имеет, поэтому не задаётся; `auth.existingSecret` —
    # альтернативная (боевая) форма того же требования, но тумблером ей тут не
    # место: секций томов она не гейтит, а в матрицу входят только те ключи,
    # чьи условия реально охватывают volumes / volumeMounts / volumeClaimTemplates.
    Chart("registry", "services/registry/deploy",
          ["--set", "zot.auth.password=render-guard-not-a-real-password"],
          [("mtls.enable", "false", "true"),
           # zot.enabled гейтит САМ StatefulSet, то есть тот единственный
           # workload платформы, чей том приходит из volumeClaimTemplates.
           # Условий вокруг секций томов он не несёт, но без обеих его ветвей
           # матрица не покрывает набор с состоянием ни в одном рендере.
           ("zot.enabled", "false", "true"),
           ("zot.storage.persistence", "false", "true"),
           ("service.dataplaneLB.enabled", "false", "true"),
           ("service.dataplaneLB.tlsSidecar.enabled", "false", "true")]),
    Chart("storage", "services/storage/deploy",
          ["--set", "image.repository=x", "--set", "image.tag=y"],
          [("migrator.enabled", "false", "true"), ("mtls.enable", "false", "true"),
           ("mtls.edges.geo", "false", "true"), ("mtls.edges.iam", "false", "true")]),
    Chart("kaname", "deploy/helm/umbrella/charts/kaname", [],
          [("mtls.enable", "false", "true"), ("mtls.httpListeners", "false", "true"),
           ("opaSidecar.enabled", "false", "true"),
           ("initContainer.migrator.enabled", "false", "true"),
           ("initContainer.waitForExtAuth.enabled", "false", "true")]),
    Chart("kacho-geo", "deploy/helm/umbrella/charts/kacho-geo", [],
          [("mtls.enable", "false", "true"), ("dataMigration.enabled", "false", "true")]),
    Chart("uif", "ui-future/deploy", [], []),
]


def pod_spec(doc):
    """Достаёт podSpec из любого workload-типа (CronJob вложен глубже)."""
    spec = doc.get("spec") or {}
    if doc.get("kind") == "CronJob":
        spec = ((spec.get("jobTemplate") or {}).get("spec")) or {}
    tmpl = spec.get("template") or {}
    return tmpl.get("spec") or {}


def claim_volume_names(doc) -> set:
    """Имена, объявленные заявками на том — вторая половина предиката.

    ТОЛЬКО у StatefulSet: `spec.volumeClaimTemplates[].metadata.name` создаёт PVC
    на каждую реплику, и монт на такое имя совершенно корректен. У прочих видов
    это поле не действует, и принимать его за объявление значило бы пропускать
    настоящий дефект (обе стороны заперты в self-test).
    """
    if doc.get("kind") != "StatefulSet":
        return set()
    names = set()
    for vct in ((doc.get("spec") or {}).get("volumeClaimTemplates") or []):
        if isinstance(vct, dict):
            name = (vct.get("metadata") or {}).get("name")
            if name:
                names.add(name)
    return names


def declared_volume_names(doc, sp) -> set:
    """Имена, которые ЭТОТ workload объявляет как тома.

    Два источника, и оба обязательны. `spec.template.spec.volumes` — обычный,
    `volumeClaimTemplates` — форма набора с состоянием (см. claim_volume_names).
    Читать только первый источник значит краснеть на правильном StatefulSet.
    """
    names = {v["name"] for v in (sp.get("volumes") or [])
             if isinstance(v, dict) and "name" in v}
    return names | claim_volume_names(doc)


def analyze(docs):
    """Находки, число workload'ов, контейнеров и workload'ов с заявками на том.

    Четвёртое число — предпосылка гейта, а не украшение: на нём держится вторая
    половина дизъюнкции «volumes ЛИБО volumeClaimTemplates». Ноль за весь прогон
    означает, что про заявки гейт не доказал ничего (см. шапку файла).
    """
    bad, workloads, containers, claim_backed = [], 0, 0, 0
    for d in docs:
        if not isinstance(d, dict) or d.get("kind") not in WORKLOADS:
            continue
        workloads += 1
        sp = pod_spec(d)
        if claim_volume_names(d):
            claim_backed += 1
        vols = declared_volume_names(d, sp)
        for c in (sp.get("containers") or []) + (sp.get("initContainers") or []):
            containers += 1
            mounts = {m["name"] for m in (c.get("volumeMounts") or [])
                      if isinstance(m, dict) and "name" in m}
            missing = mounts - vols
            if missing:
                name = (d.get("metadata") or {}).get("name", "?")
                bad.append("{} {} / контейнер {}: монт без тома {} (объявлены тома: {})".format(
                    d["kind"], name, c.get("name", "?"), sorted(missing), sorted(vols) or "нет"))
    return bad, workloads, containers, claim_backed


def analyze_text(text: str):
    return analyze(list(yaml.safe_load_all(text)))


def stdin_mode() -> int:
    """Анализ одного рендера со stdin — анализатор отдельно от драйвера."""
    try:
        bad, workloads, _, _ = analyze_text(sys.stdin.read())
    except yaml.YAMLError as e:
        print("не удалось разобрать YAML: {}".format(e), file=sys.stderr)
        return 2
    for line in bad:
        print(line)
    if not workloads:
        print("в рендере НЕТ ни одного workload'а — проверять было нечего, "
              "и это провал, а не чистота", file=sys.stderr)
        return 2
    return 1 if bad else 0


def coverage_findings() -> list:
    """Ни один сервисный чарт умбреллы не может оказаться вне гейта молча.

    Прежняя обёртка несла РУЧНОЙ список из четырёх чартов при восьми сервисных.
    Список, который никто не сверяет, отстаёт молча: новый чарт просто не
    проверяется, и об этом ничто не сообщает.
    """
    out = []
    try:
        dep_doc = yaml.safe_load(UMBRELLA_CHART.read_text(encoding="utf-8"))
    except OSError as e:
        return ["не прочитан {}: {} — покрытие НЕ проверено".format(UMBRELLA_CHART, e)]

    first_party = {d["name"] for d in (dep_doc.get("dependencies") or [])
                   if str(d.get("repository", "")).startswith("file://")}

    # СВОЙ ЧАРТ БЫВАЕТ ПРОВЯЗАН ДВУМЯ СПОСОБАМИ, и покрытие обязано видеть оба.
    #
    # Сабчарт, чьи исходники версионируются прямо в `charts/`, помечать
    # зависимостью НЕЛЬЗЯ: `file://./charts/<имя>` заставляет helm материализовать
    # второй экземпляр рядом с исходником, и какой из двух попадёт в рендер — не
    # определено (см. шапку deploy/helm/umbrella/Chart.yaml). Такие чарты объявлений
    # не несут — но своими они быть не перестают, и выпасть из покрытия по причине
    # «нет строки в dependencies» не должны: ровно так этот гейт и потерял бы
    # kaname с kacho-geo, не сказав ни слова.
    #
    # Признак — «каталог в charts/, который является чартом и которого НИКТО НЕ
    # ОБЪЯВЛЯЛ», а не список имён. Внешние чарты попадают в `charts/` только по
    # объявлению, поэтому необъявленный каталог там может быть только своим. Брать
    # просто «все каталоги в charts/» нельзя: старые версии helm распаковывали туда
    # и внешние (ingress-nginx, postgresql — они и перечислены в .gitignore), и
    # тогда гейт потребовал бы покрыть чужой чарт.
    declared = {d.get("alias") or d.get("name")
                for d in (dep_doc.get("dependencies") or [])} | \
               {d.get("name") for d in (dep_doc.get("dependencies") or [])}
    charts_dir = UMBRELLA_CHART.parent / "charts"
    vendored = set()
    if charts_dir.is_dir():
        vendored = {p.name for p in charts_dir.iterdir()
                    if p.is_dir() and (p / "Chart.yaml").is_file()
                    and p.name not in declared}
    first_party |= vendored

    if not first_party:
        return ["в {} не разобрано ни одной своей зависимости и не найдено ни одного "
                "вендоренного сабчарта — покрытие сверять не с чем, а это не "
                "«покрыто всё»".format(UMBRELLA_CHART)]

    covered = {c.name for c in CHARTS}
    for name in sorted(first_party - covered - NO_WORKLOAD_CHARTS):
        out.append("чарт '{}' — first-party зависимость умбреллы, но его нет в таблице этого "
                   "гейта: его монты не проверяются, и об этом ничто не сообщает".format(name))
    for name in sorted(covered - first_party):
        out.append("чарт '{}' есть в таблице гейта, но он больше не зависимость умбреллы — "
                   "запись без предмета, удали её".format(name))
    for name in sorted(NO_WORKLOAD_CHARTS - first_party):
        out.append("'{}' объявлен беззагрузочным исключением, но он больше не зависимость "
                   "умбреллы — запись без предмета, удали её".format(name))
    return out


def render(chart: Chart, sets: list):
    """Рендер чарта. Возвращает (манифесты | None, первая строка диагностики)."""
    p = subprocess.run(["helm", "template", "t", str(REPO / chart.path)] + sets,
                       capture_output=True, text=True)
    if p.returncode != 0:
        return None, (p.stderr or p.stdout).strip().split("\n")[0]
    return p.stdout, ""


def main() -> int:
    rc = 0
    for line in coverage_findings():
        print("  ✗ покрытие: {}".format(line))
        rc = 1

    renders = workloads_seen = claims_seen = 0
    for chart in CHARTS:
        for label, sets in chart.renders():
            manifests, err = render(chart, sets)
            if manifests is None:
                print("  ✗ {} ({}) — рендер упал: {}".format(chart.name, label, err))
                rc = 1
                continue
            try:
                bad, workloads, _, claim_backed = analyze_text(manifests)
            except yaml.YAMLError as e:
                print("  ✗ {} ({}) — YAML не разобран: {}".format(chart.name, label, e))
                rc = 1
                continue
            renders += 1
            workloads_seen += workloads
            claims_seen += claim_backed
            if not workloads:
                print("  ✗ {} ({}) — в рендере НЕТ workload'ов: проверять было нечего".format(
                    chart.name, label))
                rc = 1
            elif bad:
                print("  ✗ {} ({}):".format(chart.name, label))
                for b in bad:
                    print("      {}".format(b))
                rc = 1
            else:
                print("  ✓ {} ({})".format(chart.name, label))

    print("\nосмотрено: чартов {}, рендеров {}, workload'ов {} "
          "(из них с заявками на том {})".format(
              len(CHARTS), renders, workloads_seen, claims_seen))
    if renders == 0 or workloads_seen == 0:
        print("FAIL: гейт не осмотрел НИ ОДНОГО workload'а — это провал, а не чистота")
        return 1
    if claims_seen == 0:
        print("FAIL: за весь прогон не осмотрено НИ ОДНОГО workload'а с "
              "volumeClaimTemplates — вторая половина предиката («volumes ЛИБО "
              "volumeClaimTemplates») не исполнилась ни разу, значит про заявки на том "
              "гейт не доказал ничего, а его зелень про них пуста. Таблица объявляет "
              "такой рендер (registry, zot.enabled=true): либо этот рендер перестал "
              "происходить, либо тумблер ушёл из матрицы — разбирать надо это, а не "
              "снимать проверку.")
        return 1
    return rc


def self_test() -> int:
    """Инъекции в синтетические манифесты: гейт краснеет на дефекте и молчит на
    законной конструкции той же формы. helm не нужен."""
    rc = 0

    # want_claims утверждается в КАЖДОМ кейсе и по умолчанию 0: счётчик предпосылки
    # обязан быть способен как посчитать заявку, так и НЕ посчитать её там, где её
    # нет. Иначе «ноль claim-backed за прогон» неотличимо от сломанного счётчика, и
    # проверка предпосылки сама стала бы формой без содержания.
    def case(name, text, want, want_in="", want_claims=0):
        nonlocal rc
        bad, workloads, containers, claim_backed = analyze_text(text)
        ok = (len(bad) == want and (not want_in or any(want_in in b for b in bad))
              and claim_backed == want_claims)
        print("  {} {} (находок {}, ожидалось {}; workload'ов {}, контейнеров {}, "
              "с заявками {}, ожидалось {})".format(
                  "ОК    " if ok else "ПРОВАЛ", name, len(bad), want, workloads,
                  containers, claim_backed, want_claims))
        if not ok:
            for b in bad:
                print("        {}".format(b))
            rc = 1

    case("монт без тома → находка", """
kind: Deployment
metadata: {name: d}
spec:
  template:
    spec:
      containers:
        - name: app
          volumeMounts: [{name: tmp, mountPath: /tmp}]
""", 1, "tmp")

    case("монт с томом из volumes → молчание", """
kind: Deployment
metadata: {name: d}
spec:
  template:
    spec:
      containers:
        - name: app
          volumeMounts: [{name: tmp, mountPath: /tmp}]
      volumes: [{name: tmp, emptyDir: {}}]
""", 0)

    # Контроль, ради которого правился анализатор.
    case("StatefulSet: том из volumeClaimTemplates → молчание", """
kind: StatefulSet
metadata: {name: s}
spec:
  volumeClaimTemplates:
    - metadata: {name: storage}
      spec: {accessModes: [ReadWriteOnce]}
  template:
    spec:
      containers:
        - name: app
          volumeMounts: [{name: storage, mountPath: /var/lib/data}]
""", 0, want_claims=1)

    # И обратное: заявки не делают StatefulSet бланкетно правым.
    case("StatefulSet: монт мимо volumes И заявок → находка", """
kind: StatefulSet
metadata: {name: s}
spec:
  volumeClaimTemplates:
    - metadata: {name: storage}
      spec: {accessModes: [ReadWriteOnce]}
  template:
    spec:
      containers:
        - name: app
          volumeMounts:
            - {name: storage, mountPath: /var/lib/data}
            - {name: config, mountPath: /etc/app}
""", 1, "config", want_claims=1)

    case("initContainer судится так же → находка", """
kind: StatefulSet
metadata: {name: s}
spec:
  volumeClaimTemplates:
    - metadata: {name: storage}
      spec: {accessModes: [ReadWriteOnce]}
  template:
    spec:
      initContainers:
        - name: init
          volumeMounts: [{name: seed, mountPath: /seed}]
      containers:
        - name: app
          volumeMounts: [{name: storage, mountPath: /d}]
""", 1, "seed", want_claims=1)

    case("CronJob вложен глубже, монт всё равно виден → находка", """
kind: CronJob
metadata: {name: c}
spec:
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: app
              volumeMounts: [{name: tmp, mountPath: /tmp}]
""", 1, "tmp")

    # Заявка на том у НЕ-StatefulSet именем не объявляет: поле там не действует,
    # и принимать его за объявление значило бы пропускать настоящий дефект.
    case("Deployment с полем заявок: имя НЕ объявлено → находка", """
kind: Deployment
metadata: {name: d}
spec:
  volumeClaimTemplates:
    - metadata: {name: storage}
  template:
    spec:
      containers:
        - name: app
          volumeMounts: [{name: storage, mountPath: /d}]
""", 1, "storage")

    bad, workloads, _, _ = analyze_text("kind: ConfigMap\nmetadata: {name: c}\n")
    if workloads == 0 and not bad:
        print("  ОК     манифест без workload'ов даёт ноль осмотренного — отличимо от чистоты")
    else:
        print("  ПРОВАЛ учёт осмотренного: workload'ов {}, находок {}".format(workloads, len(bad)))
        rc = 1

    cov = coverage_findings()
    if cov:
        print("  ПРОВАЛ покрытие таблицы разошлось с умбреллой:")
        for line in cov:
            print("        {}".format(line))
        rc = 1
    else:
        print("  ОК     покрытие: все first-party чарты умбреллы в таблице ({})".format(len(CHARTS)))

    print()
    print("PASS: check-volume-mounts --self-test" if rc == 0
          else "FAIL: check-volume-mounts --self-test")
    return rc


if __name__ == "__main__":
    if "--self-test" in sys.argv[1:]:
        sys.exit(self_test())
    if "--stdin" in sys.argv[1:]:
        sys.exit(stdin_mode())
    sys.exit(main())
