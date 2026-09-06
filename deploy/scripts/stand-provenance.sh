#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-provenance.sh — какую РЕВИЗИЮ дерева исполняет каждый контейнер стенда.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# «Спроси стенд, что он исполняет» — первое действие при разборе красного: без
# ответа вердикт выносится про неизвестно что. Цена уже уплачена — разбор красного
# консоли стоил трёх ложных гипотез против стенда, исполнявшего две чужие ревизии
# сразу, а разбор удостоверения упёрся в поиск символов по бинарю внутри
# контейнера.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ФАЙЛ В ОБРАЗЕ, А НЕ КЛЕЙМО (и почему клеймо всё-таки осталось)
#
# Прежняя редакция читала клеймо `org.opencontainers.image.revision` у демона
# ХОСТА по идентификатору работающего образа. У этого пути две дыры, и обе
# наблюдались:
#
#   1. образа может не быть в демоне хоста. Локальный стенд держит образы в
#      containerd УЗЛА kind; чистка диска (`docker image prune`) уносит копию
#      хоста, а стенд продолжает работать. Замер 2026-08-25: образов `kacho-*` в
#      демоне хоста 0, в containerd узла 17 — и цель печатала «метки ревизии нет»
#      у ВСЕХ 18 контейнеров при живых, верных клеймах;
#   2. у управляемого кластера демона хоста нет by construction. Там образы
#      тянутся из реестра, и прочитать их клеймо можно только сетевым обращением
#      к реестру с учётными данными — то есть цель работала бы на одном стенде из
#      двух.
#
# Файл `/etc/kacho/image-revision` читается у РАБОТАЮЩЕГО контейнера одним
# `kubectl exec`, одинаково на kind и на управляемом кластере, без демона и без
# реестра. Он часть содержимого образа: подменить его объявлением пода нельзя, в
# отличие от переменной окружения.
#
# Отдельно про клеймо: на образе, собранном ОТ помеченной базы, клеймо того же
# имени приезжает ОТ БАЗЫ. Проверено на образе консоли: `image.source`,
# `image.title` и `maintainer` там принадлежат nginx. Значит клеймо, прочитанное
# без нашей сборки, выглядит нашим и им не является — поэтому запасной путь
# называет источник вслух, а не выдаёт величину за установленную.
#
# ─────────────────────────────────────────────────────────────────────────────
# КОДЫ ВОЗВРАТА
#
#   0  ревизия установлена у КАЖДОГО осмотренного контейнера
#   1  у части контейнеров установить не удалось — они названы поимённо
#   2  предпосылка исчезла: кластер недоступен либо контейнеров продукта ноль
#
# Код 2 отделён намеренно: «ноль находок» обязано быть отличимо от «ноль
# прочитанного». Прежняя редакция на недоступном кластере печатала ОДИН заголовок
# и выходила успехом — то есть молчание неотличимо от исправного стенда.
set -uo pipefail

NAMESPACE="${KACHO_NAMESPACE:-kacho}"
MODE="report"
EXEC_TIMEOUT="${KACHO_PROVENANCE_EXEC_TIMEOUT:-20}"

# REVISION_PATH — путь величины внутри образа. Объявлен ОДИН раз и здесь;
# Dockerfile'ы пишут этот же путь, и что все они его пишут, держит
# deploy/stand_provenance_declaration_test.go.
REVISION_PATH="/etc/kacho/image-revision"

usage() {
  cat <<'EOF'
stand-provenance.sh [--namespace <ns>] [--self-test]

  --namespace   пространство имён стенда (умолчание: kacho, либо KACHO_NAMESPACE)
  --self-test   доказательство инъекцией в обе стороны, без кластера
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --namespace) NAMESPACE="${2:?--namespace требует значения}"; shift 2 ;;
    --self-test) MODE="self-test"; shift ;;
    -h|--help)   usage; exit 0 ;;
    *) echo "неизвестный аргумент: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# ИМЕНА ЧАСТЕЙ ПРОДУКТА — ОДИН ИСТОЧНИК, И ЧИТАЮТСЯ ОНИ ЗДЕСЬ, А НЕ В ЦИКЛЕ.
