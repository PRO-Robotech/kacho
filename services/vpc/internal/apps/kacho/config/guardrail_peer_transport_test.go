// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Fail-closed boot-гардрейл S4 (SEC-hardening r9, 2026-07-06): транспортная
// аутентификация ИСХОДЯЩИХ ребер vpc→iam. До этого гарда production/production-strict
// boot был валиден, даже когда per-RPC authz Check edge (authzConn →
// InternalIAMService.Check) и/или ProjectService.Get edge дилились по cleartext gRPC:
// оба per-edge флага (mtls.IAMAuthzMTLS.Enable / authz.iam-tls.enable и
// mtls.IAMProjectMTLS.Enable / extapi.iam.tls.enable) по умолчанию false, а
// dialPeer тихо откатывался в insecure.NewCredentials(). Сетевой MITM authz-ответа мог
// подделать allowed=true → полный обход авторизации. ValidateServerMTLS энфорсит mTLS
// только на ЛИСТЕНЕРАХ; исходящие authz-рёбра оставались незащищёнными.
//
// Гард зеркалит S2 (listener-guard): в ЛЮБОМ production-режиме authz Check edge и
// ProjectService.Get edge обязаны нести verified transport (client-mTLS ЛИБО
// verified server-TLS), иначе старт отклоняется.

// prodCfgSecurePeers — production Config, у которого ОБА vpc→iam ребра защищены
// (authz Check edge через client-mTLS, ProjectService.Get edge через server-TLS
// из prodCfg). База, чтобы точечно ослаблять одно ребро в конкретном тесте.
func prodCfgSecurePeers(mode Mode, iamEndpoint string) (Config, MTLSConfig) {
	c := prodCfg(mode, iamEndpoint) // ExtAPI.IAM.TLS.Enable=true → project edge ok
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true // authz Check edge ok (client-mTLS)
	return c, m
}

// vpc9-C-00: демонстрация ДЫРЫ, которую закрывает S4. Существующие boot-гарды
// (S1 Validate + S2 ValidateServerMTLS) ОБА пропускают production-конфиг, в котором
// исходящий authz Check edge полностью незашифрован — то есть сервис «загрузился бы».
// Именно ValidatePeerTransport теперь отклоняет такой старт.
func TestPeerTransport_GapDemonstration_S1S2Pass(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091") // ExtAPI.IAM.TLS.Enable=true
	c.Repository.Postgres.SSLMode = "verify-full"
	var m MTLSConfig
	// Листенеры защищены (S2 удовлетворён), но исходящий authz Check edge — нет.
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = true
	// authz Check edge: iam-tls.enable=false + IAMAuthzMTLS.Enable=false → cleartext.
	require.False(t, c.AuthZ.IAMTLS.Enable)
	require.False(t, m.IAMAuthzMTLS.Enable)
	// S1 и S2 не видят проблемы (обе валидации проходят) → boot успешен без S4.
	require.NoError(t, c.Validate(), "S1 alone does not catch the insecure authz edge")
	require.NoError(t, c.ValidateServerMTLS(m), "S2 guards listeners only, not the outbound authz edge")
	// S4 отклоняет.
	require.Error(t, c.ValidatePeerTransport(m))
}

// vpc9-C-01: production + authz Check edge cleartext (нет ни mTLS, ни server-TLS) → отказ.
func TestValidatePeerTransport_Production_AuthzEdgeInsecure_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091") // project edge ok (server-TLS)
	var m MTLSConfig                            // IAMAuthzMTLS off; c.AuthZ.IAMTLS off
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz Check edge")
	require.Contains(t, err.Error(), "production mode (production)")
	// project edge удовлетворён (extapi.iam.tls.enable=true) → его сообщение не всплывает.
	require.NotContains(t, err.Error(), "ProjectService.Get edge")
}

// vpc9-C-02: production-strict + authz Check edge cleartext → тот же отказ (любой IsProduction()).
func TestValidatePeerTransport_ProductionStrict_AuthzEdgeInsecure_Fails(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kaname:9091")
	var m MTLSConfig
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz Check edge")
	require.Contains(t, err.Error(), "production mode (production-strict)")
}

