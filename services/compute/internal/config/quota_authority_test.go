// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// quota_authority_test.go — страж посадки объявления домена величин.
//
// Приёмка ухода модуля квотирования из службы доступа, стадия S1, сценарии
// KAN-Q1-02, KAN-Q1-05, KAN-Q1-06. Имя документа приёмки здесь не цитируется:
// страж комментариев этой службы отвергает процессный лексикон, а он входит в
// имя файла.

import (
	"testing"

	"github.com/stretchr/testify/require"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

// TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart — незаданное объявление
// отвергается, и текст отказа называет ручку. Без имени ручки стенд не поднять.
func TestQuotaAuthority_KAN_Q1_02_UnsetRefusesStart(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	err := c.ValidateQuotaAuthority()
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_COMPUTE_QUOTA_AUTHORITY")
	require.Contains(t, err.Error(), corequota.NotDeployed,
		"отказ обязан назвать ОБА законных значения")
}

// TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture — положительный
// близнец к отказам старта: без него «процесс не поднимается» зеленело бы на
// объявлении, которое не принимается никогда.
func TestQuotaAuthority_KAN_Q1_06_NotDeployedIsALegalPosture(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	c.QuotaAuthority = corequota.NotDeployed

	a, err := c.QuotaAuthorityDeclaration()
	require.NoError(t, err, "объявленное отсутствие — законная посадка, а не отказ")
	require.False(t, a.Deployed())
	require.Equal(t, corequota.AuthorityAbsent, a.State())
}

// TestQuotaAuthority_KAN_Q1_05_HalfAPairRefusesStart — адрес есть,
// удостоверения нет.
func TestQuotaAuthority_KAN_Q1_05_HalfAPairRefusesStart(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	c.QuotaAuthority = "kaname-internal.kacho.svc:9091"

	err := c.ValidateQuotaAuthority()
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_ENABLE",
		"отказ обязан назвать НЕДОСТАЮЩУЮ половину пары")
}

// TestQuotaAuthority_HalfAPairIsSilentOutsideProduction — зеркало предыдущего.
//
// Требование транспорта у ребра величин ТО ЖЕ, что у остальных рёбер службы:
// собственная строгость сделала бы локальный стенд неподнимаемым там, где все
// прочие рёбра ходят открытым текстом законно.
func TestQuotaAuthority_HalfAPairIsSilentOutsideProduction(t *testing.T) {
	c := Config{AuthMode: "dev"}
	c.QuotaAuthority = "kaname-internal.kacho.svc:9091"

	a, err := c.QuotaAuthorityDeclaration()
	require.NoError(t, err)
	require.True(t, a.Deployed())
}

// TestQuotaAuthority_ValidateCarriesTheGuard — страж входит в общий валидатор
// посадки.
//
// Проверка, не попавшая туда, становится ловушкой: валидатор выглядит как
// «полная проверка старта», и переведённый на него композиционный корень тихо
// остаётся без неё.
func TestQuotaAuthority_ValidateCarriesTheGuard(t *testing.T) {
	c := Config{AuthMode: "production-strict"}
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_COMPUTE_QUOTA_AUTHORITY",
		"общий валидатор обязан нести стража объявления домена величин")
}
