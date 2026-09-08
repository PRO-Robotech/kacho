// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/multierr"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// Frozen-тексты boot-гардрейлов. Это часть контракта (наблюдаемый отказ старта),
// меняются только осознанно. %s подставляет Mode.String() (production|production-strict).
const (
	errAuthzEndpointRequired = "production mode (%s): authz.iam-endpoint is required " +
		"(set the kaname internal endpoint; there is no knob that turns authz off)"
	errPublicMTLSRequired = "production mode (%s): public listener mTLS required " +
		"(set KACHO_VPC_PUBLIC_SERVER_MTLS_ENABLE=true with cert/key/ca) — the public :9090 " +
		"listener derives the authorization principal from client-asserted x-kacho-* metadata; " +
		"without verified transport auth any direct caller can spoof an arbitrary principal " +
		"(cross-tenant authz bypass). If the listener sits behind an authenticated " +
		"forwarder/service-mesh that terminates client identity, set authn.trusted-forwarder=true " +
		"to acknowledge that trust boundary (production-strict ignores this escape hatch)"
	errQuotaAuthorityPeerTransportRequired = "production mode (%s): verified transport required on the " +
		"vpc→limit-authority edge — set KACHO_VPC_QUOTA_AUTHORITY_MTLS_ENABLE=true (with cert/key/CA). " +
		"Without it the limit resolve on the request path and the background delta both travel over " +
		"cleartext gRPC: the client credentials silently degrade to insecure, so the process starts " +
		"and reports the edge as configured"
	errInternalMTLSRequired = "production mode (%s): internal listener mTLS required " +
		"(set KACHO_VPC_INTERNAL_SERVER_MTLS_ENABLE=true with cert/key/ca) — the internal :9091 " +
		"listener hosts admin/IPAM RPC (InternalAddressPoolService, InternalNetworkService.GetNetwork " +
		"which leaks infra vrf_id, InternalAddressService) and derives the authorization subject from " +
		"client-asserted x-kacho-* metadata; internal is service→service, so mTLS is mandatory in any " +
		"production mode (no trusted-forwarder escape hatch — that applies to the public user→edge listener only)"

	// S3-гардрейлы (list-filter обязателен на ЛЮБОЙ развёрнутой посадке, включая dev).
	//
	// Тексты читает оператор, которому стенд отказал в старте, поэтому они обязаны
	// называть ручку и причину: без этого стенд не поднять. Это рантайм-диагностика,
	// а не публичный артефакт, и требование сдержанности к ней не относится.
	//
	// Формулировка «production mode (%s)» снята намеренно: страж больше не освобождает
	// dev, и заголовок, называющий режим условием, снова сделал бы вид, что он им
	// является. %s = Mode.String(); %s = clause про ScopeFiltered-набор.
	errListFilterRequired = "mode %s: authz.list-filter.enabled=true is required on every deployed stand " +
		"(core rule #16 does not exempt dev) — with the filter off a project-tier viewer sees every row of " +
		"the project, and %s. Enable authz.list-filter"
	errListFilterEndpointRequired = "mode %s: authz.list-filter.enabled=true but no resolvable " +
		"authorize/iam endpoint (authz.list-filter.authorize-endpoint and authz.iam-endpoint both empty) " +
		"— the filter degrades to passthrough, which returns the same unfiltered page as having it off, and " +
		"%s. Set authz.list-filter.authorize-endpoint (or authz.iam-endpoint)"
	// errListFilterFailOpenForbidden — «включён» ещё не значит «решает». Для
	// ScopeFiltered RPC фильтр — ЕДИНСТВЕННОЕ место, где вызывающего сверяют с
	// объектами: per-RPC Check для них не задаётся (интерцептор отдаёт
	// DecisionInternal). При мягком проходе любая ошибка соседа — таймаут, отказ в
	// правах, недоступность — возвращает НЕфильтрованный список, то есть снимает
	// эту единственную авторизацию целиком и молча. Требование к такому фильтру не
	// «включён», а «включён И отказывает при сбое».
	// %s = Mode.String(); %d = число ScopeFiltered RPC; %s = их имена через ", ".
	errListFilterFailOpenForbidden = "mode %s: authz.list-filter.fail-open must be false " +
		"— %d RPC(s) are ScopeFiltered (%s) and the data-level list-filter is their ONLY object-scope " +
		"authorization (no per-RPC Check is configured for them); with fail-open any filter error returns " +
		"the unfiltered page, so a single peer timeout or refusal silently removes that authorization. " +
		"Set authz.list-filter.fail-open=false or drop ScopeFiltered from the permission map"

	// S4-гардрейлы (транспорт исходящих vpc→iam рёбер обязан быть verified в
	// production). %s = Mode.String(). Тексты — часть контракта (наблюдаемый отказ
	// старта), меняются только осознанно.
	errAuthzPeerTransportRequired = "production mode (%s): outbound vpc→iam authz Check edge " +
		"(authz.iam-endpoint → InternalIAMService.Check) requires verified transport — set client mTLS " +
		"(KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE=true) or verified server-TLS (authz.iam-tls.enable=true). Without it " +
		"the per-RPC authorization Check is dialed over cleartext gRPC (dialPeer falls back to insecure creds); " +
		"a network attacker can MITM the response and forge allowed=true — full authz bypass"
	errProjectPeerTransportRequired = "production mode (%s): outbound vpc→iam ProjectService.Get edge " +
		"(extapi.iam → project existence / account lookup) requires verified transport — set client mTLS " +
		"(KACHO_VPC_IAM_PROJECT_MTLS_ENABLE=true) or verified server-TLS (extapi.iam.tls.enable=true). Without it " +
		"the edge is dialed over cleartext gRPC (CWE-319 / MITM of resource-ownership validation)"

	// S4b-гардрейлы (SEC-hardening r9b): те же требования, что project/authz рёбра,
	// но для оставшихся двух исходящих рёбер. %s = Mode.String(). Тексты — часть
	// контракта (наблюдаемый отказ старта).
	errGeoPeerTransportRequired = "production mode (%s): outbound vpc→geo edge " +
		"(extapi.geo → geo.v1.ZoneService.Get / RegionService.Get) requires verified transport — set client mTLS " +
		"(KACHO_VPC_GEO_MTLS_ENABLE=true) or verified server-TLS (extapi.geo.tls.enable=true). Without it the " +
		"cross-domain zone_id/region_id reference-validation edge is dialed over cleartext gRPC (CWE-319 / MITM " +
		"forges a geo existence-OK for an invalid or foreign zone/region, defeating Subnet/AddressPool scope validation)"
	errRegisterPeerTransportRequired = "production mode (%s): outbound vpc→iam owner-tuple register edge " +
		"(register-drainer + sync registrar → InternalIAMService.RegisterResource, :9091) requires client mTLS " +
		"(KACHO_VPC_IAM_REGISTER_MTLS_ENABLE=true) — this edge uses client-cert creds only (no server-TLS variant). " +
		"Without it the FGA owner-tuple registration that grants resource ownership is dialed over cleartext gRPC " +
		"(CWE-319 / MITM tampers with authorization-relevant ownership tuples)"

	// errListFilterPeerTransportRequired — соединение фильтра видимости.
	// Отдельное от ребра per-RPC Check: свой адрес и свои ручки транспорта, поэтому
	// защита Check-ребра его НЕ покрывает. Какой это адрес — решает профиль (при
	// пустом authorize-endpoint он наследуется от authz.iam-endpoint), и на
	// требование это не влияет: гард стережёт ТРАНСПОРТ соединения, а не то, на чей
	// слушатель оно приходит. Ответ этого ребра решает, какие объекты вызывающий увидит в List,
	// то есть оно несёт решение о доступе и подпадает под то же требование, что
	// остальные исходящие. %s = Mode.String().
	errListFilterPeerTransportRequired = "production mode (%s): outbound vpc→iam list-filter authorize edge " +
		"(authz.list-filter.authorize-endpoint → AuthorizeService.BatchCheck) requires client mTLS — set " +
		"KACHO_VPC_AUTHZ__LIST_FILTER__AUTHORIZE_TLS__ENABLE=true (its own client-cert knob) or " +
		"KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE=true (the client identity shared with the Check edge). This is a " +
		"SEPARATE connection from the authz Check edge — it has its own address and its own transport knobs, " +
		"and it carries the per-object visibility decision behind every List, so securing the Check edge alone " +
		"does not cover it"

	// S5-гардрейлы (профиль возможностей исполнителя датаплейна, см. dataplane.go).
	//
	// Тексты читает оператор, которому стенд отказал в старте, поэтому они обязаны
	// называть ручку — и ключ файла настроек, и переменную окружения: посадка
	// задаёт профиль то так, то так, а искать имя в исходниках оператор не обязан.
	// %s = Mode.String().
	errExecutorOverlapRequired = "mode %s: dataplane.executor.overlapping-tenant-addresses=true is required " +
		"(env KACHO_VPC_DATAPLANE__EXECUTOR__OVERLAPPING_TENANT_ADDRESSES) — tenant address ranges in vpc are " +
		"unique only WITHIN a network by construction (subnet/pool migrations constrain overlap with " +
		"`EXCLUDE USING gist (network_id WITH =, …)`), so two tenants holding the same range is the normal case, " +
		"not an edge one. An executor that does not isolate identical addresses of different tenants merges " +
		"their traffic. Until that isolation is declared, accepting overlapping ranges is not allowed: declare " +
		"the capability, or run this stand against an executor that has it"
	errExecutorStateTrackingRequired = "mode %s: dataplane.executor.state-tracking-families is not declared " +
		"(env KACHO_VPC_DATAPLANE__EXECUTOR__STATE_TRACKING_FAMILIES, allowed: %s) — an empty declaration means " +
		"UNKNOWN, not «tracks nothing»: return traffic is permitted by connection state, so on unknown " +
		"statefulness a rule is accepted and cannot be realized. Note that a lone comma is NOT a declaration — " +
		"it normalises to zero families, and the guard reads the same normalised value the rest of the process reads"
	errExecutorUnknownFamily = "dataplane.executor.state-tracking-families: unknown family %s " +
		"(allowed: %s) — a silently dropped entry would leave a profile the operator considers declared and the " +
		"guard does not; fix the spelling or drop the entry"
	errExecutorNamedSetRequired = "mode %s: dataplane.executor.named-set-reference-in-rule=true is required " +
		"(env KACHO_VPC_DATAPLANE__EXECUTOR__NAMED_SET_REFERENCE_IN_RULE) — a security-group rule target is " +
		"either address ranges OR a group id (mutually exclusive branches of the accepted contract), so an " +
		"executor without named-set references leaves part of the already-accepted rules unrealizable"
	errExecutorGuaranteeRequired = "mode %s: %s must be a positive number (env %s, got %d) — zero is the " +
		"ABSENCE of a guarantee, not a guarantee of zero, and the control plane has nothing to state to the " +
		"tenant about what the executor will hold"
	errExecutorGuaranteeNegative = "%s must not be negative (env %s, got %d) — a negative number is not " +
		"«no limit», it is an unusable declaration"
	// %[1]s = Mode.String(), %[2]d = объявлено, %[3]d = обещано продуктом.
	//
	// Оба числа обязательны: без объявленного оператор не знает, ЧТО чинить, без
	// обещанного — ДО КАКОЙ величины. Имя инкапсуляции и арифметика вычета сюда не
	// попадают намеренно — по величине накладных расходов опознаётся сетевая
	// фабрика (см. godoc domain.GuaranteedPayloadFloorBytes).
	errExecutorPayloadBelowProductFloor = "mode %[1]s: dataplane.executor.guaranteed-payload-bytes=%[2]d is " +
		"below the payload floor this product promises the tenant (%[3]d bytes, env " +
		"KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_PAYLOAD_BYTES) — the promise is a product-wide lower bound " +
		"the tenant reads in the documentation and cannot verify against this stand, so a stand whose executor " +
		"carries less makes it false for everyone who arrives here, and makes it false SILENTLY (the control " +
		"plane keeps accepting the same traffic). Either point this stand at an executor that carries at least " +
		"the promised payload, or raise the declaration to what the executor actually holds; lowering the " +
		"promise is a product decision and changes the documentation that states it"
	errExecutorTenantLimitWithoutBand = "dataplane.executor.tenant-settable-bandwidth-limit=true while " +
		"dataplane.executor.guaranteed-bandwidth-per-interface-mbps is not declared (got %d) — the declaration " +
		"contradicts itself: a tenant-settable limit is a ceiling under the guaranteed band, and there is no " +
		"band to limit against. Declare the guaranteed band, or drop the tenant-settable limit"
	// %[1]d = объявлено стендом, %[2]d = обещано продуктом.
	//
	// Оба числа обязательны по той же причине, что и у размера кадра: без первого
	// оператор не знает, ЧТО чинить, без второго — ДО КАКОЙ величины.
	errExecutorTenantLimitRangeEmpty = "dataplane.executor.tenant-settable-bandwidth-limit=true while " +
		"dataplane.executor.guaranteed-bandwidth-per-interface-mbps=%[1]d is not ABOVE the band this product " +
		"promises every interface (%[2]d Mbps, env " +
		"KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_BANDWIDTH_PER_INTERFACE_MBPS) — a tenant limit is accepted " +
		"strictly above the promised floor and up to what this stand guarantees, so these two numbers leave an " +
		"EMPTY interval: the capability is declared and cannot be used once. Either raise the declared " +
		"guarantee to what the executor actually holds, or drop the tenant-settable limit"
	errExecutorBandBelowProductFloor = "mode %[1]s: dataplane.executor.guaranteed-bandwidth-per-interface-mbps=%[2]d " +
		"is below the band this product promises the tenant (%[3]d Mbps, env " +
		"KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_BANDWIDTH_PER_INTERFACE_MBPS) — the promise is a product-wide " +
		"lower bound the tenant reads in the documentation and cannot verify against this stand, so a stand whose " +
		"executor carries less makes it false for everyone who arrives here, and makes it false SILENTLY. Either " +
		"point this stand at an executor that carries at least the promised band, or raise the declaration to " +
		"what the executor actually holds; lowering the promise is a product decision and changes the " +
		"documentation that states it"

	// errExecutorCeilingBelowPublished — стенд умеет МЕНЬШЕ опубликованного
	// потолка интерфейса (kacho#290).
	//
	// Один текст на три величины, а не три копии: они отличаются только именем
	// ручки и числом, и обе подставляются. Три расходящихся текста об одном
	// предмете разошлись бы на первой же правке формулировки.
	//
	// Направление сравнения то же, что у пола, хотя это ПОТОЛКИ: разница «пол
	// против потолка» — про то, как число читает арендатор, а со стороны стенда
	// обе величины означают «обязан уметь выдать опубликованное».
	errExecutorCeilingBelowPublished = "mode %[1]s: %[2]s=%[3]d is below the per-interface ceiling this " +
		"product publishes to the tenant (%[4]d, env %[5]s) — the published number is product-wide, the " +
		"tenant plans against it and has no way to measure this stand, so a stand whose executor carries " +
		"less breaks that tenant SILENTLY and at a threshold nobody documented. A stand that carries MORE " +
		"is lawful and is not rejected. Either point this stand at an executor that holds at least the " +
		"published ceiling, or raise the declaration to what the executor actually holds; lowering the " +
		"published number is a product decision and changes the documentation that states it"

	// S6-гардрейлы (перечень адресных диапазонов, которые платформа держит за
	// собой, см. dataplane.go `ReservedPrefixes`).
	//
	// Тексты читает оператор, которому стенд отказал в старте, поэтому они обязаны
	// называть ручку — и ключ файла настроек, и переменную окружения: посадка
	// задаёт перечень то так, то так, а искать имя в исходниках оператор не обязан.
	// Это рантайм-диагностика, и требование сдержанности к публичным артефактам к
	// ней не относится. Сами служебные диапазоны отказ НЕ печатает — их незачем
	// пересказывать тому, у кого они и так в файле настроек.
	// %s = Mode.String().
	errReservedPrefixesRequired = "mode %s: dataplane.reserved-prefixes is not declared " +
		"(env KACHO_VPC_DATAPLANE__RESERVED_PREFIXES) — part of the address space serves the platform " +
		"itself (node service addresses, in-subnet service addresses, the instance metadata endpoint), and " +
		"an EMPTY list means «we do not narrow», not «there is nothing to narrow»: every tenant prefix is " +
		"then accepted, including one laid over a service range, and the result is a product that «sometimes " +
		"does not work» with the investigation going into the network instead of into the overlap. Declare the " +
		"ranges this stand keeps for itself. Note that a lone comma is NOT a declaration — it normalises to " +
		"zero entries, and the guard reads the same normalised value the rest of the process reads"
	// errReservedPrefixUnusable — негодное ОБЪЯВЛЕНИЕ, отвергается в любом режиме.
	// %s = запись оператора (в кавычках), %s = причина от доменного конструктора.
	errReservedPrefixUnusable = "dataplane.reserved-prefixes: entry %s is unusable (%s) — a silently " +
		"dropped entry would leave a range the operator considers reserved and the control plane does not, " +
		"so a tenant subnet laid over it would be accepted; fix the entry or drop it"

	// S7-гардрейл: величины допуска запросов. Тот же довод, что у S6, и та же
	// граница между «посадка не объявила» (вопрос режима) и «объявление
	// противоречит себе» (негодность в любом режиме).
	//
	// Текст читает оператор, которому стенд отказал в старте, поэтому он называет
	// и ключ файла настроек, и переменные окружения. Опубликованные величины
	// (§8.6) в отказе НЕ пересказываются: два места об одном числе разъезжаются
	// молча, а их единственный дом — таблица решений и values.yaml чарта.
	// %s = ключ листенера (api-server.rate-limit.public|internal);
	// %s = Mode.String(); %s = префикс переменных окружения этого листенера.
	errRateLimitRequired = "%s: request admission limits are not declared for this listener in mode %s " +
		"(env %s__{READ_PER_SEC,MUTATION_PER_SEC,BURST_FACTOR,IN_FLIGHT}) — a request costs three rows " +
		"in the database on every mutation and up to a full page of objects with a batched permission " +
		"check on every read, so an unbounded rate does not hit the network, it hits the DATABASE. " +
		"Zero values mean «we do not limit», not «there is nothing to limit»: the limiter is then either " +
		"absent or empty, and in both cases it looks armed while never having refused once. Declare all " +
		"four axes for this listener"
	// errRateLimitUnusable — негодное ОБЪЯВЛЕНИЕ, отвергается в любом режиме.
	// %s = ключ листенера, %s = причина от конструктора величин.
	errRateLimitUnusable = "%s: %s — a declaration the process cannot execute is worse than none: " +
		"the operator reads the knob as set while the axis limits nothing (or, with a burst below the " +
		"sustained rate, refuses even a lawful flow)"
)

