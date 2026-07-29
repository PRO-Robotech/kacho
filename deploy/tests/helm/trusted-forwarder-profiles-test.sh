#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# КАЖДЫЙ профиль стенда обязан сузить круг отправителей у КАЖДОГО сервиса,
# который принимает переданную личность.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ПРОВЕРКА ПО ПРОФИЛЯМ, А НЕ ПО СЕРВИСАМ
#
# Круг отправителей уже проверяется у kacho-iam и registry — каждым своим
# скриптом, и каждый смотрит СВОЙ чарт. Такая нарезка не отвечает на вопрос
# «есть ли хоть один сервис на этом стенде, который никого не сузил»: чарт может
# нести здоровое умолчание, а профиль — переопределить его пустым, и наоборот.
# Ровно так и вышло: у kacho-nlb умолчание чарта пустое (как у vpc), но vpc свой
# круг в dev-профиле объявлял, а nlb — нет. Шесть сервисов сужены, седьмой нет,
# и ни одна из существующих проверок такого вопроса не задавала.
#
# Поэтому здесь единица проверки — ПРОФИЛЬ ЦЕЛИКОМ: рендерим умбреллу ровно тем
# набором `-f`, которым поднимается стенд, и грузим круг у каждого сервиса из
# отрендеренного манифеста — то есть после всех слоёв values, а не из умолчания.
#
# ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ ГЕЙТА ПОСАДКИ. Тот спрашивает ЖИВОЙ процесс и потому
# авторитетнее, но приходит только после развёртывания. Этот — офлайновый, ловит
# то же самое в CI до деплоя. Оба нужны: живой ловит «под не перекатился»,
# офлайновый — «профиль так и не завели».
#
# ПУСТОЙ КРУГ — ЭТО НЕ «НЕ СУЖАЕМ», А «ДОВЕРЯЕМ ВСЕМ. Транспорт сверяет
# сертификат отправителя со списком ТОЛЬКО когда список непуст; на пустом он
# принимает переданную личность от любого пира, прошедшего проверку сертификата.
# Поэтому считаем НЕПУСТЫЕ записи, а не длину значения: строка "," и список из
# одних пустых строк дают ноль принимаемых транспортом записей и обязаны
# краснеть так же, как пустота.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

ASSERTIONS=0
FAILURES=0
fail() { echo "FAIL: $1"; FAILURES=$((FAILURES + 1)); }
ok()   { ASSERTIONS=$((ASSERTIONS + 1)); }
fatal() { echo "FATAL: $1"; exit 2; }

command -v helm >/dev/null 2>&1 || fatal "helm не найден"
command -v python3 >/dev/null 2>&1 || fatal "python3 не найден"
python3 -c 'import yaml' 2>/dev/null || fatal "нужен PyYAML (python3 -c 'import yaml')"

# ─────────────────────────────────────────────────────────────────────────────
# КАРТА ИЗМЕРЕНИЯ: где у каждого сервиса лежит круг.
#
# `<deployment>|<service>|<локатор>` — локатор либо `env:<ИМЯ_ПЕРЕМЕННОЙ>`
# (значение через запятую), либо `cm:<имя-ConfigMap>:<путь-в-config.yaml>`.
#
# Состав списка — это ОТВЕТ НА ВОПРОС «кто принимает переданную личность», и он
# совпадает с FORWARDER_NARROWING_REQUIRED в scripts/assert-production-posture.sh.
# api-gateway сюда НЕ входит намеренно: он личность не принимает, а чеканит из
# проверенного токена — требовать с него сужения значило бы красить сервис за
# отсутствие механизма, которого у него и не должно быть.
#
# ЛОКАТОР, КОТОРЫЙ НИЧЕГО НЕ НАШЁЛ, — ЭТО ОТКАЗ, А НЕ ПРОПУСК. Сервис волен
# переехать на другую ручку; тогда эта карта устареет, и «ничего не нашли»
# обязано читаться как «проверка не выполнена», иначе устаревший локатор
# превращает гейт в зелёную печать.
MEASURED="
compute|compute|env:KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS
kacho-geo|geo|env:KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS
kacho-storage|storage|env:KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS
registry|registry|env:KACHO_REGISTRY_AUTHZ_TRUSTED_FORWARDER_SANS
vpc|vpc|cm:vpc-config:authz.trusted-forwarder-sans
kacho-nlb|nlb|cm:kacho-nlb-config:authz.trusted-forwarder-sans
kacho-iam|iam|cm:kacho-iam-config:authn.trusted-forwarder-sans
"

