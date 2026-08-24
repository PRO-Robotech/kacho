#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Соединение фильтра видимости vpc обязано быть защищено В КАЖДОМ профиле, где
# vpc поднимается в боевом режиме.
#
# Фильтр видимости спрашивает `AuthorizeService.BatchCheck` о том, какие объекты
# вызывающий увидит в List. Это ОТДЕЛЬНОЕ соединение от ребра per-RPC Check: у
# него свой адрес (публичный листенер iam против внутреннего) и свои ручки
# транспорта, поэтому защита Check-ребра его не покрывает. Boot-гардрейл vpc
# (ValidatePeerTransport) в любом боевом режиме ОТКАЗЫВАЕТ старту, пока это
# соединение поднимается без проверяемого транспорта.
#
# Отсюда — эта проверка. Забытая ручка в профиле означает не «чуть менее строгую
# посадку», а под, который не поднимется вовсе; узнать об этом на рендере дешевле,
# чем на стенде. Проверка читает ОТРЕНДЕРЕННЫЙ манифест профиля, а не значения по
# отдельности: доставка идёт двумя разными путями (переменная окружения ребра и
# ключ в настройках), и утверждать надо о том, что реально доедет до процесса.
set -uo pipefail
# Состав стендов — из ЕДИНСТВЕННОЙ таблицы дерева (deploy/stacks.txt).
# Своей копии цепочек здесь нет: копии разъезжались молча.
. "$(dirname "$0")/stacks.sh"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UMBRELLA="$(cd "$HERE/../../helm/umbrella" && pwd)"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано (плюс текст, который сказал сам helm). Прежде «профиль не
# отрендерился» объявлялось находкой — тем же кодом 1, что и настоящий дефект
# посадки, — и несобранные зависимости умбреллы читались как дефект vpc (#1214).
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_helm

# Профили, которыми РЕАЛЬНО поднимается стенд, — из единственной таблицы дерева.
# `<имя>|<файлы через запятую>`. Здесь стоял свой список из трёх стеков: новый
# стенд приходил бы под проверку только правкой этого файла, то есть его «ноль
# находок» был бы «ноль прочитанного» ровно до тех пор, пока кто-нибудь не
# вспомнит.
PROFILES="$(stacks_table | tr ':' '|')"

# render <файлы через запятую> <имя профиля> — манифест профиля в $HELM_OUT.
# Отказ рендера — УСЛОВИЕ прогона, а не свойство дерева: код 2 и текст helm.
render() {
  local args=() f
  local IFS=,
  for f in $1; do args+=(-f "$UMBRELLA/$f"); done
  unset IFS
  helm_try kacho-umbrella "$UMBRELLA" "${args[@]}"
  render_or_fatal "профиль $2"
}