// Validate проверяет инварианты Config — чистая функция без побочных эффектов и без
// логгера. Возвращает multierr со ВСЕМИ найденными проблемами сразу, а именно:
//   - authn.mode — известное значение ENUM;
//   - logger.level — известный уровень;
//   - listen-endpoint'ы парсятся в адрес;
//   - ssl-mode из допустимого набора;
//   - в production (любом) требуется authz.iam-endpoint;
//   - в production-strict дополнительно требуется extapi.iam.tls.enable и защищенный ssl-mode.
//
// Fail-closed boot-гардрейл (S1): secure-by-default (`authn.mode=production`)
// подтверждается ОТКАЗОМ старта при невалидной prod-конфигурации, а не тихим
// небезопасным запуском. server-mTLS (S2) проверяется отдельно через
// ValidateServerMTLS (MTLSConfig грузится вне viper-Config).
func (c Config) Validate() error {
	var errs error

	errs = multierr.Append(errs, c.validateMode())

	if _, err := ParseLogLevel(c.Logger.Level); err != nil {
		errs = multierr.Append(errs, err)
	}

	if listenAddress(c.APIServer.Endpoint) == "" {
		errs = multierr.Append(errs,
			fmt.Errorf("api-server.endpoint is empty"))
	}
	if listenAddress(c.APIServer.InternalEndpoint) == "" {
		errs = multierr.Append(errs,
			fmt.Errorf("api-server.internal-endpoint is empty"))
	}

	// Словарь принимаемых значений — НЕ свой: он приходит из дома семантики
	// строки подключения (`pkg/db`), объявленный один раз на всё дерево (задача
	// продукта #1464). Текст отказа собирается оттуда же, поэтому пополнение
	// словаря не оставит здесь устаревшего перечисления.
	switch {
	case coredb.SSLModeConfigurable(c.Repository.Postgres.SSLMode):
	case strings.TrimSpace(c.Repository.Postgres.SSLMode) == "":
		// допускаем — baseDSN подставит "disable"
	default:
		errs = multierr.Append(errs,
			fmt.Errorf("repository.postgres.ssl-mode=%q (allowed: %s)",
				c.Repository.Postgres.SSLMode,
				strings.Join(coredb.ConfigurableSSLModes(), ", ")))
	}

	if strings.TrimSpace(c.Repository.Postgres.URL) == "" {
		errs = multierr.Append(errs,
			fmt.Errorf("repository.postgres.url is empty"))
	}

	// S1: production (любой вариант) обязан нести сконфигурированный authz-эндпоинт.
	// Ручки, снимающей это требование, больше нет: носитель контура ставит звено
	// решения о доступе всегда, а ребро к владельцу модели объявляется дескриптором
	// ЯВНО — незаданный адрес отвергает его конструктор на ЛЮБОЙ посадке. Здесь
	// остаётся более ранний и более понятный оператору отказ, называющий ручку.
	if c.AuthN.Mode.IsProduction() && strings.TrimSpace(c.AuthZ.IAMEndpoint) == "" {
		errs = multierr.Append(errs,
			fmt.Errorf(errAuthzEndpointRequired, c.AuthN.Mode))
	}

	// S1c: круг отправителей чужой личности обязан быть сужен на ЛЮБОМ старте —
	// не только в боевом режиме. Стража общая на все семь сервисов
	// (grpcsrv.TrustedForwarders.Require), поэтому исход и текст отказа у них
	// одинаковы, а различаются только имена ручек. Вне боевого режима пустой круг
	// остаётся возможным, но как ЯВНЫЙ опт-ин: его надо попросить, а не получить
	// умолчанием.
	//
	// Освобождения у этой стражи БОЛЬШЕ НЕТ: прежде её снимал аварийный режим — на
	// том основании, что он и так убирает пообъектную проверку целиком. Режим
	// снят, и вместе с ним исчезло единственное условие, при котором круг мог
	// остаться несужённым молча.
	errs = multierr.Append(errs, c.TrustedForwarders().Require(grpcsrv.ForwarderGate{
		Production:   c.AuthN.Mode.IsProduction(),
		DevTrustAny:  c.AuthZ.TrustAnyForwarder,
		SANsKnob:     "authz.trusted-forwarder-sans (env KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS)",
		TrustAnyKnob: "authz.trust-any-forwarder (env KACHO_VPC_AUTHZ__TRUST_ANY_FORWARDER)",
	}))

	// S1b: защищённый DB-транспорт требуется в ЛЮБОМ production-режиме, не только
	// strict (CWE-319). ssl-mode=disable в production → пароль KACHO_VPC_DB_PASSWORD
	// и весь query-трафик идут открытым текстом; sniffer в DB-сегменте перехватывает
	// креды. dev допускает disable (plaintext локально).
	// Судится ИСХОД, а не намерение: в пул уходит строка, собранная `DSN()`, и
	// `sslmode` приходит в неё ДВУМЯ путями — из поля настройки и из сырого URL,
	// причём заданный в URL режим поле НЕ перетирает (`composeDSN`). Прежняя
	// редакция читала поле, поэтому расходилась с пулом в обе стороны: стенд,
	// задавший режим прямо в URL, она отвергала при исправной посадке, а
	// `ssl-mode: require` при `sslmode=disable` в URL — пропускала, и открытый
	// канал ловил уже центральный дескриптор — ПОСЛЕ открытия пула, то есть
	// после того, как пароль по этому каналу ушёл (задача продукта #1464).
	if c.AuthN.Mode.IsProduction() {
		if mode := coredb.SSLModeFromDSN(c.DSN()); !coredb.SSLModeSecure(mode) {
			errs = multierr.Append(errs,
				fmt.Errorf("production mode (%s): repository.postgres.ssl-mode must be one of %s (got %q)",
					c.AuthN.Mode, strings.Join(coredb.SecureSSLModes(), "|"), mode))
		}
	}

	if c.AuthN.Mode == ModeProductionStrict {
		if !c.ExtAPI.IAM.TLS.Enable {
			errs = multierr.Append(errs,
				fmt.Errorf("production-strict mode: extapi.iam.tls.enable=true required"))
		}
	}

	return errs
}

