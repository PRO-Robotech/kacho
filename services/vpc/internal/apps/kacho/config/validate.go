// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/multierr"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Frozen-тексты boot-гардрейлов. Это часть контракта (наблюдаемый отказ старта),
// меняются только осознанно. %s подставляет Mode.String() (production|production-strict).
const (
	errAuthzEndpointRequired = "production mode (%s): authz.iam-endpoint is required " +
		"(set the kacho-iam internal endpoint; there is no knob that turns authz off)"
	errPublicMTLSRequired = "production mode (%s): public listener mTLS required " +
		"(set KACHO_VPC_PUBLIC_SERVER_MTLS_ENABLE=true with cert/key/ca) — the public :9090 " +
		"listener derives the authorization principal from client-asserted x-kacho-* metadata; " +
		"without verified transport auth any direct caller can spoof an arbitrary principal " +
		"(cross-tenant authz bypass). If the listener sits behind an authenticated " +
		"forwarder/service-mesh that terminates client identity, set authn.trusted-forwarder=true " +
		"to acknowledge that trust boundary (production-strict ignores this escape hatch)"
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
	// Отдельное от ребра per-RPC Check: свой адрес (iam public :9090 против
	// internal :9091) и свои ручки транспорта, поэтому защита Check-ребра его НЕ
	// покрывает. Ответ этого ребра решает, какие объекты вызывающий увидит в List,
	// то есть оно несёт решение о доступе и подпадает под то же требование, что
	// остальные исходящие. %s = Mode.String().
	errListFilterPeerTransportRequired = "production mode (%s): outbound vpc→iam list-filter authorize edge " +
		"(authz.list-filter.authorize-endpoint → AuthorizeService.BatchCheck) requires client mTLS — set " +
		"KACHO_VPC_AUTHZ__LIST_FILTER__AUTHORIZE_TLS__ENABLE=true (its own client-cert knob) or " +
		"KACHO_VPC_IAM_AUTHZ_MTLS_ENABLE=true (the client identity shared with the Check edge). This is a " +
		"SEPARATE connection from the authz Check edge — it targets the iam public listener and carries the " +
		"per-object visibility decision behind every List, so securing the Check edge alone does not cover it"

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
	errExecutorTenantLimitWithoutBand = "dataplane.executor.tenant-settable-bandwidth-limit=true while " +
		"dataplane.executor.guaranteed-bandwidth-per-interface-mbps is not declared (got %d) — the declaration " +
		"contradicts itself: a tenant-settable limit is a ceiling under the guaranteed band, and there is no " +
		"band to limit against. Declare the guaranteed band, or drop the tenant-settable limit"
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

	switch strings.ToLower(c.Repository.Postgres.SSLMode) {
	case "disable", "require", "verify-ca", "verify-full":
	case "":
		// допускаем — baseDSN подставит "disable"
	default:
		errs = multierr.Append(errs,
			fmt.Errorf("repository.postgres.ssl-mode=%q (allowed: disable, require, verify-ca, verify-full)",
				c.Repository.Postgres.SSLMode))
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
	if c.AuthN.Mode.IsProduction() {
		switch strings.ToLower(c.Repository.Postgres.SSLMode) {
		case "require", "verify-ca", "verify-full":
			// OK
		default:
			errs = multierr.Append(errs,
				fmt.Errorf("production mode (%s): repository.postgres.ssl-mode must be one of require|verify-ca|verify-full (got %q)",
					c.AuthN.Mode, c.Repository.Postgres.SSLMode))
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
//   - vpc→iam list-filter authorize edge (authorizeConn → AuthorizeService.BatchCheck, iam
//     public :9090): per-page фильтр видимости для List. ОТДЕЛЬНОЕ соединение от authz Check
//     edge — другой листенер и другие ручки транспорта, поэтому защита Check-ребра его НЕ
//     покрывает; именно на этом расхождении оно и поднималось незащищённым при довольной
//     страже. Активен, когда фильтр включён И адрес резолвится (ListFilterAuthorizeEndpoint).
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
	return errs
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
	}
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
// S5 (ValidateExecutorProfile) входит сюда по той же причине и с того же дня, что
// заведён сам: проверка, не попавшая в агрегатор, становится той самой ловушкой.
func (c Config) ValidateBoot(m MTLSConfig) error {
	return multierr.Append(
		multierr.Append(
			multierr.Append(
				multierr.Append(c.Validate(), c.ValidateServerMTLS(m)),
				c.ValidateListFilter()),
			c.ValidateExecutorProfile()),
		c.ValidatePeerTransport(m),
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
