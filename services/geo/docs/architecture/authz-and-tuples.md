<!--
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
-->

# Авторизация Region/Zone и FGA-таплы

Осознанные решения по модели авторизации kacho-geo. Зафиксированы здесь, чтобы их
не приняли за пробел при ревью.

## Region/Zone авторизуются на cluster-синглтоне — per-resource таплов нет

Region и Zone — глобальный cluster-scoped каталог: они не принадлежат проекту и
одинаковы для всего кластера. Поэтому авторизация ведётся **не по объекту
конкретного региона/зоны**, а по cluster-синглтону.

- В модели OpenFGA (kacho-iam) **нет типов `region`/`zone`** — есть `type cluster`
  с синглтоном `cluster:cluster_kacho_root`.
- `internal/check/permission_map.go` мапит каждый RPC Region/Zone на объект
  `cluster:cluster_kacho_root`: публичное чтение → relation `viewer`, admin-CRUD →
  `system_admin`. Это в точности совпадает с аннотациями proto.
- Публичное чтение разрешается через `user:*` (любой аутентифицированный) и прямой
  `service_account`; admin-CRUD — через тапл `cluster:cluster_kacho_root#system_admin`,
  который сидит bootstrap kacho-iam.

**Следствие:** geo **намеренно НЕ участвует** в потоке owner-таплов
(`RegisterResource`/`UnregisterResource`), которым vpc/compute регистрируют свои
ресурсы в FGA. Per-resource таплов для Region/Zone не существует, поэтому на
Create/Update/Delete **нечего регистрировать и нечему устаревать** — Check работает
через cluster-синглтон без таплов. Это та же модель, что у admin-ресурса
`vpc.AddressPool`. `geo_outbox` — **audit-only** (строки CREATED/UPDATED/DELETED через
corelib `outbox.Emit` в writer-транзакции), не драйвер FGA-таплов.

## Модель breakglass: secure-by-default, аварийный полный обход

По умолчанию (`KACHO_GEO_AUTHZ_BREAKGLASS=false`) сервис **fail-closed**: per-RPC
authz Check (через kacho-iam) и mTLS на обоих листенерах обязательны; без них сервис
не стартует (`validateSecurityConfig` в `cmd/kacho-geo/serve.go`).

`KACHO_GEO_AUTHZ_BREAKGLASS=true` — **аварийный полный обход**: пропускает и authz
Check, и требование mTLS на обоих листенерах. Это единственный способ работать без
авторизации и транспортной защиты.

> ⚠️ **Security-риск (осознанный).** Под breakglass на plaintext-листенере forged
> `x-kacho-principal-*` в metadata принимается как доверенный principal (dev
> back-compat в corelib), а authz Check выключен — значит любой сетевой peer,
> дотянувшийся до :9091, получает admin-доступ к InternalRegion/ZoneService. Поэтому
> breakglass — строго **emergency-only**: локальная отладка или инцидент, никогда не
> рабочий стенд. `KACHO_GEO_AUTH_MODE` (dev/production/production-strict) влияет
> только на строгость TLS к БД и breakglass не ограничивает.

## Trusted-forwarder allow-list: secure-by-default

Форвардить end-user principal (`x-kacho-principal-*` metadata) вправе только доверенный
пир — обычно единственный: api-gateway SA. `KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS`
пинит allow-list его cert-identity SAN'ов; на обоих листенерах principal trust-gated
через `grpcsrv.UnaryTrustedPrincipalExtract(WithTrustedForwarders(...))`. Verified-но-
не-форвардер пир (внутренний сервис со своим client-cert'ом) не может выдать себя за
пользователя — ни эскалировать до admin-CRUD Region/Zone (:9091), ни подделать
viewer-principal на публичном read-endpoint (:9090).

**Secure-by-default (fail-closed):** на любом non-breakglass старте allow-list обязан
быть непустым. Пустой список означает «доверять ЛЮБОМУ mTLS-verified пиру как
форвардеру» (corelib `WithTrustedForwarders` при пустом входе → trust-any) —
principal-spoofing / confused-deputy. Поэтому:

- **production / production-strict** — обязателен непустой SAN. Trust-any недопустим ни
  при каких условиях.
- **dev** — пустой allow-list разрешён **только** с явным опт-ином
  `KACHO_GEO_AUTHZ_TRUST_ANY_FORWARDER=true` (local back-compat без api-gateway SAN).
  Без опт-ина и без запиненного SAN сервис **не стартует** — раньше пустой список молча
  проходил в dev (insecure-by-default gap), теперь trust-any — осознанный dev-only
  выбор, а не дефолт. `KACHO_GEO_AUTHZ_BREAKGLASS=true` по-прежнему полностью снимает
  требование (emergency-only).