// ValidateServerMTLS — boot-гардрейл S2: транспортная аутентификация публичного
// (:9090) и internal (:9091) листенеров. MTLSConfig грузится отдельно от
// viper-Config (envconfig, LoadMTLS), поэтому проверка — отдельный метод,
// вызываемый сразу после config.LoadMTLS() и ДО net.Listen.
//
// Публичный :9090 listener выводит authz-principal'а из client-asserted x-kacho-*
// metadata. В ЛЮБОМ production-режиме доверять этой metadata по незашифрованному
// транспорту запрещено (CWE-290 spoofing): иначе прямой вызов :9090 подделывает
// произвольного principal'а и обходит tenant-изоляцию. Поэтому:
//   - production-strict — server-mTLS обязателен на ОБОИХ листенерах, без
//     исключений (trusted-forwarder-флаг игнорируется).
//   - production (non-strict) — публичный listener требует ЛИБО PublicServerMTLS,
//     ЛИБО явного authn.trusted-forwarder=true (оператор подтверждает, что :9090
//     стоит за аутентифицированным forwarder'ом/mesh, который сам терминирует
//     идентичность клиента). Internal listener (:9091) — service→service, поэтому
//     server-mTLS обязателен в ЛЮБОМ production-режиме (security.md AuthN-инвариант:
//     «Internal (:9091) НЕ освобождён: mTLS обязателен»); trusted-forwarder на него
//     НЕ распространяется — это escape-hatch только для user→edge публичного listener'а.
//   - dev — требований нет.
//
// Возвращает multierr со всеми нарушениями сразу.
func (c Config) ValidateServerMTLS(m MTLSConfig) error {
	if !c.AuthN.Mode.IsProduction() {
		return nil
	}
	var errs error

	// Публичный listener: server-mTLS ИЛИ (только в non-strict) trusted-forwarder ack.
	publicAuthenticated := m.PublicServerMTLS.Enable ||
		(c.AuthN.Mode == ModeProduction && c.AuthN.TrustedForwarder)
	if !publicAuthenticated {
		errs = multierr.Append(errs, fmt.Errorf(errPublicMTLSRequired, c.AuthN.Mode))
	}

	// Internal listener (:9091): service→service, server-mTLS обязателен в ЛЮБОМ
	// production-режиме (не только strict). Без транспортной аутентификации admin/
	// IPAM-поверхность доверяет client-asserted x-kacho-* subject — principal-spoofing
	// (CWE-306/290). trusted-forwarder сюда НЕ применяется (он для публичного listener'а).
	if !m.InternalServerMTLS.Enable {
		errs = multierr.Append(errs, fmt.Errorf(errInternalMTLSRequired, c.AuthN.Mode))
	}
	return errs
}