# Профили, которыми РЕАЛЬНО поднимается стенд (см. deploy/Makefile: dev-up
# рендерит values.dev.yaml, dev-prod-up накладывает values.dev-prod.yaml поверх).
# `<имя>|<файлы через запятую>` — запятая, а не пробел: строки разбираются
# пословно, и файл со слоями иначе развалился бы на два «профиля».
#
# ВЕСЬ СМЫСЛ ЭТОЙ ПРОВЕРКИ В ТОМ, ЧТО ЕДИНИЦА ИЗМЕРЕНИЯ — НАБОР `-f`, КОТОРЫМ
# ПОДНИМАЮТСЯ. Значит боевой набор обязан быть среди измеряемых. Одного
# values.prod.yaml для этого недостаточно: боевой кластер поднимается им ПЛЮС
# двумя оверлеями (см. шапку values.fe3455-prod.yaml и шаг рендера в CI), а
# круг отправителей — величина послойная: слой волен переопределить здоровое
# умолчание нижнего. Профиль, которым никто не поднимается, и профиль, которым
# поднимаются, — разные вопросы, и мерить надо второй.
PROFILES="
dev|values.dev.yaml
dev-prod|values.dev.yaml,values.dev-prod.yaml
prod|values.prod.yaml
fe3455|values.prod.yaml,values.fe3455.yaml,values.fe3455-prod.yaml
"

# Четвёртый слой боевого набора — values.fe3455-ory.yaml — НЕ в репозитории
# (несёт учётные данные стенда, см. его шапку). Поэтому он добавляется к набору
# fe3455, только когда файл на месте: в CI меряются три слоя, на машине, где
# файл есть, — все четыре. Что именно измерено, печатается ниже — «измерили
# меньше» не должно выглядеть как «измерили всё».
OPTIONAL_ORY="values.fe3455-ory.yaml"
if [ -f "$UMBRELLA/$OPTIONAL_ORY" ]; then
  PROFILES="$PROFILES
fe3455+ory|values.prod.yaml,values.fe3455.yaml,values.fe3455-prod.yaml,$OPTIONAL_ORY"
fi

# render <файлы-через-запятую> — рендер умбреллы; пустой вывод считаем сорванным.
render() {
  local args=() f
  local IFS=,
  for f in $1; do args+=(-f "$UMBRELLA/$f"); done
  unset IFS
  helm template kacho-umbrella "$UMBRELLA" "${args[@]}" 2>/dev/null
}

# ─────────────────────────────────────────────────────────────────────────────
# Извлечение круга из ОТРЕНДЕРЕННОГО манифеста, по ВСЕМ путям доставки.
#
# Пути доставки перечислены не для полноты, а потому что через них уже утекал
# дефект: массовый импорт `envFrom` был слепым пятном гейта посадки, и сервис,
# получавший настройки только так, не осматривался вовсе.
#
# Печатает по строке `<service> <кол-во-непустых-записей>`; `-1` = локатор не
# разрешился (объект есть, ручки нет), пустая строка = объекта нет в рендере.
# ─────────────────────────────────────────────────────────────────────────────
extract() {
  MEASURED="$MEASURED" python3 - "$1" <<'PY'
import os, sys, yaml

render = open(sys.argv[1]).read()
docs = [d for d in yaml.safe_load_all(render) if isinstance(d, dict)]

cms = {d["metadata"]["name"]: (d.get("data") or {})
       for d in docs if d.get("kind") == "ConfigMap"}
deps = {d["metadata"]["name"]: d
        for d in docs if d.get("kind") == "Deployment"}


def env_of(dep):
    """Every env name→value of every container, from all delivery paths."""
    out = {}
    spec = dep["spec"]["template"]["spec"]
    for c in (spec.get("containers") or []) + (spec.get("initContainers") or []):
        for src in c.get("envFrom") or []:
            ref = (src.get("configMapRef") or {}).get("name")
            if ref in cms:
                out.update(cms[ref])
        for e in c.get("env") or []:
            if "value" in e:
                out[e["name"]] = e["value"]
            ref = ((e.get("valueFrom") or {}).get("configMapKeyRef") or {})
            if ref.get("name") in cms:
                out[e["name"]] = cms[ref["name"]].get(ref["key"], "")
    return out


def nonempty(entries):
    return len([s for s in entries if str(s).strip() != ""])


for row in os.environ["MEASURED"].split():
    dep_name, svc, locator = row.split("|", 2)
    dep = deps.get(dep_name)
    if dep is None:
        print(svc, "")            # сервис не разворачивается этим профилем
        continue
    kind, rest = locator.split(":", 1)
    if kind == "env":
        env = env_of(dep)
        if rest not in env:
            print(svc, -1)
            continue
        print(svc, nonempty(env[rest].split(",")))
    else:
        cm_name, path = rest.split(":", 1)
        data = cms.get(cm_name)
        if data is None or "config.yaml" not in data:
            print(svc, -1)
            continue
        node = yaml.safe_load(data["config.yaml"])
        for part in path.split("."):
            if not isinstance(node, dict) or part not in node:
                node = None
                break
            node = node[part]
        if node is None:
            print(svc, -1)
            continue
        print(svc, nonempty(node if isinstance(node, list) else str(node).split(",")))
PY
}

# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан ПОКРАСНЕТЬ на воспроизведённом дефекте.
#
# Без неё проверка доказывает лишь, что она отработала — а не что она способна
# отличить сужённый круг от пустого. Инъекция ровно та, что и была в жизни:
# профиль объявляет kacho-nlb пустой круг.
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== self-test: инъекция пустого круга у kacho-nlb — в КАЖДЫЙ объявленный профиль ==="
  # Инъекция прогоняется по ВСЕМ профилям из PROFILES, а не по одному dev.
  # Иначе самопроверка доказывала бы работоспособность гейта на профиле, который
  # в список внесли первым, и молчала бы о том, что остальные строки списка,
  # возможно, не измеряются вовсе — а именно этим и был дефект: боевой набор,
  # которым поднимается кластер, в списке отсутствовал, и никто этого не видел.
  # Теперь добавить профиль в список, не измеряя его, нельзя: самопроверка
  # потребует, чтобы внесённый в него дефект стал виден.
  inj="$(mktemp)"; trap 'rm -f "$inj"' EXIT
  cat >"$inj" <<'EOF'
kacho-nlb:
  config:
    authz:
      trustedForwarderSANs: []
EOF
  rc=0
  checked=0
  for profile in $PROFILES; do
    name="${profile%%|*}"; files="${profile#*|}"
    args=(); IFS=,
    for f in $files; do args+=(-f "$UMBRELLA/$f"); done
    unset IFS
    r="$(helm template kacho-umbrella "$UMBRELLA" "${args[@]}" -f "$inj" 2>/dev/null)"
    if [ -z "$r" ]; then
      echo "  ПРОВАЛ $name: рендер с инъекцией сорвался — профиль объявлен, но не измеряется"
      rc=1; continue
    fi
    tmp="$(mktemp)"; printf '%s' "$r" >"$tmp"
    got="$(extract "$tmp" | awk '$1=="nlb"{print $2}')"
    rm -f "$tmp"
    checked=$((checked + 1))
    if [ "$got" = "0" ]; then
      echo "  ОК     $name: пустой круг извлечён как 0 записей → гейт покраснеет"
    else
      echo "  ПРОВАЛ $name: при пустом круге извлечено '$got', ожидалось 0 —"
      echo "         на этом профиле гейт не отличает пустой круг от сужённого."
      rc=1
    fi
  done

  # «Ноль находок» обязано быть отличимо от «ноль осмотренного».
  if [ "$checked" -eq 0 ]; then
    echo "FAIL: не проверено ни одного профиля — список PROFILES пуст или не разбирается."
    exit 1
  fi
  echo
  if [ $rc -eq 0 ]; then
    echo "PASS: $SCRIPT --self-test (профилей проверено: $checked)"
  else
    echo "FAIL: $SCRIPT --self-test"
  fi
  exit $rc
fi

# ─────────────────────────────────────────────────────────────────────────────
echo "=== круг отправителей чужой личности: по профилям стенда ==="

for profile in $PROFILES; do
  name="${profile%%|*}"; files="${profile#*|}"
  echo "--- профиль $name ($files)"

  tmp="$(mktemp)"
  render "$files" >"$tmp"
  if [ ! -s "$tmp" ]; then
    rm -f "$tmp"
    fail "профиль $name не рендерится — измерение НЕ ВЫПОЛНЕНО (пустой рендер это не «чисто»)"
    continue
  fi
  if ! measured="$(extract "$tmp")" || [ -z "$measured" ]; then
    rm -f "$tmp"
    fail "профиль $name: разбор рендера СОРВАЛСЯ — измерение НЕ ВЫПОЛНЕНО"
    continue
  fi
  rm -f "$tmp"

  while read -r svc count; do
    [ -z "$svc" ] && continue
    case "$count" in
      "")  echo "  — $name/$svc: не разворачивается этим профилем" ;;
      -1)  fail "$name/$svc: ручка круга отправителей НЕ НАЙДЕНА в рендере — карта измерения в $SCRIPT устарела, и «не нашли» здесь означает «не проверили», а не «всё хорошо»" ;;
      0)   fail "$name/$svc: круг отправителей ПУСТ — переданную личность примет ЛЮБОЙ пир с сертификатом внутреннего CA. Профиль обязан объявить круг по фактическим отправителям сервиса." ;;
      *)   ok; echo "  ✓ $name/$svc: круг сужен ($count записей)" ;;
    esac
  done <<EOF
$measured
EOF
done

echo
if [ "$FAILURES" -ne 0 ]; then
  echo "FAILED: $SCRIPT — $FAILURES нарушений"
  exit 1
fi
[ "$ASSERTIONS" -gt 0 ] || fatal "ни одного утверждения не выполнено — гейт ничего не проверил"
echo "PASS: $SCRIPT ($ASSERTIONS assertions)"
