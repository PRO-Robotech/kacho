#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# dev-prod-secrets.sh — provision the AuthN secrets that kaname production-strict
# Config.Validate REQUIRES (fail-closed, defense-in-depth).
#
#   - kaname-jwks-enc-key key=enc_key  — 32-byte-hex JWKS private-key encryption key
#   - kaname-second-factor-enc-key key=enc_key — 32-byte-hex ключ обёртки секретов
#                                       второго фактора (требуется стражем старта службы)
#   - kaname-mail-keys    keys=mail-window.key,device-label.key — ключи почтовой
#                                       полосы, 32 случайных байта каждый (читаются на любой посадке)
#   - kaname-bootstrap-sa-key key=private_key_pem — ES256-ключ учётки первичной чеканки
#   - kacho-api-gateway-anon-mail-pow-key key=pow.key — ключ подписи вызовов
#                                       proof-of-work края, 32 случайных байта
#                                       (требуется стражем старта края на любой посадке)
#
# ЗАПУСКАЕТСЯ ДО ПЕРВОГО ПРОГОНА helm, А НЕ МЕЖДУ ПРОГОНАМИ (задача #948): под,
# которому не хватает секрета, ждёт молча, и `helm --wait` истекает по сроку, а не
# по причине.
#
# ЗДЕСЬ БЫЛ И ОБЩИЙ СЕКРЕТ ОБРАТНЫХ ВЫЗОВОВ (`kaname-hook-token`). Его читали две
# стороны — издатель поставщика личности и слушатель хуков службы доступа. Обе
# сняты (слушатель — kacho#2818, поставщик — kacho#1276), ссылок на секрет не
# осталось ни в одном объявлении зонта, и посев снят вместе с ними: секрет,
# который чеканится и никем не читается, — объявление, пережившее потребителя.
# Держит deploy/seed_secret_has_a_consumer_test.go.
#
# Idempotent (atomic create; AlreadyExists = reuse). NB: these are LOCAL kind
# dev-stand secrets, generated fresh each run — NOT committed, NOT production key
# material. A real cluster provisions them out-of-band / via external-secrets.
#
# dev-stand secrets — NOT committed, NOT production key material. A real cluster
# provisions them out-of-band / via external-secrets.
#
# ЧТО ЗДЕСЬ ПОРОЖДАЕТСЯ ОДНАЖДЫ, А ЧТО КАЖДЫЙ РАЗ — это не стиль, а свойство
# величины (задача #1062). Величина, которой УЖЕ ЧТО-ТО ЗАПИСАНО в базе,
# порождается ровно один раз и переиспользуется на каждом следующем прогоне:
# перевыпуск делает записанное нечитаемым НАВСЕГДА, и обнаруживается это не
# отказом, а тем, что клиент перестаёт верить выданным токенам. Дисциплину
# держит гейт deploy/tests/helm/secret-material-survives-recreation-test.sh.
set -euo pipefail
NS="${KACHO_NAMESPACE:-kacho}"

# ── «ЕСТЬ ЛИ СЕКРЕТ» СПРАШИВАЕТСЯ У API-СЕРВЕРА, А НЕ ВЫВОДИТСЯ ИЗ КОДА ВОЗВРАТА ─
#
# (задача #2803) Прежде присутствие решал код возврата `kubectl get secret`, и
# ЛЮБОЙ отказ — срок, сеть, RBAC — читался как «секрета нет». Величина чеканилась
# заново и уходила `kubectl apply`, а он существующий объект ПЕРЕЗАПИСЫВАЕТ. Для
# ключей обёртки это необратимо: записанное прежним ключом больше не открывается,
# и отказ при этом тихий.
#
# Поэтому исходов у проверки ТРИ, и различаются они ответом сервера:
#   есть                → переиспользовать, величину не трогать;
#   NotFound            → завести — АТОМАРНО на сервере (`create`, не `apply`):
#                         объект, заведённый кем-то между проверкой и заведением,
#                         отвечает AlreadyExists и переиспользуется, а не
#                         перезаписывается;
#   любой иной отказ    → отказ шага. «Не знаю» — не «нет».
#
# Держит deploy/tests/helm/seed-secrets-refuse-unknown-state-test.sh.