// ValidateListFilter — boot-гардрейл S3: если permission-map несёт хотя бы один
// ScopeFiltered RPC, его object-scope авторизация возлагается на data-level
// list-filter (authz-interceptor отдаёт для ScopeFiltered DecisionInternal и
// пропускает per-RPC Check). В production фильтр ОБЯЗАН быть включён и иметь
// резолвимый authorize/iam эндпоинт — иначе такой RPC остаётся вообще без
// object-scope авторизации (cross-project enumeration).
//
// Карта ScopeFiltered-методов читается ВНУТРИ (scopeFilteredRPCs, соседний файл),
// а не принимается аргументом. Прежняя сигнатура принимала список от композиционного
// корня «чтобы config не импортировал check», и первым же дизъюнктом выходила на
// пустом списке — то есть «вызывающий не передал» было неотличимо от «защищать
// нечего». Ту же поправку storage у себя уже сделал и записал довод; здесь она
// применена дословно. Цикла нет: check на config не зависит.
//
// # Что теперь НЕ является условием
//
// Ни наличие ScopeFiltered-меток, ни режим посадки.
//
//   - Метки. Метка означает «за этим RPC per-RPC Check не задаётся вовсе», а не
//     «сужение нужно только здесь». Семь публичных List сужаются этим же фильтром
//     ПОВЕРХ project-tier Check: без фильтра любой участник проекта видит каждую
//     его строку (over-show внутри проекта), а у помеченных RPC пропадает вообще
//     единственная object-scope авторизация (межтенантно). Совпадение «меток нет»
//     и «фильтр не нужен» никем не обеспечено, и стражу, который на него опёрся,
//     достаточно снятия метки, чтобы замолчать.
//   - Режим. Здесь стояло «dev-режим гардом не затронут (может гонять unfiltered)».
//     Директива (core rule #16) стенды не делит: dev-посадка допустима ТОЛЬКО в
//     in-process фикстурах, а этот страж исполняется в композиционном корне
//     (cmd/vpc/main.go), то есть исключительно на РАЗВЁРНУТОМ процессе. Фикстуры
//     его не зовут вовсе — покрытие dev в них ничего не ломает и закрывает
//     развёрнутый dev-стенд, который до сих пор был вправе отдавать нефильтрованные
//     страницы.
//
// Довод, по которому оба дизъюнкта сняты, взят у соседа, а не выведен здесь: nlb
// (internal/authzfilter/subject.go) записал, что привязка контроля к тому, что он
// же и защищает, означает «контроль существует ровно до первой конфигурации,
// которая его не включила».
//
// # Что осталось условным — и намеренно
//
// Мягкий проход (fail-open). Он запрещён, лишь пока карта несёт хоть одну
// ScopeFiltered-метку: за такими RPC per-RPC Check не стоит, поэтому первая же
// ошибка соседа снимает их ЕДИНСТВЕННУЮ авторизацию. За публичными List Check
// остаётся, там мягкий проход — over-show внутри проекта, а не межтенантный.
// Клауза сама снимется, когда метки уйдут из карты, и не требует помнить о себе
// при этом (та же конструкция и тот же довод у storage).
//
// «Включён» не равно «решает»: фильтр обязан быть (а) включён, (б) с резолвимым
// адресом и (в) fail-closed. Первые два — безусловны, третье — по карте.
func (c Config) ValidateListFilter() error {
	return c.validateListFilterAgainst(scopeFilteredRPCs())
}