#
# Читатель подключается по МЕСТУ ЭТОГО ФАЙЛА, а не относительно текущего
# каталога: цель зовут и из `deploy/`, и из корня дерева.
#
# Загрузка стоит ДО развилки режимов и вне `report`, потому что `report`
# исполняется в подоболочке (`out="$(report …)"`): загрузи мы внутри, ведомость
# умирала бы вместе с подоболочкой, а инструмент собирался бы на каждый вызов.
#
# Отказ источника ГРОМКИЙ и это код 2, а не 1: не прочитав имена, мы не знаем,
# какой контейнер наш, — то есть не выносим вердикта ни об одном. Молча считать
# всё чужим значило бы вернуть ровно ту слепоту, ради снятия которой источник и
# заведён.
# shellcheck source=deploy/scripts/lib/product-names.sh
. "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/lib/product-names.sh"
_dirs="$(product_service_dirs)" || {
  echo "ОТКАЗ: перечень частей продукта не прочитан — чей контейнер наш, неизвестно." >&2
  exit 2
}
# shellcheck disable=SC2086 -- перечень каталогов передаётся отдельными словами
product_names_load $_dirs || {
  echo "ОТКАЗ: имена частей продукта не прочитаны — вердикта нет ни об одном контейнере." >&2
  echo "       Это «не выполнилось», а не «наших контейнеров нет»." >&2
  exit 2
}

# ─────────────────────────────────────────────────────────────────────────────
# ЯДРО
# ─────────────────────────────────────────────────────────────────────────────

# containers_of <ns> — печатает «под<TAB>контейнер<TAB>образ<TAB>идентификатор»
# по КАЖДОМУ работающему контейнеру, нашему и чужому. Пустой вывод — не «чисто»:
# решает вызывающий по коду возврата kubectl, который здесь НЕ проглатывается.
#
# Отбор «наш/чужой» здесь НАМЕРЕННО не делается. Прежняя редакция отбирала
# выражением jq — и тем выводила чужие контейнеры из переписи МОЛЧА: отчёт не
# отличал «осмотрено 17» от «осмотрено 17 из 36». Хуже того, свойство было
# непроверяемо: самопроверка подменяет jq заглушкой, поэтому отбор не исполнялся
# ни на одной оси. Решает теперь is_our_image ниже — в оболочке, где инъекция до
# него дотягивается.
containers_of() {
  kubectl -n "$1" get pods -o json --request-timeout=30s \
    | jq -r '
        .items[]
        | select(.status.phase == "Running")
        | . as $pod
        | .status.containerStatuses[]?
        | [$pod.metadata.name, .name, .image, .imageID] | @tsv
      '
}

# is_our_image <образ> — образ продукта или чужой?
#
# ОТВЕТ СПРАШИВАЕТСЯ У ИСТОЧНИКА ИМЁН, А НЕ ВЫВОДИТСЯ ЗДЕСЬ ПРИСТАВКОЙ.
#
# Прежняя редакция судила одной приставкой платформы (`kacho-*`). Пока имя
# платформы было и именем каждой её части, это было верно; служба управления
# доступом получила СВОЁ имя продукта — и распознаватель ослеп ровно на ней.
# Слепота эта не красная и не зелёная, она МОЛЧАЛИВАЯ: чужого имени предикат не
# отвергает, он его не видит. Наблюдалось дословно — на стенде с тремя нашими
# подами цель печатала «работающих контейнеров ПРОДУКТА ноль», отнеся
# собственный образ в чужие и назвав его в списке посторонних.
#
# Судится по-прежнему ПОСЛЕДНИЙ сегмент ссылки, а не вся строка: и
# `docker.io/library/…`, и `prorobotech/…` — наши; `docker.io/bitnami/postgresql`,
# `hydra:v26.2.0` и голая ссылка по дайджесту `sha256:…` — чужие.
#
# Чужой образ метки провенанса нести НЕ ОБЯЗАН: он собран не нами. Поэтому он
# граница инструмента, а не находка, — но пропущен он вслух, числом и именем.
is_our_image() {
  product_image_is_ours "$1"
}

