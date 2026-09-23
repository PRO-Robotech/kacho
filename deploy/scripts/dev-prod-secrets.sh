#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# dev-prod-secrets.sh — provision the AuthN secrets that kaname production-strict
# Config.Validate REQUIRES (fail-closed, defense-in-depth).
#
#   - kaname-hook-token   key=token    — общий секрет обратных вызовов провайдера
#   - kaname-jwks-enc-key key=enc_key  — 32-byte-hex JWKS private-key encryption key
#   - kaname-second-factor-enc-key key=enc_key — 32-byte-hex ключ обёртки секретов
#                                       второго фактора (читается под identityProvider: own)
#
# ЗАПУСКАЕТСЯ ДО ПЕРВОГО ПРОГОНА helm, А НЕ МЕЖДУ ПРОГОНАМИ (задача #948). Общий
# секрет обратных вызовов — ПРЕДУСЛОВИЕ: провайдер берёт его величину обязательной
# ссылкой уже в базовом профиле, потому что необязательная ссылка на отсутствующий
# секрет даёт ПУСТУЮ величину, полосу отвергают на первом же обращении, а стенд при
# этом выглядит поднятым. Здесь стояло «Run BEFORE the production helm upgrade» —
# верно для прежнего порядка, когда ссылку нёс только слой боевой посадки.
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
# Требуется стражем старта службы ТОЛЬКО под `config.authn.identityProvider: own`
# (Ф12, kacho#1281; переменная KANAME_SECOND_FACTOR_ENC_KEY). Ссылка у пода —
# `optional: true`, поэтому недостающий секрет даёт ИМЕНОВАННЫЙ отказ стража, а
# не `CreateContainerConfigError`, по которому не видно, какой ручки не хватает.
# Под `external` второго фактора нет, секрета может не быть вовсе, и такой стенд
# остаётся поднимаемым.
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

# ─── ОБЩИЙ СЕКРЕТ ОБРАТНОГО ВЫЗОВА: СОЗДАЁТСЯ ОДИН РАЗ, НЕ РОТИРУЕТСЯ ────────
#
# Величину читают ДВА пода — отправитель (провайдер) и проверяющая сторона
# (служба прав), — и каждый читает её ОДИН РАЗ, при старте контейнера.
# Перевыпуск на каждом прогоне поэтому не «обновляет секрет», а заводит окно, в
# котором стороны держат РАЗНЫЕ величины: повторный `make dev-up` пересобирает
# образы служб, поэтому под службы прав перекатывается и берёт новую величину, а
# под провайдера остаётся прежним и продолжает подписывать старой. Исход — тот
# самый `401` на обратном вызове, ради которого заведена задача #948, только
# приходящий не с первой выкатки, а со второй.
#
# Поэтому: сгенерировать ОДИН раз, дальше переиспользовать — той же формой, что
# у подписного ключа ниже. Ротация общего секрета — осознанное действие: снять
# секрет и перекатить ОБА пода, а не побочный эффект подъёма стенда.
if out="$(kubectl -n "$NS" get secret kaname-hook-token -o name 2>&1)"; then
  echo "kaname-hook-token already present — reusing (обе стороны держат одну величину)"
elif [[ "$out" == *"(NotFound)"* ]]; then
  kubectl -n "$NS" create secret generic kaname-hook-token \
    --from-literal=token="$(openssl rand -hex 24)" \
    --dry-run=client -o yaml | create_once kaname-hook-token "token, 24B hex"
else
  refuse_unknown kaname-hook-token "$out"
fi

# Bootstrap-admin SA ES256 (P-256, PKCS#8) signing key — the private key that
# InternalBootstrapTokenService (#58) uses to sign the private_key_jwt
# client_assertion it exchanges at Hydra (aud=https://{API_DOMAIN}) for the first
# non-interactive RS256 admin Bearer. The mint use-case derives the PUBLIC JWK
# from this key and self-registers the Hydra OAuth client on first mint — so the
# key MUST be STABLE across re-runs (regenerating it would orphan the already-
# registered Hydra client's JWK → assertion signature no longer verifies). Hence:
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

echo "prerequisite secrets ready in ns/$NS"
