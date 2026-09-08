# Security — handler layer

Снимок состояния AuthZ / AuthN / transport-security на уровне handler'ов: что
**уже сделано** и что осталось.

## Сообщить об уязвимости

Не открывайте публичный issue на security-проблему. Сообщайте приватно через
GitHub Security Advisories репозитория (`Security` → `Report a vulnerability`).
Опишите затронутую версию, шаги воспроизведения и предполагаемое влияние; мы
подтвердим получение и согласуем сроки раскрытия после выпуска фикса.

## Инварианты authN / authZ

Правила одинаковы для public (external TLS) и internal (:9091) листенеров —
неаутентифицированных и неавторизованных запросов нет ни на одном порту:

- **Транспорт / AuthN** — service→service через mTLS (verified client-cert),
  user→edge через TLS + validated JWT. Plaintext/insecure-gRPC в проде запрещен.
- **AuthZ** — каждый RPC проходит per-RPC authz-Check; read-RPC гейтятся
  viewer-tier, мутации — admin-tier. Internal-периметр не считается доверенным
  (defense-in-depth против lateral movement).
- **Internal-vs-external** — `Internal*` методы не публикуются на external TLS
  endpoint, только на cluster-internal listener.
- **Без leak'а инфра-данных** — публичная поверхность отдает лишь tenant-facing
  «намерение + результат»; placement/underlay/wiring — только через `Internal*`.

## Что сделано

### Tenant isolation — permission-модель, не handler-код

Каждый public RPC, читающий/мутирующий конкретный ресурс, авторизуется **per-RPC
FGA-Check'ом** из `internal/check.PermissionMap` (`v_get`/`v_list`/
`v_update`/`v_delete` per-object; `viewer`/`editor` на `project:<project_id>` для
top-level List/Create), на **обоих** листенерах, fail-closed для RPC вне карты.
Handler'ы (address/network/subnet/route_table/security_group/gateway) собственной
ownership-проверки НЕ делают и НЕ дублируют её: их `repo.Get` остаётся только
ради existence-контракта (`NOT_FOUND`). `internal/handler/authn_interceptor.go`
отвечает ровно на один вопрос — предъявлен ли принципал — и не авторизует. Deny на
существующий-но-недоступный ресурс маскируется под тот же `404`, что и настоящий
miss (info-leak prevention).

Единственное исключение из «решает per-RPC Check» —
`InternalNetworkInterfaceService/ListByInstance`: инстансы называет вызывающий, а
ответ касается интерфейсов с разными владельцами, поэтому единичного объекта для
вопроса нет. Он помечен `ScopeFiltered` и сужает страницу per-object через ту же
модель (`viewer ∪ v_list` на `vpc_network_interface:<id>`); production boot-guard
`ValidateListFilter` не даёт стартовать без включённого фильтра.

`KACHO_VPC_AUTH_MODE` (`internal/apps/kacho/config/config.go`):
- `dev` — anonymous-mode, callers без AuthN-headers пропускаются как admin
  (только для локальных фикстур; в развернутом стенде/проде недопустимо).
- `production` — **fail-closed**: запрос без forwarded-принципала (`x-kacho-principal-*`)
  → `PermissionDenied` (защита от misconfigured prod-deploy, где IAM-sidecar/
  reverse-proxy забыт). Личность, объявленная вызывающим о себе в plaintext-заголовке,
  аутентификацией не является и больше не читается.
- `production-strict` — то же + дополнительно требует `ResourceManagerTLS=true` && `DBSSLMode != disable`.

### Internal-port (:9091) — оборона

`:9091` (`Internal*` RPC) защищен несколькими слоями:
1. **NetworkPolicy** (helm) — ingress на `:9091` только от api-gateway и admin-tooling pod'ов.
2. **mTLS** — verified client-cert на обоих листенерах.
3. **per-RPC FGA-Check** — та же цепочка, что на public: cluster-scoped admin-RPC на
   `cluster:cluster_root` (`system_admin`/`system_viewer`), IPAM-примитивы —
   per-object на `vpc_address`. «Internal = доверенный» — запрещённое допущение.
4. **production-mode fail-closed** — без forwarded-принципала отказ.

Отдельного admin-гейта здесь **нет**: он поднимал привилегию из клиентского заголовка
`x-kacho-admin`, то есть звонящий объявлял себя администратором сам. Привилегию
выдаёт и отзывает модель прав.

`Internal*` методы **не регистрируются** на external TLS endpoint
(`api.kacho.local:443`, advertised для внешних клиентов) — только на
cluster-internal listener api-gateway.

### Без raw-pgx-leak в Internal handlers