# inspect <файл-манифеста> — печатает три поля через пробел: режим, включён ли
# фильтр, защищено ли его соединение. Разбирается ВЕСЬ рендер: Deployment vpc
# (переменные окружения) и ConfigMap vpc (ключи настроек).
#
# Манифест передаётся ФАЙЛОМ, а не потоком: у `python3 - <<PY` на stdin лежит сам
# скрипт, поэтому чтение stdin внутри него вернуло бы пусто — и проверка
# отрапортовала бы «ок», не осмотрев ни одного объекта. Так и вышло при первом
# прогоне: три профиля из трёх «прошли» с нераспознанным режимом.
inspect() {
  python3 - "$1" <<'PY'
import sys, yaml

mode = ""
filter_on = None
armed = False

def walk_config(text):
    """Настройки vpc приходят YAML-документом внутри ConfigMap."""
    global mode, filter_on
    try:
        cfg = yaml.safe_load(text) or {}
    except Exception:
        return
    if not isinstance(cfg, dict):
        return
    authn = cfg.get("authn") or {}
    if isinstance(authn, dict) and authn.get("mode"):
        mode = str(authn["mode"])
    authz = cfg.get("authz") or {}
    lf = (authz.get("list-filter") or {}) if isinstance(authz, dict) else {}
    if isinstance(lf, dict) and "enabled" in lf:
        filter_on = bool(lf["enabled"])
    if isinstance(lf, dict):
        tls = lf.get("authorize-tls") or {}
        if isinstance(tls, dict) and tls.get("enable") is True:
            globals()["armed"] = True

with open(sys.argv[1], encoding="utf-8") as fh:
    manifest = fh.read()

seen_docs = 0
for doc in yaml.safe_load_all(manifest):
    seen_docs += 1
    if not isinstance(doc, dict):
        continue
    name = ((doc.get("metadata") or {}).get("name") or "")
    kind = doc.get("kind") or ""
    if kind == "ConfigMap" and "vpc" in name:
        for v in (doc.get("data") or {}).values():
            if isinstance(v, str):
                walk_config(v)
    if kind == "Deployment" and "vpc" in name and "operator" not in name:
        spec = ((doc.get("spec") or {}).get("template") or {}).get("spec") or {}
        for cnt in (spec.get("containers") or []):
            for e in (cnt.get("env") or []):
                k, v = e.get("name"), str(e.get("value", ""))
                if k == "KACHO_VPC_AUTH_MODE" and v:
                    mode = v
                if k == "KACHO_VPC_AUTHZ__LIST_FILTER__ENABLED":
                    filter_on = v.lower() == "true"
                # Обе ручки включают ОДИН и тот же путь с проверяемым
                # транспортом — ровно так их читает и сам сервис.
                if k in ("KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE",
                         "KACHO_VPC_AUTHZ__LIST_FILTER__AUTHORIZE_TLS__ENABLE") and v.lower() == "true":
                    armed = True

# Перепись осмотренного — отдельное утверждение: «ничего не нашли» обязано
# отличаться от «нашли и всё хорошо».
if seen_docs == 0:
    print("?", "?", "bare", 0)
    sys.exit(0)
print(mode or "?", "on" if filter_on else ("off" if filter_on is not None else "?"),
      "armed" if armed else "bare", seen_docs)
PY
}

fails=0
examined=0
# Ожидание объявляется ДО обхода: «утверждений выполнено 0 из 0» зеленело бы на
# пустой таблице стендов.
EXPECTED_ASSERTIONS="$(printf '%s\n' "$PROFILES" | grep -c . || true)"
while read -r line; do
  [ -n "$line" ] || continue
  profile="${line%%|*}"
  files="${line#*|}"

  render "$files" "$profile"
  manifest="$HELM_OUT"
  # Успешный рендер, отдавший ПУСТО, — это уже свойство дерева, а не условие:
  # helm ответил, и ответил ничем.
  [ -n "$manifest" ] || fail "профиль $profile отрендерился ПУСТЫМ — проверка ничего не осмотрела"
  examined=$((examined + 1))

  tmp="$(mktemp)"
  printf '%s' "$manifest" > "$tmp"
  read -r mode filter armed docs <<<"$(inspect "$tmp")"
  rm -f "$tmp"
  echo "profile=$profile mode=$mode list-filter=$filter transport=$armed (объектов в рендере: $docs)"

  # Нераспознанный режим — это не «не боевой», а неразобранный манифест.
  if [ "$mode" = "?" ]; then
    echo "  FAIL — режим vpc в рендере не распознан; проверка не осмотрела сервис"
    fails=1
    continue
  fi

  case "$mode" in
    production|production-strict) ;;
    *) echo "  ok — не боевой режим, гардрейл не действует"; ok; continue ;;
  esac
  if [ "$filter" != "on" ]; then
    echo "  ok — фильтр видимости выключен, соединение не поднимается"
    ok
    continue
  fi
  if [ "$armed" != "armed" ]; then
    echo "  FAIL — боевой режим + включённый фильтр видимости, но транспорт его"
    echo "         соединения не вооружён: под НЕ ПОДНИМЕТСЯ (vpc отказывает старту)."
    echo "         Включите mtls.edges.iamAuthz=true либо"
    echo "         authz.listFilter.authorizeTls.enable=true в профиле $profile."
    fails=1
    continue
  fi
  echo "  ok — транспорт соединения фильтра вооружён"
  ok
done <<<"$PROFILES"

# «Ноль находок» обязано быть отличимо от «ноль осмотренного».
[ "$examined" -gt 0 ] || fatal "ни один профиль не осмотрен; проверка ничего не доказывает"

[ "$fails" -eq 0 ] || fail "$SCRIPT — профилей осмотрено $examined, нарушения перечислены выше"
outcome_verdict "профилей осмотрено: $examined"
