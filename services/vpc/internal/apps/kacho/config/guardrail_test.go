// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// Fail-closed prod-guardrail: secure-by-default (`authn.mode=production`) обязан
// подтверждаться отказом старта при невалидной prod-конфигурации, а не тихим
// небезопасным запуском. Тесты покрывают S1 (authz-endpoint required) и S2
// (production-strict требует server-mTLS на обоих листенерах).

// prodCfg — минимально-валидный production Config (URL/listen заданы), с
// настраиваемыми authz-полями.
func prodCfg(mode Mode, iamEndpoint string) Config {
	var c Config
	c.AuthN.Mode = mode
	c.APIServer.Endpoint = "tcp://0.0.0.0:9090"
	c.APIServer.InternalEndpoint = "tcp://0.0.0.0:9091"
	c.Repository.Postgres.URL = "postgres://u@h:5432/db"
	c.Repository.Postgres.SSLMode = "verify-full"
	c.Logger.Level = "INFO"
	c.AuthZ.IAMEndpoint = iamEndpoint
	// strict-смежные инварианты удовлетворены, чтобы изолировать проверяемый гард.
	c.ExtAPI.IAM.TLS.Enable = true
	// Исходящий vpc→geo edge удовлетворён server-TLS по умолчанию (как IAM выше),
	// чтобы S4-тесты изолировали проверяемое ребро; geo-специфичные тесты явно
	// снимают его через c.ExtAPI.Geo.TLS.Enable=false.
	c.ExtAPI.Geo.TLS.Enable = true
	// Круг отправителей чужой личности сужен — тоже чтобы изолировать проверяемый
	// гард (S1c держит собственный набор случаев в trusted_forwarders_test.go).
	c.AuthZ.TrustedForwarderSANs = []string{"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"}
	// Профиль возможностей исполнителя объявлен полностью — по той же причине, что
	// и всё выше: чтобы каждый случай изолировал СВОЙ гард. Объявление живёт здесь,
	// в ОДНОМ месте: «законная боевая посадка» — величина, и два её описания
	// разошлись бы на первом же новом требовании (S5 держит собственный набор
	// случаев в guardrail_executor_profile_test.go).
	//
	// Фикстура обязана быть не снисходительнее продукта: пока страж требует
	// объявления, фикстура, оставившая профиль пустым, утверждала бы «всё
	// защищено» на посадке, которая не поднимается.
	c.Dataplane.Executor = ExecutorProfileConfig{
		OverlappingTenantAddresses:          true,
		StateTrackingFamilies:               []string{"v4", "v6"},
		NamedSetReferenceInRule:             true,
		GuaranteedPayloadBytes:              1450,
		GuaranteedBandwidthPerInterfaceMbps: 1000,
		ConnectionLimitPerInterface:         65536,
		// Темп и всплеск — величины того же профиля и объявлены здесь по той же
		// причине, что и остальные: фикстура не вправе быть снисходительнее
		// продукта. Числа взяты ЗАВЕДОМО ВЫШЕ опубликованных потолков (2 000 и
		// 8 000), потому что законная боевая посадка обязана нести исполнителя,
		// который держит обещанное продуктом, — фикстура ровно на потолке проходила
		// бы и не отличала «граница включающая» от «граница не проверяется».
		ConnectionRateLimitPerInterfacePerSecond: 4000,
		ConnectionRateBurstPerInterface:          16000,
		TenantSettableBandwidthLimit:             false,
	}
	// Перечень служебных диапазонов объявлен — по той же причине и с той же
	// оговоркой: фикстура обязана быть не снисходительнее продукта. Пока страж
	// требует объявления, фикстура с пустым перечнем утверждала бы «всё защищено»
	// на посадке, которая не поднимается (S6 держит собственный набор случаев в
	// guardrail_reserved_prefixes_test.go).
	c.Dataplane.ReservedPrefixes = []string{"169.254.0.0/16", "fe80::/10"}
	// Величины допуска запросов объявлены — по той же причине и с той же
	// оговоркой (S7 держит собственный набор случаев в
	// guardrail_request_rate_test.go). Требование к числам ровно одно — фикстура
	// не снисходительнее продукта, то есть набор объявлен полностью и проходит
	// стража. Совпадение с числами чарта здесь НЕ утверждается и не требуется: у
	// стенда и у фикстуры разные предметы, а годность самих чисел чарта держит
	// отдельная проба (ratelimit_chart_test.go).
	c.APIServer.RateLimit.Public = AdmissionLimitsConfig{
		ReadPerSec: 100, MutationPerSec: 20, BurstFactor: 5, InFlight: 16,
	}
	c.APIServer.RateLimit.Internal = AdmissionLimitsConfig{
		ReadPerSec: 1000, MutationPerSec: 500, BurstFactor: 5, InFlight: 256,
	}
	return c
}

