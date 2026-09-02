#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# outcome-contract-inject.sh — ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ ДЛЯ СВЕДЁННОЙ ЧЕТВЁРКИ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЭТО ДОКАЗЫВАЕТ
#
# Четыре проверки посадки — перепись адресов административного API, политика его
# порта, форма адреса соседа и фасад боевого профиля — держали КАЖДАЯ СВОЮ копию
# различения «условие не создано» против «находка о дереве». Копии сведены к общей
# реализации (`outcome.sh`). Сведение обязано было ничего не отнять, и это
# утверждение проверяется здесь ВЫЗОВОМ, а не чтением диффа: у каждой из четырёх
# спрашивают ОБЕ стороны.
#
#   сторона «условие не создано» — отнять предпосылку (инструмент, чарт
#     зависимости, свежесть архива) ⇒ код 2 и НЕПУСТОЙ текст, называющий причину;
#   сторона «находка о дереве»   — внести настоящий дефект в копию дерева
#     ⇒ код 1 и КООРДИНАТА (стек, адрес, ручка профиля) в выводе.
#
# Гейт, доказанный с одной стороны, ловит форму, а не существо: проверка, которая
# на всё отвечает «условие не создано», прошла бы левую половину и была бы при
# этом полностью слепой.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЖИВОЙ РАБОЧЕЙ КОПИИ НЕ КАСАЕТСЯ ВОВСЕ
#
# Каждый случай получает СВОЁ зеркало: символьные ссылки на всё дерево, кроме
# умбреллы, — она копируется (`cp -a`, с сохранением времён: без них проверка
# свежести архивов сработала бы на самой копии и все случаи покраснели бы по
# причине, к предмету не относящейся). Дефект вносится ТОЛЬКО в копию.
#
# Отдельная осторожность со свежестью: исходники локальных зависимостей лежат ВНЕ
# умбреллы (`file://../../../services/*/deploy`) и в зеркале остаются символьными
# ссылками на живое дерево. Поэтому «архив старше исходника» создаётся не
# прикосновением к исходнику, а СТАРЕНИЕМ АРХИВА в копии.
#
# ─────────────────────────────────────────────────────────────────────────────
# КАК ОТНИМАЕТСЯ ИНСТРУМЕНТ
#
# Не правкой PATH «вырезать каталог» — helm лежит в /usr/sbin, yq в ~/.local/bin,
# python3 в /usr/bin, и вырезание каталога унесло бы половину оболочки. Вместо
# этого собирается КАТАЛОГ-ЗАМЕНА со ссылками на всё нужное, и из него убирается
# ровно один инструмент. Тогда `command -v <инструмент>` отвечает отказом, а всё
# остальное работает — то есть измеряется отсутствие инструмента, а не поломка
# окружения.
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"

# ── ПРЕДПОСЫЛКА: ЗАВИСИМОСТИ УМБРЕЛЛЫ МАТЕРИАЛИЗОВАНЫ (задача #1769) ─────────
# Спрашивается по ЖИВОМУ дереву и ДО первого зеркала: четыре сведённые проверки
# рендерят чарт, и на несобранных зависимостях каждая отвечает кодом 2 — тем
# самым, который здесь ОЖИДАЕТСЯ от случаев «отнятая предпосылка». Тогда левая
# половина проходит вакуумно, а правая («внесённый дефект → находка») краснеет
# по чужой причине. Замер до правки: 8 случаев из 18 упали именно так.
# shellcheck source=tests/helm/premise.sh
. "$HERE/premise.sh" || { echo "ОТКАЗ: библиотека предпосылки не подключилась — молчаливый пропуск предпосылки хуже её отсутствия"; exit 1; }
premise_chart_deps

RC=0
CHECKED=0
WORKROOT="$(mktemp -d)"
trap 'rm -rf "$WORKROOT"' EXIT