// vpc9-C-03: production + authz Check edge через client-mTLS → проходит.
func TestValidatePeerTransport_Production_AuthzEdgeMTLS_Passes(t *testing.T) {
	c, m := prodCfgSecurePeers(ModeProduction, "kaname:9091")
	require.True(t, m.IAMAuthzMTLS.Enable)
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9-C-04: production + authz Check edge через verified server-TLS (authz.iam-tls.enable)
// → проходит даже без client-mTLS.
func TestValidatePeerTransport_Production_AuthzEdgeServerTLS_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.AuthZ.IAMTLS.Enable = true // verified server-TLS вместо mTLS
	var m MTLSConfig             // client-mTLS выключен
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9-C-05: освобождения у ребра решения о доступе БОЛЬШЕ НЕТ.
//
// Прежде здесь стояли два случая: аварийный обход освобождал это ребро от
// требования проверенного транспорта (обход снимал пообъектную проверку, значит
// ребро якобы не несло решения). Обход снят с контракта вместе с переводом vpc на
// носитель контура — звено решения о доступе ставится всегда, — поэтому ребро
// несёт решение на КАЖДОЙ посадке, и предметом стал сам этот факт: тот же
// незашифрованный транспорт теперь отвергается.
func TestValidatePeerTransport_Production_AuthzEdgeHasNoExemptionLeft(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091") // project edge ok
	var m MTLSConfig                            // authz edge cleartext
	err := c.ValidatePeerTransport(m)
	require.Error(t, err, "ребро решения о доступе больше нечем освободить")
	require.Contains(t, err.Error(), "authz Check edge")
}

// vpc9-C-05b: пустой authz-endpoint → ребро не дилится вовсе → требования нет
// (project edge остаётся под гардом). Это НЕ освобождение: адрес обязателен по
// S1 (Validate), и такая посадка вообще не поднимется — здесь изолирован предмет
// именно этого стража.
func TestValidatePeerTransport_Production_NoEndpoint_AuthzEdgeNotDialled(t *testing.T) {
	c := prodCfg(ModeProduction, "")
	var m MTLSConfig
	m.IAMProjectMTLS.Enable = true // project edge ok, authz edge неактивен
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9-C-06: production + ProjectService.Get edge cleartext (extapi.iam.tls.enable=false,
// IAMProjectMTLS выключен) → отказ (тот же класс гарда). authz edge изолирован через mTLS.
func TestValidatePeerTransport_Production_ProjectEdgeInsecure_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.ExtAPI.IAM.TLS.Enable = false // project edge cleartext
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true // authz edge удовлетворён → изолируем project
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ProjectService.Get edge")
	require.Contains(t, err.Error(), "production mode (production)")
	require.NotContains(t, err.Error(), "authz Check edge")
}

// vpc9-C-07: production + ProjectService.Get edge через client-mTLS (server-TLS off) → проходит.
func TestValidatePeerTransport_Production_ProjectEdgeMTLS_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.ExtAPI.IAM.TLS.Enable = false
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true   // authz edge ok
	m.IAMProjectMTLS.Enable = true // project edge ok через mTLS
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9-C-08: production + ProjectService.Get edge через verified server-TLS → проходит.
func TestValidatePeerTransport_Production_ProjectEdgeServerTLS_Passes(t *testing.T) {
	c, m := prodCfgSecurePeers(ModeProduction, "kaname:9091") // ExtAPI.IAM.TLS.Enable=true
	require.True(t, c.ExtAPI.IAM.TLS.Enable)
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9-C-09: production + ОБА ребра cleartext → оба сообщения агрегируются в один multierr.
func TestValidatePeerTransport_Production_BothEdgesInsecure_AggregatesBoth(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.ExtAPI.IAM.TLS.Enable = false // project edge cleartext
	var m MTLSConfig                // authz edge cleartext
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz Check edge")
	require.Contains(t, err.Error(), "ProjectService.Get edge")
}

// vpc9-C-10: dev-режим гардом не затронут (исходящие рёбра могут быть insecure).
func TestValidatePeerTransport_Dev_NoGuard(t *testing.T) {
	c := prodCfg(ModeDev, "kaname:9091")
	c.ExtAPI.IAM.TLS.Enable = false
	var m MTLSConfig // всё insecure
	require.NoError(t, c.ValidatePeerTransport(m))
}

// --- SEC-hardening r9b (2026-07-06): S4 расширен на исходящие рёбра vpc→geo и
// vpc→iam owner-tuple register. Оба несли ту же дыру, что authz/project рёбра:
// per-edge флаги (mtls.GeoMTLS.Enable / extapi.geo.tls.enable и
// mtls.IAMRegisterMTLS.Enable) по умолчанию false, dialPeer/register-dial тихо
// откатывались в insecure. geo — cross-domain zone_id/region_id reference-validation
// (MITM форжит существование чужой/несуществующей zone/region); register —
// owner-tuple, гранты владения ресурсом (MITM тамперит authz-relevant tuple). ---

// vpc9b-C-01: production + vpc→geo edge cleartext (нет ни mTLS, ни server-TLS) → отказ.
func TestValidatePeerTransport_Production_GeoEdgeInsecure_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.ExtAPI.Geo.TLS.Enable = false // geo edge cleartext
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true // authz edge удовлетворён → изолируем geo
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vpc→geo edge")
	require.Contains(t, err.Error(), "production mode (production)")
	require.NotContains(t, err.Error(), "authz Check edge")
	require.NotContains(t, err.Error(), "ProjectService.Get edge")
}

// vpc9b-C-02: production-strict + geo edge cleartext → тот же отказ (любой IsProduction()).
func TestValidatePeerTransport_ProductionStrict_GeoEdgeInsecure_Fails(t *testing.T) {
	c := prodCfg(ModeProductionStrict, "kaname:9091")
	c.ExtAPI.Geo.TLS.Enable = false
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vpc→geo edge")
	require.Contains(t, err.Error(), "production mode (production-strict)")
}

// vpc9b-C-03: production + geo edge через client-mTLS (server-TLS off) → проходит.
func TestValidatePeerTransport_Production_GeoEdgeMTLS_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.ExtAPI.Geo.TLS.Enable = false
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true
	m.GeoMTLS.Enable = true // geo edge ok через mTLS
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9b-C-04: production + geo edge через verified server-TLS → проходит (default prodCfg).
func TestValidatePeerTransport_Production_GeoEdgeServerTLS_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091") // ExtAPI.Geo.TLS.Enable=true
	require.True(t, c.ExtAPI.Geo.TLS.Enable)
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9b-C-05: production + register-drainer активен + register edge cleartext
// (IAMRegisterMTLS off) → отказ. Ребро использует ТОЛЬКО client-mTLS (нет server-TLS
// варианта), поэтому гард требует именно IAMRegisterMTLS.Enable.
func TestValidatePeerTransport_Production_RegisterEdgeInsecure_Fails(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.IAM.RegisterDrainerEnabled = true // register edge активен (endpoint задан)
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true // authz edge удовлетворён → изолируем register
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "register edge")
	require.Contains(t, err.Error(), "production mode (production)")
	require.NotContains(t, err.Error(), "authz Check edge")
}

// vpc9b-C-06: production + register edge через client-mTLS → проходит.
func TestValidatePeerTransport_Production_RegisterEdgeMTLS_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.IAM.RegisterDrainerEnabled = true
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true
	m.IAMRegisterMTLS.Enable = true // register edge ok
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9b-C-07: register-drainer выключен → register edge неактивен → нет требования,
// даже если IAMRegisterMTLS off.
func TestValidatePeerTransport_Production_RegisterDisabled_NoRequirement(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.IAM.RegisterDrainerEnabled = false // register edge НЕ дилится
	var m MTLSConfig
	m.IAMAuthzMTLS.Enable = true
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9b-C-08: register-drainer включён, но authz.iam-endpoint пуст → register edge
// не дилится (нет iam-internal endpoint) → нет требования.
//
// Прежняя редакция поясняла пустой адрес аварийным обходом («breakglass, чтобы S1
// не требовал endpoint»). Механизма нет — он снят вместе с переходом на носитель,
// и проба зелена по другой причине: требование адреса живёт теперь в конструкторе
// дескриптора, а этот страж судит ТРАНСПОРТ уже объявленных рёбер.
func TestValidatePeerTransport_Production_RegisterEnabled_NoEndpoint_NoRequirement(t *testing.T) {
	c := prodCfg(ModeProduction, "") // endpoint пуст: ребро не объявлено, судить транспорт нечему
	c.IAM.RegisterDrainerEnabled = true
	var m MTLSConfig
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9b-C-09: все четыре ребра cleartext → все сообщения агрегируются в один multierr.
func TestValidatePeerTransport_Production_AllEdgesInsecure_AggregatesAll(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	c.ExtAPI.IAM.TLS.Enable = false // project edge cleartext
	c.ExtAPI.Geo.TLS.Enable = false // geo edge cleartext
	c.IAM.RegisterDrainerEnabled = true
	var m MTLSConfig // authz + register edges cleartext
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz Check edge")
	require.Contains(t, err.Error(), "ProjectService.Get edge")
	require.Contains(t, err.Error(), "vpc→geo edge")
	require.Contains(t, err.Error(), "register edge")
}

// vpc9b-C-10: dev-режим гардом не затронут (geo/register рёбра могут быть insecure).
func TestValidatePeerTransport_Dev_GeoRegister_NoGuard(t *testing.T) {
	c := prodCfg(ModeDev, "kaname:9091")
	c.ExtAPI.Geo.TLS.Enable = false
	c.IAM.RegisterDrainerEnabled = true
	var m MTLSConfig
	require.NoError(t, c.ValidatePeerTransport(m))
}

// --- Исходящее ребро фильтра видимости (list-filter → AuthorizeService.BatchCheck).
//
// Это ОТДЕЛЬНОЕ соединение, а не то же самое, что authz Check edge: у него свой
// адрес и свой набор ручек транспорта. Адрес может совпасть с адресом Check-ребра
// (при пустом authorize-endpoint он от него и наследуется — так и устроена
// фикстура ниже), и это ничего не меняет: соединения два, транспорт у них свой.
// Ответ этого ребра решает, какие объекты вызывающий увидит в List, поэтому
// требование к нему то же, что к остальным исходящим: проверяемый транспорт в
// любом боевом режиме. ---

// prodCfgListFilterEdge — production Config, где ВСЕ прочие исходящие рёбра
// удовлетворены, а фильтр видимости включён со своим адресом. База, чтобы
// изолировать проверяемое ребро.
func prodCfgListFilterEdge(mode Mode) (Config, MTLSConfig) {
	c := prodCfg(mode, "kaname-internal:9091")
	c.AuthZ.IAMTLS.Enable = true // authz Check edge — verified server-TLS
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = "kaname:9090"
	var m MTLSConfig // client-mTLS выключен на всех рёбрах
	return c, m
}

// vpc9c-C-00: демонстрация ДЫРЫ. Конфигурация проходит S1, S2, S3 и S4 в его
// прежнем виде — то есть сервис стартовал бы, — при том что соединение фильтра
// видимости поднимается без проверяемого транспорта.
func TestPeerTransport_GapDemonstration_ListFilterAuthorizeEdge(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = true

	// Прочие гарды довольны.
	require.NoError(t, c.Validate(), "S1 does not look at the visibility-filter edge")
	require.NoError(t, c.ValidateServerMTLS(m), "S2 guards listeners only")
	require.NoError(t, c.validateListFilterAgainst([]string{"/svc/List"}),
		"S3 requires the filter to be ON, not to be dialled over verified transport")

	// Проводка при этом реально выбирает незащищённый путь.
	require.False(t, c.ListFilterEdgeUsesMTLS(m),
		"this is the condition under which the composition root dials insecure")

	// S4 обязан отклонить такой старт.
	require.Error(t, c.ValidatePeerTransport(m))
}

// vpc9c-C-01: production + ребро фильтра без проверяемого транспорта → отказ,
// и сообщение называет именно это ребро (прочие удовлетворены).
func TestValidatePeerTransport_Production_ListFilterEdgeInsecure_Fails(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "list-filter authorize edge")
	require.Contains(t, err.Error(), "production mode (production)")
	// Изоляция ребра — по НАЧАЛУ чужого сообщения: прозу «в отличие от Check-ребра»
	// собственный текст содержит намеренно, и совпадение по подстроке «authz Check
	// edge» проверяло бы формулировку, а не то, какое ребро отвергнуто.
	require.NotContains(t, err.Error(), "outbound vpc→iam authz Check edge")
	require.NotContains(t, err.Error(), "ProjectService.Get edge")
	require.NotContains(t, err.Error(), "vpc→geo edge")
}

// vpc9c-C-02: production-strict — тот же отказ (любой IsProduction()).
func TestValidatePeerTransport_ProductionStrict_ListFilterEdgeInsecure_Fails(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProductionStrict)
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "list-filter authorize edge")
	require.Contains(t, err.Error(), "production mode (production-strict)")
}