Все `Internal*` handler'ы маппят ошибки через `internalMapErr`
(`internal/handler/internal_maperr.go`; обертки `mapPoolErr`/`mapGeoErr`/`mapAllocErr`) —
sentinel'ы классифицируются, raw `pgErr` → generic `Internal` без
hostname/db/query-fragment в тексте. Прямых `status.Errorf(codes.Internal, "...: %v", err)`
в `internal_*_handler.go` не осталось.

### Transport-security — per-edge / per-listener mTLS (opt-in)

- **mTLS на обоих listener'ах** — `internal/apps/kacho/config/mtls.go` несет
  `PublicServerMTLS` (:9090) и `InternalServerMTLS` (:9091); `cmd/vpc/main.go`
  поднимает оба через `PublicServerCreds()` / `InternalServerCreds()`. Каждое ребро
  независимо: `enable=false` (default) → insecure (dev backward-compat); `enable=true`
  → `RequireAndVerifyClientCert` (server-cert + client-CA), fail-closed при отсутствии
  cert-тройки (без тихого downgrade в insecure). Исходящие client-ребра
  (`vpc→iam` register/project/authz, `vpc→geo`) — тот же per-edge opt-in.
- `KACHO_VPC_DB_SSLMODE` (default `disable` для dev; в production helm-values — `verify-full`) — `internal/apps/kacho/config/config.go`.
- Здесь стояла ручка TLS до сервиса управления ресурсами и функция, которая её
  читает. В дереве нет ни того, ни другого: ни одного вхождения ни имени ручки, ни
  самого этого сервиса под доменом vpc — имена не воспроизводятся, чтобы их не искали.
- `production-strict` требует **включённого TLS на ребре к iam** (`extapi.iam.tls.enable`),
  иначе загрузка конфигурации возвращает ошибку и сервис не стартует. Прежняя редакция
  говорила «оба включены», имея в виду в том числе несуществующую ручку.

## Что осталось (зависит от интеграции с `kaname`)

- **Реальный AuthN (JWT-validating interceptor)** — сейчас claims приходят от
  upstream-proxy без валидации токена и без реальной проверки членства в
  project/cloud через resource-manager. Контракт `TenantFromCtx` спроектирован
  так, чтобы interceptor можно было заменить без правок handler'ов (объектная
  авторизация от него не зависит — она в permission-модели).
> [!warning] Раздел был устаревшим: три пункта объявляли ОТКРЫТЫМИ дыры, закрытые в коде
> Ниже они оставлены как закрытые — с указанием, чем именно закрыты. Держать их в
> списке «что осталось» хуже, чем не иметь списка: документ по безопасности,
> перечисляющий несуществующие дыры, обесценивает и те пункты, которые настоящие, а
> читателю, планирующему работу, показывает фронт, которого нет. Проверено по коду
> 2026-07-30.

- ~~**`OperationService.Get(operation_id)` без project-ownership-check**~~ — **ЗАКРЫТО.**
  Ownership энфорсится в общем слое: `operationspb.Handler.Get/Cancel` идут через
  ownership-scoped `GetOwned`/`CancelOwned` (владелец — creator-principal из доверенного
  контекста), чужой id → `NotFound` без утечки существования. В карте разрешений оба RPC
  помечены `Public`, и это означает «ReBAC-exempt», а НЕ «без проверки»: в модели нет
  object type `vpc_operation` и per-operation tuple не эмитится, поэтому Check не имел бы
  пути и отверг бы даже поллинг владельцем; anti-anon в production-режиме сохраняется.
- ~~**Per-RPC FGA-gate на IPAM**~~ — **ЗАКРЫТО.** `InternalAddressService.*`
  (`AllocateInternalIP`/`AllocateExternalIP`/референс-tracking) присутствуют в
  `internal/check/permission_map.go` и гейтятся **object-scoped** на самом
  `vpc_address:<address_id>` (мутации → `v_update`, чтение референта → `v_get`). Наличие в
  карте снимает их с прежнего `methodIsInternal`-обхода; закрытие залочено регрессией
  (`internal/check/interceptor_test.go`).
- ~~**Production boot fail-fast по authz**~~ — **ЗАКРЫТО.** `Config.Validate()` в
  production-режиме ОТКАЗЫВАЕТ в старте без `authz.iam-endpoint`
  (`errAuthzEndpointRequired`), а не логирует предупреждение и запускается в degraded
  state. Отдельно `ValidateListFilter` отказывает в старте, если карта несёт ScopeFiltered
  RPC, а list-filter выключен, без резолвимого адреса или с мягким проходом.
</content>
</invoke>
