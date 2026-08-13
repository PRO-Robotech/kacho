#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# gen-managed-image-pins.sh — ПОРОЖДАЮЩИЙ для пинов образов продукта в профиле
# посадки на управляемый кластер (умолчание — helm/umbrella/values.a8f60d.yaml).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# Пин, поставленный рукой, свежести не имеет и иметь не может. Он верен ровно в
# момент написания: со следующего коммита профиль разворачивает не то состояние,
# которое описывает, и узнать об этом можно только на кластере и только тому,
# кто знает, чего ждать. Профиль при этом объявлял себя порождаемым, а
# порождающего в дереве не было — то есть указание «правится порождающий» не
# переадресовывало правку, а отменяло её.
#
# Здесь пины ВЫВОДЯТСЯ из ОДНОЙ записи в самом профиле:
#
#     # порождено-от: <ветка> <коммит из сорока знаков>
#
# Две формы тега выводятся из неё же, потому что их публикуют две разные сборки
# одного дерева: сборка сервисов режет коммит до восьми знаков
# (.github/workflows/docker-build.yml), сборка консоли не режет
# (.github/workflows/ui.yml). Обе срабатывают на пуш в main без фильтра по
# путям — значит у любого коммита main есть образы обеих семей.
#
# ─────────────────────────────────────────────────────────────────────────────
# РЕЖИМЫ
#
#   (без аргументов)   переписать пины профиля от --ref (умолчание HEAD)
#   --check            сверить пины с записью профиля; находки → код 1,
#                      исчезнувшая предпосылка → код 2
#   --self-test        доказательство инъекцией в обе стороны
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧЕГО ЭТОТ СКРИПТ НЕ ДЕЛАЕТ
#
# Не решает, ПОРА ли обновлять стенд: какой коммит там развёрнут — решение
# оператора, а не свойство дерева. Он снимает другое — возможность разойтись
# между записью и пинами, и возможность собрать пины руками так, что ни одна
# проверка этого не заметит.
set -uo pipefail

DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"

PROFILE="helm/umbrella/values.a8f60d.yaml"
REF="HEAD"
BRANCH="main"
MODE="write"

usage() {
  cat <<'EOF'
gen-managed-image-pins.sh [--profile <путь от deploy/>] [--ref <git-ref>] [--branch <имя>]
                          [--check] [--self-test]
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --profile) PROFILE="${2:?--profile требует значения}"; shift 2 ;;
    --ref)     REF="${2:?--ref требует значения}"; shift 2 ;;
    --branch)  BRANCH="${2:?--branch требует значения}"; shift 2 ;;
    --check)     MODE="check"; shift ;;
    --self-test) MODE="self-test"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "неизвестный аргумент: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# ─────────────────────────────────────────────────────────────────────────────
# ЯДРО — ЧИСТЫЕ ФУНКЦИИ НАД ТЕКСТОМ.
#
# Вынесены затем, чтобы самопроверка кормила их файлом из временного каталога,
# без git, без сети и без кластера, и требовала покраснеть на внесённом дефекте
# И промолчать на законной конструкции той же формы.
# ─────────────────────────────────────────────────────────────────────────────

# record_of <файл> — печатает «<ветка> <коммит>» из записи профиля.
record_of() {
  sed -nE 's/^#[[:space:]]*порождено-от:[[:space:]]*([A-Za-z0-9._-]+)[[:space:]]+([0-9a-f]{40})[[:space:]]*$/\1 \2/p' "$1" | head -1
}

