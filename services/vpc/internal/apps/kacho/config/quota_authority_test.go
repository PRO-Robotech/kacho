// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority_test.go — страж посадки объявления домена величин.
//
// Приёмка `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
// стадия S1, сценарии KAN-Q1-02, KAN-Q1-04, KAN-Q1-05, KAN-Q1-06.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

// TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart — незаданное объявление
// отвергается, и текст отказа называет ручку.
//
// Без имени ручки в тексте стенд не поднять: это одно из трёх мест, прямо
// выведенных из-под запрета `security.md` §«Публичные артефакты».
func TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart(t *testing.T) {
	var c Config
	err := c.ValidateQuotaAuthority(MTLSConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "quota.authority")
	require.Contains(t, err.Error(), corequota.NotDeployed,
		"отказ обязан назвать ОБА законных значения, иначе он не восстанавливает "+
			"следующий шаг оператора")
}

// TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture — положительный
// близнец к отказам старта.
//
// Без него утверждения «процесс не поднимается» зеленели бы на объявлении,
// которое не принимается никогда.
func TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture(t *testing.T) {
	c := Config{Quota: QuotaConfig{Authority: corequota.NotDeployed}}
	c.AuthN.Mode = ModeProductionStrict

	a, err := c.QuotaAuthority(MTLSConfig{})
	require.NoError(t, err, "объявленное отсутствие — законная посадка, а не отказ")
	require.False(t, a.Deployed())
	require.Equal(t, corequota.AuthorityAbsent, a.State())
}

// TestQuotaAuthority_KAN_Q1_05_HalfAPairRefusesStart — адрес есть,
// удостоверения нет.
func TestQuotaAuthority_KAN_Q1_05_HalfAPairRefusesStart(t *testing.T) {
	c := Config{Quota: QuotaConfig{Authority: "kaname-internal.kacho.svc:9091"}}
	c.AuthN.Mode = ModeProductionStrict

	err := c.ValidateQuotaAuthority(MTLSConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_VPC_QUOTA_AUTHORITY_MTLS_ENABLE",
		"отказ обязан назвать НЕДОСТАЮЩУЮ половину пары")
}

// TestQuotaAuthority_HalfAPairIsSilentOutsideProduction — зеркало предыдущего.
//
// Требование транспорта у ребра величин ТО ЖЕ, что у остальных рёбер службы.
// Собственная строгость сделала бы локальный стенд неподнимаемым там, где все
// прочие рёбра ходят открытым текстом законно.
func TestQuotaAuthority_HalfAPairIsSilentOutsideProduction(t *testing.T) {
	c := Config{Quota: QuotaConfig{Authority: "kaname-internal.kacho.svc:9091"}}
	c.AuthN.Mode = ModeDev

	a, err := c.QuotaAuthority(MTLSConfig{})
	require.NoError(t, err)
	require.True(t, a.Deployed())
}

// TestQuotaAuthority_KAN_Q1_04_AddressIsNotDerivedFromAuthz — адрес величин и
// адрес авторизации разводятся, и смена одного не трогает другой.
//
// До S1 сценарий был неисполним ПО ПОСТРОЕНИЮ: оба брались из одного соединения,
// и развести значения было нечем.
func TestQuotaAuthority_KAN_Q1_04_AddressIsNotDerivedFromAuthz(t *testing.T) {
	c := Config{Quota: QuotaConfig{Authority: "limits.kacho.svc:9091"}}
	c.AuthN.Mode = ModeDev
	c.AuthZ.IAMEndpoint = "kaname-internal.kacho.svc:9091"

	a, err := c.QuotaAuthority(MTLSConfig{})
	require.NoError(t, err)
	require.Equal(t, "limits.kacho.svc:9091", a.Endpoint(),
		"величина берётся по СВОЕМУ адресу")
	require.NotEqual(t, c.AuthZ.IAMEndpoint, a.Endpoint())

	// Смена адреса авторизации не трогает адрес величин.
	c.AuthZ.IAMEndpoint = "kaname-internal.other.svc:9091"
	b, err := c.QuotaAuthority(MTLSConfig{})
	require.NoError(t, err)
	require.Equal(t, a.Endpoint(), b.Endpoint())

	// И наоборот: смена адреса величин не трогает адрес авторизации.
	before := c.AuthZ.IAMEndpoint
	c.Quota.Authority = corequota.NotDeployed
	_, err = c.QuotaAuthority(MTLSConfig{})
	require.NoError(t, err)
	require.Equal(t, before, c.AuthZ.IAMEndpoint)
}

// TestQuotaAuthority_ValidateBootCarriesTheGuard — страж входит в АГРЕГАТОР.
//
// Проверка, не попавшая в агрегатор, становится ловушкой: он выглядит как
// «полная проверка старта», и переведённый на него композиционный корень тихо
// остаётся без неё.
func TestQuotaAuthority_ValidateBootCarriesTheGuard(t *testing.T) {
	var c Config
	err := c.ValidateBoot(MTLSConfig{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "quota.authority"),
		"ValidateBoot обязан нести стража объявления домена величин; получено: %v", err)
}

// TestQuotaAuthority_KnobReachesTheField — ручка ДОЕЗЖАЕТ до поля.
//
// Проба существует потому, что дефект был ровно здесь и он тихий: viper
// подхватывает переменную окружения только для ИЗВЕСТНОГО ключа. Без объявления
// ключа в умолчаниях профиль задавал бы `KACHO_VPC_QUOTA__AUTHORITY`, чарт
// доставлял бы её, а процесс читал бы пустую строку и отказывал в старте, называя
// ручку, которую оператор ЗАДАЛ. Класс «принято-и-проигнорировано» с обратным
// знаком: не «принято и выброшено», а «задано и не прочитано».
//
// Утверждается ИСХОД загрузки, а не наличие вызова SetDefault: проба на вызов
// зеленела бы и на ключе, написанном с опечаткой.
func TestQuotaAuthority_KnobReachesTheField(t *testing.T) {
	t.Setenv("KACHO_VPC_QUOTA__AUTHORITY", "limits.kacho.svc:9091")

	cfg, err := Load("")
	require.NoError(t, err, "загрузка настроек")
	require.Equal(t, "limits.kacho.svc:9091", cfg.Quota.Authority,
		"значение ручки обязано доехать до поля; иначе процесс отказывает в старте, "+
			"называя ручку, которую оператор задал")
}