// validateListFilterAgainst — та же проверка над ЯВНО переданной картой.
// Неэкспортируемая намеренно: снаружи страж вызывается только как
// ValidateListFilter(), который карту читает сам, поэтому «передать пустой список
// и тем снять проверку» из композиционного корня по-прежнему нельзя. Внутри
// пакета список нужен, чтобы прогнать ОБЕ ветви условной клаузы (мягкий проход
// запрещён при непустой карте и допустим при пустой) — без этого одна из ветвей
// не проверялась бы вовсе.
func (c Config) validateListFilterAgainst(scopeFiltered []string) error {
	if !c.AuthZ.ListFilter.Enabled {
		return fmt.Errorf(errListFilterRequired, c.AuthN.Mode, scopeFilteredClause(scopeFiltered))
	}
	// Enabled, но без резолвимого endpoint'а → buildListFilter даёт passthrough
	// (conn==nil, WARN + nil-фильтр) — тот же fail-open. Отказываем. Адрес резолвим
	// ТЕМ ЖЕ методом, что и проводка (своё поле → запасной authz.iam-endpoint).
	if c.ListFilterAuthorizeEndpoint() == "" {
		return fmt.Errorf(errListFilterEndpointRequired, c.AuthN.Mode, scopeFilteredClause(scopeFiltered))
	}
	// Enabled + резолвим, но мягкий проход. См. выше: условен по карте намеренно.
	if c.AuthZ.ListFilter.FailOpen && len(scopeFiltered) > 0 {
		return fmt.Errorf(errListFilterFailOpenForbidden,
			c.AuthN.Mode, len(scopeFiltered), strings.Join(scopeFiltered, ", "))
	}
	return nil
}

// scopeFilteredClause — часть текста отказа, описывающая, что именно останется без
// авторизации. Пустая карта — не «нечего защищать», поэтому и на ней у отказа есть
// предмет: семь публичных List. Текст отказа читает оператор, поднимающий стенд,
// и он обязан называть ручку и причину — иначе стенд не поднять.
func scopeFilteredClause(scopeFiltered []string) string {
	if len(scopeFiltered) == 0 {
		return "no RPC currently carries the ScopeFiltered mark, and the requirement does not depend on that: " +
			"the public List RPCs narrow their page through this filter on top of the project-tier check"
	}
	return fmt.Sprintf("%d RPC(s) carry no per-RPC Check at all and rely on this filter as their only "+
		"object-scope authorization: %s", len(scopeFiltered), strings.Join(scopeFiltered, ", "))
}

// ValidatePeerTransport — boot-гардрейл S4: транспортная аутентификация ИСХОДЯЩИХ
// рёбер vpc→iam. Зеркалит S2 (ValidateServerMTLS), но для клиентской стороны:
// ValidateServerMTLS энфорсит mTLS на ЛИСТЕНЕРАХ (:9090/:9091), тогда как исходящие
// authz/project dial'ы оставались незащищёнными — оба per-edge флага
// (mtls.IAM{Authz,Project}MTLS.Enable и authz.iam-tls.enable / extapi.iam.tls.enable)
// по умолчанию false, а dialPeer тихо откатывается в insecure.NewCredentials().
//
// Рёбра под гардом (все исходящие cross-service):
//   - authz Check edge (authzConn → InternalIAMService.Check, :9091): несёт per-RPC
//     authorization-решение. Cleartext → сетевой MITM подделывает allowed=true →
//     ПОЛНЫЙ обход авторизации. Активен только когда authz.iam-endpoint задан И authz
//     задан (ручки, отменяющей Check, больше нет; ребро не несёт
//     security-решения — тот же escape, что в S1). Требует client-mTLS
//     (IAMAuthzMTLS.Enable) ЛИБО verified server-TLS (AuthZ.IAMTLS.Enable).
//   - ProjectService.Get edge (iamConn → extapi.iam, :9090): валидация project-existence /
//     account-lookup на request-path Create/Update. Активен в любом production (обязательная
//     валидация — она от настроек authz не зависит).
//     Требует client-mTLS (IAMProjectMTLS.Enable) ЛИБО verified server-TLS (ExtAPI.IAM.TLS.Enable).
//   - vpc→geo edge (geoConn → geo.v1.ZoneService.Get / RegionService.Get, :9090): cross-domain
//     zone_id/region_id reference-validation на request-path Subnet/AddressPool.Create. Дилится
//     безусловно, поэтому активен в любом production. Cleartext → MITM форжит существование
//     чужой/несуществующей zone/region, обходя scope-валидацию. Требует client-mTLS
//     (GeoMTLS.Enable) ЛИБО verified server-TLS (ExtAPI.Geo.TLS.Enable).
//   - vpc→iam owner-tuple register edge (register-drainer + sync registrar →
//     InternalIAMService.RegisterResource, :9091): пишет FGA owner-tuple, гранты владения
//     ресурсом. Активен, когда register-drainer включён И authz.iam-endpoint задан (иначе не
//     дилится). Ребро использует ТОЛЬКО client-cert creds (IAMRegisterClientCreds) — server-TLS
//     варианта нет, поэтому гард требует именно client-mTLS (IAMRegisterMTLS.Enable).
//   - vpc→iam list-filter authorize edge (authorizeConn → AuthorizeService.BatchCheck):
//     per-page фильтр видимости для List. ОТДЕЛЬНОЕ соединение от authz Check edge — свой
//     адрес (ListFilterAuthorizeEndpoint) и свои ручки транспорта, поэтому защита Check-ребра
//     его НЕ покрывает; именно на этом расхождении оно и поднималось незащищённым при
//     довольной страже. Слушатель здесь не назван намеренно: он свойство профиля и на
//     требование не влияет. Активен, когда фильтр включён И адрес резолвится (ListFilterAuthorizeEndpoint).
//     Ребро использует client-cert creds,
//     поэтому требование — ListFilterEdgeUsesMTLS (тот же предикат, что читает проводка).
//
// MTLSConfig грузится отдельно от viper-Config (envconfig, LoadMTLS), поэтому проверка —
// отдельный метод, вызываемый сразу после config.LoadMTLS() и ДО cross-service dial'ов.
// dev-режим гардом не затронут. Возвращает multierr со всеми нарушениями сразу.
func (c Config) ValidatePeerTransport(m MTLSConfig) error {
	if !c.AuthN.Mode.IsProduction() {
		return nil
	}
	var errs error

	// authz Check edge — только когда реально дилится и несёт authz-решение.
	authzEdgeActive := strings.TrimSpace(c.AuthZ.IAMEndpoint) != ""
	if authzEdgeActive && !m.IAMAuthzMTLS.Enable && !c.AuthZ.IAMTLS.Enable {
		errs = multierr.Append(errs, fmt.Errorf(errAuthzPeerTransportRequired, c.AuthN.Mode))
	}

	// ProjectService.Get edge — всегда активен в production (обязательная валидация).
	if !m.IAMProjectMTLS.Enable && !c.ExtAPI.IAM.TLS.Enable {
		errs = multierr.Append(errs, fmt.Errorf(errProjectPeerTransportRequired, c.AuthN.Mode))
	}

	// vpc→geo edge — дилится безусловно, поэтому всегда активен в production.
	if !m.GeoMTLS.Enable && !c.ExtAPI.Geo.TLS.Enable {
		errs = multierr.Append(errs, fmt.Errorf(errGeoPeerTransportRequired, c.AuthN.Mode))
	}

	// register edge — активен, только когда register-drainer/sync-registrar реально
	// дилятся (RegisterDrainerEnabled И задан iam-internal endpoint). Client-cert-only:
	// нет server-TLS варианта, поэтому требуется именно IAMRegisterMTLS.Enable.
	registerEdgeActive := c.IAM.RegisterDrainerEnabled && strings.TrimSpace(c.AuthZ.IAMEndpoint) != ""
	if registerEdgeActive && !m.IAMRegisterMTLS.Enable {
		errs = multierr.Append(errs, fmt.Errorf(errRegisterPeerTransportRequired, c.AuthN.Mode))
	}

	// Ребро vpc→домен величин. Провод живой ровно тогда, когда объявление
	// разрешилось адресом, — тем же методом, каким его читает проводка. Полос у
	// ребра две (разрешение на пути запроса и фоновая дельта), и обе идут по
	// этому проводу, поэтому удостоверение одно.
	if c.QuotaAuthorityEdgeLive() && !m.QuotaAuthorityMTLS.Enable {
		errs = multierr.Append(errs, fmt.Errorf(errQuotaAuthorityPeerTransportRequired, c.AuthN.Mode))
	}

	// list-filter authorize edge — активен ровно тогда, когда его поднимает
	// composition root: фильтр включён И адрес резолвится. Оба условия читаются
	// теми же методами, что и проводка, поэтому «страж видит ребро» ⟺ «ребро
	// дилится».
	listFilterEdgeActive := c.AuthZ.ListFilter.Enabled && c.ListFilterAuthorizeEndpoint() != ""
	if listFilterEdgeActive && !c.ListFilterEdgeUsesMTLS(m) {
		errs = multierr.Append(errs, fmt.Errorf(errListFilterPeerTransportRequired, c.AuthN.Mode))
	}

	return errs
}