// vpc8-C-01: production с настроенным authz-endpoint проходит Validate.
func TestValidate_Production_WithAuthzEndpoint_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname.kacho.svc:9091")
	require.NoError(t, c.Validate())
}

// vpc8-C-02: production без authz-endpoint → отказ.
func TestValidate_Production_NoAuthzEndpoint_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "")
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.iam-endpoint is required")
	require.Contains(t, err.Error(), "production mode (production)")
}

// vpc8-C-03: production-strict без authz-endpoint → тот же отказ (любой IsProduction()).
func TestValidate_ProductionStrict_NoAuthzEndpoint_Fails(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "")
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz.iam-endpoint is required")
	require.Contains(t, err.Error(), "production mode (production-strict)")
}

// vpc8-C-04: снятая ручка аварийного обхода больше не участвует в решении.
//
// Прежде здесь стояли ДВА случая: боевая посадка с аварийным обходом отвергается,
// dev — принимает. Оба пережили свой предмет: обход снят с контракта вместе с
// переводом vpc на носитель контура — тот ставит звено решения о доступе всегда,
// и поля, которым его можно отменить, в дескрипторе процесса не существует.
//
// Что осталось предметом: требование адреса владельца модели в боевой посадке
// теперь БЕЗУСЛОВНО. Раньше его снимал аварийный обход, то есть боевой процесс
// мог подняться без ребра решения о доступе; проверяется именно это — отказ по
// пустому адресу наступает в каждом боевом режиме, и снять его нечем.
//
// Полный запрет самого имени снятой ручки во всём дереве держит
// `retired_knobs_test.go`; здесь — что ПОВЕДЕНИЕ от неё не зависит.
func TestValidate_AuthzEndpointRequiredUnconditionallyInProduction(t *testing.T) {
	for _, mode := range []Mode{ModeProduction, ModeProductionStrict} {
		t.Run(mode.String(), func(t *testing.T) {
			c := prodCfg(mode, "")
			err := c.Validate()
			require.Errorf(t, err, "боевая посадка без ребра решения о доступе не поднимается (%s)", mode)
			require.Contains(t, err.Error(), "authz.iam-endpoint is required")
			require.Contains(t, err.Error(), "there is no knob that turns authz off")
		})
	}
}

// Контроль к предыдущему: та же посадка С адресом принимается. Без него отрицание
// выше зеленело бы и от «Validate отвергает вообще всё».
func TestValidate_ProductionWithAuthzEndpointPasses(t *testing.T) {
	for _, mode := range []Mode{ModeProduction, ModeProductionStrict} {
		t.Run(mode.String(), func(t *testing.T) {
			require.NoError(t, prodCfg(mode, "kaname.kacho.svc:9091").Validate())
		})
	}
}

// vpc8-C-05: dev-режим гардрейлом authz-эндпоинта не затронут.
//
// Круг отправителей — отдельное измерение: его стража срабатывает на ЛЮБОМ
// старте, поэтому здесь выставлен явный dev-опт-ин. Иначе тест
// перестал бы отвечать на свой вопрос (про authz.iam-endpoint) и падал бы по
// чужой причине.
func TestValidate_Dev_NoGuardrail(t *testing.T) {
	var c Config
	c.AuthN.Mode = ModeDev
	c.APIServer.Endpoint = "tcp://0.0.0.0:9090"
	c.APIServer.InternalEndpoint = "tcp://0.0.0.0:9091"
	c.Repository.Postgres.URL = "postgres://u@h:5432/db"
	c.Repository.Postgres.SSLMode = "disable"
	c.AuthZ.TrustAnyForwarder = true
	c.Logger.Level = "INFO"
	require.NoError(t, c.Validate())
	require.NotContains(t, errString(c.Validate()), "authz.iam-endpoint is required")
}