# ── каталог-замена PATH ──────────────────────────────────────────────────────
STUB="$WORKROOT/stub"
mkdir -p "$STUB"
STUB_TOOLS='bash sh env sed grep awk cat ls head tail sort uniq tr wc mktemp rm
mkdir cp ln find xargs git python3 helm yq tar dirname basename date timeout comm
cut diff chmod touch readlink seq expr id uname tee printf realpath md5sum stat'
for t in $STUB_TOOLS; do
  p="$(command -v "$t" 2>/dev/null)" || continue
  ln -sfn "$p" "$STUB/$t"
done
for t in helm yq python3 git; do
  [ -e "$STUB/$t" ] || { echo "FATAL: в каталоге-замене нет $t — отнимать нечего, доказательство было бы вакуумным"; exit 2; }
done

# ── зеркало ──────────────────────────────────────────────────────────────────
link_except() { # <src> <dst> <пропустить…>
  local src="$1" dst="$2"; shift 2
  local e b skip
  mkdir -p "$dst"
  for e in "$src"/* "$src"/.[!.]*; do
    [ -e "$e" ] || continue
    b="$(basename "$e")"
    for skip in "$@"; do [ "$b" = "$skip" ] && continue 2; done
    ln -sfn "$e" "$dst/$b"
  done
}

mirror() { # <имя> → печатает путь зеркала
  local dst="$WORKROOT/$1"
  link_except "$REPO_ROOT" "$dst" deploy
  link_except "$DEPLOY_ROOT" "$dst/deploy" helm tests
  link_except "$DEPLOY_ROOT/tests" "$dst/deploy/tests" helm
  link_except "$HERE" "$dst/deploy/tests/helm"
  link_except "$DEPLOY_ROOT/helm" "$dst/deploy/helm" umbrella
  # `-a`, а не `-r`: времена файлов здесь ЗНАЧИМЫ (свежесть архивов зависимостей).
  cp -a "$DEPLOY_ROOT/helm/umbrella" "$dst/deploy/helm/umbrella"
  printf '%s' "$dst"
}

# ── проба ────────────────────────────────────────────────────────────────────
probe() { # <метка> <ожидаемый код> <обязательная подстрока> <каталог> <скрипт> [PATH]
  local label="$1" want_rc="$2" want_txt="$3" dir="$4" script="$5" path="${6:-$PATH}"
  local out rc
  CHECKED=$((CHECKED + 1))
  out="$(cd "$dir/deploy" && PATH="$path" timeout 900 bash "tests/helm/$script" 2>&1)" && rc=0 || rc=$?
  if [ "$rc" -ne "$want_rc" ]; then
    echo "  ✗ $label — код $rc, ожидался $want_rc"
    printf '%s\n' "$out" | tail -12 | sed 's/^/      /'
    RC=1; return
  fi
  case "$out" in
    *"$want_txt"*) echo "  ✓ $label — код $rc, назван «$want_txt»" ;;
    *) echo "  ✗ $label — код $rc верен, но в выводе НЕТ «$want_txt»:"
       printf '%s\n' "$out" | tail -12 | sed 's/^/      /'
       RC=1 ;;
  esac
}

echo "=== $SCRIPT: сведённая четвёрка — обе стороны различения ==="
echo "    зеркало: $WORKROOT (живая рабочая копия не трогается)"

# ═════════════════════════════════════════════════════════════════════════════
# 1. prod-profile-fail-closed — фасад боевого профиля
# ═════════════════════════════════════════════════════════════════════════════
echo
echo "── prod-profile-fail-closed-test.sh ──"
M="$(mirror pp-clean)"
probe "законный вход → зелено" 0 "PASS: prod-profile-fail-closed-test.sh" "$M" prod-profile-fail-closed-test.sh

# Отнят helm. До сведения этой проверки в файле НЕ БЫЛО ВОВСЕ: код 2 получался
# по случайности — через отказ первого же рендера.
rm -f "$STUB/helm"
probe "нет helm → условие не создано" 2 "нужен helm" "$M" prod-profile-fail-closed-test.sh "$STUB"
ln -sfn "$(command -v helm)" "$STUB/helm"

rm -f "$STUB/yq"
probe "нет yq → условие не создано" 2 "нет yq" "$M" prod-profile-fail-closed-test.sh "$STUB"
ln -sfn "$(command -v yq)" "$STUB/yq"

M="$(mirror pp-nofile)"
rm -f "$M/deploy/helm/umbrella/values.prod.yaml"
probe "нет профиля на диске → условие не создано" 2 "не найден на диске" "$M" prod-profile-fail-closed-test.sh

# ДЕФЕКТ: боевой профиль перестал ограждать хранилища. Координата — ручка.
M="$(mirror pp-defect)"
python3 - "$M/deploy/helm/umbrella/values.prod.yaml" <<'PY'
import io,sys
p=sys.argv[1]; s=io.open(p,encoding='utf-8').read()
old="networkPolicy:\n  datastore:\n    enabled: true\n"
assert old in s, "точка инъекции не найдена — доказательство было бы вакуумным"
io.open(p,'w',encoding='utf-8').write(s.replace(old,"networkPolicy:\n  datastore:\n    enabled: false\n",1))
PY
probe "дефект профиля → находка с координатой" 1 "networkPolicy.datastore.enabled" "$M" prod-profile-fail-closed-test.sh

# ═════════════════════════════════════════════════════════════════════════════
# 2. admin-hop-address-census — перепись потребителей адреса
# ═════════════════════════════════════════════════════════════════════════════
echo
echo "── admin-hop-address-census-test.sh ──"
M="$(mirror ac-clean)"
probe "законный вход → зелено" 0 "PASS: admin-hop-address-census-test.sh" "$M" admin-hop-address-census-test.sh

rm -f "$STUB/python3"
probe "нет python3 → условие не создано" 2 "python3" "$M" admin-hop-address-census-test.sh "$STUB"
ln -sfn "$(command -v python3)" "$STUB/python3"

M="$(mirror ac-nochart)"
rm -f "$M"/deploy/helm/umbrella/charts/hydra-*.tgz
rm -rf "$M/deploy/helm/umbrella/charts/hydra"
probe "чарт провайдера не материализован → условие не создано" 2 "не материализован" "$M" admin-hop-address-census-test.sh

# ДЕФЕКТ: нагрузка, называющая адрес перехода и не объявленная ни одной записью
# реестра, — ровно тот класс, ради которого перепись написана.
M="$(mirror ac-defect)"
cat > "$M/deploy/helm/umbrella/templates/zz-inject.yaml" <<'TPL'
apiVersion: batch/v1
kind: Job
metadata:
  name: zz-inject-unaccounted
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: probe
          image: busybox
          env:
            - name: ADMIN_URL
              value: "https://{{ .Release.Name }}-hydra-admin-tls:4445"
TPL
probe "потребитель вне реестра → находка с координатой" 1 "zz-inject-unaccounted" "$M" admin-hop-address-census-test.sh

# ═════════════════════════════════════════════════════════════════════════════
# 3. admin-hop-port-policy — порт перехода против политик
# ═════════════════════════════════════════════════════════════════════════════
echo
echo "── admin-hop-port-policy-test.sh ──"
M="$(mirror pol-clean)"
probe "законный вход → зелено" 0 "PASS: admin-hop-port-policy-test.sh" "$M" admin-hop-port-policy-test.sh

rm -f "$STUB/python3"
probe "нет python3 → условие не создано" 2 "python3" "$M" admin-hop-port-policy-test.sh "$STUB"
ln -sfn "$(command -v python3)" "$STUB/python3"

M="$(mirror pol-nochart)"
rm -f "$M"/deploy/helm/umbrella/charts/hydra-*.tgz
rm -rf "$M/deploy/helm/umbrella/charts/hydra"
probe "чарт провайдера не материализован → условие не создано" 2 "не материализован" "$M" admin-hop-port-policy-test.sh

# ДЕФЕКТ: сосед перестал объявлять схему исходного запроса. Провайдер за ним
# начал бы считать переход открытым, и координата — стек.
M="$(mirror pol-defect)"
python3 - "$M/deploy/helm/umbrella/templates/_hydra-admin-tls.tpl" <<'PY'
import io,sys
p=sys.argv[1]; s=io.open(p,encoding='utf-8').read()
old="    proxy_set_header X-Forwarded-Proto https;\n"
assert old in s, "точка инъекции не найдена — доказательство было бы вакуумным"
io.open(p,'w',encoding='utf-8').write(s.replace(old,"",1))
PY
probe "сосед не объявляет схему → находка с координатой" 1 "X-Forwarded-Proto" "$M" admin-hop-port-policy-test.sh

# ═════════════════════════════════════════════════════════════════════════════
# 4. neighbour-address-form — форма адреса соседа
# ═════════════════════════════════════════════════════════════════════════════
echo
echo "── neighbour-address-form-test.sh ──"
M="$(mirror nb-clean)"
probe "законный вход → зелено" 0 "PASS: neighbour-address-form-test.sh" "$M" neighbour-address-form-test.sh

rm -f "$STUB/helm"
probe "нет helm → условие не создано" 2 "нужен helm" "$M" neighbour-address-form-test.sh "$STUB"
ln -sfn "$(command -v helm)" "$STUB/helm"

M="$(mirror nb-nocharts)"
rm -rf "$M/deploy/helm/umbrella/charts"
probe "зависимости не материализованы → условие не создано" 2 "не материализованы" "$M" neighbour-address-form-test.sh

# УНИКАЛЬНЫЙ СЛУЧАЙ ЭТОГО ФАЙЛА, перенесённый в общую реализацию: рендер УДАЁТСЯ
# и непуст, но собран по архиву СТАРШЕ своего исходника — то есть описывает
# ПРОШЛОЕ состояние дерева. Стареет архив в копии: исходники в зеркале —
# символьные ссылки на живое дерево, и трогать их нельзя.
M="$(mirror nb-stale)"
touch -d '2001-01-01 00:00:00' "$M"/deploy/helm/umbrella/charts/*.tgz
probe "архив старше исходника → условие не создано" 2 "НОВЕЕ собранного архива" "$M" neighbour-address-form-test.sh

# ДЕФЕКТ: адрес соседа полной формы, которого нет в перечне законных.
M="$(mirror nb-defect)"
cat > "$M/deploy/helm/umbrella/templates/zz-inject.yaml" <<'TPL'
apiVersion: v1
kind: ConfigMap
metadata:
  name: zz-inject-badform
data:
  peer: "zz-inject.kacho.svc.cluster.local:9090"
TPL
probe "адрес дефектной формы → находка с координатой" 1 "zz-inject" "$M" neighbour-address-form-test.sh

# ═════════════════════════════════════════════════════════════════════════════
echo
# Число объявлено, а не выведено из самого обхода: иначе проба, не дошедшая до
# вызова, уменьшила бы и знаменатель — и «исполнено всё» стало бы истинным by
# construction. Разбивка: по 1 законному входу на скрипт (4) + отнятые
# предпосылки (3 у профиля, 2 у переписи, 2 у политики, 3 у формы = 10) +
# по 1 внесённому дефекту на скрипт (4).
echo "случаев проверено: $CHECKED (законных входов 4, отнятых предпосылок 10, внесённых дефектов 4)"
[ "$CHECKED" -eq 18 ] || { echo "FAIL: исполнено $CHECKED случаев из 18 — часть проб не дошла до вызова"; RC=1; }
[ $RC -eq 0 ] && echo "PASS: $SCRIPT" || echo "FAIL: $SCRIPT"
exit $RC
