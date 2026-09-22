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
# Idempotent (apply). NB: these are LOCAL kind
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
if kubectl -n "$NS" get secret kaname-jwks-enc-key >/dev/null 2>&1; then
  echo "kaname-jwks-enc-key already present — reusing (wrapping key, must survive re-runs)"
else
  ENC_KEY="$(openssl rand -hex 32)"
  kubectl -n "$NS" create secret generic kaname-jwks-enc-key \
    --from-literal=enc_key="$ENC_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kaname-jwks-enc-key (enc_key, 32B hex) — generated ONCE"
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
if kubectl -n "$NS" get secret kaname-second-factor-enc-key >/dev/null 2>&1; then
  echo "kaname-second-factor-enc-key already present — reusing (wrapping key, must survive re-runs)"
else
  kubectl -n "$NS" create secret generic kaname-second-factor-enc-key \
    --from-literal=enc_key="$(openssl rand -hex 32)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kaname-second-factor-enc-key (enc_key, 32B hex) — generated ONCE"
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
if kubectl -n "$NS" get secret kaname-hook-token >/dev/null 2>&1; then
  echo "kaname-hook-token already present — reusing (обе стороны держат одну величину)"
else
  kubectl -n "$NS" create secret generic kaname-hook-token \
    --from-literal=token="$(openssl rand -hex 24)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kaname-hook-token (token, 24B hex)"
fi

# Bootstrap-admin SA ES256 (P-256, PKCS#8) signing key — the private key that
# InternalBootstrapTokenService (#58) uses to sign the private_key_jwt
# client_assertion it exchanges at Hydra (aud=https://{API_DOMAIN}) for the first
# non-interactive RS256 admin Bearer. The mint use-case derives the PUBLIC JWK
# from this key and self-registers the Hydra OAuth client on first mint — so the
# key MUST be STABLE across re-runs (regenerating it would orphan the already-
# registered Hydra client's JWK → assertion signature no longer verifies). Hence:
# generate ONCE; reuse the existing secret on re-run (idempotent, NOT rotate).
if kubectl -n "$NS" get secret kaname-bootstrap-sa-key >/dev/null 2>&1; then
  echo "kaname-bootstrap-sa-key already present — reusing (stable signing key)"
else
  BOOTSTRAP_KEY="$(openssl ecparam -name prime256v1 -genkey -noout | openssl pkcs8 -topk8 -nocrypt)"
  kubectl -n "$NS" create secret generic kaname-bootstrap-sa-key \
    --from-literal=private_key_pem="$BOOTSTRAP_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kaname-bootstrap-sa-key (private_key_pem, ES256 P-256)"
fi

echo "prerequisite secrets ready in ns/$NS"