# revision_from_file <ns> <под> <контейнер> — печатает содержимое величины,
# возвращает 0 при успешном чтении (в т.ч. пустого файла) и 1 иначе.
#
# Причина отказа уходит в ФАЙЛ, а не в переменную: вызывающий берёт результат
# подстановкой команды, то есть функция исполняется в ПОДОБОЛОЧКЕ, и присваивание
# внутри неё до родителя не доезжает. Первая редакция так и делала — и печатала
# «не прочитан — ; клейма тоже нет», то есть отказ БЕЗ причины ровно там, где
# причина и есть весь ответ.
ERROR_SINK=""
revision_from_file() {
  local out rc
  out="$(kubectl -n "$1" exec "$2" -c "$3" --request-timeout="${EXEC_TIMEOUT}s" \
          -- cat "$REVISION_PATH" 2>&1)"
  rc=$?
  if [ "$rc" -ne 0 ]; then
    printf '%s' "$out" | tr '\n' ' ' | cut -c1-140 > "$ERROR_SINK"
    return 1
  fi
  : > "$ERROR_SINK"
  printf '%s' "$out" | tr -d '\r\n'
  return 0
}

# revision_from_label <идентификатор> — запасной путь: клеймо у демона хоста.
# Может вернуть УНАСЛЕДОВАННОЕ от базового образа значение, поэтому источник
# называется вызывающим вслух.
revision_from_label() {
  local sha="${1##*@}" out
  out="$(docker inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$sha" 2>/dev/null)"
  case "$out" in
    ""|"<no value>") return 1 ;;
  esac
  printf '%s' "$out"
}

