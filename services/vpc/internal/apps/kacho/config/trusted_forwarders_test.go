// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// Стража старта на измерение «кому разрешено передавать чужую личность».
//
// Оба листенера строят доверенную пару
// grpcsrv.CertIdentityExtract → TrustedPrincipalExtract(WithTrustedForwarders(список)).
// Контракт corelib (pkg/grpcsrv principalIsTrusted) сужает круг ТОЛЬКО когда список
// НЕПУСТ; на пустом он отвечает «доверяем» ЛЮБОМУ пиру, прошедшему проверку
// сертификата. Внутренний периметр у нас объявлен НЕдоверенным, значит пустой список
// означает не «никому», а «всем» — и это надо ловить отказом старта, а не WARN'ом.
//
// Считаем ЗАПИСИ, КОТОРЫЕ ПРИМЕТ ТРАНСПОРТ, а не длину сырого среза: там же, где
// сужение реально происходит, пустые строки отбрасываются, поэтому значение вида
// `","` вырождается в пустое множество. Стража, считающая длину, такое значение
// пропустила бы и вернула дыру.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

// fwdCfg — минимально-валидный production Config с заданным списком отправителей.
func fwdCfg(mode Mode, sans []string) Config {
	c := prodCfg(mode, "kaname-internal.kacho.svc:9091")
	c.AuthZ.TrustedForwarderSANs = sans
	return c
}

// Непустой список — старт разрешён.
func TestValidate_Production_TrustedForwardersPinned_Passes(t *testing.T) {
	require.NoError(t, fwdCfg(ModeProduction, []string{gatewaySAN}).Validate())
}

// Пустой список в production — ОТКАЗ СТАРТА, с называнием ручки (это сообщение
// читает оператор, поднимающий стенд).
func TestValidate_Production_EmptyTrustedForwarders_RefusesToStart(t *testing.T) {
	for name, sans := range map[string][]string{
		"поля нет вовсе": nil,
		"пустой срез":    {},
	} {
		t.Run(name, func(t *testing.T) {
			err := fwdCfg(ModeProduction, sans).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "authz.trusted-forwarder-sans")
		})
	}
}

// production-strict — тот же отказ (любой IsProduction()).
func TestValidate_ProductionStrict_EmptyTrustedForwarders_RefusesToStart(t *testing.T) {
	err := fwdCfg(ModeProductionStrict, nil).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authz.trusted-forwarder-sans")
}

// Значение, которое ПРОХОДИТ проверку длины и при этом не сужает НИЧЕГО.
//
// `","` — это срез из двух пустых строк. corelib их отбрасывает
// (WithTrustedForwarders пропускает только s != ""), поэтому реальный круг остаётся
// пустым, то есть «доверяем любому». Стража обязана считать то же, что примет
// транспорт, иначе она отчитывается о сужении, которого нет.
func TestValidate_Production_ForwardersThatDegenerateToEmpty_RefusesToStart(t *testing.T) {
	for name, sans := range map[string][]string{
		"одни пустые строки": {"", ""},
		"одни пробелы":       {"   ", "\t"},
	} {
		t.Run(name, func(t *testing.T) {
			err := fwdCfg(ModeProduction, sans).Validate()
			require.Error(t, err, "список, вырождающийся в пустой, не сужает круг отправителей")
			assert.Contains(t, err.Error(), "authz.trusted-forwarder-sans")
		})
	}
}

// Вне боевого режима пустой круг ВОЗМОЖЕН, но только как ЯВНЫЙ опт-ин: стража
// срабатывает на ЛЮБОМ старте (прежде — на любом не-аварийном; ручки аварийного
// обхода больше нет, и исключения у стражи не осталось). Молчащая вне боевого режима стража —
// контроль, чья ветка на локальном стенде не исполняется ни разу, поэтому «забыл
// выставить круг» находится только на боевом профиле, где цена ошибки максимальна.
func TestValidate_Dev_EmptyTrustedForwarders_RefusesWithoutOptIn(t *testing.T) {
	c := fwdCfg(ModeDev, nil)
	c.Repository.Postgres.SSLMode = "disable"
	err := c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authz.trusted-forwarder-sans")
	assert.Contains(t, err.Error(), "authz.trust-any-forwarder")
}

func TestValidate_Dev_EmptyTrustedForwarders_AllowedWithExplicitOptIn(t *testing.T) {
	c := fwdCfg(ModeDev, nil)
	c.Repository.Postgres.SSLMode = "disable"
	c.AuthZ.TrustAnyForwarder = true
	require.NoError(t, c.Validate())
}

// Опт-ин НЕ действует в боевом режиме: иначе он был бы ручкой, снимающей защиту
// на развёрнутом стенде.
func TestValidate_Production_OptInDoesNotUnlockAnEmptyCircle(t *testing.T) {
	c := fwdCfg(ModeProduction, nil)
	c.AuthZ.TrustAnyForwarder = true
	err := c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authz.trusted-forwarder-sans")
}

// TrustedForwarders() — ЕДИНСТВЕННЫЙ источник значения на процесс: его читает и
// проводка (cmd/vpc/main.go), и стража выше, и самоотчёт о посадке. Поэтому «стража
// пропустила» ⟺ «круг реально сужен» по построению, а не по совпадению.
func TestTrustedForwarders_DropsEntriesTheTransportWouldIgnore(t *testing.T) {
	c := fwdCfg(ModeProduction, []string{"", gatewaySAN, "   ", "  " + gatewaySAN + "\t"})
	assert.Equal(t, []string{gatewaySAN}, c.TrustedForwarders().SANs(),
		"пустые записи отбрасываются (их отбрасывает и транспорт), пробелы по краям "+
			"срезаются — иначе запись не совпала бы ни с одним сертификатом побайтово; "+
			"повтор схлопывается — круг это множество, и транспорт всегда складывал его в map")
}

// Ручка обязана быть ДОСТИЖИМА из окружения: список задаёт чарт, а не литерал в коде.
// Без этого теста ручка может существовать в структуре и не приезжать из ConfigMap.
func TestLoad_TrustedForwardersFromEnv(t *testing.T) {
	t.Setenv("KACHO_VPC_AUTHZ__TRUSTED_FORWARDER_SANS",
		gatewaySAN+",spiffe://kacho.cloud/ns/kacho/sa/kacho-compute")

	c, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, []string{
		gatewaySAN,
		"spiffe://kacho.cloud/ns/kacho/sa/kacho-compute",
	}, c.TrustedForwarders().SANs())
}