// ValidateExecutorProfile — boot-гардрейл S5: профиль возможностей исполнителя
// датаплейна (секция `dataplane.executor`, см. dataplane.go).
//
// # Что здесь проверяется и почему это не косметика
//
// Управляющий контур принимает от арендатора то, что исполнять будет НЕ он:
// адресные диапазоны, правила со ссылкой на именованный набор, ограничение полосы.
// Возможностей исполнителя контур не знает и вывести не может — их объявляет
// посадка. Пока объявления нет, «принято» и «реализуемо» неотличимы.
//
// # Два разных вида отказа, и они разделены НАМЕРЕННО
//
//   - ПОСАДКА (пересечение адресов, отслеживание состояния, ссылка на именованный
//     набор, положительность гарантий) — требуется в боевом режиме. Dev остаётся
//     режимом внутрипроцессных фикстур, где датаплейна нет вовсе; любой
//     РАЗВЁРНУТЫЙ стенд работает в боевом режиме (core rule #16), поэтому
//     освобождение dev не оставляет дыры ни на одном стенде.
//   - ОБЪЯВЛЕНИЕ (неизвестное семейство, отрицательное число, ограничение полосы
//     без объявленной полосы) — негодно САМО ПО СЕБЕ, вне зависимости от посадки, и
//     отвергается в любом режиме. Это опечатка или самопротиворечие, а не выбор
//     оператора: приняв её молча, мы получили бы профиль, который оператор считает
//     объявленным, а страж — нет.
//
// Предикат непустоты семейств — ТОТ ЖЕ, что читает остальной процесс
// (Config.StateTrackingFamilies().IsDeclared()), а не длина сырой настройки:
// одинокая запятая разбирается в две пустые записи, то есть «непусто» по длине и
// «пусто» по существу. Разошедшись здесь, страж и читатель разошлись бы ровно там,
// где расхождение опасно.
//
// Возвращает multierr со ВСЕМИ нарушениями сразу: оператор обязан увидеть полный
// список за один прогон, а не чинить профиль по одному отказу за перезапуск.
func (c Config) ValidateExecutorProfile() error {
	var errs error

	e := c.Dataplane.Executor
	families := c.StateTrackingFamilies()

	// (1) Негодное ОБЪЯВЛЕНИЕ — в любом режиме.
	for _, u := range families.Unknown() {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorUnknownFamily,
			strconv.Quote(u), strings.Join(KnownAddressFamilies(), ", ")))
	}
	for _, g := range c.executorGuarantees() {
		if g.value < 0 {
			errs = multierr.Append(errs,
				fmt.Errorf(errExecutorGuaranteeNegative, g.knob, g.env, g.value))
		}
	}
	if e.TenantSettableBandwidthLimit && e.GuaranteedBandwidthPerInterfaceMbps <= 0 {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorTenantLimitWithoutBand,
			e.GuaranteedBandwidthPerInterfaceMbps))
	}
	// Умение объявлено, но промежуток приёма ПУСТ. Арендаторское ограничение
	// принимается строго выше опубликованного пола продукта и не выше того, что
	// гарантирует этот стенд (`domain.BandwidthLimitPolicy`); гарантия на уровне
	// пола или ниже делает эти два края несовместимыми, и объявленным умением
	// нельзя воспользоваться НИ РАЗУ. Это негодность самого объявления, а не
	// требование к посадке, поэтому проверяется в любом режиме — как и остальные
	// самопротиворечия профиля. Второй отказ про то же число не выдаётся: ветвь
	// выше уже сработала на неположительной гарантии.
	if e.TenantSettableBandwidthLimit &&
		e.GuaranteedBandwidthPerInterfaceMbps > 0 &&
		e.GuaranteedBandwidthPerInterfaceMbps <= domain.GuaranteedInterfaceBandwidthFloorMbps {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorTenantLimitRangeEmpty,
			e.GuaranteedBandwidthPerInterfaceMbps, domain.GuaranteedInterfaceBandwidthFloorMbps))
	}

	// (2) Требования к ПОСАДКЕ — только там, где исполнитель есть.
	if !c.AuthN.Mode.IsProduction() {
		return errs
	}
	if !e.OverlappingTenantAddresses {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorOverlapRequired, c.AuthN.Mode))
	}
	if !families.IsDeclared() {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorStateTrackingRequired,
			c.AuthN.Mode, strings.Join(KnownAddressFamilies(), ", ")))
	}
	if !e.NamedSetReferenceInRule {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorNamedSetRequired, c.AuthN.Mode))
	}
	for _, g := range c.executorGuarantees() {
		if g.value == 0 {
			errs = multierr.Append(errs, fmt.Errorf(errExecutorGuaranteeRequired,
				c.AuthN.Mode, g.knob, g.env, g.value))
		}
	}
	// Полезный размер кадра — гарантия профиля, у которой есть ОБЕЩАНИЕ ПРОДУКТА
	// (domain.GuaranteedPayloadFloorBytes): арендатор читает нижнюю границу в
	// документации и рассчитывает на неё, не зная ни этого стенда, ни его
	// исполнителя. Поэтому здесь проверяется не только «объявлено», но и «не меньше
	// обещанного».
	//
	// Прежняя редакция называла её ЕДИНСТВЕННОЙ такой гарантией. Это было верно на
	// день, когда писалось, и перестало быть верным вместе с публикацией полосы на
	// интерфейс: обещаний продукта в профиле стало два, и второе проверяется ниже —
	// тем же порядком и по той же причине.
	//
	// Ноль отсеян выше как ОТСУТСТВИЕ гарантии, и второй отказ про то же число
	// назвал бы оператору две проблемы там, где она одна. Граница ВКЛЮЧАЮЩАЯ —
	// обещание звучит «не ниже», и ровно обещанное законно.
	if p := e.GuaranteedPayloadBytes; p > 0 && p < domain.GuaranteedPayloadFloorBytes {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorPayloadBelowProductFloor,
			c.AuthN.Mode, p, domain.GuaranteedPayloadFloorBytes))
	}
	// Полоса на интерфейс — ВТОРАЯ гарантия профиля с обещанием продукта
	// (`domain.GuaranteedInterfaceBandwidthFloorMbps`, «не менее 1 Гбит/с»), и
	// проверяется она тем же порядком и по той же причине, что и размер кадра.
	// Ноль отсеян выше как ОТСУТСТВИЕ гарантии; граница ВКЛЮЧАЮЩАЯ — обещание
	// звучит «не менее», и ровно обещанное законно.
	if b := e.GuaranteedBandwidthPerInterfaceMbps; b > 0 && b < domain.GuaranteedInterfaceBandwidthFloorMbps {
		errs = multierr.Append(errs, fmt.Errorf(errExecutorBandBelowProductFloor,
			c.AuthN.Mode, b, domain.GuaranteedInterfaceBandwidthFloorMbps))
	}
	// Три ПОТОЛКА интерфейса — та же проверка и по той же причине, что две
	// гарантии выше (kacho#290). До неё эти числа не читал никто: они стояли
	// объявлением в домене, повторялись в документации и не участвовали ни в одной
	// ветке кода, то есть арендатору обещали то, чего не проверяли даже у себя.
	//
	// Ноль отсеян выше как ОТСУТСТВИЕ объявления, поэтому здесь только `> 0`:
	// второй отказ про то же число назвал бы оператору две проблемы там, где она
	// одна. Граница ВКЛЮЧАЮЩАЯ — ровно опубликованное законно.
	for _, cl := range c.executorPublishedCeilings() {
		if cl.declared > 0 && cl.declared < cl.published {
			errs = multierr.Append(errs, fmt.Errorf(errExecutorCeilingBelowPublished,
				c.AuthN.Mode, cl.knob, cl.declared, cl.published, cl.env))
		}
	}
	return errs
}