# pins_of <файл> — печатает «<образ> <тег>» по каждому пину образа продукта,
# который ТЯНЕТСЯ из реестра. Образ стенда (`kacho-vpc:dev`, без пространства
# имён) сюда не попадает: его грузит `kind load`, а не kubelet, и коммита он не
# называет. Сторонние образы не попадают тем более — предикат требует имени,
# начинающегося с `kacho-`.
pins_of() {
  awk '
    /^[[:space:]]*#/ { next }
    match($0, /^[[:space:]]*repository:[[:space:]]*/) {
      repo = $0; sub(/^[[:space:]]*repository:[[:space:]]*/, "", repo); sub(/[[:space:]]*(#.*)?$/, "", repo); next
    }
    match($0, /^[[:space:]]*image:[[:space:]]*/) {
      v = $0; sub(/^[[:space:]]*image:[[:space:]]*/, "", v); sub(/[[:space:]]*(#.*)?$/, "", v)
      if (v ~ /\/kacho-[a-z0-9-]+:/) {
        n = split(v, part, ":")
        img = part[1]; sub(/^.*\//, "", img)
        print img, part[n]
      }
      next
    }
    match($0, /^[[:space:]]*tag:[[:space:]]*/) {
      v = $0; sub(/^[[:space:]]*tag:[[:space:]]*/, "", v); sub(/[[:space:]]*(#.*)?$/, "", v)
      gsub(/"/, "", v)
      if (repo ~ /\/kacho-[a-z0-9-]+$/) {
        img = repo; sub(/^.*\//, "", img)
        print img, v
      }
      next
    }
  ' "$1"
}

# want_tag <образ> <ветка> <коммит40> — тег, который публикует CI.
want_tag() {
  case "$1" in
    kacho-ui-future-*) printf '%s-%s' "$2" "$3" ;;
    *)                 printf '%s-%s' "$2" "${3:0:8}" ;;
  esac
}

# check_profile <файл> — печатает находки, возвращает их число кодом (1),
# 2 — исчезнувшая предпосылка (нет записи или нет ни одного пина).
check_profile() {
  local file="$1" rec branch sha findings=0 seen=0
  rec="$(record_of "$file")"
  if [ -z "$rec" ]; then
    echo "ОТКАЗ: $file не несёт записи «# порождено-от: <ветка> <коммит>» — выводить пины не из чего"
    return 2
  fi
  branch="${rec%% *}"; sha="${rec##* }"
  while read -r img tag; do
    [ -n "$img" ] || continue
    seen=$((seen + 1))
    local want; want="$(want_tag "$img" "$branch" "$sha")"
    if [ "$tag" != "$want" ]; then
      echo "НАХОДКА: $file — образ $img запинен «$tag», из записи ($branch $sha) выводится «$want»"
      findings=$((findings + 1))
    fi
  done < <(pins_of "$file")
  if [ "$seen" -eq 0 ]; then
    echo "ОТКАЗ: $file — ни одного пина образа продукта не найдено; «пины сходятся» здесь означало бы «ни один не прочитан»"
    return 2
  fi
  echo "осмотрено: $file — пинов образов продукта $seen, запись ($branch $sha), находок $findings"
  [ "$findings" -eq 0 ] || return 1
  return 0
}

# rewrite_profile <файл> <ветка> <коммит40> — переписывает пины и запись.
rewrite_profile() {
  local file="$1" branch="$2" sha="$3" tmp
  tmp="$(mktemp)"
  awk -v branch="$branch" -v sha="$sha" '
    function tag_for(img,   short) {
      if (img ~ /^kacho-ui-future-/) return branch "-" sha
      short = substr(sha, 1, 8)
      return branch "-" short
    }
    /^#[[:space:]]*порождено-от:/ { print "# порождено-от: " branch " " sha; next }
    /^[[:space:]]*#/ { print; next }
    match($0, /^[[:space:]]*repository:[[:space:]]*/) {
      repo = $0; sub(/^[[:space:]]*repository:[[:space:]]*/, "", repo); sub(/[[:space:]]*(#.*)?$/, "", repo)
      print; next
    }
    match($0, /^[[:space:]]*image:[[:space:]]*/) {
      v = $0; sub(/^[[:space:]]*image:[[:space:]]*/, "", v); sub(/[[:space:]]*(#.*)?$/, "", v)
      if (v ~ /\/kacho-[a-z0-9-]+:/) {
        n = split(v, part, ":"); img = part[1]; base = img; sub(/^.*\//, "", base)
        indent = $0; sub(/[^[:space:]].*$/, "", indent)
        print indent "image: " img ":" tag_for(base)
        next
      }
      print; next
    }
    match($0, /^[[:space:]]*tag:[[:space:]]*/) {
      if (repo ~ /\/kacho-[a-z0-9-]+$/) {
        base = repo; sub(/^.*\//, "", base)
        indent = $0; sub(/[^[:space:]].*$/, "", indent)
        print indent "tag: " tag_for(base)
        next
      }
      print; next
    }
    { print }
  ' "$file" > "$tmp" || { rm -f "$tmp"; return 2; }
  mv "$tmp" "$file"
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА — доказательство инъекцией в ОБЕ стороны.
# ─────────────────────────────────────────────────────────────────────────────
self_test() {
  local work; work="$(mktemp -d)"
  # shellcheck disable=SC2064
  trap "rm -rf '$work'" EXIT
  local f="$work/profile.yaml" rc pass=0 fail=0

  say() { printf '%s\n' "$*"; }
  ok()  { pass=$((pass + 1)); say "  OK   $*"; }
  bad() { fail=$((fail + 1)); say "  ПРОВАЛ $*"; }

  local sha40="0123456789abcdef0123456789abcdef01234567"
  write_fixture() {
    cat > "$f" <<EOF
# профиль-фикстура
# порождено-от: main $sha40
vpc:
  image: docker.io/prorobotech/kacho-vpc:main-01234567
storage:
  image:
    repository: docker.io/prorobotech/kacho-storage
    tag: main-01234567
uif:
  image: docker.io/prorobotech/kacho-ui-future-host:main-$sha40
EOF
  }

  say "1. законный файл — молчание (положительный контроль)"
  write_fixture
  check_profile "$f" >/dev/null; rc=$?
  [ "$rc" -eq 0 ] && ok "код 0 на сходящемся файле" || bad "код $rc на сходящемся файле"

  say "2. внесён дефект: тег сервиса разошёлся с записью"
  write_fixture
  sed -i 's|kacho-vpc:main-01234567|kacho-vpc:main-deadbeef|' "$f"
  out="$(check_profile "$f")"; rc=$?
  if [ "$rc" -eq 1 ] && printf '%s' "$out" | grep -q "kacho-vpc"; then
    ok "код 1 и координата названа"
  else
    bad "код $rc, вывод: $out"
  fi

  say "3. внесён дефект: тег консоли обрезан до восьми знаков"
  write_fixture
  sed -i "s|kacho-ui-future-host:main-$sha40|kacho-ui-future-host:main-01234567|" "$f"
  out="$(check_profile "$f")"; rc=$?
  if [ "$rc" -eq 1 ] && printf '%s' "$out" | grep -q "kacho-ui-future-host"; then
    ok "код 1 и координата названа"
  else
    bad "код $rc, вывод: $out"
  fi

  say "4. законный близнец: сторонний образ и образ стенда — молчание"
  write_fixture
  cat >> "$f" <<'EOF'
pg-vpc:
  image:
    repository: bitnamilegacy/postgresql
    tag: 16.4.0-debian-12-r0
minioDev:
  image: quay.io/minio/minio:RELEASE.2024-12-18T13-15-44Z
localStand:
  image: kacho-vpc:dev
EOF
  out="$(check_profile "$f")"; rc=$?
  [ "$rc" -eq 0 ] && ok "код 0 — чужие теги и образ стенда не считаются пинами продукта" \
                  || bad "код $rc, вывод: $out"

  say "5. исчезла предпосылка: записи нет — это ОТКАЗ, а не чистота"
  write_fixture
  sed -i '/порождено-от/d' "$f"
  out="$(check_profile "$f")"; rc=$?
  [ "$rc" -eq 2 ] && ok "код 2 на файле без записи" || bad "код $rc, вывод: $out"

  say "6. исчезла предпосылка: пинов нет — это ОТКАЗ, а не чистота"
  printf '# порождено-от: main %s\nfoo: bar\n' "$sha40" > "$f"
  out="$(check_profile "$f")"; rc=$?
  [ "$rc" -eq 2 ] && ok "код 2 на файле без пинов" || bad "код $rc, вывод: $out"

  say "7. запись переписывается вместе с пинами (порождение, а не сверка)"
  write_fixture
  local sha2="89abcdef89abcdef89abcdef89abcdef89abcdef"
  rewrite_profile "$f" main "$sha2"
  out="$(check_profile "$f")"; rc=$?
  if [ "$rc" -eq 0 ] && grep -q "kacho-vpc:main-89abcdef" "$f" \
     && grep -q "kacho-ui-future-host:main-$sha2" "$f" \
     && grep -q "^# порождено-от: main $sha2$" "$f"; then
    ok "обе семьи тегов и запись переписаны согласованно"
  else
    bad "код $rc; файл: $(cat "$f")"
  fi

  say "8. порождение НЕ трогает чужие теги"
  write_fixture
  cat >> "$f" <<'EOF'
pg-vpc:
  image:
    repository: bitnamilegacy/postgresql
    tag: 16.4.0-debian-12-r0
EOF
  rewrite_profile "$f" main "$sha2"
  grep -q "tag: 16.4.0-debian-12-r0" "$f" && ok "тег стороннего чарта не тронут" \
                                          || bad "тег стороннего чарта переписан"

  say "итог самопроверки: прошло $pass, провалено $fail"
  [ "$fail" -eq 0 ]
}

# ─────────────────────────────────────────────────────────────────────────────
cd "$DEPLOY_ROOT" || exit 2

case "$MODE" in
  self-test)
    self_test; exit $?
    ;;
  check)
    [ -f "$PROFILE" ] || { echo "ОТКАЗ: профиль $PROFILE не найден" >&2; exit 2; }
    check_profile "$PROFILE"; exit $?
    ;;
  write)
    [ -f "$PROFILE" ] || { echo "ОТКАЗ: профиль $PROFILE не найден" >&2; exit 2; }
    sha="$(git -C "$REPO_ROOT" rev-parse --verify "${REF}^{commit}" 2>/dev/null)" || {
      echo "ОТКАЗ: ссылка «$REF» не разрешается в коммит этого дерева." >&2
      echo "       Пин, выведенный из неразрешимой ссылки, назвал бы образ, которого нет." >&2
      exit 2
    }
    # Тег несёт ИМЯ ВЕТКИ, и образ публикуется только сборкой этой ветки. Коммит,
    # в неё не входящий, дал бы синтаксически правильный пин на несуществующий
    # образ — отказ здесь дешевле, чем ImagePullBackOff через минуты после
    # успешного «helm upgrade».
    contained=""
    for candidate in "origin/$BRANCH" "$BRANCH"; do
      if git -C "$REPO_ROOT" rev-parse --verify "$candidate" >/dev/null 2>&1; then
        contained="$candidate"
        break
      fi
    done
    if [ -z "$contained" ]; then
      echo "ОТКАЗ: ни «origin/$BRANCH», ни «$BRANCH» в этом клоне не существует —" >&2
      echo "       принадлежность коммита ветке проверить нечем (частичный клон?)." >&2
      exit 2
    fi
    if ! git -C "$REPO_ROOT" merge-base --is-ancestor "$sha" "$contained"; then
      echo "ОТКАЗ: коммит $sha не входит в «$contained»." >&2
      echo "       Образы с тегом «$BRANCH-…» публикует сборка этой ветки; для стороннего" >&2
      echo "       коммита такого образа нет." >&2
      exit 2
    fi
    rewrite_profile "$PROFILE" "$BRANCH" "$sha" || { echo "ОТКАЗ: переписать $PROFILE не удалось" >&2; exit 2; }
    check_profile "$PROFILE"; exit $?
    ;;
esac
