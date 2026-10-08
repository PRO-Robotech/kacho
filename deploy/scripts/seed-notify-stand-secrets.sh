#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# seed-notify-stand-secrets.sh — объекты СТЕНДА шлюза уведомлений, его пробы-
# источника (NTF-1, полоса D6; Д123) и ключ подписи вызовов proof-of-work
# почтовой полосы края (NTF-2 Р5, замысел З9; kacho#2917).
#
# ПОЧЕМУ ОТДЕЛЬНЫЙ ПОСЕВ, А НЕ dev-prod-secrets.sh. Тот посев читает предполёт
# боевой раскатки площадки (helm/umbrella/cutover-fe3455.sh, (б) «секреты,
# которые на стенде заводит посев») — и требует каждый его секрет на площадке.
# notify на площадке не рендерится (перечень источников пуст), и требование
# его объектов там остановило бы раскатку по предмету, которого у неё нет.
# Требует их тот, кто их монтирует: рендер стенда (scripts/stack-secrets.sh
# выводит требуемое из рендера и зовёт этот посев производителем).
#
# Ключ proof-of-work края — здесь же, а не в dev-prod-secrets.sh: его
# производитель тот же, что у остальных объектов почтовой полосы стенда, и
# предполёт боевой раскатки (helm/umbrella/cutover-fe3455.sh) его посевом не
# требует: приёмка NTF-2 (посев «после NTF-2», DoD п.13–п.15, строка 7 таблицы
# дерева) правит в нём только абзац о разделе `courier`.
#
# Каждый объект заводится ОДИН раз и не перечеканивается; повторный прогон
# переиспользует существующий. Отказ сервера, отличный от NotFound, — отказ
# посева: «не знаю» не читается как «нет» (иначе величина перечеканилась бы
# поверх существующей).
set -euo pipefail
NS="${KACHO_NAMESPACE:-kacho}"

create_once() {
  local name="$1" what="$2" out
  if out="$(kubectl -n "$NS" create -f - 2>&1)"; then
    echo "provisioned $name ($what) — generated ONCE"
    return 0
  fi
  case "$out" in
    *"(AlreadyExists)"*)
      echo "$name появился между проверкой и заведением — переиспользуется, величина не тронута"
      return 0 ;;
  esac
  echo "ABORT: seed-notify-stand-secrets — заведение секрета $name отказало: $out" >&2
  return 1
}

refuse_unknown() {
  echo "ABORT: seed-notify-stand-secrets — есть ли секрет $1, НЕ УСТАНОВЛЕНО: сервер ответил не" >&2
  echo "       NotFound, а отказом: $2" >&2
  echo "       Прочитать это как «секрета нет» значило бы перечеканить величину поверх" >&2
  echo "       существующей. Повтори, когда кластер отвечает." >&2
  exit 1
}

# ─── ОБЪЕКТЫ СТЕНДА NOTIFY (NTF-1, полоса D6; Д123) ───────────────────────────
#
# Пять объектов, каждый ОДИН раз и не перечеканивается — довод тот же, что у
# ключей выше, и у каждого он свой:
#   · kacho-notify-db, kacho-notifyprobe-db — пароли баз kacho_notify и
#     kacho_notifyprobe. Их берут и экземпляр базы (`existingSecret`: ключи
#     `password` пользователя службы и `postgres-password` администратора), и
#     служба (`password`). Новый пароль поверх тома базы, заведённой со старым,
#     оставил бы службу без входа в собственную базу;
#   · kacho-notify-recipient-key — ключ сетки на адресата (З24), ключ
#     `recipientKey`, 64 hex-символа (страж старта требует ≥ 32 байт, Д89).
#     Новый ключ — ротация сетки (З24): окна адресатов начинаются заново;
#   · kacho-notify-address-key — ключ отпечатка адреса (NTF-4 Р15, Д23), ключ
#     `addressKey`, 64 hex-символа (страж старта разбирает hex и требует
#     ≥ 32 октетов). Ключ отпечатка не ротируется: новый сделал бы чужими все
#     отпечатки журнала отправленного и подавлений;
#   · kacho-notify-probe-feed-keyring — кольцо ключей ленты пробы-источника
#     (З11), ключ `keyring` в форме corelib `feed.ParseKeyring`
#     `{"active":{"id":1,"key":"<base64 32 байт>"}}`. Новое кольцо сделало бы
#     нечитаемыми строки ленты, запечатанные прежним.
# Величины идут процессной подстановкой в манифест и не печатаются.
if out="$(kubectl -n "$NS" get secret kacho-notify-db -o name 2>&1)"; then
  echo "kacho-notify-db already present — reusing (пароли базы kacho_notify, ключи password и postgres-password)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kacho-notify-db \
    --from-file=password=<(openssl rand -hex 24 | tr -d '\n') \
    --from-file=postgres-password=<(openssl rand -hex 24 | tr -d '\n') \
    --dry-run=client -o yaml | create_once kacho-notify-db "пароли базы kacho_notify, ключи password и postgres-password"