// executorPublishedCeiling — потолок интерфейса, объявленный ПОСАДКОЙ, рядом с
// числом, которое по этому же поводу продукт обещает АРЕНДАТОРУ.
//
// Пара живёт в одной структуре, чтобы отказ не мог назвать не ту ручку и не то
// число: три величины проверяются одинаково, и общий текст «что-то ниже
// обещанного» не сказал бы оператору, что именно чинить. Ровно тот же приём, что
// у executorGuarantee выше, и заведён он по той же причине.
type executorPublishedCeiling struct {
	knob      string
	env       string
	declared  int
	published int
}

// executorPublishedCeilings связывает объявление посадки с обещанием продукта.
//
// Обещание берётся ИЗ ДОМЕНА (`domain.Interface…Ceiling`), а не переписывается
// сюда числом: копия разошлась бы с оригиналом молча, и разошлась бы там, где это
// не видно, — страж сравнивал бы посадку с числом, которого арендатору никто не
// обещал. Совпадение домена с документацией держит отдельный гейт
// (`internal/repohygiene` `TestPublishedInterfaceLimitsAreOnePlace`), поэтому
// цепочка «документация → домен → страж» замкнута целиком.
func (c Config) executorPublishedCeilings() []executorPublishedCeiling {
	e := c.Dataplane.Executor
	return []executorPublishedCeiling{
		{
			knob:      "dataplane.executor.connection-limit-per-interface",
			env:       "KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_LIMIT_PER_INTERFACE",
			declared:  e.ConnectionLimitPerInterface,
			published: domain.InterfaceConnectionCeiling,
		},
		{
			knob:      "dataplane.executor.connection-rate-limit-per-interface-per-second",
			env:       "KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_LIMIT_PER_INTERFACE_PER_SECOND",
			declared:  e.ConnectionRateLimitPerInterfacePerSecond,
			published: domain.InterfaceConnectionRateCeilingPerSecond,
		},
		{
			knob:      "dataplane.executor.connection-rate-burst-per-interface",
			env:       "KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_BURST_PER_INTERFACE",
			declared:  e.ConnectionRateBurstPerInterface,
			published: domain.InterfaceConnectionRateBurstCeiling,
		},
	}
}

// executorGuarantee — числовая гарантия профиля вместе с именами, которыми её
// задают. Имена лежат РЯДОМ со значением, чтобы отказ не мог назвать не ту ручку:
// три числа проверяются одинаково, и общий текст «что-то не объявлено» не сказал бы
// оператору, что именно чинить.
type executorGuarantee struct {
	knob  string
	env   string
	value int
}

func (c Config) executorGuarantees() []executorGuarantee {
	e := c.Dataplane.Executor
	return []executorGuarantee{
		{
			knob:  "dataplane.executor.guaranteed-payload-bytes",
			env:   "KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_PAYLOAD_BYTES",
			value: e.GuaranteedPayloadBytes,
		},
		{
			knob:  "dataplane.executor.guaranteed-bandwidth-per-interface-mbps",
			env:   "KACHO_VPC_DATAPLANE__EXECUTOR__GUARANTEED_BANDWIDTH_PER_INTERFACE_MBPS",
			value: e.GuaranteedBandwidthPerInterfaceMbps,
		},
		{
			knob:  "dataplane.executor.connection-limit-per-interface",
			env:   "KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_LIMIT_PER_INTERFACE",
			value: e.ConnectionLimitPerInterface,
		},
		{
			knob:  "dataplane.executor.connection-rate-limit-per-interface-per-second",
			env:   "KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_LIMIT_PER_INTERFACE_PER_SECOND",
			value: e.ConnectionRateLimitPerInterfacePerSecond,
		},
		{
			knob:  "dataplane.executor.connection-rate-burst-per-interface",
			env:   "KACHO_VPC_DATAPLANE__EXECUTOR__CONNECTION_RATE_BURST_PER_INTERFACE",
			value: e.ConnectionRateBurstPerInterface,
		},
	}
}

// ValidateReservedPrefixes — boot-гардрейл S6: перечень адресных диапазонов,
// которые платформа держит ЗА СОБОЙ (секция `dataplane.reserved-prefixes`, см.
// dataplane.go).
//
// # Что здесь проверяется и почему у этого обязан быть страж, а не умолчание
//
// Часть адресного пространства обслуживает саму платформу: служебные адреса узлов,
// адреса служб внутри подсети, точка получения метаданных экземпляра. Подсеть
// арендатора, объявленная поверх такого диапазона, проходит все проверки контура и
// не работает — причём симптом выглядит сетевым, а причина лежит в перекрытии.
//
// Перечень нельзя зашить в код: у разных посадок платформы служебные диапазоны
// разные, и литерал описывал бы один стенд, оставаясь ложью про остальные. А раз
// это настройка, у неё есть состояние «не задана», и оно НЕ безобидно: пустой
// перечень означает «не сужаем», а не «нечего сужать» — проверка на пути запроса
// присутствует, исполняется на каждом создании подсети и не отвергает ничего.
// Поэтому боевая посадка с необъявленным перечнем не поднимается.
//
// # Два разных вида отказа, и они разделены НАМЕРЕННО (как в S5)
//
//   - ПОСАДКА (перечень не объявлен) — требуется в боевом режиме. Dev остаётся
//     режимом внутрипроцессных фикстур, где датаплейна нет вовсе; любой
//     РАЗВЁРНУТЫЙ стенд работает в боевом режиме (core rule #16), поэтому
//     освобождение dev не оставляет дыры ни на одном стенде.
//   - ОБЪЯВЛЕНИЕ (запись, которая диапазоном стать не может) — негодно САМО ПО
//     СЕБЕ, вне зависимости от посадки, и отвергается в любом режиме. Это опечатка
//     или самопротиворечие, а не выбор оператора: приняв её молча, мы получили бы
//     диапазон, который оператор считает зарезервированным, а контур — нет.
//
// Предикат непустоты — ТОТ ЖЕ, что читает путь запроса
// (Config.ReservedPrefixes().IsDeclared()), а не длина сырой настройки: одинокая
// запятая разбирается в две пустые записи, то есть «непусто» по длине и «пусто» по
// существу. Разошедшись здесь, страж и читатель разошлись бы ровно там, где
// расхождение опасно.
//
// Возвращает multierr со ВСЕМИ нарушениями сразу: оператор обязан увидеть полный
// список за один прогон, а не чинить перечень по одной записи за перезапуск.
func (c Config) ValidateReservedPrefixes() error {
	var errs error

	reserved := c.ReservedPrefixes()

	// (1) Негодное ОБЪЯВЛЕНИЕ — в любом режиме.
	for _, bad := range reserved.Rejected() {
		errs = multierr.Append(errs, fmt.Errorf(errReservedPrefixUnusable,
			strconv.Quote(bad.Entry), bad.Reason))
	}

	// (2) Требование к ПОСАДКЕ — только там, где датаплейн есть.
	if !c.AuthN.Mode.IsProduction() {
		return errs
	}
	if !reserved.IsDeclared() {
		errs = multierr.Append(errs, fmt.Errorf(errReservedPrefixesRequired, c.AuthN.Mode))
	}
	return errs
}