// vpc8-C-07: production-strict без public-mTLS → отказ (ValidateServerMTLS).
func TestValidateServerMTLS_ProductionStrict_RequiresPublicMTLS(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kaname:9091")
	var m MTLSConfig
	m.InternalServerMTLS.Enable = true
	m.PublicServerMTLS.Enable = false
	err := c.ValidateServerMTLS(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "public listener mTLS required")
}

// vpc8-C-08: production-strict без internal-mTLS → отказ.
func TestValidateServerMTLS_ProductionStrict_RequiresInternalMTLS(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kaname:9091")
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = false
	err := c.ValidateServerMTLS(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "internal listener mTLS required")
}

// vpc8-C-09: production-strict с обоими server-mTLS → старт разрешен.
func TestValidateServerMTLS_ProductionStrict_BothOn_Passes(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kaname:9091")
	var m MTLSConfig
	m.PublicServerMTLS = grpcsrv.TLSServer{Enable: true}
	m.InternalServerMTLS = grpcsrv.TLSServer{Enable: true}
	require.NoError(t, c.ValidateServerMTLS(m))
}

// vpc8-C-10: production (не strict) БЕЗ public-mTLS и БЕЗ trusted-forwarder → отказ.
// SEC-hardening r2 (2026-07-05): публичный :9090 listener выводит authz-principal'а
// из client-asserted x-kacho-* metadata; в production он не должен доверять ей по
// незашифрованному транспорту без явного подтверждения границы доверия (CWE-290).
func TestValidateServerMTLS_Production_NoMTLS_NoForwarder_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	var m MTLSConfig // оба server-mTLS выключены, trusted-forwarder не выставлен
	err := c.ValidateServerMTLS(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "public listener mTLS required")
	require.Contains(t, err.Error(), "production mode (production)")
	// SEC-hardening r6: internal :9091 — service→service, mTLS обязателен и в
	// non-strict production → его сообщение тоже всплывает.
	require.Contains(t, err.Error(), "internal listener mTLS required")
}

// vpc8-C-10b: production + public-mTLS включён (без trusted-forwarder) → старт разрешён.
func TestValidateServerMTLS_Production_PublicMTLS_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true
	// internal :9091 — service→service, mTLS обязателен в ЛЮБОМ production-режиме.
	m.InternalServerMTLS.Enable = true
	require.NoError(t, c.ValidateServerMTLS(m))
}

// vpc8-C-10c: production + trusted-forwarder=true (без public-mTLS) → старт разрешён
// (оператор явно подтвердил, что listener за аутентифицированным forwarder'ом).
func TestValidateServerMTLS_Production_TrustedForwarder_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.AuthN.TrustedForwarder = true
	var m MTLSConfig // public-mTLS выключен, но internal обязателен всегда в production
	m.InternalServerMTLS.Enable = true
	require.NoError(t, c.ValidateServerMTLS(m))
}

// vpc8-C-10f: production (non-strict) БЕЗ internal-mTLS → отказ.
// SEC-hardening r6 (2026-07-05): internal :9091 — service→service, поэтому mTLS
// обязателен в ЛЮБОМ production-режиме (security.md AuthN-инвариант: «Internal
// (:9091) НЕ освобождён: mTLS обязателен»). Раньше non-strict production запускал
// internal listener без транспортной аутентификации, доверяя client-asserted
// x-kacho-* subject на admin/IPAM поверхности (InternalAddressPoolService,
// InternalNetworkService.GetNetwork с infra vrf_id, InternalAddressService) —
// principal-spoofing (CWE-306/290). У internal НЕТ trusted-forwarder escape-hatch
// (в отличие от публичного user→edge listener'а).
func TestValidateServerMTLS_Production_NoInternalMTLS_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true    // публичный удовлетворён
	m.InternalServerMTLS.Enable = false // internal выключен → отказ
	err := c.ValidateServerMTLS(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "internal listener mTLS required")
	require.Contains(t, err.Error(), "production mode (production)")
}

// vpc8-C-10g: production (non-strict) + trusted-forwarder НЕ спасает internal —
// escape-hatch действует только для публичного listener'а.
func TestValidateServerMTLS_Production_TrustedForwarder_StillRequiresInternalMTLS(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.AuthN.TrustedForwarder = true
	var m MTLSConfig // оба выключены; trusted-forwarder закрывает только public
	err := c.ValidateServerMTLS(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "internal listener mTLS required")
	// public удовлетворён trusted-forwarder'ом — его сообщение всплыть не должно.
	require.NotContains(t, err.Error(), "public listener mTLS required")
}