// vpc9c-C-03: ребро защищено собственной ручкой authz.list-filter.authorize-tls → проходит.
func TestValidatePeerTransport_Production_ListFilterEdgeOwnMTLS_Passes(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	c.AuthZ.ListFilter.AuthorizeTLS.Enable = true
	require.True(t, c.ListFilterEdgeUsesMTLS(m))
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9c-C-04: ребро защищено общим с Check-ребром client-cert'ом → проходит
// (проводка переиспользует ту же личность, гард обязан считать так же).
func TestValidatePeerTransport_Production_ListFilterEdgeSharedAuthzMTLS_Passes(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	m.IAMAuthzMTLS.Enable = true
	require.True(t, c.ListFilterEdgeUsesMTLS(m))
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9c-C-05: фильтр выключен → ребро не дилится → требования нет.
func TestValidatePeerTransport_Production_ListFilterDisabled_NoRequirement(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	c.AuthZ.ListFilter.Enabled = false
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9c-C-06: фильтр включён, но адреса нет ни своего, ни запасного → ребро не
// дилится (buildAuthorizeConn отдаёт nil) → требования нет. Отсутствие фильтра при
// ScopeFiltered RPC ловит S3, а не это ребро.
func TestValidatePeerTransport_Production_ListFilterNoEndpoint_NoRequirement(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	c.AuthZ.ListFilter.AuthorizeEndpoint = ""
	c.AuthZ.IAMEndpoint = "" // S1 требует адрес, но он проверяется в Validate, не здесь
	require.NoError(t, c.ValidatePeerTransport(m))
}

// vpc9c-C-07: адрес берётся запасным (authorize-endpoint пуст, iam-endpoint задан) —
// ребро дилится, значит требование действует.
func TestValidatePeerTransport_Production_ListFilterEndpointFallback_Requires(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	c.AuthZ.ListFilter.AuthorizeEndpoint = "" // → fallback на authz.iam-endpoint
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "list-filter authorize edge")
}

// vpc9c-C-08: dev-режим гардом не затронут.
func TestValidatePeerTransport_Dev_ListFilterEdge_NoGuard(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeDev)
	require.NoError(t, c.ValidatePeerTransport(m))
	// vpc9c-C-09: ребро фильтра видимости охраняется НЕЗАВИСИМО от ребра решения о
}

// доступе. Прежде этот случай проверял, что аварийный обход (снят с контракта)
// освобождал одно ребро и не освобождал другое; освобождения больше нет ни у
// одного, поэтому предметом остаётся сама независимость: ослабив ТОЛЬКО
// транспорт фильтра, мы обязаны получить отказ, называющий именно его.
func TestValidatePeerTransport_Production_ListFilterEdgeGuardedIndependently(t *testing.T) {
	c, m := prodCfgListFilterEdge(ModeProduction)
	err := c.ValidatePeerTransport(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "list-filter authorize edge")
	// Ребро решения о доступе в этой посадке защищено — его сообщения быть не должно.
	require.NotContains(t, err.Error(), "outbound vpc→iam authz Check edge")
}

// vpc9c-C-10: ListFilterEdgeUsesMTLS — ОДИН предикат для проводки и для стражи.
// Таблица фиксирует все четыре комбинации двух ручек: расхождение между тем, что
// проверяет гард, и тем, что выбирает composition root, — это и есть дыра.
func TestListFilterEdgeUsesMTLS_TableMatchesWiring(t *testing.T) {
	for _, tc := range []struct {
		name       string
		authorize  bool
		sharedAuth bool
		want       bool
	}{
		{"neither knob", false, false, false},
		{"own authorize-tls", true, false, true},
		{"shared authz client-cert", false, true, true},
		{"both", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c Config
			var m MTLSConfig
			c.AuthZ.ListFilter.AuthorizeTLS.Enable = tc.authorize
			m.IAMAuthzMTLS.Enable = tc.sharedAuth
			require.Equal(t, tc.want, c.ListFilterEdgeUsesMTLS(m))
		})
	}
}