# create_once <имя> <что заведено> — манифест на stdin заводится атомарно.
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
  echo "ABORT: dev-prod-secrets — заведение секрета $name отказало: $out" >&2
  return 1
}

# refuse_unknown <имя> <ответ сервера> — присутствие не установлено: отказ шага.
refuse_unknown() {
  echo "ABORT: dev-prod-secrets — есть ли секрет $1, НЕ УСТАНОВЛЕНО: сервер ответил не" >&2
  echo "       NotFound, а отказом: $2" >&2
  echo "       Прочитать это как «секрета нет» значило бы перечеканить величину поверх" >&2
  echo "       существующей. Повтори, когда кластер отвечает." >&2
  exit 1
}

# ── Ключ ОБЁРТКИ приватной половины подписного ключа ────────────────────────
# 32-byte hex (64 chars) — iam ResolveJWKSEncryptionKey() requires exactly 32 bytes.
#
# Им обёрнута колонка kaname.token_signing_keys.private_key_wrapped
# (services/iam/internal/keywrap, AES-256-GCM). Поэтому ключ ОБЯЗАН пережить
# пересоздание стенда: новый ключ не разворачивает ни одной уже записанной
# приватной половины, и вернуть их нечем. Порождаем ОДНАЖДЫ, дальше —
# переиспользуем (идемпотентно, НЕ ротация).
#
# Значение негодной формы здесь не чинится намеренно: iam сверяет длину при
# старте и отказывается подниматься, называя ручку. Молча заменить его на новое
# значило бы, что ошибка настройки становится рабочим режимом.
if out="$(kubectl -n "$NS" get secret kaname-jwks-enc-key -o name 2>&1)"; then
  echo "kaname-jwks-enc-key already present — reusing (wrapping key, must survive re-runs)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  ENC_KEY="$(openssl rand -hex 32)"
  kubectl -n "$NS" create secret generic kaname-jwks-enc-key \
    --from-literal=enc_key="$ENC_KEY" \
    --dry-run=client -o yaml | create_once kaname-jwks-enc-key "enc_key, 32B hex"
else
  refuse_unknown kaname-jwks-enc-key "$out"
fi

# ─── КЛЮЧ ОБЁРТКИ СЕКРЕТОВ ВТОРОГО ФАКТОРА: ОДИН РАЗ, НЕ РОТИРУЕТСЯ ──────────
#
# 32-byte hex (64 знака) — та же форма, что у ключа обёртки ключницы выше:
# резолв декодирует hex и требует РОВНО 32 байта. Величина может быть перечнем
# через запятую (первый оборачивает, все открывают); так ключ и меняется.
#
# Требуется стражем старта службы на каждом старте (Ф12, kacho#1281; переменная
# KANAME_SECOND_FACTOR_ENC_KEY): посадка у службы одна (kaname#363). Ссылка у
# пода — `optional: true`, поэтому недостающий секрет даёт ИМЕНОВАННЫЙ отказ
# стража, а не `CreateContainerConfigError`, по которому не видно, какой ручки
# не хватает.
#
# ПОЧЕМУ ПОСЕВ, А НЕ ШАБЛОН ЧАРТА — довод безопасности, а не вкуса. Репозиторий
# ПУБЛИЧЕН, поэтому величина в дерево не попадает ни при каких условиях. Шаблон,
# который её ПОРОЖДАЕТ (`randAlphaNum` / `genPrivateKey`), порождает её заново на
# КАЖДОМ рендере — то есть на каждом `helm upgrade`, `helm template` и
# `--dry-run`: уже обёрнутые секреты второго фактора становятся нечитаемыми
# НАВСЕГДА, и отказ при этом тихий. Шаблон с `lookup` дыру не закрывает: на любом
# рендере без кластера `lookup` пуст, и величина расходится между тем, что
# показали, и тем, что применили. Третий довод — площадка: на настоящем кластере
# ключевой материал приходит ВНЕ дерева (external-secrets / хранилище секретов),
# а чарт, чеканящий его сам, этот путь закрывает.
#
# Ключ ОБЯЗАН пережить повторный подъём по той же причине, что и ключ ключницы
# выше: новый ключ не разворачивает ни одного уже записанного секрета второго
# фактора, и вернуть их нечем. Порождаем ОДНАЖДЫ, дальше переиспользуем
# (идемпотентно, НЕ ротация) — дисциплину держит
# deploy/tests/helm/secret-material-survives-recreation-test.sh.
if out="$(kubectl -n "$NS" get secret kaname-second-factor-enc-key -o name 2>&1)"; then
  echo "kaname-second-factor-enc-key already present — reusing (wrapping key, must survive re-runs)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kaname-second-factor-enc-key \
    --from-literal=enc_key="$(openssl rand -hex 32)" \
    --dry-run=client -o yaml | create_once kaname-second-factor-enc-key "enc_key, 32B hex"