else
  refuse_unknown kacho-notify-db "$out"
fi

if out="$(kubectl -n "$NS" get secret kacho-notifyprobe-db -o name 2>&1)"; then
  echo "kacho-notifyprobe-db already present — reusing (пароли базы kacho_notifyprobe, ключи password и postgres-password)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kacho-notifyprobe-db \
    --from-file=password=<(openssl rand -hex 24 | tr -d '\n') \
    --from-file=postgres-password=<(openssl rand -hex 24 | tr -d '\n') \
    --dry-run=client -o yaml | create_once kacho-notifyprobe-db "пароли базы kacho_notifyprobe, ключи password и postgres-password"
else
  refuse_unknown kacho-notifyprobe-db "$out"
fi

if out="$(kubectl -n "$NS" get secret kacho-notify-recipient-key -o name 2>&1)"; then
  echo "kacho-notify-recipient-key already present — reusing (ключ сетки на адресата, ключ recipientKey, 64 hex)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kacho-notify-recipient-key \
    --from-file=recipientKey=<(openssl rand -hex 32 | tr -d '\n') \
    --dry-run=client -o yaml | create_once kacho-notify-recipient-key "ключ сетки на адресата, ключ recipientKey, 64 hex"
else
  refuse_unknown kacho-notify-recipient-key "$out"
fi

if out="$(kubectl -n "$NS" get secret kacho-notify-address-key -o name 2>&1)"; then
  echo "kacho-notify-address-key already present — reusing (ключ отпечатка адреса, ключ addressKey, 64 hex)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kacho-notify-address-key \
    --from-file=addressKey=<(openssl rand -hex 32 | tr -d '\n') \
    --dry-run=client -o yaml | create_once kacho-notify-address-key "ключ отпечатка адреса, ключ addressKey, 64 hex"
else
  refuse_unknown kacho-notify-address-key "$out"
fi

if out="$(kubectl -n "$NS" get secret kacho-notify-probe-feed-keyring -o name 2>&1)"; then
  echo "kacho-notify-probe-feed-keyring already present — reusing (кольцо ключей ленты пробы, ключ keyring)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kacho-notify-probe-feed-keyring \
    --from-file=keyring=<(printf '{"active":{"id":1,"key":"%s"}}' "$(openssl rand -base64 32 | tr -d '\n')") \
    --dry-run=client -o yaml | create_once kacho-notify-probe-feed-keyring "кольцо ключей ленты пробы, ключ keyring"
else
  refuse_unknown kacho-notify-probe-feed-keyring "$out"
fi

# ─── КЛЮЧ ПОДПИСИ ВЫЗОВОВ PROOF-OF-WORK КРАЯ: ОДИН РАЗ, НЕ РОТИРУЕТСЯ ─────────
#
# (kacho#2917, приёмка NTF-2 Р5; замысел З9). Край читает файл
# KACHO_API_GATEWAY_ANON_MAIL_POW_KEY_FILE — случайные байты, не короче 32, ОДИН
# ключ на весь флот края. Страж старта края требует его на ЛЮБОЙ посадке, поэтому
# объект нужен каждому стенду. Том у пода обязательный: недостающий объект держит
# под до старта с именем секрета.
#
# Порождаем ОДИН раз, дальше переиспользуем: ключом подписаны вызовы, уже
# выданные клиентам. Смена ключа делает каждый выданный и ещё не решённый вызов
# недействительным — клиент получает отказ на доказательстве, которое честно
# посчитал. Величина проходит процессной подстановкой прямо в манифест и НЕ
# печатается.
if out="$(kubectl -n "$NS" get secret kacho-api-gateway-anon-mail-pow-key -o name 2>&1)"; then
  echo "kacho-api-gateway-anon-mail-pow-key already present — reusing (смена ключа гасит выданные вызовы)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kacho-api-gateway-anon-mail-pow-key \
    --from-file=pow.key=<(openssl rand 32) \
    --dry-run=client -o yaml | create_once kacho-api-gateway-anon-mail-pow-key "ключ подписи вызовов proof-of-work края, ключ pow.key, 32 случайных байта"
else
  refuse_unknown kacho-api-gateway-anon-mail-pow-key "$out"
fi

echo "notify stand secrets ready in ns/$NS"