report() {
  local ns="$1"
  local rows rc

  command -v kubectl >/dev/null || { echo "ОТКАЗ: kubectl не найден — спрашивать стенд нечем" >&2; return 2; }
  command -v jq      >/dev/null || { echo "ОТКАЗ: jq не найден — разбирать ответ нечем" >&2; return 2; }

  rows="$(containers_of "$ns")"; rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "ОТКАЗ: кластер не отвечает (пространство имён «$ns») — стенд не опрошен." >&2
    echo "       Это НЕ «ревизия сходится»: не прочитан ни один контейнер." >&2
    return 2
  fi
  if [ -z "$rows" ]; then
    echo "ОТКАЗ: работающих контейнеров в «$ns» ноль — осматривать нечего." >&2
    return 2
  fi

  # Разделение на наших и чужих — ДО заголовка: стенд из одних чужих это не
  # «ревизия сходится», а исчезнувшая предпосылка, и код у него 2, а не 0.
  local product_rows="" foreign_pods=0 foreign_raw=""
  local r_pod r_name r_image r_id
  while IFS=$'\t' read -r r_pod r_name r_image r_id; do
    [ -n "$r_pod" ] || continue
    if is_our_image "$r_image"; then
      product_rows="${product_rows}${r_pod}\t${r_name}\t${r_image}\t${r_id}\n"
    else
      foreign_pods=$((foreign_pods + 1))
      foreign_raw="${foreign_raw}${r_image##*/}\n"
    fi
  done < <(printf '%s\n' "$rows")

  local foreign_list foreign_seen
  foreign_list="$(printf '%b' "$foreign_raw" | sed '/^$/d' | sort -u | tr '\n' ' ')"
  foreign_seen="$(printf '%b' "$foreign_raw" | sed '/^$/d' | sort -u | grep -c . || true)"

  if [ -z "$product_rows" ]; then
    echo "ОТКАЗ: работающих контейнеров ПРОДУКТА в «$ns» ноль — осматривать нечего." >&2
    echo "       чужих при этом осмотрено: контейнеров $foreign_pods, различных образов $foreign_seen [$foreign_list]" >&2
    echo "       Это НЕ «ревизия сходится»: ни один наш образ не опрошен." >&2
    return 2
  fi

  # Шапка выписана строкой, а не выровнена printf: `printf %-16s` считает БАЙТЫ,
  # а кириллическая буква их занимает два — выровненная им шапка съезжает над
  # ровными столбцами и читается как поломка вывода.
  # Столбцов ТРИ, и третий свободный: `printf %-9s` считает БАЙТЫ, а «клеймо» их
  # занимает двенадцать при шести знаках — выровненный им столбец съезжает ровно
  # на кириллице, то есть всегда. Источник ушёл в начало свободного столбца.
  echo "КОНТЕЙНЕР        ОБРАЗ                              ИСТОЧНИК И РЕВИЗИЯ"
  ERROR_SINK="$(mktemp)"

  local seen=0 pods=0 by_file=0 by_label=0 unknown=0
  local prev_key="" pod name image imageid key rev
  # Сортировка по ключу (контейнер, образ, идентификатор): реплики одного
  # развёртывания несут ОДИН образ, и спрашивать каждую значило бы платить
  # временем за заведомо тот же ответ. Число подов при этом называется.
  while IFS=$'\t' read -r pod name image imageid; do
    [ -n "$pod" ] || continue
    pods=$((pods + 1))
    key="$name|$image|$imageid"
    if [ "$key" = "$prev_key" ]; then continue; fi
    prev_key="$key"
    seen=$((seen + 1))

    local src="нет"
    if rev="$(revision_from_file "$ns" "$pod" "$name")"; then
      if [ -n "$rev" ]; then
        src="файл"; by_file=$((by_file + 1))
      else
        rev="(величина ПУСТА: образ собран без --build-arg KACHO_IMAGE_REVISION)"
        unknown=$((unknown + 1))
      fi
    elif rev="$(revision_from_label "$imageid")"; then
      src="клеймо"; by_label=$((by_label + 1))
      rev="$rev  ← запасной путь: клеймо могло быть УНАСЛЕДОВАНО от базового образа"
    else
      rev="(не установлена: $REVISION_PATH не прочитан — $(cat "$ERROR_SINK"); клейма у демона хоста тоже нет)"
      unknown=$((unknown + 1))
    fi
    printf "%-16s %-34s [%s] %s\n" "$name" "${image##*/}" "$src" "$rev"
  done < <(printf '%b' "$product_rows" | sed '/^$/d' | sort -t$'\t' -k2,4)

  rm -f "$ERROR_SINK"
  echo
  echo "осмотрено: контейнеров всего $((pods + foreign_pods)) — продукта $pods (различных образов $seen), чужих $foreign_pods (различных образов $foreign_seen)"
  echo "ревизия установлена у $((by_file + by_label)) (по файлу $by_file, по клейму $by_label), НЕ установлена у $unknown"
  if [ "$foreign_seen" -gt 0 ]; then
    echo "чужие образы пропущены — метки провенанса нести не обязаны, это ГРАНИЦА инструмента, а не находка: $foreign_list"
  fi
  [ "$unknown" -eq 0 ] || return 1
  return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА — доказательство инъекцией в ОБЕ стороны.
#
# Кластера не требует: `kubectl` и `docker` подменяются заглушками в PATH, и
# ядро выше исполняется НАСТОЯЩЕЕ. Проба, читающая ядро глазами, доказывала бы
# только то, что оно написано.
# ─────────────────────────────────────────────────────────────────────────────
self_test() {
  local work; work="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "rm -rf '$work'" EXIT
  mkdir -p "$work/bin"
  local pass=0 fail=0 out rc

  ok()  { pass=$((pass + 1)); printf '  OK   %s\n' "$*"; }
  bad() { fail=$((fail + 1)); printf '  ПРОВАЛ %s\n' "$*"; }

  # Заглушка kubectl. Поведение задают файлы в $work:
  #   pods.tsv   — что вернёт `get pods` (уже в форме, которую даёт jq)
  #   getrc      — код возврата `get pods`
  #   exec.<контейнер> — содержимое величины; отсутствие файла = отказ exec
  cat > "$work/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
W="$KACHO_SELFTEST_WORK"
args=("$@")
for a in "${args[@]}"; do
  if [ "$a" = "exec" ]; then
    # kubectl -n NS exec POD -c NAME --request-timeout=... -- cat PATH
    c=""
    for ((i=0; i<${#args[@]}; i++)); do
      [ "${args[$i]}" = "-c" ] && c="${args[$((i+1))]}"
    done
    if [ -f "$W/exec.$c" ]; then cat "$W/exec.$c"; exit 0; fi
    echo "Error from server: container \"$c\" not found" >&2
    exit 1
  fi
done
cat "$W/pods.tsv"
exit "$(cat "$W/getrc")"
STUB
  # jq заглушке не нужен: pods.tsv уже в конечной форме, поэтому подменяется и он.
  cat > "$work/bin/jq" <<'STUB'
#!/usr/bin/env bash
cat
STUB
  # Заглушка docker: клеймо отдаёт только для идентификаторов, перечисленных в
  # labels.tsv (строка «<идентификатор> <ревизия>»).
  cat > "$work/bin/docker" <<'STUB'
#!/usr/bin/env bash
W="$KACHO_SELFTEST_WORK"
sha="${!#}"
if [ -f "$W/labels.tsv" ]; then
  while read -r id rev; do
    [ "$id" = "$sha" ] && { printf '%s' "$rev"; exit 0; }
  done < "$W/labels.tsv"
fi
echo "Error: No such object: $sha" >&2
exit 1
STUB
  chmod +x "$work/bin/kubectl" "$work/bin/jq" "$work/bin/docker"
  export KACHO_SELFTEST_WORK="$work"
  local saved_path="$PATH"
  PATH="$work/bin:$PATH"

  local rev40="c11f1d52b93471f7321683c516403def8ae632c8"

  fixture_two_good() {
    printf 'vpc-1\tvpc\tdocker.io/library/kacho-vpc:dev\tsha256:aaa\nui-1\tvpc\tdocker.io/library/kacho-ui-future-vpc:dev\tsha256:bbb\n' > "$work/pods.tsv"
    echo 0 > "$work/getrc"
    rm -f "$work"/exec.* "$work/labels.tsv"
    printf '%s\n' "$rev40" > "$work/exec.vpc"
  }

  echo "1. величина у каждого контейнера — код 0 (положительный контроль)"
  fixture_two_good
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" == *"$rev40"* ]] && [[ "$out" == *"НЕ установлена у 0"* ]]; then
    ok "код 0, ревизия напечатана, перепись сошлась"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "2. внесён дефект: у одного образа величины нет и клейма нет — код 1 и ИМЯ"
  fixture_two_good
  # два разных контейнера, у второго своё имя, для которого файла нет
  printf 'vpc-1\tvpc\tdocker.io/library/kacho-vpc:dev\tsha256:aaa\ngeo-1\tkacho-geo\tdocker.io/library/kacho-geo:dev\tsha256:ccc\n' > "$work/pods.tsv"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 1 ] && [[ "$out" == *"kacho-geo"* ]] && [[ "$out" == *"не установлена"* ]] \
     && [[ "$out" == *"НЕ установлена у 1"* ]]; then
    ok "код 1, координата названа, перепись назвала одного"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "3. законный близнец: файла нет, но есть клеймо — код 0 и ИСТОЧНИК назван"
  fixture_two_good
  printf 'vpc-1\tvpc\tdocker.io/library/kacho-vpc:dev\tsha256:aaa\ngeo-1\tkacho-geo\tdocker.io/library/kacho-geo:dev\tsha256:ccc\n' > "$work/pods.tsv"
  printf 'sha256:ccc %s\n' "$rev40" > "$work/labels.tsv"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" == *"клеймо"* ]] && [[ "$out" == *"УНАСЛЕДОВАНО"* ]] \
     && [[ "$out" == *"по клейму 1"* ]]; then
    ok "запасной путь сработал и назвал себя оговоркой"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "4. внесён дефект: образ собран без ревизии — величина ПУСТА, это не «прочитать не удалось»"
  fixture_two_good
  printf 'vpc-1\tvpc\tdocker.io/library/kacho-vpc:dev\tsha256:aaa\n' > "$work/pods.tsv"
  printf '\n' > "$work/exec.vpc"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 1 ] && [[ "$out" == *"ПУСТА"* ]] && [[ "$out" == *"KACHO_IMAGE_REVISION"* ]]; then
    ok "код 1 и названа ПРИЧИНА — сборка не проставила величину"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "5. исчезла предпосылка: кластер не отвечает — код 2, а НЕ молчаливый успех"
  fixture_two_good
  echo 1 > "$work/getrc"
  : > "$work/pods.tsv"
  out="$(report kacho 2>&1)"; rc=$?
  if [ "$rc" -eq 2 ] && [[ "$out" == *"не отвечает"* ]]; then
    ok "код 2 на недоступном кластере"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "6. исчезла предпосылка: контейнеров продукта ноль — код 2"
  fixture_two_good
  : > "$work/pods.tsv"
  out="$(report kacho 2>&1)"; rc=$?
  if [ "$rc" -eq 2 ] && [[ "$out" == *"ноль"* ]]; then
    ok "код 2 на пустом стенде"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "7. законный близнец: реплики одного образа спрашиваются ОДИН раз"
  fixture_two_good
  printf 'iam-1\tkaname\tdocker.io/library/kaname:dev\tsha256:ddd\niam-2\tkaname\tdocker.io/library/kaname:dev\tsha256:ddd\niam-3\tkaname\tdocker.io/library/kaname:dev\tsha256:ddd\n' > "$work/pods.tsv"
  printf '%s\n' "$rev40" > "$work/exec.kaname"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" == *"продукта 3 (различных образов 1)"* ]]; then
    ok "перепись назвала ОБЕ величины — поды и образы"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "8. законный близнец: две РАЗНЫЕ ревизии на одном стенде видны как две"
  fixture_two_good
  printf 'vpc-1\tvpc\tdocker.io/library/kacho-vpc:dev\tsha256:aaa\ngeo-1\tkacho-geo\tdocker.io/library/kacho-geo:dev\tsha256:ccc\n' > "$work/pods.tsv"
  printf '%s\n' "1111111111111111111111111111111111111111" > "$work/exec.kacho-geo"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" == *"$rev40"* ]] && [[ "$out" == *"1111111111111111111111111111111111111111"* ]]; then
    ok "смешанный стенд читается как смешанный, а не как один коммит"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "9. отказ обязан НАЗВАТЬ причину — пустое место на её месте это не отчёт"
  # Ось заведена по СВОЕЙ ошибке: причина писалась в переменную, а функция
  # исполняется в подоболочке, поэтому строка выходила «не прочитан — ; клейма
  # тоже нет». Восемь предыдущих осей этого не увидели: они спрашивали ЧТО
  # напечатано, а не ПОЛОН ли ответ.
  fixture_two_good
  printf 'geo-1\tkacho-geo\tdocker.io/library/kacho-geo:dev\tsha256:ccc\n' > "$work/pods.tsv"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 1 ] && [[ "$out" == *"container \"kacho-geo\" not found"* ]]; then
    ok "причина отказа доехала из подоболочки до строки отчёта"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "10. ГРАНИЦА: чужой образ — не находка, но обязан быть НАЗВАН переписью"
  # Ось заведена потому, что различение «наш/чужой» жило в выражении jq, а jq в
  # этой самопроверке подменён на `cat`. То есть свойство, ради которого фильтр
  # написан, не проверяла ни одна из девяти осей выше, и чужой контейнер
  # отбрасывался МОЛЧА: перепись не отличала «осмотрели 17» от «осмотрели 17 из 36».
  fixture_two_good
  printf 'vpc-1\tvpc\tdocker.io/library/kacho-vpc:dev\tsha256:aaa\npg-0\tpostgresql\tdocker.io/bitnami/postgresql:16.4.0\tsha256:eee\n' > "$work/pods.tsv"
  out="$(report kacho)"; rc=$?
  if [ "$rc" -eq 0 ] && [[ "$out" == *"чужих"* ]] && [[ "$out" == *"postgresql"* ]] \
     && [[ "$out" == *"НЕ установлена у 0"* ]]; then
    ok "чужой посчитан и назван, находкой не стал"
  else
    bad "код $rc; вывод: $out"
  fi

  echo "11. ГРАНИЦА: стенд из ОДНИХ чужих — код 2, и сказано сколько их было"
  fixture_two_good
  printf 'pg-0\tpostgresql\tdocker.io/bitnami/postgresql:16.4.0\tsha256:eee\n' > "$work/pods.tsv"
  out="$(report kacho 2>&1)"; rc=$?
  if [ "$rc" -eq 2 ] && [[ "$out" == *"ноль"* ]] && [[ "$out" == *"чужих"* ]]; then
    ok "код 2, и «чужие были» отличимо от «стенд пуст»"
  else
    bad "код $rc; вывод: $out"
  fi

  PATH="$saved_path"
  echo "итог самопроверки: прошло $pass, провалено $fail"
  [ "$fail" -eq 0 ]
}

case "$MODE" in
  self-test) self_test; exit $? ;;
  report)    report "$NAMESPACE"; exit $? ;;
esac