else
  refuse_unknown kaname-second-factor-enc-key "$out"
fi

# Bootstrap-admin SA ES256 (P-256, PKCS#8) key of InternalBootstrapTokenService
# (#58). The mint use-case derives the PUBLIC half (SPKI) from this key and records
# it in the bootstrap client mapping row on first mint; the token itself is signed
# by the service's OWN signer (kaname#1119 — the exchange with the former external
# identity provider is gone). The key MUST be STABLE across re-runs: regenerating
# it would leave the recorded public half describing a key nobody holds. Hence:
# generate ONCE; reuse the existing secret on re-run (idempotent, NOT rotate).
if out="$(kubectl -n "$NS" get secret kaname-bootstrap-sa-key -o name 2>&1)"; then
  echo "kaname-bootstrap-sa-key already present — reusing (stable signing key)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  BOOTSTRAP_KEY="$(openssl ecparam -name prime256v1 -genkey -noout | openssl pkcs8 -topk8 -nocrypt)"
  kubectl -n "$NS" create secret generic kaname-bootstrap-sa-key \
    --from-literal=private_key_pem="$BOOTSTRAP_KEY" \
    --dry-run=client -o yaml | create_once kaname-bootstrap-sa-key "private_key_pem, ES256 P-256"
else
  refuse_unknown kaname-bootstrap-sa-key "$out"
fi

# ─── КЛЮЧИ ПОЧТОВОЙ ПОЛОСЫ k_window и k_device: ОДИН РАЗ, НЕ РОТИРУЮТСЯ ───────
#
# (kacho#2915, Д64; замысел NTF-2 службы З18). Служба читает ДВА файла —
# `mail-window.key` (свёртка ключа окна адресата и адресов ожидающих
# регистраций) и `device-label.key` (подпись метки доверенного устройства), —
# случайные байты не короче 32 каждый. Страж старта требует оба на ЛЮБОЙ посадке,
# поэтому секрет нужен каждому стенду, а не только `own`. Том у пода
# ОБЯЗАТЕЛЬНЫЙ (проекция поимённо, без `optional`): недостающий объект или ключ
# держит под до старта с именем ключа.
#
# Порождаем ОДИН раз, дальше переиспользуем — довод тот же, что у ключей
# обёртки выше, и он измерим по самой службе: смена k_window начинает окна
# адресатов и ожидающие регистрации заново, смена k_device делает
# недействительными все выданные метки устройств. Величины проходят процессной
# подстановкой прямо в манифест и НЕ печатаются: ни в лог, ни в переменную
# окружения этого процесса.
if out="$(kubectl -n "$NS" get secret kaname-mail-keys -o name 2>&1)"; then
  echo "kaname-mail-keys already present — reusing (смена ключей сбрасывает окна адресатов и метки устройств)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kaname-mail-keys \
    --from-file=mail-window.key=<(openssl rand 32) \
    --from-file=device-label.key=<(openssl rand 32) \
    --dry-run=client -o yaml | create_once kaname-mail-keys "mail-window.key + device-label.key, 32B random each"
else
  refuse_unknown kaname-mail-keys "$out"
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
    --dry-run=client -o yaml | create_once kacho-api-gateway-anon-mail-pow-key "pow.key, 32B random"
else
  refuse_unknown kacho-api-gateway-anon-mail-pow-key "$out"
fi

echo "prerequisite secrets ready in ns/$NS"