// ratelimitKnob — ключ настроек и префикс переменных окружения одного листенера.
// Пара, а не две строки в двух местах: отказ обязан назвать оба имени, потому что
// посадка задаёт величины то файлом, то окружением.
type ratelimitKnob struct {
	key    string
	envPfx string
}

var (
	publicRateLimitKnob = ratelimitKnob{
		key:    "api-server.rate-limit.public",
		envPfx: "KACHO_VPC_API_SERVER__RATE_LIMIT__PUBLIC",
	}
	internalRateLimitKnob = ratelimitKnob{
		key:    "api-server.rate-limit.internal",
		envPfx: "KACHO_VPC_API_SERVER__RATE_LIMIT__INTERNAL",
	}
)

// ValidateRequestRateLimits — boot-гардрейл S7: величины допуска запросов на
// ОБОИХ листенерах.
//
// # Что защищается
//
// Стоимость запроса в этом продукте высокая по построению: каждая мутация — три
// строки в базе (ресурс, очередь намерения, операция), каждое чтение — до 1000
// объектов на страницу с проверкой прав партиями. Неограниченный темп бьёт не в
// сеть, а в базу, и один арендатор занимает процесс, обслуживающий всех.
//
// # Почему это настройка со стражем, а не константа
//
// Исполняется предел ведром В ПРОЦЕССЕ, поэтому при N репликах эффективная
// величина равна N × объявленного, и посадка обязана уметь назвать свою. А раз
// это настройка, у неё есть состояние «не задана», и оно НЕ безобидно: нулевые
// величины означают «не ограничиваем», а не «ограничивать нечего». Ограничитель
// тогда либо не навешивается вовсе, либо навешивается пустым — и в обоих случаях
// выглядит включённым, ни разу не отказав.
//
// # Два разных вида отказа, разделены НАМЕРЕННО (как в S5/S6)
//
//   - ПОСАДКА (величины не объявлены) — требуется в боевом режиме. Dev остаётся
//     режимом внутрипроцессных фикстур; любой РАЗВЁРНУТЫЙ стенд работает в боевом
//     режиме (правило #16), поэтому освобождение dev не оставляет дыры ни на одном
//     стенде.
//   - ОБЪЯВЛЕНИЕ (неполный набор осей, отрицательная величина, всплеск ниже
//     устойчивого темпа) — негодно САМО ПО СЕБЕ и отвергается в любом режиме. Это
//     опечатка или самопротиворечие, а не выбор оператора.
//
// Предикат объявленности — ТОТ ЖЕ, что читает композиционный корень
// (grpcsrv.AdmissionLimits.IsDeclared через Config.*AdmissionLimits), а не
// сравнение полей на месте: разойдясь здесь, страж и проводка разошлись бы ровно
// там, где расхождение опасно.
//
// Возвращает multierr со ВСЕМИ нарушениями обоих листенеров: оператор обязан
// увидеть полный список за один прогон, а не чинить их по одному за перезапуск.
func (c Config) ValidateRequestRateLimits() error {
	var errs error
	for _, l := range []struct {
		knob   ratelimitKnob
		limits grpcsrv.AdmissionLimits
	}{
		{publicRateLimitKnob, c.PublicAdmissionLimits()},
		{internalRateLimitKnob, c.InternalAdmissionLimits()},
	} {
		// (1) Негодное ОБЪЯВЛЕНИЕ — в любом режиме.
		for _, reason := range l.limits.Unusable() {
			errs = multierr.Append(errs, fmt.Errorf(errRateLimitUnusable, l.knob.key, reason))
		}
		// (2) Требование к ПОСАДКЕ — только там, где листенер принимает трафик.
		if !c.AuthN.Mode.IsProduction() {
			continue
		}
		if !l.limits.IsDeclared() && len(l.limits.Unusable()) == 0 {
			errs = multierr.Append(errs, fmt.Errorf(errRateLimitRequired,
				l.knob.key, c.AuthN.Mode, l.knob.envPfx))
		}
	}
	return errs
}

// ValidateBoot — единый boot-валидатор: агрегирует Validate (S1 + базовые
// инварианты), ValidateServerMTLS (S2) и ValidatePeerTransport (S4) в один multierr,
// чтобы оператор увидел полный список проблем за один прогон. Используется как
// single-shot gate перед привязкой листенеров и cross-service dial'ами.
//
// S3 (ValidateListFilter) ВХОДИТ сюда. Здесь стояло обратное — «ему нужен список
// ScopeFiltered RPC из permission-map (пакет check), который config не
// импортирует», — и это перестало быть правдой вместе со снятием параметра: карту
// страж читает сам (scope_filtered_rpcs.go), цикла нет. Пока обоснование
// оставалось записанным, но неверным, агрегатор был ЛОВУШКОЙ: он выглядит как
// «полная проверка старта», и тот, кто перевёл бы на него композиционный корень
// вместо явной пары вызовов, тихо остался бы без проверки фильтра.
//
// S5 (ValidateExecutorProfile), S6 (ValidateReservedPrefixes) и S7
// (ValidateRequestRateLimits) входят сюда по той же причине и каждый с того же
// дня, что заведён сам: проверка, не попавшая в агрегатор, становится той самой
// ловушкой.
func (c Config) ValidateBoot(m MTLSConfig) error {
	return multierr.Combine(
		c.Validate(),
		c.ValidateServerMTLS(m),
		c.ValidateListFilter(),
		c.ValidateExecutorProfile(),
		c.ValidateReservedPrefixes(),
		c.ValidateRequestRateLimits(),
		c.ValidatePeerTransport(m),
		c.ValidateQuotaAuthority(m),
	)
}

// validateMode гарантирует, что Mode — известное значение (ENUM).
func (c Config) validateMode() error {
	switch c.AuthN.Mode {
	case ModeDev, ModeProduction, ModeProductionStrict:
		return nil
	default:
		return fmt.Errorf("authn.mode invalid (got %s)", c.AuthN.Mode)
	}
}

// InsecureDevWarnings возвращает список «не блокирующих» предупреждений
// о небезопасных dev-defaults. В production-режиме возвращает nil.
func (c Config) InsecureDevWarnings() []string {
	if c.AuthN.Mode.IsProduction() {
		return nil
	}
	var out []string
	if !c.ExtAPI.IAM.TLS.Enable {
		out = append(out,
			"extapi.iam.tls.enable=false — cross-service gRPC plaintext (dev only)")
	}
	mode := strings.ToLower(c.Repository.Postgres.SSLMode)
	if mode == "" || mode == "disable" {
		out = append(out,
			"repository.postgres.ssl-mode=disable — DB plaintext (dev only)")
	}
	return out
}