// vpc9c-C-11: ListFilterAuthorizeEndpoint — ОДИН резолвер адреса для проводки,
// S3 и S4. Своё поле выигрывает; пусто → запасной authz.iam-endpoint; оба пусты →
// пусто (ребро не дилится).
func TestListFilterAuthorizeEndpoint_ResolutionOrder(t *testing.T) {
	var c Config
	c.AuthZ.ListFilter.AuthorizeEndpoint = "authorize:9090"
	c.AuthZ.IAMEndpoint = "internal:9091"
	require.Equal(t, "authorize:9090", c.ListFilterAuthorizeEndpoint())

	c.AuthZ.ListFilter.AuthorizeEndpoint = "  "
	require.Equal(t, "internal:9091", c.ListFilterAuthorizeEndpoint())

	c.AuthZ.IAMEndpoint = ""
	require.Equal(t, "", c.ListFilterAuthorizeEndpoint())
}

// vpc9-C-11: ValidateBoot агрегирует S4 — insecure authz edge в production всплывает
// в едином boot-валидаторе (single-shot gate).
func TestValidateBoot_Production_IncludesPeerTransport(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091")
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true   // S2 public ok
	m.InternalServerMTLS.Enable = true // S2 internal ok
	// authz Check edge остаётся cleartext (IAMAuthzMTLS off, authz.iam-tls off).
	err := c.ValidateBoot(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "authz Check edge")
}

// vpc9-C-12: ValidateBoot зелёный, когда всё (листенеры + исходящие рёбра +
// сужение страницы) защищено.
//
// Фильтр видимости добавлен в фикстуру вместе с включением S3 в агрегатор: без
// него посадка «всё защищено» больше не является таковой — публичные List отдавали
// бы каждому участнику проекта все его строки. Это не ослабление положительного
// контроля, а приведение его к тому, что он объявляет: ослаблено по-прежнему ноль
// измерений, просто измерений стало на одно больше.
func TestValidateBoot_Production_AllSecure_Passes(t *testing.T) {
	c := prodCfg(ModeProduction, "kaname:9091") // project edge server-TLS
	c.Repository.Postgres.SSLMode = "verify-full"
	c.AuthZ.ListFilter.Enabled = true
	c.AuthZ.ListFilter.AuthorizeEndpoint = "kaname:9090"
	var m MTLSConfig
	m.PublicServerMTLS.Enable = true
	m.InternalServerMTLS.Enable = true
	m.IAMAuthzMTLS.Enable = true // authz Check edge ok
	require.NoError(t, c.ValidateBoot(m))
}