// vpc8-C-10d: production-strict ИГНОРИРУЕТ trusted-forwarder — server-mTLS обязателен
// всегда (escape-hatch не действует в strict).
func TestValidateServerMTLS_ProductionStrict_TrustedForwarder_StillRequiresMTLS(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kaname:9091")
	c.AuthN.TrustedForwarder = true
	var m MTLSConfig // оба выключены
	err := c.ValidateServerMTLS(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "public listener mTLS required")
	require.Contains(t, err.Error(), "internal listener mTLS required")
}

// vpc8-C-10e: dev-режим гардом не затронут (public-mTLS не требуется).
func TestValidateServerMTLS_Dev_NoMTLSRequired(t *testing.T) {
	c := prodCfg(ModeDev, "kaname:9091")
	var m MTLSConfig
	require.NoError(t, c.ValidateServerMTLS(m))
}

// vpc8-C-11: множественные нарушения strict агрегируются в один multierr
// (single boot-validation через ValidateBoot).
func TestValidateBoot_ProductionStrict_AggregatesAllViolations(t *testing.T) {
	var c Config
	c.AuthN.Mode = ModeProductionStrict
	c.APIServer.Endpoint = "tcp://0.0.0.0:9090"
	c.APIServer.InternalEndpoint = "tcp://0.0.0.0:9091"
	c.Repository.Postgres.URL = "postgres://u@h:5432/db"
	c.Repository.Postgres.SSLMode = "disable"
	c.Logger.Level = "INFO"
	c.AuthZ.IAMEndpoint = ""
	c.ExtAPI.IAM.TLS.Enable = false
	var m MTLSConfig // оба server-mTLS выключены

	err := c.ValidateBoot(m)
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "authz.iam-endpoint is required")
	require.Contains(t, msg, "extapi.iam.tls.enable=true required")
	require.Contains(t, msg, "ssl-mode must be one of require|verify-ca|verify-full")
	require.Contains(t, msg, "public listener mTLS required")
	require.Contains(t, msg, "internal listener mTLS required")
}

// vpc8-C-12: production (non-strict) с ssl-mode=disable → отказ (DB-трафик и пароль
// открытым текстом). SEC-hardening r2 (2026-07-05, CWE-319): защищённый sslmode
// требуется в ЛЮБОМ IsProduction() режиме, не только strict.
func TestValidate_Production_SSLModeDisable_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.Repository.Postgres.SSLMode = "disable"
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ssl-mode must be one of require|verify-ca|verify-full")
	require.Contains(t, err.Error(), "production mode (production)")
}

// vpc8-C-13: production с ssl-mode=require → проходит.
func TestValidate_Production_SSLModeRequire_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.Repository.Postgres.SSLMode = "require"
	require.NoError(t, c.Validate())
}

// vpc8-C-14: dev с ssl-mode=disable — не затронут (dev допускает plaintext).
func TestValidate_Dev_SSLModeDisable_Passes(t *testing.T) {
	var c Config
	c.AuthN.Mode = ModeDev
	c.APIServer.Endpoint = "tcp://0.0.0.0:9090"
	c.APIServer.InternalEndpoint = "tcp://0.0.0.0:9091"
	c.Repository.Postgres.URL = "postgres://u@h:5432/db"
	c.Repository.Postgres.SSLMode = "disable"
	c.AuthZ.TrustAnyForwarder = true
	c.Logger.Level = "INFO"
	require.NoError(t, c.Validate())
}

// H-D3: невалидный logger.level → ошибка валидации при старте (fail-fast,
// без тихого fallback в INFO).
func TestValidate_InvalidLoggerLevel_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.Logger.Level = "LOUD"
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger.level")
}

// H-D1/H-D2: ParseLogLevel переводит конфиг-строку в slog.Level (уважение порога).
func TestParseLogLevel_KnownLevels(t *testing.T) {
	cases := map[string]bool{"DEBUG": true, "info": true, "Warn": true, "ERROR": true, "FATAL": true, "loud": false}
	for in, ok := range cases {
		_, err := ParseLogLevel(in)
		if ok {
			require.NoError(t, err, "level %q must parse", in)
		} else {
			require.Error(t, err, "level %q must be rejected", in)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
